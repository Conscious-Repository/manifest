package server

import (
	"bytes"
	"encoding/json"
	"errors"
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
	if !validStoredShareReview(payload) {
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
		review.SourceRevision != p.Revision ||
		t.ImportSource != sessionConversation(review.Session).Key || t.ImportRevision != review.Revision {
		return deny()
	}
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
	key := agentConversation("hermes", se.Origin.Agent, se.Origin.ID, "private", "").Key
	turns, submissions := s.projectConversationNativeTurns(se, key, transcript.Turns)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{
		"id": se.ID, "agent": se.Kind, "turns": turns, "submissions": submissions,
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

// Verify the original compact JSON field order while blanking only the
// top-level review digest. Schema additions must not revoke older grants.
func validStoredShareReview(payload []byte) bool {
	var compact bytes.Buffer
	if json.Compact(&compact, payload) != nil {
		return false
	}
	raw := compact.Bytes()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	revision := ""
	var unsigned []byte
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return false
		}
		seen[key] = true
		start := int(decoder.InputOffset())
		if start >= len(raw) || raw[start] != ':' {
			return false
		}
		start++
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return false
		}
		if key == "revision" {
			if json.Unmarshal(value, &revision) != nil || !artifacts.ValidHash(revision) {
				return false
			}
			unsigned = append(unsigned, raw[:start]...)
			unsigned = append(unsigned, '"', '"')
			unsigned = append(unsigned, raw[int(decoder.InputOffset()):]...)
		}
	}
	return revision != "" && artifacts.Hash(unsigned) == revision
}

func (s *Server) terminalSharedConversation(se termSession) *conversationDescriptor {
	o := se.Origin
	if o == nil || o.Backend != "" || o.Mode != "continue" || s.agentChat == nil {
		return nil
	}
	source, _, _, ok := s.agentChat.store.Get(o.Agent, o.ID)
	if !ok || source.Sharing == nil || source.Sharing.State != "shared" {
		return nil
	}
	p := source.Sharing
	ag, _ := s.portalChatAgent(p.Agent)
	if _, err := s.sharedTerminal(ag, p.Thread, se.ID); err != nil {
		return nil
	}
	d := agentConversation("portal", ag.Name, p.Thread, "team:"+ag.Domain, "")
	return &d
}
