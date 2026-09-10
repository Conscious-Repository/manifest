package server

import (
	"context"
	"encoding/json"
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
func (s *Server) sharedInputContext(ctx context.Context, scope *sharedTerminalInputScope, refs []artifactContextRef) (string, string, int, error) {
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
	allowed := map[string]map[string]bool{}
	for _, v := range review.Continuations {
		seen := map[string]bool{}
		for _, turn := range v.Turns {
			seen[turn.ID] = true
		}
		allowed[v.ID] = seen
	}
	views := []codingContinuationView{}
	for _, included := range review.Continuations {
		se, err := s.sharedTerminal(scope.Agent, scope.Thread, included.ID)
		if err != nil {
			return "", "", 0, err
		}
		v := s.projectCodingContinuation(ctx, se, sessionConversation(review.Session).Key)
		seen := allowed[v.ID]
		if !v.HistoryAvailable {
			return "", "", 0, fmt.Errorf("shared terminal history is unavailable; try again when it reconnects")
		}
		fresh := []termTurn{}
		for _, turn := range v.Turns {
			if !seen[turn.ID] {
				fresh = append(fresh, turn)
			}
		}
		v.Turns = fresh // historical turns are already in the imported thread
		views = append(views, v)
		delete(allowed, v.ID)
	}
	if len(allowed) != 0 {
		return "", "", 0, fmt.Errorf("a shared terminal is unavailable; restore its history before continuing")
	}
	body := portalChatBody(scope.Agent, scope.Agent.Store.Messages(thread.ID), "")
	source := agentchat.Session{Agent: scope.Agent.Name, ID: thread.ID, Created: thread.Created.Format("2006-01-02T15:04:05Z07:00")}
	key := agentConversation("portal", scope.Agent.Name, thread.ID, "team:"+scope.Agent.Domain, "").Key
	text, omitted := timelineContinuationContext(key, conversationTimeline(source, body, views))
	return text + attached, key, omitted, nil
}
