package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// WsHandshakeError is returned when HTTP upgrade is not 101.
type WsHandshakeError struct {
	StatusCode int
	StatusLine string
	Headers    map[string]string
	Location   string
}

func (e *WsHandshakeError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.StatusLine)
}

// IsRedirect reports 301/302/303/307/308.
func (e *WsHandshakeError) IsRedirect() bool {
	switch e.StatusCode {
	case 301, 302, 303, 307, 308:
		return true
	default:
		return false
	}
}

// RawWebSocket is a minimal client speaking masked binary frames to Telegram kws.
type RawWebSocket struct {
	conn   net.Conn
	br     *bufio.Reader
	closed atomic.Bool
}

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA

	maxWSFrameSize = 16 * 1024 * 1024 // 16 MB — guard against OOM/panic from crafted frames
)

// ConnectWS dials TLS to ip:443 with SNI=domain, performs WebSocket upgrade to path.
func ConnectWS(ctx context.Context, ip, domain, path string, timeout time.Duration, bufBytes int) (*RawWebSocket, error) {
	if path == "" {
		path = "/apiws"
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	d := net.Dialer{}
	raw, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
	if err != nil {
		return nil, err
	}
	tcp, _ := raw.(*net.TCPConn)
	setSockOpts(tcp, bufBytes)

	tlsConn := tls.Client(raw, &tls.Config{
		ServerName: domain,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = tlsConn.Close()
		return nil, err
	}

	var key16 [16]byte
	if _, err := rand.Read(key16[:]); err != nil {
		_ = tlsConn.Close()
		return nil, err
	}
	wsKey := base64.StdEncoding.EncodeToString(key16[:])

	var req strings.Builder
	req.WriteString("GET ")
	req.WriteString(path)
	req.WriteString(" HTTP/1.1\r\n")
	fmt.Fprintf(&req, "Host: %s\r\n", domain)
	req.WriteString("Upgrade: websocket\r\n")
	req.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&req, "Sec-WebSocket-Key: %s\r\n", wsKey)
	req.WriteString("Sec-WebSocket-Version: 13\r\n")
	req.WriteString("Sec-WebSocket-Protocol: binary\r\n")
	req.WriteString("Origin: https://web.telegram.org\r\n")
	req.WriteString("User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36\r\n")
	req.WriteString("\r\n")

	if _, err := tlsConn.Write([]byte(req.String())); err != nil {
		_ = tlsConn.Close()
		return nil, err
	}

	const (
		maxHandshakeHeaders    = 100
		maxHandshakeHeaderSize = 64 * 1024 // 64 KB total
	)
	br := bufio.NewReader(tlsConn)
	var lines []string
	var totalHeaderBytes int
	for {
		line, err := br.ReadBytes('\n')
		totalHeaderBytes += len(line)
		if totalHeaderBytes > maxHandshakeHeaderSize {
			_ = tlsConn.Close()
			return nil, fmt.Errorf("ws handshake response headers too large (>%d bytes)", maxHandshakeHeaderSize)
		}
		if err != nil {
			_ = tlsConn.Close()
			return nil, err
		}
		if len(line) >= 1 && (line[len(line)-1] == '\n') {
			line = line[:len(line)-1]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
		}
		if len(line) == 0 {
			break
		}
		if len(lines) >= maxHandshakeHeaders {
			_ = tlsConn.Close()
			return nil, fmt.Errorf("ws handshake response has too many headers (>%d)", maxHandshakeHeaders)
		}
		lines = append(lines, string(line))
	}
	if len(lines) == 0 {
		_ = tlsConn.Close()
		return nil, &WsHandshakeError{StatusCode: 0, StatusLine: "empty response"}
	}

	parts := strings.SplitN(lines[0], " ", 3)
	code := 0
	if len(parts) >= 2 {
		code, _ = strconv.Atoi(parts[1])
	}
	if code == http.StatusSwitchingProtocols {
		return &RawWebSocket{conn: tlsConn, br: br}, nil
	}

	hdr := make(map[string]string)
	for _, hl := range lines[1:] {
		if idx := strings.IndexByte(hl, ':'); idx >= 0 {
			k := strings.ToLower(strings.TrimSpace(hl[:idx]))
			v := strings.TrimSpace(hl[idx+1:])
			hdr[k] = v
		}
	}
	_ = tlsConn.Close()
	loc := hdr["location"]
	return nil, &WsHandshakeError{
		StatusCode: code,
		StatusLine: lines[0],
		Headers:    hdr,
		Location:   loc,
	}
}

// Closed reports whether Close was called.
func (w *RawWebSocket) Closed() bool {
	return w.closed.Load()
}

// Send sends one masked binary frame.
func (w *RawWebSocket) Send(data []byte) error {
	if w.closed.Load() {
		return net.ErrClosed
	}
	frame := buildWSFrame(opBinary, data, true)
	_, err := w.conn.Write(frame)
	return err
}

// SendBatch sends multiple masked binary frames in one write syscall batch.
func (w *RawWebSocket) SendBatch(parts [][]byte) error {
	if w.closed.Load() {
		return net.ErrClosed
	}
	var buf []byte
	for _, p := range parts {
		buf = append(buf, buildWSFrame(opBinary, p, true)...)
	}
	_, err := w.conn.Write(buf)
	return err
}

// Recv returns next binary/text payload, or nil on clean close.
func (w *RawWebSocket) Recv() ([]byte, error) {
	for !w.closed.Load() {
		opcode, payload, err := w.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case opClose:
			w.closed.Store(true)
			echo := payload
			if len(echo) > 2 {
				echo = echo[:2]
			}
			reply := buildWSFrame(opClose, echo, true)
			_, _ = w.conn.Write(reply)
			return nil, nil
		case opPing:
			pong := buildWSFrame(opPong, payload, true)
			_, _ = w.conn.Write(pong)
			continue
		case opPong:
			continue
		case opText, opBinary:
			return payload, nil
		default:
			continue
		}
	}
	return nil, net.ErrClosed
}

// Close sends close frame and shuts down the connection.
func (w *RawWebSocket) Close() error {
	if w.closed.Swap(true) {
		return nil
	}
	_, _ = w.conn.Write(buildWSFrame(opClose, nil, true))
	return w.conn.Close()
}

func (w *RawWebSocket) readFrame() (opcode byte, payload []byte, err error) {
	br := w.br
	var hdr [2]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		return 0, nil, err
	}
	opcode = hdr[0] & 0x0F
	length := int(hdr[1] & 0x7F)
	switch length {
	case 126:
		var ln [2]byte
		if _, err := io.ReadFull(br, ln[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(ln[:]))
	case 127:
		var ln [8]byte
		if _, err := io.ReadFull(br, ln[:]); err != nil {
			return 0, nil, err
		}
		n := binary.BigEndian.Uint64(ln[:])
		if n > maxWSFrameSize {
			return 0, nil, fmt.Errorf("ws frame length %d exceeds limit (%d)", n, maxWSFrameSize)
		}
		length = int(n)
	}
	if length < 0 || length > maxWSFrameSize {
		return 0, nil, fmt.Errorf("ws frame length %d exceeds limit (%d)", length, maxWSFrameSize)
	}
	if hdr[1]&0x80 != 0 {
		var mk [4]byte
		if _, err := io.ReadFull(br, mk[:]); err != nil {
			return 0, nil, err
		}
		payload = make([]byte, length)
		if _, err := io.ReadFull(br, payload); err != nil {
			return 0, nil, err
		}
		xorMask(payload, mk[:])
		return opcode, payload, nil
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return 0, nil, err
	}
	return opcode, payload, nil
}

func xorMask(data, mask []byte) {
	for i := range data {
		data[i] ^= mask[i%4]
	}
}

func buildWSFrame(opcode byte, data []byte, mask bool) []byte {
	length := len(data)
	fb := byte(0x80 | opcode)
	if !mask {
		return appendUnmasked(fb, data, length)
	}
	var mk [4]byte
	_, _ = rand.Read(mk[:])
	payload := append([]byte(nil), data...)
	xorMask(payload, mk[:])
	return appendMasked(fb, payload, length, mk)
}

func appendUnmasked(fb byte, data []byte, length int) []byte {
	switch {
	case length < 126:
		out := make([]byte, 2+length)
		out[0], out[1] = fb, byte(length) // #nosec G115 -- length < 126, fits in byte
		copy(out[2:], data)
		return out
	case length < 65536:
		out := make([]byte, 4+length)
		out[0], out[1] = fb, 126
		out[2], out[3] = byte(length>>8), byte(length) // #nosec G115 -- length < 65536, truncation is intentional WS framing
		copy(out[4:], data)
		return out
	default:
		out := make([]byte, 10+length)
		out[0], out[1] = fb, 127
		binary.BigEndian.PutUint64(out[2:10], uint64(length))
		copy(out[10:], data)
		return out
	}
}

func appendMasked(fb byte, masked []byte, length int, mk [4]byte) []byte {
	switch {
	case length < 126:
		out := make([]byte, 2+4+length)
		out[0], out[1] = fb, byte(0x80|length) // #nosec G115 -- length < 126, fits in byte
		copy(out[2:], mk[:])
		copy(out[6:], masked)
		return out
	case length < 65536:
		out := make([]byte, 4+4+length)
		out[0], out[1] = fb, 0x80|126
		out[2], out[3] = byte(length>>8), byte(length) // #nosec G115 -- length < 65536, truncation is intentional WS framing
		copy(out[4:], mk[:])
		copy(out[8:], masked)
		return out
	default:
		out := make([]byte, 10+4+length)
		out[0], out[1] = fb, 0x80|127
		binary.BigEndian.PutUint64(out[2:10], uint64(length))
		copy(out[10:], mk[:])
		copy(out[14:], masked)
		return out
	}
}

func setSockOpts(c *net.TCPConn, bufBytes int) {
	_ = c.SetNoDelay(true)
	if bufBytes > 0 {
		_ = c.SetReadBuffer(bufBytes)
		_ = c.SetWriteBuffer(bufBytes)
	}
}
