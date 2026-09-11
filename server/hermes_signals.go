package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/signals"
)

// H1 of the excalibur-deprecation hardening (plan §4b, owner 2026-09-11):
// the agent runtime's failures must ANNOUNCE THEMSELVES. The 09-11 07:01
// aion-scout fire died with "Connection error." and surfaced nowhere but a
// chip on a page nobody was looking at; the 08-30..09-01 skips were three
// silent days. This emitter is the bank-feed pattern over the files Hermes
// already writes — no watchdog daemon, no second liveness system to watch
// the first:
//
//   - cron/ticker_heartbeat stale (or missing) while enabled jobs exist →
//     the scheduler itself is down and every ritual is off. That headline
//     suppresses the per-job missed-fire chips it would otherwise imply.
//   - an enabled, scheduled job whose next_run_at passed by more than a
//     grace window without last_run_at advancing → that fire was missed.
//   - a job whose last fire errored → the job's name and the error,
//     VERBATIM (a code with no sentence is what cost an afternoon on the
//     Ashby archive, 2026-09-04).
//
// Dismissals re-arm on state change: each Hash carries the heartbeat mtime,
// the missed fire's own time, or the error+run pair — so a dismissed chip
// stays quiet until something NEW is wrong, exactly like bank-feed-attention.

const (
	hermesTickerStale = 5 * time.Minute
	hermesFireGrace   = 15 * time.Minute
)

type hermesCronEmitter struct{ s *Server }

// HermesCronEmitter watches Alfred's cron plane. Registered beside the
// bank-feed emitter; a box with no Hermes tree emits nothing (absence of the
// runtime is a setup state, not an alarm).
func (s *Server) HermesCronEmitter() signals.Emitter { return hermesCronEmitter{s} }

func (e hermesCronEmitter) Emit(now time.Time) ([]signals.Signal, error) {
	s := e.s
	home := s.hermesHome()
	if home == "" {
		return nil, nil
	}
	if _, err := os.Stat(filepath.Join(home, "cron")); err != nil {
		return nil, nil
	}
	// jobs.json is the source; the CLI fallback inside hermesJobs is bounded
	// so a FEED load never hangs on a wedged binary
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	jobs, _, _ := s.hermesJobs(ctx)

	enabled := 0
	for _, j := range jobs {
		if j.Enabled {
			enabled++
		}
	}
	var out []signals.Signal

	// 1 — the ticker itself. Only meaningful while something is scheduled.
	tickerDead := false
	if enabled > 0 {
		hb, err := os.Stat(filepath.Join(home, "cron", "ticker_heartbeat"))
		if err != nil || now.Sub(hb.ModTime()) > hermesTickerStale {
			tickerDead = true
			mark, age := "missing", 0
			if err == nil {
				mark = hb.ModTime().UTC().Format(time.RFC3339)
				age = int(now.Sub(hb.ModTime()).Hours() / 24)
			}
			out = append(out, signals.Signal{
				ID:     "agent-cron-dead:alfred",
				Kind:   "agent-cron-dead",
				Entity: "alfred",
				Label:  "alfred · cron ticker silent — no rituals are firing",
				Age:    age, ActHref: "#/agents", Hash: mark,
			})
		}
	}

	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		name := strings.TrimSpace(j.Name)
		if name == "" {
			name = j.ID
		}

		// 2 — a missed fire (suppressed under a dead ticker: one headline,
		// not one chip per job it already explains)
		if !tickerDead && strings.EqualFold(j.State, "scheduled") {
			if next, ok := hermesTime(j.NextRunAt); ok && now.After(next.Add(hermesFireGrace)) {
				last, lok := hermesTime(j.LastRunAt)
				if !lok || last.Before(next) {
					out = append(out, signals.Signal{
						ID:     "agent-cron-missed:" + j.ID,
						Kind:   "agent-cron-missed",
						Entity: name,
						Label:  name + " · fire due " + next.Format("Jan 2 15:04") + " never ran",
						Age:    int(now.Sub(next).Hours() / 24),
						ActHref: "#/agents",
						Hash:    j.NextRunAt,
					})
				}
			}
		}

		// 3 — the last fire errored
		if strings.TrimSpace(j.LastError) != "" || strings.EqualFold(j.LastStatus, "error") {
			msg := strings.TrimSpace(j.LastError)
			if msg == "" {
				msg = "last fire errored"
			}
			if len(msg) > 120 {
				msg = msg[:120] + "…"
			}
			age := 0
			if last, ok := hermesTime(j.LastRunAt); ok {
				age = int(now.Sub(last).Hours() / 24)
			}
			out = append(out, signals.Signal{
				ID:     "agent-cron-error:" + j.ID,
				Kind:   "agent-cron-error",
				Entity: name,
				Label:  name + " · " + msg,
				Age:    age, ActHref: "#/agents",
				Hash: j.LastRunAt + "|" + msg,
			})
		}
	}
	return out, nil
}

// hermesTime parses the timestamp spellings jobs.json has used.
func hermesTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
