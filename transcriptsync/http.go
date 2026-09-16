package transcriptsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Transport deliberately reports only status codes, never response bodies or
// credential-bearing URLs. All calls are bounded and safe GETs.
type transport struct {
	key, base string
	client    *http.Client
	interval  time.Duration
	mu        sync.Mutex
	last      time.Time
}
type GranolaClient struct{ *transport }
type PocketClient struct{ *transport }

func NewGranolaClient(key string) *GranolaClient {
	return &GranolaClient{newTransport(key, "https://public-api.granola.ai/v1", 200*time.Millisecond)}
}
func NewPocketClient(key string) *PocketClient {
	return &PocketClient{newTransport(key, "https://public.heypocketai.com/api/v1", 500*time.Millisecond)}
}
func newTransport(key, base string, interval time.Duration) *transport {
	return &transport{key: key, base: base, interval: interval, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (c *transport) fetch(ctx context.Context, path string, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == "" {
		return fmt.Errorf("source credential missing")
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := wait(ctx, c.interval-time.Since(c.last)); err != nil {
			return err
		}
		c.last = time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
		if err != nil {
			return fmt.Errorf("invalid source request")
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Accept", "application/json")
		resp, err := c.client.Do(req)
		if err != nil {
			return fmt.Errorf("source request failed")
		}
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, (32<<20)+1))
		resp.Body.Close()
		if readErr != nil || len(b) > 32<<20 {
			return fmt.Errorf("source response unreadable or too large")
		}
		if (resp.StatusCode == 429 || resp.StatusCode >= 500) && attempt < 2 {
			if err := wait(ctx, time.Duration(1<<attempt)*time.Second); err != nil {
				return err
			}
			continue
		}
		if resp.StatusCode != 200 {
			return fmt.Errorf("source HTTP %d", resp.StatusCode)
		}
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("invalid source JSON")
		}
		return nil
	}
	return fmt.Errorf("source retries exhausted")
}
func (c *GranolaClient) get(ctx context.Context, path string, out any) error {
	return c.fetch(ctx, path, out)
}
func (c *PocketClient) get(ctx context.Context, path string, q url.Values, out any) error {
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return c.fetch(ctx, path, out)
}
