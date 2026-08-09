package server

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"manifest/attention"
	"manifest/errands"
)

// ERRANDS — the action layer's API (errands-aside plan): compose + list +
// cancel/retry + transcript. Records live under <dataDir>/errands/ (never the
// vault); the records ARE the receipts the FEED renders. Nothing runs that
// the user didn't explicitly submit or approve (§4).

func (s *Server) UseErrands(store *errands.Store, exec *errands.Executor) {
	s.errands = store
	s.errandExec = exec
}

// SetErrandAccounts installs the §6 allowlist ("" = any signed-in account).
func (s *Server) SetErrandAccounts(ids []string) { s.errandAccounts = ids }

func (s *Server) errandsOK(w http.ResponseWriter) bool {
	if s.errands == nil || s.errandExec == nil {
		http.Error(w, "errands not available", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// receiptView is one receipt card (client contract).
type receiptView struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Text         string `json:"text"`
	Account      string `json:"account"`
	GoalID       string `json:"goalId,omitempty"`
	Source       string `json:"source"`
	ApprovalID   string `json:"approvalId,omitempty"`
	Created      string `json:"created"`
	Started      string `json:"started,omitempty"`
	Finished     string `json:"finished,omitempty"`
	DurationS    int    `json:"durationS,omitempty"`
	Outcome      string `json:"outcome,omitempty"`
	QueuePos     int    `json:"queuePos,omitempty"`
	Transcript   bool   `json:"transcript"`
	Acknowledged bool   `json:"acknowledged,omitempty"`
}

func (s *Server) receiptViews() []receiptView {
	if s.errands == nil {
		return []receiptView{}
	}
	out := []receiptView{}
	for _, r := range s.errands.List() {
		v := receiptView{
			ID: r.ID, Status: string(r.Status), Text: r.Text, Account: r.Account,
			GoalID: r.GoalID, Source: r.Source, ApprovalID: r.ApprovalID,
			Created: r.Created, Started: r.Started, Finished: r.Finished,
			Outcome: r.Outcome, Acknowledged: r.Acknowledged,
		}
		if r.Transcript != "" {
			if _, err := os.Stat(s.errands.TranscriptPath(r.ID)); err == nil {
				v.Transcript = true
			}
		}
		// done cards show the agent's full final answer block, recomputed from
		// the transcript (records written before OutcomeBlock existed hold only
		// the last line; failures keep their stored one-liner, incl. timeouts).
		if r.Status == errands.StatusDone && v.Transcript {
			if raw, err := os.ReadFile(s.errands.TranscriptPath(r.ID)); err == nil {
				if tail := raw; len(tail) > 0 {
					if len(tail) > 16384 {
						tail = tail[len(tail)-16384:]
					}
					if block := errands.OutcomeBlock(tail); block != "" {
						v.Outcome = block
					}
				}
			}
		}
		if r.Status == errands.StatusQueued && s.errandExec != nil {
			v.QueuePos = s.errandExec.QueuePosition(r.ID)
		}
		if r.Started != "" && r.Finished != "" {
			if a, err1 := time.Parse(time.RFC3339, r.Started); err1 == nil {
				if b, err2 := time.Parse(time.RFC3339, r.Finished); err2 == nil {
					v.DurationS = int(b.Sub(a).Seconds())
				}
			}
		}
		out = append(out, v)
	}
	return out
}

// receiptsSource is the §5 attention kind, backed by the errand store —
// LifecyclePermanent: never kept/discarded/dismissed, never expires; the
// audit trail renders as-is. Badge counts only what needs attention:
// queued + running + failed (done is history, not a demand).
type receiptsSource struct{ s *Server }

func (rc receiptsSource) Kind() string                   { return "receipt" }
func (rc receiptsSource) Lifecycle() attention.Lifecycle { return attention.LifecyclePermanent }
func (rc receiptsSource) Active(_ time.Time, q url.Values) []attention.Card {
	view := q.Get("status")
	out := []attention.Card{}
	if view == "kept" {
		return out // receipts are never "kept" — they're a trail, not verdicts
	}
	for _, v := range rc.s.receiptViews() {
		if view != "all" && v.Acknowledged {
			continue // cleared from the inbox; the ALL view keeps the full trail
		}
		out = append(out, v)
	}
	return out
}
func (rc receiptsSource) Count(time.Time) int {
	n := 0
	if rc.s.errands == nil {
		return 0
	}
	for _, r := range rc.s.errands.List() {
		if r.Acknowledged {
			continue // cleared — no longer a demand (record persists)
		}
		switch r.Status {
		case errands.StatusQueued, errands.StatusRunning, errands.StatusFailed:
			n++
		}
	}
	return n
}

// handleErrands: GET = list receipts; POST = compose one (the explicit user
// action IS the authorization).
func (s *Server) handleErrands(w http.ResponseWriter, r *http.Request) {
	if !s.errandsOK(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"errands": s.receiptViews()})
	case http.MethodPost:
		if !errands.CLIPresent() {
			http.Error(w, "aside CLI not installed", http.StatusServiceUnavailable)
			return
		}
		var b struct{ Text, Account, GoalID string }
		if err := decode(r, &b); err != nil {
			httpError(w, errBadRequest("bad errand payload"))
			return
		}
		if len(s.errandAccounts) > 0 && !contains(s.errandAccounts, b.Account) {
			httpError(w, errBadRequest("account not in errandAccounts allowlist"))
			return
		}
		rec, err := s.errandExec.Enqueue(b.Text, b.Account, "user", "", b.GoalID)
		if err != nil {
			httpError(w, errBadRequest(err.Error()))
			return
		}
		writeJSON(w, rec)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleErrandAck clears a finished receipt from the inbox (read-state only —
// the record and transcript persist; the ALL view still shows them).
func (s *Server) handleErrandAck(w http.ResponseWriter, r *http.Request) {
	if !s.errandsOK(w) {
		return
	}
	if err := s.errands.Ack(r.PathValue("id")); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleErrandInput answers a running errand's ask-gate: the text goes into
// the pty (the same wire the question streamed out of) and echoes into the
// transcript, so the exchange stays on the audit trail.
func (s *Server) handleErrandInput(w http.ResponseWriter, r *http.Request) {
	if !s.errandsOK(w) {
		return
	}
	var b struct{ Text string }
	if err := decode(r, &b); err != nil {
		httpError(w, errBadRequest("bad input payload"))
		return
	}
	if err := s.errandExec.SendInput(r.PathValue("id"), b.Text); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleErrandCancel(w http.ResponseWriter, r *http.Request) {
	if !s.errandsOK(w) {
		return
	}
	if err := s.errandExec.Cancel(r.PathValue("id")); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleErrandRetry re-enqueues a failed/cancelled errand as a NEW record
// (no session ids exist to continue — §0; the old receipt stays, audit trail).
func (s *Server) handleErrandRetry(w http.ResponseWriter, r *http.Request) {
	if !s.errandsOK(w) {
		return
	}
	old, err := s.errands.Get(r.PathValue("id"))
	if err != nil {
		httpError(w, errBadRequest("no such errand"))
		return
	}
	if old.Status != errands.StatusFailed && old.Status != errands.StatusCancelled {
		httpError(w, errBadRequest("only failed or cancelled errands retry"))
		return
	}
	rec, err := s.errandExec.Enqueue(old.Text, old.Account, old.Source, old.ApprovalID, old.GoalID)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, rec)
}

// handleErrandTranscript serves the raw transcript read-only (a dataDir file,
// never a vault note — §5).
func (s *Server) handleErrandTranscript(w http.ResponseWriter, r *http.Request) {
	if !s.errandsOK(w) {
		return
	}
	raw, err := os.ReadFile(s.errands.TranscriptPath(r.PathValue("id")))
	if err != nil {
		http.Error(w, "no transcript", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(raw)
}

// handleErrandAccounts lists aside accounts for the compose picker.
func (s *Server) handleErrandAccounts(w http.ResponseWriter, r *http.Request) {
	if !errands.CLIPresent() {
		writeJSON(w, map[string]any{"accounts": []errands.Account{}, "cli": false})
		return
	}
	accs, err := errands.Accounts()
	if err != nil {
		writeJSON(w, map[string]any{"accounts": []errands.Account{}, "cli": true, "err": err.Error()})
		return
	}
	if len(s.errandAccounts) > 0 { // §6 allowlist narrows the picker
		filtered := accs[:0]
		for _, a := range accs {
			if contains(s.errandAccounts, a.ID) {
				filtered = append(filtered, a)
			}
		}
		accs = filtered
	}
	writeJSON(w, map[string]any{"accounts": accs, "cli": true})
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if strings.TrimSpace(x) == s {
			return true
		}
	}
	return false
}
