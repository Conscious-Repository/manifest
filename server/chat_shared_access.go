package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"manifest/agentchat"
	"manifest/artifacts"
	"net/http"
	"strconv"
)

var errSharedConversationAccess = errors.New("the requested resource is not available in this shared conversation")

// sharedConversationReview checks consent on both sides of the conversion.
// A thread's source link, matching task, agent name, or guessed session ID is
// never sufficient to grant a portal access to an owner's private runtime.
func (s *Server) sharedConversationReview(ag *chatAgent, threadID string) (chatShareReview, error) {
	deny := func() (chatShareReview, error) { return chatShareReview{}, errSharedConversationAccess }
	if ag == nil || ag.Store == nil || s.agentChat == nil {
		return deny()
	}
	t, exists := portalChatThread(ag, threadID)
	if !exists || t.Archived || t.SharedSource == nil || t.ImportFingerprint == "" {
		return deny()
	}
	from := t.SharedSource
	if !((ag.Name == "kairos" && ag.Domain == "aion" && from.Agent == "kairos-private") ||
		(ag.Name == "zeck" && ag.Domain == "ooda" && from.Agent == "zeck-private")) {
		return deny()
	}
	source, payload, err := s.agentChat.store.ReviewedShare(from.Agent, from.ID)
	if err != nil {
		return deny()
	}
	p := source.Sharing
	if p == nil || p.State != "shared" || p.Agent != ag.Name || p.Thread != t.ID {
		return deny()
	}
	var review chatShareReview
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber() // preserve exact numbers inside projected operation maps
	if decoder.Decode(&review) != nil {
		return deny()
	}
	if review.Session.Agent != from.Agent || review.Session.ID != from.ID || review.Session.Sharing != nil ||
		review.TargetAgent != ag.Name || !review.FutureMessages || len(review.Blockers) != 0 ||
		review.SourceRevision != p.Revision || review.SourceRevision != agentchat.ShareRevision(review.Session, review.Body) ||
		t.ImportSource != sessionConversation(review.Session).Key || t.ImportRevision != review.Revision {
		return deny()
	}
	// Check the approved review digest as well as the store's envelope digest.
	// The latter protects persisted bytes; the former binds the UI confirmation.
	revision := review.Revision
	review.Revision = ""
	b, err := json.Marshal(review)
	if err != nil || revision == "" || artifacts.Hash(b) != revision {
		return deny()
	}
	review.Revision = revision
	return review, nil
}

func (s *Server) AionChatTerminalRead(w http.ResponseWriter, r *http.Request) {
	s.sharedTerminalRead(s.kairosAgent(), w, r)
}

func (s *Server) OodaChatTerminalRead(w http.ResponseWriter, r *http.Request) {
	s.sharedTerminalRead(s.zeckAgent(), w, r)
}

// The portal authenticates first and chooses the audience through a fixed
// callback. Resolve the approved terminal before asking the runtime for data;
// there is no route here for enumerating the owner's terminal registry.
func (s *Server) sharedTerminalRead(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	se, err := s.sharedTerminal(ag, r.PathValue("thread"), r.PathValue("terminal"))
	if err != nil {
		http.Error(w, errSharedConversationAccess.Error(), http.StatusForbidden)
		return
	}
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if r.URL.Query().Get("after") != "" && (err != nil || after < 0) {
		http.Error(w, "invalid transcript offset", http.StatusBadRequest)
		return
	}
	_, transcript, observation, live := s.projectTerminalTranscript(r.Context(), se, after)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{
		"id": se.ID, "agent": se.Kind, "turns": transcript.Turns,
		"offset": transcript.Offset, "available": transcript.Available,
		"live": live, "process": observation.Process,
		"agentState": observation.AgentState, "connectivity": observation.Connectivity,
	})
}

// sharedTerminal resolves only a runtime explicitly included in the reviewed
// whole-conversation share. The caller supplies the authenticated portal's
// chatAgent, never a domain or agent accepted from request JSON.
func (s *Server) sharedTerminal(ag *chatAgent, threadID, terminalID string) (termSession, error) {
	if s.terminal == nil {
		return termSession{}, errSharedConversationAccess
	}
	review, err := s.sharedConversationReview(ag, threadID)
	if err != nil {
		return termSession{}, err
	}
	for _, included := range review.Continuations {
		if included.ID != terminalID || !isCodingAgent(included.Agent) {
			continue
		}
		se, exists, err := s.terminal.findChecked(terminalID)
		if err != nil || !exists || se.Device != "" || se.Kind != included.Agent || se.Origin == nil {
			return termSession{}, errSharedConversationAccess
		}
		o := se.Origin
		if o.Mode != "continue" || o.Backend != "" || o.Agent != review.Session.Agent || o.ID != review.Session.ID {
			return termSession{}, errSharedConversationAccess
		}
		return se, nil
	}
	return termSession{}, errSharedConversationAccess
}
