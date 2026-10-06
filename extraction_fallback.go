package main

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	"manifest/domainextract"
	"manifest/hermes"
	"manifest/server"
)

// defaultLabAddr is the lab model's endpoint (hermes extractionConfig).
const defaultLabAddr = "192.168.87.11:8000"

// extractionFallback builds the lab-down behaviour: always wait for an
// unreachable lab; run cleared notes on Claude when the owner enabled it.
func extractionFallback(c ExtractionFallbackConfig, runner *hermes.Runner, srv *server.Server) *domainextract.Fallback {
	addr := c.LabAddr
	if addr == "" {
		addr = defaultLabAddr
	}
	f := &domainextract.Fallback{LabUp: labProbe(addr)}
	if !c.Enabled {
		return f
	}
	rituals, tiers := c.Rituals, c.Tiers
	if len(rituals) == 0 {
		rituals = []string{"aion"}
	}
	if len(tiers) == 0 {
		tiers = []string{"open", "internal"}
	}
	choice := hermes.ClaudeExtraction{Binary: c.Binary, Model: c.Model, OwnerAction: c.OwnerAction}
	f.Route = srv.ExtractionRoute(rituals, tiers)
	f.Run = func(ctx context.Context, ritual, prompt string) (hermes.Result, error) {
		return runner.RunClaudeExtraction(ctx, ritual, prompt, choice)
	}
	log.Printf("extraction: Claude stand-in enabled for %v (tiers %v) while %s is unreachable", rituals, tiers, addr)
	return f
}

// labProbe answers whether the lab model accepts a TCP connection, cached
// for a minute (a sweep of many jobs probes once).
func labProbe(addr string) func(context.Context) bool {
	var mu sync.Mutex
	var at time.Time
	var up bool
	return func(ctx context.Context) bool {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) < time.Minute {
			return up
		}
		d := net.Dialer{Timeout: 3 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			conn.Close()
		}
		was := up
		up, at = err == nil, time.Now()
		if was != up {
			log.Printf("extraction: lab model %s reachable=%v", addr, up)
		}
		return up
	}
}
