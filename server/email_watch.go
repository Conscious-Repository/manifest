package server

import (
	"context"
	"manifest/gmailsync"
	"net/http"
	"time"
)

func (s *Server) pollEmailReplies() {
	if s.gmail == nil || s.manifestOperations == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	_ = s.manifestOperations.PollEmailReplies(ctx, time.Now().UTC(), func(ctx context.Context, sender, thread string) ([]gmailsync.Msg, error) {
		source, err := s.gmail.ReadSource(ctx, sender)
		if err != nil {
			return nil, err
		}
		_, messages, err := gmailsync.NewMailboxClient(source, sender).ThreadFull(ctx, thread)
		return messages, err
	})
}
func (s *Server) handleEmailWatch(w http.ResponseWriter, r *http.Request) {
	if s.manifestOperations == nil {
		http.Error(w, "Email tracking unavailable", 503)
		return
	}
	var b struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	result, err := s.manifestOperations.SetEmailWatch(r.PathValue("id"), b.Enabled)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, result)
}
