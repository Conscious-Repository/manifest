package server

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"manifest/portals"
	"manifest/transcriptsync"
)

func (s *Server) UseTranscriptSync(svc *transcriptsync.Service) { s.transcriptSync = svc }
func (s *Server) transcriptPortalRow(source string) panelRow {
	id, name := "granola", "Granola"
	if source == "pocket" {
		id, name = heypocketID, "HeyPocket"
	}
	row := panelRow{ID: id, Name: name, Kind: "apikey", State: "sealed", Note: "Manifest sync · pending notes require confirmation", Fields: []portals.CredField{{Key: "apiKey", Label: "API key", Secret: true}}}
	if s.transcriptSync.HasKey(source) {
		row.State = "open"
		row.Masked = "configured"
	}
	if os.Getenv(strings.ToUpper(source)+"_API_KEY") != "" {
		row.Env = strings.ToUpper(source) + "_API_KEY"
	}
	st, e := s.transcriptSync.Status(source)
	if e != nil {
		row.State = "degraded"
		row.Err = e.Error()
		return row
	}
	if !st.LastSuccess.IsZero() {
		row.LastCrossing = st.LastSuccess.Format(time.RFC3339)
	}
	if st.Error != "" {
		row.State = "degraded"
		row.Err = st.Error
	}
	row.Extra = map[string]any{"owner": "manifest", "lastAttempt": st.LastAttempt, "fetched": st.Fetched, "filed": st.Filed, "skipped": st.Skipped, "waiting": st.Waiting}
	return row
}
func (s *Server) handleTranscriptPortal(w http.ResponseWriter, r *http.Request, action string) bool {
	source := r.PathValue("id")
	if source == heypocketID {
		source = "pocket"
	}
	if s.transcriptSync == nil || !s.transcriptSync.Enabled(source) {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	var err error
	switch action {
	case "key":
		var body struct {
			Fields map[string]string `json:"fields"`
		}
		if err = decode(r, &body); err == nil {
			err = s.transcriptSync.SetKey(source, body.Fields["apiKey"])
		}
	case "test":
		err = s.transcriptSync.Test(ctx, source)
	case "poll":
		_, err = s.transcriptSync.Poll(ctx, source)
	case "disconnect":
		err = s.transcriptSync.Disconnect(source)
	}
	if err != nil {
		httpError(w, err)
		return true
	}
	writeJSON(w, s.transcriptPortalRow(source))
	return true
}
