package server

import (
	"errors"
	"manifest/agentchat"
	"net/http"
	"sync"
)

func privateShareSource(agent, id string) bool {
	return (agent == "kairos-private" || agent == "zeck-private") && agentchat.ValidID(id)
}

func (s *Server) chatShareMutex(agent, id string) *sync.RWMutex {
	value, _ := s.chatShareWriters.LoadOrStore(agent+"/"+id, &sync.RWMutex{})
	return value.(*sync.RWMutex)
}

// A source-scoped read lock spans the entire writer, including a raw terminal
// socket's lifetime. Publication takes the exclusive side without waiting for
// an open socket. Different conversations and their terminals remain independent.
func (s *Server) chatShareMutation(agent, id string) (func(), error) {
	noop := func() {}
	if !privateShareSource(agent, id) || s.agentChat == nil {
		return noop, nil
	}
	mu := s.chatShareMutex(agent, id)
	mu.RLock()
	source, _, _, ok := s.agentChat.store.Get(agent, id)
	if ok && source.Sharing != nil && source.Sharing.State != "shared" {
		mu.RUnlock()
		return noop, agentchat.ErrShared
	}
	return mu.RUnlock, nil
}

func (s *Server) terminalShareSource(se termSession) (string, string) {
	if o := se.Origin; o != nil && o.Mode == "continue" && o.Backend == "" {
		return o.Agent, o.ID
	}
	return "", ""
}
func (s *Server) guardTerminalShare(w http.ResponseWriter, se termSession) (func(), bool) {
	agent, id := s.terminalShareSource(se)
	release, err := s.chatShareMutation(agent, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return release, false
	}
	return release, true
}

// Raw terminal handles must resolve any existing conversation association;
// otherwise an owner could bypass the cutover fence by using the live pane URL.
func (s *Server) terminalForHandle(id terminalIdentity) (termSession, error) {
	rows, err := s.terminal.loadChecked()
	if err != nil {
		return termSession{}, err
	}
	selected := termSession{Backend: "herdr", Runtime: id}
	for _, se := range rows {
		a := se.Runtime
		if se.backend() != "herdr" || a.Host != id.Host || a.Session != id.Session || a.Generation != id.Generation || a.Workspace != id.Workspace || a.Pane != id.Pane || a.Occupant != id.Occupant {
			continue
		}
		if selected.ID != "" {
			return termSession{}, errors.New("terminal handle has multiple associations; resolve its session mapping first")
		}
		selected = se
	}
	return selected, nil
}
