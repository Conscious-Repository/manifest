package recruiting

import (
	"context"
	"log"
	"strings"
	"time"
)

// Poll uses the app's mechanical poller pattern. Webhooks remain the fast
// path; a missed delivery cannot strand the local application projection.
func (a *AshbySync) Poll(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		run, cancel := context.WithTimeout(ctx, 50*time.Second)
		if err := a.SyncIfStale(run, interval, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("recruiting Ashby catch-up: %v", err)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *AshbySync) SyncIfStale(ctx context.Context, interval time.Duration, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.client.Configured() {
		return nil
	}
	st := a.load()
	last, _ := time.Parse(time.RFC3339, st.LastSync)
	if !last.IsZero() && now.Sub(last) < interval {
		return nil
	}
	lastFull, _ := time.Parse(time.RFC3339, st.LastFull)
	full := lastFull.IsZero() || now.Sub(lastFull) >= 24*time.Hour
	_, err := a.syncBack(ctx, &st, full, now)
	if err != nil && !full && invalidAshbyCheckpoint(err) {
		// A fresh complete pass must start without either cursor or sync token.
		st = a.load()
		_, err = a.syncBack(ctx, &st, true, now)
	}
	if err != nil {
		return err
	}
	return a.save(st)
}
func invalidAshbyCheckpoint(err error) bool {
	text := err.Error()
	for _, code := range []string{"sync_token_expired", "sync_token_invalid", "incremental_sync_too_large", "next_cursor_expired", "cursor_invalid", "invalid_next_cursor"} {
		if strings.Contains(text, code) {
			return true
		}
	}
	return false
}
