package server

import (
	"context"
	"manifest/gmailsync"
	"manifest/manifestmcp"
	"manifest/portals"
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
		Enabled        bool  `json:"enabled"`
		StopAfterReply *bool `json:"stopAfterReply"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	result, err := s.manifestOperations.ConfigureEmailWatch(r.PathValue("id"), b.Enabled, b.StopAfterReply)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, result)
}

func (s *Server) emailReplyCards(now time.Time) []portals.Card {
	cards := []portals.Card{}
	if s.manifestOperations == nil {
		return cards
	}
	notices, err := s.manifestOperations.EmailNotices(now)
	if err != nil {
		return cards
	}
	for _, n := range notices {
		cards = append(cards, portals.Card{ID: n.ID, Type: "portal-item", Portal: "email", Title: "Reply · " + n.Subject, Detail: "Received by " + n.Sender, Actor: n.From, Date: n.At.Format(time.RFC3339), Change: "new", OperationID: n.OperationID})
	}
	return cards
}

// This receipt read never passes through syncManifestOperations (which may
// execute already approved actions). It is restricted to confirmed sent mail.
func (s *Server) handleEmailReceipt(w http.ResponseWriter, r *http.Request) {
	if s.manifestOperations == nil {
		http.Error(w, "Email receipts unavailable", 503)
		return
	}
	out, err := s.manifestOperations.Operation(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Email receipt unavailable", 404)
		return
	}
	o := out["record"].(*manifestmcp.OperationRecord)
	if o.Tool != "email.prepare" || o.Status != "succeeded" {
		http.Error(w, "Confirmed sent email required", 409)
		return
	}
	writeJSON(w, map[string]any{"record": o, "proposal": map[string]string{"id": manifestmcp.ProposalID(o.ID), "action": "email"}})
}
