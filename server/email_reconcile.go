package server

import (
	"context"
	"fmt"
	"manifest/gmailsend"
	"manifest/gmailsync"
	"net/http"
)

func (s *Server) handleEmailReconcile(w http.ResponseWriter, r *http.Request) {
	if s.manifestOperations == nil {
		http.Error(w, "Email recovery unavailable", 503)
		return
	}
	// All identity, approval and evidence comes from stored records and this
	// reader. The HTTP request supplies only the operation ID in the route.
	result, err := s.manifestOperations.ReconcileEmail(r.Context(), r.PathValue("id"), func(ctx context.Context, sender, id string) (gmailsend.SentProof, error) {
		if s.gmail == nil {
			return gmailsend.SentProof{}, fmt.Errorf("mailbox unavailable")
		}
		source, err := s.gmail.ReadSource(ctx, sender)
		if err != nil {
			return gmailsend.SentProof{}, err
		}
		evidence, err := gmailsync.NewMailboxClient(source, sender).SentMessageEvidence(ctx, id)
		if err != nil {
			return gmailsend.SentProof{}, err
		}
		return gmailsend.SentProof{Mailbox: evidence.Mailbox, Ref: gmailsend.Ref{ID: evidence.ID, ThreadID: evidence.ThreadID}, Raw: evidence.Raw}, nil
	})
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeJSON(w, result)
}
