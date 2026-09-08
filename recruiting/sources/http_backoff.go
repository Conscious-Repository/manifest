package sources

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const scholarlyAttempts = 4

// scholarlyWait is overridable by package tests; adapters carry no new capabilities.
var scholarlyWait = waitScholarlyRetry

func waitScholarlyRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func scholarlyDelay(header string, attempt int, now time.Time) time.Duration {
	base := (500 * time.Millisecond) << attempt
	delay := base + time.Duration(rand.Int64N(int64(base/2)))
	header = strings.TrimSpace(header)
	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil && seconds >= 0 {
		// Saturate rather than overflow for a hostile Retry-After value.
		maxSeconds := int64((1<<63 - 1) / time.Second)
		if seconds > maxSeconds {
			seconds = maxSeconds
		}
		if retry := time.Duration(seconds) * time.Second; retry > delay {
			delay = retry
		}
	} else if at, err := http.ParseTime(header); err == nil && at.Sub(now) > delay {
		delay = at.Sub(now)
	}
	return delay
}

// scholarlyGet retries only explicit transient statuses. The adapter supplies its
// headers, context deadline and body bound; each response closes before waiting.
func scholarlyGet(client http.Client, req *http.Request, source, path string, maxBody int64) ([]byte, error) {
	return scholarlyRequest(client, req, source, path, maxBody)
}

// scholarlyRequest also supports replayable POST bodies under the same retry budget.
func scholarlyRequest(client http.Client, req *http.Request, source, path string, maxBody int64) ([]byte, error) {
	for attempt := 0; attempt < scholarlyAttempts; attempt++ {
		next := req.Clone(req.Context())
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			next.Body = body
		}
		resp, err := client.Do(next)
		if err != nil {
			return nil, fmt.Errorf("%s: %s %s: %w", source, req.Method, path, err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		resp.Body.Close()
		transient := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable
		if transient && attempt+1 < scholarlyAttempts {
			if err := scholarlyWait(req.Context(), scholarlyDelay(resp.Header.Get("Retry-After"), attempt, time.Now())); err != nil {
				return nil, fmt.Errorf("%s: retry %s %s: %w", source, req.Method, path, err)
			}
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("%s: reading %s %s: %w", source, req.Method, path, readErr)
		}
		if resp.StatusCode != http.StatusOK {
			msg := fmt.Sprintf("%s: %s %s returned HTTP %d", source, req.Method, path, resp.StatusCode)
			if excerpt := strings.Join(strings.Fields(string(body)), " "); excerpt != "" {
				if len(excerpt) > 200 {
					excerpt = excerpt[:200] + "…"
				}
				msg += ": " + excerpt
			}
			return nil, fmt.Errorf("%s", msg)
		}
		return body, nil
	}
	panic("unreachable")
}
