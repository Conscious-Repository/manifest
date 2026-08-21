package server

// The OODA portal's ARCHIVE read (ooda-portal email plan, phase 4): the log
// of what the team has captured and decided — shared artifacts (email notes +
// chat uploads, artifacts.List("ooda")), confirmed email candidates, and the
// extraction outcomes (decided re-* proposals whose evidence names an ooda
// artifact). Flat by doctrine: everything here is CONFIRMED, so every member
// gets the same bytes. Blob fetch rides the existing Owns-gated
// GET /api/chat/attach/{hash} — no new byte-serving route.

import (
	"net/http"
	"strings"
)

func (a *oodaAPI) archive(w http.ResponseWriter, r *http.Request) {
	if _, ok := PortalIdentify(a.opt, w, r); !ok {
		return
	}
	s := a.live.s
	out := map[string]any{}
	if s.artifacts != nil {
		out["artifacts"] = s.artifacts.List("ooda")
	}
	if s.oodaEmail != nil {
		out["emailNotes"] = s.oodaEmail.List("confirmed")
	}
	// extraction outcomes: decided proposals sourced from an ooda artifact.
	// Approved AND rejected both belong in the log — the archive is history,
	// not a queue.
	type outcome struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Action string `json:"action"`
		Status string `json:"status"`
		Agent  string `json:"agent"`
		Source string `json:"source,omitempty"` // the artifact hash
	}
	var outcomes []outcome
	for _, h := range s.eachHarness() {
		if h.Approvals == nil {
			continue
		}
		for _, status := range []string{"approved", "rejected"} {
			for _, p := range h.Approvals.List(status) {
				if !oodaFeedType(p.Type) {
					continue
				}
				hash := oodaArtifactHashIn(p.Body)
				if hash == "" || s.artifacts == nil || !s.artifacts.Owns("ooda", hash) {
					continue
				}
				outcomes = append(outcomes, outcome{
					ID: p.ID, Type: p.Type, Action: p.Action,
					Status: status, Agent: p.Agent, Source: hash,
				})
			}
		}
	}
	out["outcomes"] = outcomes
	writeJSON(w, out)
}

// oodaArtifactHashIn pulls the first sha256:<64 hex> reference out of a
// proposal body (the evidence line the ooda-email ritual writes).
func oodaArtifactHashIn(body string) string {
	i := strings.Index(body, "sha256:")
	if i < 0 {
		return ""
	}
	hash := body[i+len("sha256:"):]
	end := 0
	for end < len(hash) && end < 64 {
		c := hash[end]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			break
		}
		end++
	}
	if end != 64 {
		return ""
	}
	return hash[:64]
}
