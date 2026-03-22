package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dcFailCooldown  = 30 * time.Second
	wsFailTimeout   = 2 * time.Second
	shutdownTimeout = 30 * time.Second
	maxConnections  = 512
)

// Server is the SOCKS5 + Telegram WS bridge (stateful, concurrent-safe).
type Server struct {
	DcOpt map[int]string

	Pool     *WsPool
	Stats    *Stats
	Log      *slog.Logger
	BufBytes int

	mu           sync.RWMutex
	wsBlacklist  map[dcMediaKey]struct{}
	dcFailUntil  map[dcMediaKey]time.Time
}

// NewServer constructs server state.
func NewServer(dcOpt map[int]string, pool *WsPool, stats *Stats, log *slog.Logger, bufKB int) *Server {
	return &Server{
		DcOpt:       maps.Clone(dcOpt),
		Pool:        pool,
		Stats:       stats,
		Log:         log,
		BufBytes:    max(4, bufKB) * 1024,
		wsBlacklist: make(map[dcMediaKey]struct{}),
		dcFailUntil: make(map[dcMediaKey]time.Time),
	}
}

// Run listens on host:port and serves until ctx is cancelled (uses net.ListenConfig + close on ctx.Done).
func (s *Server) Run(ctx context.Context, host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	s.Log.Info("Telegram WS Bridge Proxy")
	s.Log.Info("listening", "addr", addr)
	for dc, ip := range s.DcOpt {
		s.Log.Info("target DC", "dc", dc, "ip", ip)
	}
	s.Log.Info("configure Telegram Desktop", "socks5", addr, "auth", "none")

	go s.logStatsLoop(ctx)
	s.Pool.Warmup(s.DcOpt)

	sem := make(chan struct{}, maxConnections)
	var wg sync.WaitGroup
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.Log.Info("draining active connections", "timeout_secs", int(shutdownTimeout.Seconds()))
			}
			drainDone := make(chan struct{})
			go func() {
				wg.Wait()
				close(drainDone)
			}()
			select {
			case <-drainDone:
				if ctx.Err() != nil {
					s.Log.Info("all connections drained")
				}
			case <-time.After(shutdownTimeout):
				s.Log.Warn("shutdown timeout reached, some connections may be forcibly closed",
					"timeout_secs", int(shutdownTimeout.Seconds()))
			}
			s.Pool.Shutdown()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.SetNoDelay(true)
			if s.BufBytes > 0 {
				_ = tc.SetReadBuffer(s.BufBytes)
				_ = tc.SetWriteBuffer(s.BufBytes)
			}
		}
		select {
		case sem <- struct{}{}:
		default:
			s.Log.Warn("connection limit reached, rejecting connection",
				"limit", maxConnections, "peer", c.RemoteAddr())
			_, _ = c.Write(socks5Reply(0x05))
			_ = c.Close()
			continue
		}
		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()
			defer func() { <-sem }()
			s.handleClient(conn)
		}(c)
	}
}

func (s *Server) logStatsLoop(ctx context.Context) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.RLock()
			bl := make([]string, 0, len(s.wsBlacklist))
			for k := range s.wsBlacklist {
				tag := "DC" + strconv.Itoa(k.DC)
				if k.Media {
					tag += "m"
				}
				bl = append(bl, tag)
			}
			s.mu.RUnlock()
			if len(bl) == 0 {
				s.Log.Info("stats", "summary", s.Stats.Summary(), "ws_bl", "none")
			} else {
				s.Log.Info("stats", "summary", s.Stats.Summary(), "ws_bl", strings.Join(bl, ", "))
			}
		}
	}
}

func (s *Server) handleClient(conn net.Conn) {
	s.Stats.ConnectionsTotal.Add(1)
	peer := conn.RemoteAddr().String()
	label := peer

	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	if err := socks5ReadGreeting(conn, conn); err != nil {
		s.Log.Debug("SOCKS5 greeting failed", "peer", label, "err", err)
		_ = conn.Close()
		return
	}

	dst, port, err := socks5ReadConnect(conn)
	if err != nil {
		if errors.Is(err, errNotConnect) {
			_, _ = conn.Write(socks5Reply(0x07))
		} else if errors.Is(err, errBadATYP) {
			_, _ = conn.Write(socks5Reply(0x08))
		}
		_ = conn.Close()
		return
	}

	if strings.Contains(dst, ":") {
		s.Log.Error("IPv6 not supported; disable IPv6 in Telegram", "peer", label, "dst", dst, "port", port)
		_, _ = conn.Write(socks5Reply(0x05))
		_ = conn.Close()
		return
	}

	if !IsTelegramIP(dst) {
		_ = conn.SetDeadline(time.Time{})
		s.handlePassthrough(conn, label, dst, port)
		return
	}

	if _, err := conn.Write(socks5Reply(0x00)); err != nil {
		_ = conn.Close()
		return
	}

	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	init := make([]byte, 64)
	if _, err := io.ReadFull(conn, init); err != nil {
		s.Log.Debug("client disconnected before init", "peer", label, "err", err)
		_ = conn.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})

	if IsHTTPTransport(init) {
		s.Stats.ConnectionsHTTPRejected.Add(1)
		s.Log.Debug("HTTP transport rejected", "peer", label, "dst", dst, "port", port)
		_ = conn.Close()
		return
	}

	dc, isMedia, ok := DcFromInit(init)
	initPatched := false
	if !ok {
		if ent, has := ipToDC[dst]; has {
			dc, isMedia = ent.DC, ent.IsMedia
			if _, in := s.DcOpt[dc]; in {
				signed := int16(dc) // #nosec G115 -- dc is validated to be 1-5 or 203, safe to narrow
				if !isMedia {
					signed = -signed
				}
				init = PatchInitDC(init, signed)
				initPatched = true
			}
		}
	}

	if dc == 0 || s.DcOpt[dc] == "" {
		s.Log.Warn("unknown DC -> TCP passthrough", "peer", label, "dc", dc, "dst", dst, "port", port)
		s.tcpFallback(conn, label, dst, port, init, 0, false)
		return
	}

	target := s.DcOpt[dc]
	dcKey := dcMediaKey{DC: dc, Media: isMedia}
	mediaTag := ""
	if isMedia {
		mediaTag = " media"
	}

	s.mu.RLock()
	_, blacklisted := s.wsBlacklist[dcKey]
	s.mu.RUnlock()
	if blacklisted {
		s.Log.Debug("WS blacklisted -> TCP", "peer", label, "dc", dc, "media", mediaTag, "dst", dst)
		s.tcpFallback(conn, label, dst, port, init, dc, isMedia)
		return
	}

	s.mu.RLock()
	failUntil := s.dcFailUntil[dcKey]
	s.mu.RUnlock()
	wsTimeout := 10 * time.Second
	if time.Now().Before(failUntil) {
		wsTimeout = wsFailTimeout
	}

	domains := WsDomains(dc, isMedia)
	var ws *RawWebSocket
	wsFailedRedirect := false
	allRedirects := true

	if p := s.Pool.Get(dc, isMedia, target, domains); p != nil {
		ws = p
		s.Log.Info("pool hit", "peer", label, "dc", dc, "dst", dst, "port", port, "via", target)
	} else {
		for _, domain := range domains {
			s.Log.Info("WS connect", "peer", label, "dc", dc, "wss", "wss://"+domain+"/apiws", "via", target)
			w, err := ConnectWS(context.Background(), target, domain, "/apiws", wsTimeout, s.BufBytes)
			if err == nil {
				ws = w
				allRedirects = false
				break
			}
			s.Stats.WSErrors.Add(1)
			var he *WsHandshakeError
			if errors.As(err, &he) && he != nil {
				if he.IsRedirect() {
					wsFailedRedirect = true
					s.Log.Warn("WS redirect", "peer", label, "dc", dc, "code", he.StatusCode, "domain", domain, "location", he.Location)
					continue
				}
				allRedirects = false
				s.Log.Warn("WS handshake", "peer", label, "dc", dc, "line", he.StatusLine)
			} else {
				allRedirects = false
				s.Log.Warn("WS connect failed", "peer", label, "dc", dc, "err", err)
			}
			break
		}
	}

	if ws == nil {
		now := time.Now()
		s.mu.Lock()
		switch {
		case wsFailedRedirect && allRedirects:
			s.wsBlacklist[dcKey] = struct{}{}
			s.Log.Warn("WS blacklisted (all redirects)", "peer", label, "dc", dc)
		case wsFailedRedirect:
			s.dcFailUntil[dcKey] = now.Add(dcFailCooldown)
		default:
			s.dcFailUntil[dcKey] = now.Add(dcFailCooldown)
			s.Log.Info("WS cooldown", "peer", label, "dc", dc, "secs", int(dcFailCooldown.Seconds()))
		}
		s.mu.Unlock()

		s.Log.Info("TCP fallback", "peer", label, "dc", dc, "dst", dst, "port", port)
		s.tcpFallback(conn, label, dst, port, init, dc, isMedia)
		return
	}

	s.mu.Lock()
	delete(s.dcFailUntil, dcKey)
	s.mu.Unlock()

	s.Stats.ConnectionsWS.Add(1)

	var splitter *MsgSplitter
	if initPatched {
		var err error
		splitter, err = NewMsgSplitter(init)
		if err != nil {
			splitter = nil
		}
	}

	if err := ws.Send(init); err != nil {
		s.Log.Debug("ws send init failed", "peer", label, "err", err)
		_ = ws.Close()
		_ = conn.Close()
		return
	}

	BridgeWS(conn, ws, label, dc, dst, port, isMedia, splitter, s.Stats, s.Log)
}

func (s *Server) handlePassthrough(conn net.Conn, label, dst string, port int) {
	s.Stats.ConnectionsPassthrough.Add(1)
	s.Log.Debug("passthrough", "peer", label, "dst", dst, "port", port)

	raddr := net.JoinHostPort(dst, strconv.Itoa(port))
	remote, err := net.DialTimeout("tcp", raddr, 10*time.Second)
	if err != nil {
		s.Log.Warn("passthrough failed", "peer", label, "dst", dst, "err", err)
		_, _ = conn.Write(socks5Reply(0x05))
		_ = conn.Close()
		return
	}
	if _, err := conn.Write(socks5Reply(0x00)); err != nil {
		_ = remote.Close()
		_ = conn.Close()
		return
	}

	done := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(done) }) }

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer finish()
		_, _ = io.Copy(remote, conn)
	}()
	go func() {
		defer wg.Done()
		defer finish()
		_, _ = io.Copy(conn, remote)
	}()
	<-done
	_ = conn.Close()
	_ = remote.Close()
	wg.Wait()
}

func (s *Server) tcpFallback(client net.Conn, label, dst string, port int, init []byte, dc int, isMedia bool) {
	raddr := net.JoinHostPort(dst, strconv.Itoa(port))
	remote, err := net.DialTimeout("tcp", raddr, 10*time.Second)
	if err != nil {
		s.Log.Warn("TCP fallback connect failed", "peer", label, "dst", dst, "port", port, "err", err)
		_ = client.Close()
		return
	}
	s.Stats.ConnectionsTCPFallback.Add(1)
	if _, err := remote.Write(init); err != nil {
		_ = remote.Close()
		_ = client.Close()
		return
	}
	BridgeTCPWithStats(client, remote, label, dc, dst, port, isMedia, s.Stats, s.Log)
}

// ParseDcIPList parses repeated "DC:IP" flags into a map (last wins).
func ParseDcIPList(entries []string) (map[int]string, error) {
	out := make(map[int]string)
	for _, e := range entries {
		dcStr, ip, ok := strings.Cut(e, ":")
		if !ok || dcStr == "" {
			return nil, errors.New("invalid --dc-ip " + e)
		}
		dc, err := strconv.Atoi(dcStr)
		if err != nil {
			return nil, errors.New("invalid --dc-ip " + e)
		}
		if ip == "" || net.ParseIP(ip) == nil {
			return nil, errors.New("invalid --dc-ip " + e)
		}
		out[dc] = ip
	}
	return out, nil
}
