package server

// Delegation (big-change Phase 6): a todo becomes dispatchable to a harness.
// Strict propose-only decomposition — no new store, no new write boundary:
//
//   - "delegate" spools a run request into the CHOSEN harness (the existing
//     spool contract) with the todo's composite id riding in the request as a
//     `[todo:: <id>]` token, and marks a personal todo `[waiting:: <harness>]`
//     through the existing todos machinery. The spool file IS the work order.
//   - Results return as run reports and/or proposals (approvals inbox) that
//     carry the same token; nothing here executes anything.
//   - Read-side, every unified todo row gains a `delegation` projection
//     (queued | running | failed | done | proposed + harness) derived purely
//     from which trace files exist and carry the token.

import (
	"net/http"
	"regexp"
	"strings"
	"time"
)

var todoTokenRe = regexp.MustCompile(`\[todo::\s*([^\]]+)\]`)

// delegationView is the row projection.
type delegationView struct {
	State   string `json:"state"` // queued | running | failed | done | proposed
	Harness string `json:"harness"`
	RunID   string `json:"runId,omitempty"`
}

// delegationIndex scans every harness's traces ONCE per request: spool files
// (queued), run reports (running/failed/done), pending proposals (proposed).
// Precedence: proposed > running > queued > failed > done — a returned
// proposal is the thing awaiting the human; a live run beats history.
func (s *Server) delegationIndex() map[string]delegationView {
	rank := map[string]int{"done": 1, "failed": 2, "queued": 3, "running": 4, "proposed": 5}
	out := map[string]delegationView{}
	set := func(id string, d delegationView) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if cur, ok := out[id]; !ok || rank[d.State] > rank[cur.State] {
			out[id] = d
		}
	}
	for _, h := range s.eachHarness() {
		if h.Spirits != nil {
			for _, q := range h.Spirits.Queued() {
				if m := todoTokenRe.FindStringSubmatch(q.Request); m != nil {
					set(m[1], delegationView{State: "queued", Harness: h.Name})
				}
			}
			for _, r := range h.Spirits.Runs() {
				m := todoTokenRe.FindStringSubmatch(r.Request)
				if m == nil {
					continue
				}
				state := "done"
				switch r.Outcome {
				case "running", "":
					state = "running"
				case "completed":
					state = "done"
				default:
					state = "failed"
				}
				set(m[1], delegationView{State: state, Harness: h.Name, RunID: r.ID})
			}
		}
		if h.Approvals != nil {
			for _, p := range h.Approvals.List("pending") {
				if m := todoTokenRe.FindStringSubmatch(p.Action + "\n" + p.Body); m != nil {
					set(m[1], delegationView{State: "proposed", Harness: h.Name})
				}
			}
		}
	}
	return out
}

// delegateTarget is one dispatchable destination: a harness's on-demand
// ritual, or the bare harness itself (contract-only trees like hermes — the
// spool file still carries spirit/ritual per the contract; the tree decides
// what to do with it).
type delegateTarget struct {
	Harness string `json:"harness"`
	Spirit  string `json:"spirit"`
	Ritual  string `json:"ritual"`
	Label   string `json:"label"`
}

func (s *Server) handleDelegateTargets(w http.ResponseWriter, r *http.Request) {
	targets := []delegateTarget{}
	now := time.Now()
	for _, h := range s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		n := 0
		for _, rit := range h.Spirits.Rituals(now) {
			if rit.Cadence != "" || !rit.Valid {
				continue // scheduled or broken — not a dispatch target
			}
			targets = append(targets, delegateTarget{
				Harness: h.Name, Spirit: rit.Spirit, Ritual: rit.Ritual,
				Label: h.Name + " · " + rit.Spirit + " / " + rit.Ritual,
			})
			n++
		}
		if n == 0 {
			// contract-only tree: a generic work order it may consume
			targets = append(targets, delegateTarget{
				Harness: h.Name, Spirit: h.Name, Ritual: "delegate",
				Label: h.Name + " · (contract work order)",
			})
		}
	}
	writeJSON(w, map[string]any{"targets": targets})
}

// handleDelegate spools the work order + stamps waiting on personal todos.
func (s *Server) handleDelegate(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID      string `json:"id"` // unified composite id
		Harness string `json:"harness"`
		Spirit  string `json:"spirit"`
		Ritual  string `json:"ritual"`
		Brief   string `json:"brief"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if b.ID == "" || b.Harness == "" || b.Spirit == "" || b.Ritual == "" {
		httpError(w, errBadRequest("id, harness, spirit and ritual are required"))
		return
	}
	var target *Harness
	hs := s.eachHarness()
	for i := range hs {
		if hs[i].Name == b.Harness {
			target = &hs[i]
			break
		}
	}
	if target == nil || target.Spirits == nil {
		httpError(w, errBadRequest("unknown harness "+b.Harness))
		return
	}
	// the request line: the brief (or the todo's own text) + the durable token
	text := strings.TrimSpace(b.Brief)
	if text == "" {
		if s.todosStore != nil {
			if doc, err := s.todosStore.Load(); err == nil {
				if _, t := doc.Find(b.ID); t != nil {
					text = t.Text
				}
			}
		}
		if text == "" {
			text = "work the delegated todo"
		}
	}
	request := text + " [todo:: " + b.ID + "]"
	if err := target.Spirits.SpoolRunNow(b.Spirit, b.Ritual, request, ""); err != nil {
		httpError(w, err)
		return
	}
	// waiting stamp — personal todos only (the chip derives from traces for
	// every source; waiting is the belt on the todo line itself)
	if s.todosStore != nil && !strings.ContainsAny(b.ID, ":") {
		if doc, err := s.todosStore.Load(); err == nil {
			if _, t := doc.Find(b.ID); t != nil {
				t.Waiting = b.Harness
				t.Since = time.Now().Format("2006-01-02")
				_ = s.todosStore.Save(doc)
			}
		}
	}
	writeJSON(w, map[string]any{"ok": true, "queued": true})
}
