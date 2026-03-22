package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const wsPoolMaxAge = 120 * time.Second

type dcMediaKey struct {
	DC    int
	Media bool
}

type pooledWS struct {
	ws      *RawWebSocket
	created time.Time
}

// WsPool keeps idle WebSocket connections per (dc, media).
type WsPool struct {
	mu         sync.Mutex
	idle       map[dcMediaKey][]pooledWS
	refilling  map[dcMediaKey]struct{}
	poolSize   int
	bufBytes   int
	log        *slog.Logger
	stats      *Stats
	connectTO  time.Duration // pool refill dial timeout (Python: 8s)
}

func NewWsPool(poolSize, bufBytes int, log *slog.Logger, stats *Stats) *WsPool {
	if poolSize < 0 {
		poolSize = 0
	}
	return &WsPool{
		idle:      make(map[dcMediaKey][]pooledWS),
		refilling: make(map[dcMediaKey]struct{}),
		poolSize:  poolSize,
		bufBytes:  bufBytes,
		log:       log,
		stats:     stats,
		connectTO: 8 * time.Second,
	}
}

// Get returns an idle connection or nil.
func (p *WsPool) Get(dc int, isMedia bool, targetIP string, domains []string) *RawWebSocket {
	if p == nil || p.poolSize == 0 {
		return nil
	}
	key := dcMediaKey{DC: dc, Media: isMedia}
	now := time.Now()

	p.mu.Lock()
	bucket := p.idle[key]
	for len(bucket) > 0 {
		entry := bucket[0]
		bucket = bucket[1:]
		p.idle[key] = bucket
		age := now.Sub(entry.created)
		if age > wsPoolMaxAge || entry.ws.Closed() {
			w := entry.ws
			p.mu.Unlock()
			go func() { _ = w.Close() }()
			p.mu.Lock()
			bucket = p.idle[key]
			continue
		}
		p.stats.PoolHits.Add(1)
		p.mu.Unlock()
		p.scheduleRefill(key, targetIP, domains)
		return entry.ws
	}
	p.stats.PoolMisses.Add(1)
	p.mu.Unlock()
	p.scheduleRefill(key, targetIP, domains)
	return nil
}

func (p *WsPool) scheduleRefill(key dcMediaKey, targetIP string, domains []string) {
	p.mu.Lock()
	if _, busy := p.refilling[key]; busy {
		p.mu.Unlock()
		return
	}
	p.refilling[key] = struct{}{}
	p.mu.Unlock()

	go p.refill(key, targetIP, domains)
}

func (p *WsPool) refill(key dcMediaKey, targetIP string, domains []string) {
	defer func() {
		p.mu.Lock()
		delete(p.refilling, key)
		p.mu.Unlock()
	}()

	p.mu.Lock()
	needed := p.poolSize - len(p.idle[key])
	p.mu.Unlock()
	if needed <= 0 {
		return
	}

	dc, isMedia := key.DC, key.Media
	results := make(chan *RawWebSocket, needed)
	for range needed {
		go func() {
			var ws *RawWebSocket
			for _, domain := range domains {
				w, err := ConnectWS(context.Background(), targetIP, domain, "/apiws", p.connectTO, p.bufBytes)
				if err == nil {
					ws = w
					break
				}
				var he *WsHandshakeError
				if errors.As(err, &he) && he != nil && he.IsRedirect() {
					continue
				}
				break
			}
			results <- ws
		}()
	}

	for range needed {
		ws := <-results
		if ws == nil {
			continue
		}
		p.mu.Lock()
		p.idle[key] = append(p.idle[key], pooledWS{ws: ws, created: time.Now()})
		n := len(p.idle[key])
		p.mu.Unlock()
		if p.log != nil {
			p.log.Debug("WS pool refilled", "dc", dc, "media", isMedia, "ready", n)
		}
	}
}

// Shutdown closes all idle WebSocket connections in the pool.
func (p *WsPool) Shutdown() {
	if p == nil {
		return
	}
	p.mu.Lock()
	idle := p.idle
	p.idle = make(map[dcMediaKey][]pooledWS)
	p.mu.Unlock()

	for _, bucket := range idle {
		for _, entry := range bucket {
			go func(w *RawWebSocket) { _ = w.Close() }(entry.ws)
		}
	}
}

// Warmup pre-fills pools for all configured DCs (both media and non-media).
func (p *WsPool) Warmup(dcOpt map[int]string) {
	if p == nil || p.poolSize == 0 {
		return
	}
	for dc, ip := range dcOpt {
		if ip == "" {
			continue
		}
		for _, media := range []bool{false, true} {
			domains := WsDomains(dc, media)
			key := dcMediaKey{DC: dc, Media: media}
			p.scheduleRefill(key, ip, domains)
		}
	}
	if p.log != nil {
		p.log.Info("WS pool warmup started", "dcs", len(dcOpt))
	}
}
