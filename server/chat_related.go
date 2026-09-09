package server

import (
	"errors"
	"manifest/agentchat"
	"net/http"
	"strings"
)

// A related conversation is a separate native source. Creation does not send,
// interrupt, reassign a task, or copy transcript turns into another history.
func (s *Server) handleChatRelated(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	var b struct {
		Agent, Model, Title, RequestID, Prompt, Task string
		Artifacts                                    []agentchat.ArtifactReference
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if len(b.Prompt) > agentChatMaxChars || len(b.Title) > 240 {
		httpError(w, errBadRequest("handoff is too long"))
		return
	}
	task := strings.TrimSpace(b.Task)
	origin := agentchat.Origin{Agent: r.PathValue("agent"), ID: r.PathValue("id"), Task: task, Prompt: b.Prompt, Artifacts: b.Artifacts}
	origin.Backend = r.PathValue("originBackend")
	accepted, found, recoverErr := s.agentChat.store.RecoverRelatedCreation(b.Agent, b.Title, b.Model, b.RequestID, origin)
	if recoverErr != nil {
		if errors.Is(recoverErr, agentchat.ErrRequestConflict) {
			http.Error(w, recoverErr.Error(), http.StatusConflict)
		} else {
			httpError(w, errBadRequest(recoverErr.Error()))
		}
		return
	}
	if found {
		writeJSON(w, map[string]any{"id": accepted.ID, "agent": accepted.Agent, "conversation": sessionConversation(accepted)})
		return
	}
	source, _, _, ok := s.agentChat.store.Get(origin.Agent, origin.ID)
	if origin.Backend == "terminal" {
		ok = false
		if s.terminal != nil {
			if se, found := s.terminal.find(origin.ID); found && se.Kind == origin.Agent && se.Device == "" {
				ok = true
				source = agentchat.Session{Agent: origin.Agent, ID: origin.ID}
				for _, link := range s.terminalConversation(se).Links {
					if link.Kind == "task" {
						source.Task = link.ID
					}
				}
			}
		}
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	profile, err := s.resolveAgentChat(r.Context(), b.Agent)
	if err != nil {
		httpError(w, err)
		return
	}
	if task != "" && task != source.Task {
		if origin.Backend == "terminal" {
			httpError(w, errBadRequest("task is not linked to this coding session"))
			return
		}
		link := s.taskChatLink(task, s.listThread(task), "")
		if link == nil || link.Agent != source.Agent || link.ID != source.ID {
			httpError(w, errBadRequest("task is not linked to the source conversation"))
			return
		}
	}
	if len(b.Artifacts) > 0 && task == "" {
		httpError(w, errBadRequest("a task is required for artifact context"))
		return
	}
	if _, err = s.taskArtifactContext(task, b.Artifacts); err != nil {
		httpError(w, err)
		return
	}
	id, err := s.agentChat.store.CreateRelatedOnce(b.Agent, profile, b.Title, b.Model, b.RequestID, origin)
	if errors.Is(err, agentchat.ErrRequestConflict) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]any{"id": id, "agent": b.Agent, "conversation": agentConversation("hermes", b.Agent, id, "private", task)})
}

func (s *Server) handleTerminalChatRelated(w http.ResponseWriter, r *http.Request) {
	r.SetPathValue("originBackend", "terminal")
	s.handleChatRelated(w, r)
}

type relatedChatView struct {
	Agent    string `json:"agent"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Relation string `json:"relation"`
	Route    string `json:"route"`
}

func (s *Server) terminalRelatedChats(se termSession) []relatedChatView {
	out := []relatedChatView{}
	if s.agentChat == nil {
		return out
	}
	for _, agent := range s.agentChat.store.Agents() {
		for _, candidate := range s.agentChat.store.List(agent) {
			if o := candidate.Origin; o != nil && o.Backend == "terminal" && o.Agent == se.Kind && o.ID == se.ID {
				out = append(out, relatedChatView{agent, candidate.ID, candidate.Title, "related", sessionConversation(candidate).Route})
			}
		}
	}
	return out
}

func (s *Server) relatedChats(sess agentchat.Session) []relatedChatView {
	out := []relatedChatView{}
	add := func(agent, id, relation string) {
		target, _, _, ok := s.agentChat.store.Get(agent, id)
		if !ok {
			return
		}
		out = append(out, relatedChatView{agent, id, target.Title, relation, agentConversation("hermes", agent, id, "private", target.Task).Route})
	}
	if sess.Origin != nil {
		if sess.Origin.Backend == "terminal" {
			if s.terminal != nil {
				if se, ok := s.terminal.find(sess.Origin.ID); ok && se.Kind == sess.Origin.Agent {
					out = append(out, relatedChatView{se.Kind, se.ID, se.Name, "origin", terminalConversation(se).Route})
				}
			}
		} else {
			add(sess.Origin.Agent, sess.Origin.ID, "origin")
		}
	}
	for _, agent := range s.agentChat.store.Agents() {
		for _, candidate := range s.agentChat.store.List(agent) {
			if candidate.Origin != nil && candidate.Origin.Backend == "" && candidate.Origin.Agent == sess.Agent && candidate.Origin.ID == sess.ID {
				add(agent, candidate.ID, "related")
			}
		}
	}
	return out
}

func sessionConversation(sess agentchat.Session) conversationDescriptor {
	d := agentConversation("hermes", sess.Agent, sess.ID, "private", sess.Task)
	if sess.Origin != nil {
		origin := agentConversation("hermes", sess.Origin.Agent, sess.Origin.ID, "private", sess.Origin.Task)
		if sess.Origin.Backend == "terminal" {
			origin = terminalConversation(termSession{ID: sess.Origin.ID, Kind: sess.Origin.Agent})
		}
		d.Links = append(d.Links, conversationLink{Kind: "related-conversation", ID: origin.Key, Evidence: "session.origin"})
	}
	return d
}
