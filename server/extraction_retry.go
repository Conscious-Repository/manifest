package server

import (
	"encoding/hex"
	"net/http"
	"path/filepath"
	"strings"

	"manifest/approvals"
)

// Retry only the extraction notification of an already-approved transcript.
// Never reconfirm, rewrite the source, or replay uncertain worker jobs.
func (s *Server) handleApprovalExtractionRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := hex.DecodeString(id); err != nil || len(id) != 12 {
		http.Error(w, "invalid approval ID", http.StatusBadRequest)
		return
	}
	if s.aionSink == nil {
		http.Error(w, "extraction unavailable", http.StatusServiceUnavailable)
		return
	}
	for _, h := range s.eachHarness() {
		if h.Approvals == nil {
			continue
		}
		p, err := h.Approvals.LoadApproved(id)
		if err != nil {
			continue
		}
		if p.Type != approvals.TypeCreateVaultNote || p.ApplyPath == "" || filepath.Base(p.ApplyPath) != p.ApplyPath || !strings.HasSuffix(p.ApplyPath, ".md") {
			http.Error(w, "approved transcript required", http.StatusConflict)
			return
		}
		s.aionSink.Notify([]string{"log/" + strings.ToLower(p.ApplyPath)})
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, map[string]any{"notified": true})
		return
	}
	http.Error(w, "approved transcript not found", http.StatusNotFound)
}
