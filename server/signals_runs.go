package server

// Run-failure FEED cards (owner ask 2026-08-11 + big-change Phase 7): a ritual
// that ran and FAILED pages the human in the feed. One signal per
// harness/spirit/ritual whose LATEST terminal run inside the window failed —
// auto-clears the moment a newer run of the same pair completes (the §5
// auto-clearing rule); a dismissal re-arms on the NEXT failure (hash = run id).

import (
	"time"

	"manifest/signals"
)

// runFailureWindow: failures older than this stop paging (the run list still
// holds them; decay is the medium's norm).
const runFailureWindow = 48 * time.Hour

// RunFailureEmitter adapts the harness federation to the signals contract.
// Lazy over s.eachHarness() — wiring order in main doesn't matter.
func (s *Server) RunFailureEmitter() signals.Emitter { return runFailEmitter{s} }

type runFailEmitter struct{ s *Server }

func (e runFailEmitter) Emit(now time.Time) ([]signals.Signal, error) {
	out := []signals.Signal{}
	for _, h := range e.s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		// newest terminal run per spirit/ritual (Runs() is newest-first per store)
		latest := map[string]bool{} // spirit/ritual → seen a terminal run
		for _, r := range h.Spirits.Runs() {
			if r.Outcome == "running" || r.Outcome == "" {
				continue
			}
			key := r.Spirit + "/" + r.Ritual
			if latest[key] {
				continue // an older run — the newest terminal one already decided
			}
			latest[key] = true
			if r.Outcome == "completed" {
				continue
			}
			started, err := time.Parse(time.RFC3339, r.Started)
			if err != nil || now.Sub(started) > runFailureWindow {
				continue
			}
			label := "run failed · " + key + " · " + r.Outcome
			if tag := e.s.harnessTag(h.Name); tag != "" {
				label += " · " + tag
			}
			out = append(out, signals.Signal{
				ID:      "run-failed:" + h.Name + "/" + key,
				Kind:    "run-failed",
				Entity:  key,
				Label:   label,
				Age:     int(now.Sub(started).Hours() / 24),
				ActHref: "#/spirits",
				Hash:    r.ID, // a NEW failure re-arms a dismissal
			})
		}
	}
	return out, nil
}
