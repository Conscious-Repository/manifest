package server

import (
	"fmt"
	"strings"
	"time"

	"manifest/gmailsync"
	"manifest/spirits"
)

type agentEmailHealth struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Href   string `json:"href,omitempty"`
}

// Owner-only schedule metadata. Never includes mail subjects or bodies.
func (s *Server) agentEmailHealth(rows []spirits.RitualRow) []agentEmailHealth {
	out := []agentEmailHealth{}
	if s.gmail != nil {
		var failures []string
		for _, a := range s.gmail.Accounts(time.Now()) {
			if a.Sync && a.NeedsReauth {
				failures = append(failures, a.Email)
			}
		}
		if len(failures) > 0 {
			why := "Reconnect Gmail for " + strings.Join(failures, ", ") + "; email sync is blocked."
			out = append(out, agentEmailHealth{"Email sync", why, "#/settings/portals"})
			for i := range rows {
				if rows[i].Spirit == "ea-coordinator" && rows[i].Ritual == "email-sync" && !rows[i].Retired {
					rows[i].Observation.Health = "failed"
					rows[i].Observation.Why = why
					rows[i].Observation.Evidence = "Gmail connection status"
				}
			}
		}
	}
	if s.oodaEmail != nil && s.oodaGmail != nil {
		accounts := s.oodaGmail.List()
		needs := 0
		for _, a := range accounts {
			if a.NeedsReauth {
				needs++
			}
		}
		pending := len(oodaPendingEmailCards(s.oodaEmail.List(gmailsync.StatusPending), true, ""))
		detail := fmt.Sprintf("%d connected mailboxes · %d conversations awaiting review in OODA Feed. Confirmation starts extraction.", len(accounts)-needs, pending)
		if needs > 0 {
			detail += fmt.Sprintf(" %d mailbox(es) need sign-in again.", needs)
		}
		out = append(out, agentEmailHealth{"OODA email", detail, ""})
	}
	return out
}
