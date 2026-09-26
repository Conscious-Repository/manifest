package server

import (
	"net/http"
	"sort"
	"strings"

	"manifest/agentchat"
)

// ---- which store owns a conversation id (2026-09-26) ----
//
// A bare #/chat/<id> is the spirit route's shape, but ids reach it from
// elsewhere too (a pasted link, an old bookmark, the owner's integrator typing
// an Alfred id). The spirit backend then answered 404 and the stage told the
// owner a live Alfred conversation had been "deleted or archived". The loader
// now asks here instead of guessing: every store is asked directly, by its own
// lookup, and the answer names the owners, the stores that could not be asked,
// and the ones that were asked and said no — so the not-found copy is only
// ever used when no store has the id.

type chatOwner struct {
	Backend string `json:"backend"` // spirit | hermes | portal | terminal
	Agent   string `json:"agent"`   // "" for spirits
	Route   string `json:"route"`
}

type chatResolution struct {
	ID          string      `json:"id"`
	Owners      []chatOwner `json:"owners"`
	Checked     []string    `json:"checked"`     // stores asked that answered "not here"
	Unavailable []string    `json:"unavailable"` // stores that could not be asked
}

func (s *Server) resolveChatID(id string) chatResolution {
	out := chatResolution{ID: id, Owners: []chatOwner{}, Checked: []string{}, Unavailable: []string{}}
	miss := func(store string) { out.Checked = append(out.Checked, store) }
	if s.spirits == nil {
		out.Unavailable = append(out.Unavailable, "spirits")
	} else if sum, _, ok := s.spirits.ChatSession(id); ok {
		out.Owners = append(out.Owners, chatOwner{"spirit", "", "#/chat/" + sum.ID})
	} else {
		miss("spirits")
	}
	if s.agentChat == nil {
		out.Unavailable = append(out.Unavailable, "agents")
	} else if agentchat.ValidID(id) {
		// every agent the store holds files for, not only the ones the
		// roster lists today: a profile Hermes no longer reports still owns
		// its threads on disk
		agents := s.agentChat.store.Agents()
		if !containsString(agents, alfredAgent) {
			agents = append(agents, alfredAgent)
		}
		sort.Strings(agents)
		for _, agent := range agents {
			if sess, _, _, ok := s.agentChat.store.Get(agent, id); ok {
				out.Owners = append(out.Owners, chatOwner{"hermes", agent, sessionConversation(sess).Route})
			} else {
				miss(agent)
			}
		}
	} else {
		miss("agents")
	}
	for _, ag := range s.chatAgents() {
		if t, ok := portalChatThread(ag, id); ok {
			out.Owners = append(out.Owners, chatOwner{"portal", ag.Name, agentConversation("portal", ag.Name, t.ID, "team:"+ag.Domain, "").Route})
		} else {
			miss(ag.Name)
		}
	}
	if s.terminal == nil {
		out.Unavailable = append(out.Unavailable, "terminal")
	} else if se, ok := s.terminal.find(id); ok {
		out.Owners = append(out.Owners, chatOwner{"terminal", se.Kind, terminalConversation(se).Route})
	} else {
		miss("terminal")
	}
	return out
}

// GET /api/chat/resolve?id=<id> — the owners of a conversation id. Always
// 200: "no owner" is an answer, and the client words it from checked/unavailable.
func (s *Server) handleChatResolve(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || len(id) > 200 {
		httpError(w, errBadRequest("id required"))
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, s.resolveChatID(id))
}
