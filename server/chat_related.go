package server

import (
	"errors"
	"manifest/agentchat"
	"net/http"
	"strings"
)

type relatedChatRequest struct {
	Agent, Model, Title, RequestID, Prompt, Task string
	Backend, Cwd, Mode                           string
	Artifacts                                    []agentchat.ArtifactReference
}

// A related conversation is a separate native source. Creation does not send,
// interrupt, reassign a task, or copy transcript turns into another history.
func (s *Server) handleChatRelated(w http.ResponseWriter, r *http.Request) {
	if !s.agentChatReady(w) {
		return
	}
	var b relatedChatRequest
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
	if b.Backend == "terminal" && (origin.Agent == "kairos" || origin.Agent == "zeck") {
		origin.Backend = "portal"
	}
	if b.Backend == "terminal" {
		s.handleRelatedCodingChat(w, r, b, origin)
		return
	}
	if b.Backend != "" || (b.Mode != "" && b.Mode != "side" && (b.Mode != "continue" || origin.Backend != "terminal")) {
		httpError(w, errBadRequest("unsupported related chat backend"))
		return
	}
	release, gateErr := s.chatShareMutation(origin.Agent, origin.ID)
	if gateErr != nil {
		http.Error(w, gateErr.Error(), http.StatusConflict)
		return
	}
	defer release()

	origin.Mode = b.Mode
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
	source, sourceBody, _, ok := s.agentChat.store.Get(origin.Agent, origin.ID)
	if origin.Backend == "terminal" {
		ok = false
		if s.terminal != nil {
			if se, found := s.terminal.find(origin.ID); found && se.Kind == origin.Agent && se.Device == "" {
				if origin.Mode == "continue" && se.Origin != nil && se.Origin.Mode == "continue" {
					httpError(w, errBadRequest("continue from the original conversation instead"))
					return
				}
				ok = true
				source = agentchat.Session{Agent: origin.Agent, ID: origin.ID, Origin: se.Origin}
				if origin.Mode == "side" {
					timeline := s.terminalRootTimeline(r.Context(), se, s.terminalPlanningChildren(se), s.terminalCodingContinuations(r.Context(), se))
					origin.Context, origin.HistoryOmitted = timelineContinuationContext(s.terminalConversation(se).Key, timeline)
					origin.Context = sideSnapshotContext(se.Origin, origin.Context)
				}
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
	if origin.Mode == "side" && origin.Backend == "" {
		origin.Context, origin.HistoryOmitted = logicalContinuationContext(source, sourceBody, s.codingContinuations(r.Context(), source))
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
	// Only the source’s persisted handoff grants inherited version access.
	var handed []artifactContextRef
	if source.Origin != nil {
		handed = source.Origin.Artifacts
	}
	if _, err = s.scopedArtifactContext(task, s.originArtifactScope(origin), b.Artifacts, handed); err != nil {
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
	if o := se.Origin; o != nil {
		if o.Backend == "terminal" {
			if parent, ok := s.terminal.find(o.ID); ok && parent.Kind == o.Agent {
				out = append(out, relatedChatView{parent.Kind, parent.ID, parent.Name, "origin", terminalConversation(parent).Route})
			}
		} else if parent, _, _, ok := s.agentChat.store.Get(o.Agent, o.ID); ok {
			out = append(out, relatedChatView{parent.Agent, parent.ID, parent.Title, "origin", sessionConversation(parent).Route})
		}
	}
	for _, child := range s.terminal.load() {
		if o := child.Origin; o != nil && o.Backend == "terminal" && o.Agent == se.Kind && o.ID == se.ID {
			out = append(out, relatedChatView{child.Kind, child.ID, child.Name, "related", terminalConversation(child).Route})
		}
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
	if s.terminal != nil {
		for _, child := range s.terminal.load() {
			if o := child.Origin; o != nil && o.Backend == "" && o.Agent == sess.Agent && o.ID == sess.ID {
				relation := "related"
				if o.Mode == "continue" {
					relation = "continuation"
				}
				out = append(out, relatedChatView{child.Kind, child.ID, child.Name, relation, terminalConversation(child).Route})
			}
		}
	}
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
