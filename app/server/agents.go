package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"manifest/hermes"
)

// Approvals (Step 5) — the record-only human-in-the-loop gate. ea-coordinator drafts
// proposals (materialized into the local store); the user confirms or rejects. The app
// never sends/executes — Confirm/Reject only move the file between status folders.

// eaProfile is the profile driving the approvals draft path (§5.5).
const eaProfile = "ea-coordinator"

// handleApprovalRun runs ea-coordinator (or a chosen propose-only profile) once with a
// free-form request and materializes its DRAFTED actions into the pending queue. This
// is the §5.5 trigger: the agent only proposes — Confirm/Reject stay record-only, and
// nothing is ever sent/executed by the app.
func (s *Server) handleApprovalRun(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil || s.hermes == nil || !s.hermes.Configured() {
		http.Error(w, "approvals/hermes not configured", http.StatusServiceUnavailable)
		return
	}
	if s.profiles == nil {
		http.Error(w, "profiles not configured", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Profile string `json:"profile"`
		Request string `json:"request"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	profileName := strings.TrimSpace(b.Profile)
	if profileName == "" {
		profileName = eaProfile
	}
	request := strings.TrimSpace(b.Request)
	if request == "" {
		http.Error(w, "request is required", http.StatusBadRequest)
		return
	}
	prof, ok := s.profiles.Get(profileName)
	if !ok {
		http.Error(w, fmt.Sprintf("profile %q not found", profileName), http.StatusNotFound)
		return
	}
	// A coordinator run (calendar/email read + drafting) is slow but finite; run it on a
	// background goroutine and let the frontend poll, same as the feed scans (runs.go).
	id := s.startRun("approvals", func(ctx context.Context) (int, error) {
		text, err := s.hermes.RunOnce(ctx, hermes.ChatRequest{
			System:   prof.Brief,
			Messages: []hermes.Message{{Role: "user", Content: request}},
		})
		if err != nil {
			return 0, err
		}
		created, err := s.approvals.Materialize(text, profileName, time.Now())
		return len(created), err
	})
	writeRunAccepted(w, id)
}

func (s *Server) handleApprovalsList(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeJSON(w, map[string]any{"pending": []any{}, "counts": map[string]int{}})
		return
	}
	writeJSON(w, map[string]any{
		"pending": s.approvals.List("pending"),
		"counts":  s.approvals.Counts(),
	})
}

func (s *Server) handleApprovalConfirm(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	if err := s.approvals.Confirm(r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleApprovalReject(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Reason string `json:"reason"`
	}
	_ = decode(r, &b) // reason is optional
	if err := s.approvals.Reject(r.PathValue("id"), b.Reason); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
