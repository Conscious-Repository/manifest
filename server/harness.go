package server

// Harness federation (big-change Phase 4): manifest reads N harness trees
// behind one on-disk contract (CONTRACT.md in the harnesses repo). The FIRST
// harness is the primary — it keeps every write surface (spool run-now, the
// ritual editor, castables, aion-sink spooling, proposal filing); the
// rest surface read-side only, per the contract, voluntarily: drop run reports
// in <tree>/artifacts/runs → they appear; file proposals in the approvals
// shape → they hit the inbox. Merged rows carry a `harness` tag; the UI shows
// a muted source chip for non-primary rows.

import (
	"path/filepath"
	"sort"
	"time"

	"manifest/approvals"
	"manifest/spirits"
)

// Harness is one federated tree's stores. Surface "" = personal dashboard;
// "team" = offered only on the AION portal roster (kairos plan) — delegation
// machinery includes it either way.
type Harness struct {
	Name      string
	Surface   string
	Spirits   *spirits.Store
	Approvals *approvals.Store
}

// UseHarnesses wires the federation (primary first). It also sets the legacy
// single-store fields to the primary, so every primary-only path is untouched.
func (s *Server) UseHarnesses(list []Harness) {
	for _, h := range list {
		if h.Spirits != nil {
			h.Spirits.WithHarnessName(h.Name)
		}
	}
	s.harnessList = list
	if len(list) > 0 {
		s.spirits = list[0].Spirits
		s.approvals = list[0].Approvals
	}
}

// eachHarness returns the federation, primary first — synthesized from the
// legacy single-store wiring when UseHarnesses was never called (tests, old
// callers), so composition code has exactly one shape to walk. Terminal-backed
// owners expose the same report/library contract in dataDir; no engine consumes
// their work orders. A virtual Hermes tree holds its Tier-1 result artifacts.
func (s *Server) eachHarness() []Harness {
	hs := append([]Harness(nil), s.harnessList...)
	if len(hs) == 0 && (s.spirits != nil || s.approvals != nil || s.terminal != nil) {
		hs = []Harness{{Name: "excalibur", Spirits: s.spirits, Approvals: s.approvals}}
	}
	if s.terminal != nil {
		names := []string{"claude", "codex"}
		if s.hermesEnabled() {
			names = append([]string{"hermes"}, names...)
		}
		for _, name := range names {
			found := false
			for _, h := range hs {
				if h.Name == name {
					found = true
				}
			}
			if !found {
				hs = append(hs, Harness{Name: name, Spirits: spirits.NewStore(filepath.Join(filepath.Dir(s.terminal.regPath), "board-agents", name))})
			}
		}
	}
	return hs
}

// primaryHarnessName is the tag the UI treats as home (no chip shown).
func (s *Server) primaryHarnessName() string {
	if h := s.eachHarness(); len(h) > 0 {
		return h[0].Name
	}
	return "excalibur"
}

// harnessTag is the row tag: empty for the primary (single-harness payloads
// stay byte-identical; the UI chips any non-empty tag), the name otherwise.
func (s *Server) harnessTag(name string) string {
	if name == s.primaryHarnessName() {
		return ""
	}
	return name
}

// runView / queuedView — merged rows with their source harness.
type runView struct {
	spirits.RunSummary
	Harness string `json:"harness,omitempty"`
}

type queuedView struct {
	spirits.QueuedRun
	Harness string `json:"harness,omitempty"`
}

// mergedRuns is every harness's run reports, newest first.
func (s *Server) mergedRuns() []runView {
	out := []runView{}
	for _, h := range s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		for _, r := range h.Spirits.Runs() {
			out = append(out, runView{RunSummary: r, Harness: s.harnessTag(h.Name)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Started > out[j].Started })
	return out
}

// mergedQueued is every harness's unconsumed spool files.
func (s *Server) mergedQueued() []queuedView {
	out := []queuedView{}
	for _, h := range s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		for _, q := range h.Spirits.Queued() {
			out = append(out, queuedView{QueuedRun: q, Harness: s.harnessTag(h.Name)})
		}
	}
	return out
}

// findRun locates a run report by id across harnesses (primary first).
func (s *Server) findRun(id string) (Harness, spirits.RunSummary, string, bool) {
	for _, h := range s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		if sum, body, ok := h.Spirits.Run(id); ok {
			return h, sum, body, true
		}
	}
	return Harness{}, spirits.RunSummary{}, "", false
}

// harnessHeartbeat is one harness's engine liveness for the status payload.
type harnessHeartbeat struct {
	Name        string `json:"name"`
	EngineAlive bool   `json:"engineAlive"`
	Heartbeat   string `json:"heartbeat,omitempty"`
}

func (s *Server) harnessHeartbeats() []harnessHeartbeat {
	out := []harnessHeartbeat{}
	for _, h := range s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		alive, at := h.Spirits.EngineAlive()
		hb := harnessHeartbeat{Name: h.Name, EngineAlive: alive}
		if !at.IsZero() {
			hb.Heartbeat = at.Format(time.RFC3339)
		}
		out = append(out, hb)
	}
	return out
}

// feedHarnessFor finds the harness whose feed store holds the item id —
// the routing for feed actions (status, snooze, save-to-vault, to-task).
func (s *Server) feedHarnessFor(id string) (Harness, bool) {
	for _, h := range s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		if _, ok := h.Spirits.Feed.Get(id); ok {
			return h, true
		}
	}
	return Harness{}, false
}

// approvalsFor finds the harness whose inbox holds the pending proposal id —
// the routing for Confirm/Reject/edit. Primary first (ids are content-derived
// filenames; cross-harness collision is effectively nil).
func (s *Server) approvalsFor(id string) *approvals.Store {
	for _, h := range s.eachHarness() {
		if h.Approvals == nil {
			continue
		}
		if _, err := h.Approvals.LoadPending(id); err == nil {
			return h.Approvals
		}
	}
	return s.approvals // fall through: primary produces the honest error
}
