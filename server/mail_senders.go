package server

import (
	"fmt"
	"manifest/gmailsend"
	"net/http"
	"strings"
)

func (s *Server) UseOodaMailSend(c *gmailsend.Client) { s.oodaMailSend = c }

// Domain is the owner's correspondence context, never guessed from a recipient.
// The legacy recruiting client is deliberately not a global default.
func (s *Server) mailSender(domain string) (*gmailsend.Client, error) {
	var c *gmailsend.Client
	var expected string
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "recruiting", "aion", "aion.bio":
		c = s.gmailSend
		expected = "ben@aion.bio"
	case "ooda", "ooda.group":
		c = s.oodaMailSend
		expected = "ben@ooda.group"
	default:
		return nil, fmt.Errorf("no sender mapped for this correspondence domain; select a configured domain (personal Outlook sending is not connected)")
	}
	if c == nil {
		return nil, fmt.Errorf("sender %s is not configured", expected)
	}
	if c.Sender() != expected {
		return nil, fmt.Errorf("sender mapping mismatch: this domain requires %s", expected)
	}
	return c, nil
}

func (s *Server) settingsMailSender(w http.ResponseWriter, r *http.Request) (*gmailsend.Client, bool) {
	domain := r.PathValue("domain")
	if domain == "" {
		domain = "recruiting"
	} // Existing settings URL is recruiting-only.
	c, err := s.mailSender(domain)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, false
	}
	return c, true
}
func (s *Server) domainMailConnectionRow(domain string) panelRow {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || domain == "recruiting" || domain == "aion" || domain == "aion.bio" {
		return s.gmailSendConnectionRow()
	}
	row := panelRow{ID: "gmail-send-ooda", Name: "Gmail (send, OODA)", Kind: "gmailsend", Masked: "ben@ooda.group"}
	c, err := s.mailSender(domain)
	if err != nil {
		row.State = "sealed"
		row.Err = err.Error()
		return row
	}
	st := c.Status()
	row.State = "sealed"
	row.Note = "Connect ben@ooda.group for OODA correspondence. Approval is required before sending."
	if st.Email != "" {
		row.Accounts = []string{st.Email}
	}
	if st.SendCapable {
		row.State = "open"
		row.Note = "OODA sender connected; each send requires approval."
	} else if st.Configured {
		row.State = "degraded"
		row.Err = st.Detail
	}
	row.Extra = map[string]any{"sender": st.Sender, "sendCapable": st.SendCapable, "hasCreds": st.HasCreds, "connectionBase": "/api/settings/gmail-send/ooda"}
	return row
}
func (s *Server) handleDomainMailStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.settingsMailSender(w, r); !ok {
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, s.domainMailConnectionRow(r.PathValue("domain")))
}
