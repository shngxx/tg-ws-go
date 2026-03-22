package main

import (
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"
)

const bridgeBufSize = 65536

// BridgeWS runs bidirectional TCP <-> WebSocket until one side finishes.
func BridgeWS(client net.Conn, ws *RawWebSocket, label string, dc int, dst string, port int, isMedia bool, splitter *MsgSplitter, stats *Stats, log *slog.Logger) {
	dcTag := formatDCTag(dc, isMedia)
	dstTag := net.JoinHostPort(dst, strconv.Itoa(port))
	start := time.Now()

	var upBytes, downBytes int64
	var upPkts, downPkts int64

	done := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(done) }) }

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer finish()
		buf := make([]byte, bridgeBufSize)
		for {
			n, err := client.Read(buf)
			if n > 0 {
				stats.BytesUp.Add(int64(n))
				upBytes += int64(n)
				upPkts++
				chunk := buf[:n]
				var err2 error
				if splitter != nil {
					parts := splitter.Split(chunk)
					if len(parts) > 1 {
						err2 = ws.SendBatch(parts)
					} else {
						err2 = ws.Send(parts[0])
					}
				} else {
					err2 = ws.Send(chunk)
				}
				if err2 != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer finish()
		for {
			data, err := ws.Recv()
			if err != nil {
				return
			}
			if data == nil {
				return
			}
			stats.BytesDown.Add(int64(len(data)))
			downBytes += int64(len(data))
			downPkts++
			if _, err := client.Write(data); err != nil {
				return
			}
		}
	}()

	<-done
	_ = ws.Close()
	_ = client.Close()
	wg.Wait()
	elapsed := time.Since(start)
	if log != nil {
		log.Info("WS session closed",
			"peer", label,
			"dc", dcTag,
			"dst", dstTag,
			"up", humanBytes(upBytes), "up_pkts", upPkts,
			"down", humanBytes(downBytes), "down_pkts", downPkts,
			"secs", elapsed.Seconds(),
		)
	}
}

// BridgeTCPWithStats is bidirectional TCP relay for fallback (matches Python _bridge_tcp).
func BridgeTCPWithStats(client, remote net.Conn, label string, dc int, dst string, port int, isMedia bool, stats *Stats, log *slog.Logger) {
	dcTag := formatDCTag(dc, isMedia)
	dstTag := net.JoinHostPort(dst, strconv.Itoa(port))
	start := time.Now()

	done := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(done) }) }

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer finish()
		buf := make([]byte, bridgeBufSize)
		for {
			n, err := client.Read(buf)
			if n > 0 {
				stats.BytesUp.Add(int64(n))
				if _, werr := remote.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer finish()
		buf := make([]byte, bridgeBufSize)
		for {
			n, err := remote.Read(buf)
			if n > 0 {
				stats.BytesDown.Add(int64(n))
				if _, werr := client.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	<-done
	_ = client.Close()
	_ = remote.Close()
	wg.Wait()
	if log != nil {
		log.Info("TCP fallback session closed", "peer", label, "dc", dcTag, "dst", dstTag, "secs", time.Since(start).Seconds())
	}
}

// Pipe copies from r to w until EOF (passthrough).
func Pipe(r, w net.Conn) {
	_, _ = io.Copy(w, r)
	_ = w.Close()
}

func formatDCTag(dc int, isMedia bool) string {
	if dc <= 0 {
		return "DC?"
	}
	s := "DC" + strconv.Itoa(dc)
	if isMedia {
		s += "m"
	}
	return s
}
