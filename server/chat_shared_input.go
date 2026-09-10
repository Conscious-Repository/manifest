package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"manifest/agentchat"
	"net/http"
)

type sharedTerminalInputKey struct{}
type sharedTerminalInputScope struct {
	Agent                         *chatAgent
	Thread, Terminal, Email, Name string
}

func (s *Server) AionChatTerminalInput(w http.ResponseWriter, r *http.Request, email, name string) {
	s.sharedTerminalInput(s.kairosAgent(), w, r, email, name)
}
func (s *Server) OodaChatTerminalInput(w http.ResponseWriter, r *http.Request, email, name string) {
	s.sharedTerminalInput(s.zeckAgent(), w, r, email, name)
}

func (s *Server) sharedTerminalInput(ag *chatAgent, w http.ResponseWriter, r *http.Request, email, name string) {
	if email == "" {
		http.Error(w, "sign in before directing a shared terminal", http.StatusUnauthorized)
		return
	}
	thread, terminal := r.PathValue("thread"), r.PathValue("terminal")
	if _, err := s.sharedTerminal(ag, thread, terminal); err != nil {
		http.Error(w, errSharedConversationAccess.Error(), http.StatusForbidden)
		return
	}
	scope := &sharedTerminalInputScope{ag, thread, terminal, email, name}
	r = r.WithContext(context.WithValue(r.Context(), sharedTerminalInputKey{}, scope))
	r.SetPathValue("id", terminal)
	s.handleTermInput(w, r)
}

func sharedInputFingerprint(b terminalInput, scope *sharedTerminalInputScope) string {
	if scope == nil {
		return b.fingerprint()
	}
	// Display-name edits do not invalidate the same person's receipt. Another
	// member cannot reuse that request ID to impersonate its original sender.
	raw, _ := json.Marshal(struct{ Input, Agent, Thread, Actor string }{b.fingerprint(), scope.Agent.Name, scope.Thread, scope.Email})
	return hashTerminalText(string(raw))
}

// Build context only from the shared thread and its approved runtime set.
// Task links and sibling sessions do not silently expand a portal's access.
func (s *Server) sharedInputContext(ctx context.Context, scope *sharedTerminalInputScope, refs []artifactContextRef, files ...string) (string, string, int, error) {
	review, err := s.sharedConversationReview(scope.Agent, scope.Thread)
	if err != nil {
		return "", "", 0, err
	}
	for _, ref := range refs {
		allowed := false
		for _, f := range review.Files {
			if f.ArtifactID == ref.ID && f.Hash == ref.Revision {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", "", 0, errSharedConversationAccess
		}
	}
	attached, err := s.retainedArtifactContext(refs)
	if err != nil {
		return "", "", 0, err
	}
	thread, ok := portalChatThread(scope.Agent, scope.Thread)
	if !ok {
		return "", "", 0, errSharedConversationAccess
	}
	views, err := s.sharedNativeViews(ctx, scope.Agent, scope.Thread, review)
	if err != nil {
		return "", "", 0, err
	}
	for _, v := range views {
		if !v.HistoryAvailable {
			return "", "", 0, fmt.Errorf("shared terminal history is unavailable; try again when it reconnects")
		}
	}
	body := portalChatBody(scope.Agent, scope.Agent.Store.Messages(thread.ID), "")
	source := agentchat.Session{Agent: scope.Agent.Name, ID: thread.ID, Created: thread.Created.Format("2006-01-02T15:04:05Z07:00")}
	key := agentConversation("portal", scope.Agent.Name, thread.ID, "team:"+scope.Agent.Domain, "").Key
	text, omitted := timelineContinuationContext(key, conversationTimeline(source, body, views))
	fileContext, _, err := s.sharedSelectedFiles(scope.Agent, scope.Thread, review, files)
	if err != nil {
		return "", "", 0, err
	}
	return text + attached + fileContext, key, omitted, nil
}

// Owner cockpit routes keep their original terminal identity after sharing.
// Resolve audience from durable consent, never from a client-supplied target.
// Caller holds the session input mutex and rereads the current registry row.
func (s *Server) ownerSharedTerminalInput(se termSession, b *terminalInput) (*sharedTerminalInputScope, error) {
	o := se.Origin
	if o != nil && o.Backend == "portal" {
		ag, _ := s.portalChatAgent(o.Agent)
		if _, err := s.sharedTerminal(ag, o.ID, se.ID); err != nil {
			return nil, err
		}
		if b.Task != "" || (b.ConversationAgent != "" || b.ConversationID != "") && (b.ConversationAgent != o.Agent || b.ConversationID != o.ID) {
			return nil, errSharedConversationAccess
		}
		b.ConversationAgent, b.ConversationID = "", ""
		email, name := s.portalChatIdentity()
		return &sharedTerminalInputScope{ag, o.ID, se.ID, email, name}, nil
	}
	if o == nil || o.Mode != "continue" || o.Backend != "" || s.agentChat == nil {
		return nil, nil
	}
	source, _, _, exists := s.agentChat.store.Get(o.Agent, o.ID)
	if !exists {
		return nil, errors.New("source conversation is unavailable; nothing sent")
	}
	p := source.Sharing
	if p == nil {
		return nil, nil
	}
	if p.State != "shared" {
		return nil, agentchat.ErrShared
	}
	ag, _ := s.portalChatAgent(p.Agent)
	if _, err := s.sharedTerminal(ag, p.Thread, se.ID); err != nil {
		return nil, err
	}
	// The original chat composer still names its source and task. Accept only
	// that exact old identity, then discard selectors rather than expanding
	// team context from a private task. Selected artifacts are authorized below.
	if (b.ConversationAgent != "" || b.ConversationID != "") &&
		!(b.ConversationAgent == o.Agent && b.ConversationID == o.ID) &&
		!(b.ConversationAgent == p.Agent && b.ConversationID == p.Thread) {
		return nil, errors.New("coding session does not continue this conversation")
	}
	if b.Task != "" && b.Task != source.Task {
		return nil, errors.New("task is not linked to the shared source conversation")
	}
	b.ConversationAgent, b.ConversationID, b.Task = "", "", ""
	email, name := s.portalChatIdentity()
	return &sharedTerminalInputScope{ag, p.Thread, se.ID, email, name}, nil
}

// A send accepted before sharing can still have a lost browser response.
// Recover only the exact owner receipt included in the approved envelope;
// this never authorizes another runtime send under the old private context.
func (s *Server) reviewedOwnerInputReceipt(scope *sharedTerminalInputScope, original terminalInput, receipt terminalInputReceipt) bool {
	if receipt.SharedAgent != "" || receipt.SharedThread != "" || receipt.Fingerprint != original.fingerprint() || receipt.State != "sent" {
		return false
	}
	review, err := s.sharedConversationReview(scope.Agent, scope.Thread)
	if err != nil {
		return false
	}
	for _, approved := range review.NativeReceipts[scope.Terminal] {
		if approved.ID == receipt.ID && approved.Fingerprint == receipt.Fingerprint && approved.State == "sent" && approved.SubmittedHash == receipt.SubmittedHash {
			return true
		}
	}
	return false
}
