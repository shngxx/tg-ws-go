package main

import (
	"fmt"
	"sync/atomic"
)

// Stats mirrors Python _stats with atomic counters (Go 1.26-friendly for concurrent handlers).
type Stats struct {
	ConnectionsTotal        atomic.Int64
	ConnectionsWS           atomic.Int64
	ConnectionsTCPFallback  atomic.Int64
	ConnectionsHTTPRejected atomic.Int64
	ConnectionsPassthrough  atomic.Int64
	WSErrors                atomic.Int64
	BytesUp                 atomic.Int64
	BytesDown               atomic.Int64
	PoolHits                atomic.Int64
	PoolMisses              atomic.Int64
}

func (s *Stats) Summary() string {
	h := s.PoolHits.Load()
	m := s.PoolMisses.Load()
	return fmt.Sprintf(
		"total=%d ws=%d tcp_fb=%d http_skip=%d pass=%d err=%d pool=%d/%d up=%s down=%s",
		s.ConnectionsTotal.Load(),
		s.ConnectionsWS.Load(),
		s.ConnectionsTCPFallback.Load(),
		s.ConnectionsHTTPRejected.Load(),
		s.ConnectionsPassthrough.Load(),
		s.WSErrors.Load(),
		h, h+m,
		humanBytes(s.BytesUp.Load()),
		humanBytes(s.BytesDown.Load()),
	)
}

// humanBytes formats byte counts like Python _human_bytes. The stdlib has no SI/binary helper.
func humanBytes(n int64) string {
	x := float64(n)
	for _, u := range []string{"B", "KB", "MB", "GB"} {
		if x < 1024 || u == "GB" {
			return fmt.Sprintf("%.1f%s", x, u)
		}
		x /= 1024
	}
	return fmt.Sprintf("%.1fTB", x/1024)
}
