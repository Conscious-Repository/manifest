package server

import "net/http"

func (s *Server) handleFundraisingInvitesGet(w http.ResponseWriter, _ *http.Request) {
	if s.fundraisingInvites == nil {
		writeJSON(w, map[string]any{"enabled": false, "emails": []string{}})
		return
	}
	writeJSON(w, map[string]any{"enabled": true, "emails": s.fundraisingInvites.List()})
}

func (s *Server) handleFundraisingInvitesPut(w http.ResponseWriter, r *http.Request) {
	if s.fundraisingInvites == nil {
		http.Error(w, "fundraising portal is disabled", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Emails []string `json:"emails"`
	}
	if err := decode(r, &body); err != nil {
		httpError(w, err)
		return
	}
	if err := s.fundraisingInvites.Replace(body.Emails); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"enabled": true, "emails": s.fundraisingInvites.List()})
}
