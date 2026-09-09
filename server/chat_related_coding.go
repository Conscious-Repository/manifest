package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"manifest/agentchat"
	"net/http"
	"strings"
	"time"
)

// Related coding chats retain native transcript ownership. The registry stores
// only reviewed origin/context and an immutable creation signature, never turns.
func (s *Server) handleRelatedCodingChat(w http.ResponseWriter, r *http.Request, b relatedChatRequest, origin agentchat.Origin) {
	if s.terminal == nil {
		http.Error(w, "terminal disabled", http.StatusServiceUnavailable)
		return
	}
	validSource := origin.Backend == "" && agentchat.ValidAgent(origin.Agent) && agentchat.ValidID(origin.ID)
	validSource = validSource || origin.Backend == "terminal" && isCodingAgent(origin.Agent) && termIDRe.MatchString(origin.ID)
	if !validSource || !isCodingAgent(b.Agent) || !agentchat.ValidRequestID(b.RequestID) {
		httpError(w, errBadRequest("invalid related coding chat request"))
		return
	}
	if b.Mode != "" && (b.Mode != "continue" || origin.Backend != "") {
		httpError(w, errBadRequest("coding continuation requires a private planning conversation"))
		return
	}
	origin.Mode = b.Mode
	raw, _ := json.Marshal(struct {
		Agent, Title, Model, Cwd string
		Origin                   agentchat.Origin
	}{b.Agent, b.Title, b.Model, b.Cwd, origin})
	sum := sha256.Sum256(raw)
	signature := hex.EncodeToString(sum[:])
	reply := func(se termSession) {
		writeJSON(w, map[string]any{"id": se.ID, "agent": se.Kind, "model": se.Model, "cwd": se.Cwd, "conversation": s.terminalConversation(se)})
	}
	fail := func(err error) {
		if errors.Is(err, agentchat.ErrRequestConflict) {
			http.Error(w, err.Error(), http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
	// Recover before consulting mutable source/task links, model policy or cwd.
	if found, ok, err := s.terminal.relatedCodingOnce(b.Agent, b.RequestID, signature, nil); err != nil {
		fail(err)
		return
	} else if ok {
		reply(found)
		return
	}
	source, sourceBody, _, ok := s.agentChat.store.Get(origin.Agent, origin.ID)
	if origin.Backend == "terminal" {
		ok = false
		if parent, found := s.terminal.find(origin.ID); found && parent.Kind == origin.Agent && parent.Device == "" {
			ok = true
			source = agentchat.Session{Agent: origin.Agent, ID: origin.ID}
			for _, link := range s.terminalConversation(parent).Links {
				if link.Kind == "task" {
					source.Task = link.ID
				}
			}
		}
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	if origin.Mode == "continue" {
		origin.Context, origin.HistoryOmitted = codingContinuationContext(source, sourceBody)
	}
	if origin.Task != "" && origin.Task != source.Task {
		link := s.taskChatLink(origin.Task, s.listThread(origin.Task), "")
		if origin.Backend == "terminal" || link == nil || link.Agent != source.Agent || link.ID != source.ID {
			httpError(w, errBadRequest("task is not linked to the source conversation"))
			return
		}
	}
	if len(origin.Artifacts) > 0 && origin.Task == "" {
		httpError(w, errBadRequest("a task is required for artifact context"))
		return
	}
	if len(origin.Artifacts) > 1 {
		httpError(w, errBadRequest("the coding handoff supports one selected artifact version at a time"))
		return
	}
	if _, err := s.taskArtifactContext(origin.Task, origin.Artifacts); err != nil {
		httpError(w, err)
		return
	}
	model, note := codingModel(b.Agent, strings.TrimSpace(b.Model))
	if note != "" {
		httpError(w, errBadRequest("unsupported coding model; choose "+strings.Join(codingModels[b.Agent].allowed, ", ")))
		return
	}
	cwd, err := resolveTerminalCwd(b.Cwd, s.terminal.defaultWd)
	if err != nil {
		http.Error(w, err.Error(), terminalLaunchStatus(err))
		return
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		fail(err)
		return
	}
	now := time.Now().Format(time.RFC3339)
	se := termSession{Version: terminalRowVersion, Backend: "herdr", LaunchPhase: "draft", ID: hex.EncodeToString(id), Kind: b.Agent,
		Cwd: cwd, Name: b.Title, Model: model, CreatedAt: now, LastUsed: now, Origin: &origin, CreateRequest: b.RequestID, CreateSignature: signature}
	if strings.TrimSpace(se.Name) == "" {
		se.Name = "Related: " + source.Title
	}
	if se.Kind == "claude" {
		u := make([]byte, 16)
		if _, err := rand.Read(u); err != nil {
			fail(err)
			return
		}
		se.ResumeID = fmt.Sprintf("%x-%x-%x-%x-%x", u[:4], u[4:6], u[6:8], u[8:10], u[10:])
	}
	// Atomic check+create also handles concurrent retries after both validate.
	created, _, err := s.terminal.relatedCodingOnce(b.Agent, b.RequestID, signature, &se)
	if err != nil {
		fail(err)
		return
	}
	reply(created)
}

func (c *termCfg) relatedCodingOnce(kind, request, signature string, create *termSession) (termSession, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.loadChecked()
	if err != nil {
		return termSession{}, false, err
	}
	for _, row := range rows {
		if row.Kind == kind && row.CreateRequest == request {
			if row.CreateSignature != signature {
				return termSession{}, true, agentchat.ErrRequestConflict
			}
			return row, true, nil
		}
	}
	if create == nil {
		return termSession{}, false, nil
	}
	if err := c.writeRowsLocked(append(rows, *create)); err != nil {
		return termSession{}, false, err
	}
	return *create, true, nil
}
