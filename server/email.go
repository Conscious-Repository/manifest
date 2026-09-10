package server

import (
	"context"
	"manifest/gmailsend"
	"manifest/manifestmcp"
	"net/http"
	"time"
)

func (s *Server) UseMailSenders(r *gmailsend.Registry) {
	s.mailSenders = r
	if r != nil {
		s.UseGmailSend(r.Aion)
	}
	if s.manifestOperations != nil {
		s.manifestOperations.Mail = r
	}
}
func (s *Server) mailClient(w http.ResponseWriter, r *http.Request) *gmailsend.Client {
	domain := r.PathValue("domain")
	if domain != "aion" && domain != "ooda" {
		http.Error(w, "no sender mapped for this domain", 400)
		return nil
	}
	c, err := s.mailSenders.Resolve(domain, nil)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return nil
	}
	return c
}
func (s *Server) mailConnectionRow(domain string) panelRow {
	row := panelRow{ID: "mail-" + domain, Name: "Email sending · " + domain, Kind: "gmailsend", State: "sealed"}
	c, err := s.mailSenders.Resolve(domain, nil)
	if err != nil {
		row.Err = err.Error()
		return row
	}
	st := c.Status()
	row.Masked = c.Sender()
	row.Note = st.Detail
	if st.SendCapable {
		row.State = "open"
		row.Note = "Sends as " + c.Sender() + " after approval"
	} else if st.Configured {
		row.State = "degraded"
		row.Err = st.Detail
	}
	if st.Email != "" {
		row.Accounts = []string{st.Email}
	}
	row.Extra = map[string]any{"sender": c.Sender(), "hasCreds": st.HasCreds, "sendCapable": st.SendCapable, "base": "/api/settings/mail/" + domain}
	return row
}
func (s *Server) handleMailConnectStart(w http.ResponseWriter, r *http.Request) {
	c := s.mailClient(w, r)
	if c == nil {
		return
	}
	url, err := c.StartConnect()
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]any{"authUrl": url, "sender": c.Sender()})
}
func (s *Server) handleMailConnectFinish(w http.ResponseWriter, r *http.Request) {
	c := s.mailClient(w, r)
	if c == nil {
		return
	}
	var b struct {
		Redirect string `json:"redirect"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if _, err := c.FinishConnect(ctx, b.Redirect); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, s.mailConnectionRow(r.PathValue("domain")))
}
func (s *Server) handleMailDisconnect(w http.ResponseWriter, r *http.Request) {
	c := s.mailClient(w, r)
	if c == nil {
		return
	}
	if err := c.Disconnect(); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.mailConnectionRow(r.PathValue("domain")))
}

// Owner listener only. This prepares a proposal; it never decides or sends.
func (s *Server) handleEmailPrepare(w http.ResponseWriter, r *http.Request) {
	if s.manifestOperations == nil {
		http.Error(w, "email approvals unavailable", 503)
		return
	}
	var q manifestmcp.EmailInput
	if err := decode(r, &q); err != nil {
		httpError(w, err)
		return
	}
	out, err := s.manifestOperations.PrepareEmail(q)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, out)
}
