package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func terminalHandle(id terminalIdentity) string {
	b, _ := json.Marshal(id)
	return "herdr:" + base64.RawURLEncoding.EncodeToString(b)
}
func parseTerminalHandle(raw string) (terminalIdentity, error) {
	var id terminalIdentity
	if len(raw) > 4096 || !strings.HasPrefix(raw, "herdr:") {
		return id, errors.New("invalid backend-qualified terminal handle")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, "herdr:"))
	if err != nil {
		return id, err
	}
	if err = json.Unmarshal(b, &id); err != nil {
		return id, err
	}
	if id.Backend != "herdr" || id.Generation == "" || id.Occupant == "" || id.Workspace == "" || id.Pane == "" {
		return id, errors.New("incomplete terminal identity")
	}
	return id, nil
}

type terminalLiveRow struct {
	termSession
	Live   bool   `json:"live"`
	Handle string `json:"handle,omitempty"`
}

// Live inventory comes from the daemon, including unassociated panes. Durable
// conversation/run associations decorate exact identities only; labels never
// adopt a runtime into a chat or a work order. Legacy Keep rows remain visible
// while their existing processes drain; dead registry history is absent here.
func (s *Server) handleTermLive(w http.ResponseWriter, r *http.Request) {
	out := []terminalLiveRow{}
	if s.terminal == nil {
		writeJSON(w, map[string]any{"enabled": false, "sessions": out})
		return
	}
	rows := s.terminal.load()
	connectivity := "unavailable"
	if h := s.terminal.herdr; h != nil {
		if live, err := h.List(r.Context()); err == nil {
			connectivity = "connected"
			// Shell-only associations have no conversation history. Retire only after
			// confirmed absence in the same daemon generation; preserve legacy/remote rows.
			generation, _ := h.generation()
			present := map[string]bool{}
			for _, ob := range live {
				present[ob.Identity.Pane] = true
			}
			for _, se := range rows {
				if se.backend() == "herdr" && se.Kind == "shell" && se.Device == "" && se.BoardBrief == "" && se.LaunchPhase == "active" && se.Runtime.Generation == generation && !present[se.Runtime.Pane] {
					s.terminal.remove(se.ID)
				}
			}
			for _, ob := range live {
				se := termSession{ID: "live:" + ob.Identity.Occupant, Backend: "herdr", Kind: ob.Kind, Cwd: ob.Cwd, Name: ob.Label, Runtime: ob.Identity}
				if se.Kind == "" {
					se.Kind = "shell"
				}
				if se.Name == "" {
					se.Name = se.Kind
				}
				for _, mapped := range rows {
					if mapped.backend() == "herdr" && mapped.Runtime.Host == ob.Identity.Host && mapped.Runtime.Session == ob.Identity.Session && mapped.Runtime.Generation == ob.Identity.Generation && mapped.Runtime.Pane == ob.Identity.Pane && mapped.Runtime.Occupant == ob.Identity.Occupant && (mapped.Runtime.AgentSession == "" || mapped.Runtime.AgentSession == ob.Identity.AgentSession) {
						se = mapped
						break
					}
				}
				out = append(out, terminalLiveRow{termSession: se, Live: true, Handle: terminalHandle(ob.Identity)})
			}
		}
	}
	legacy := s.terminal.liveSet()
	for _, se := range rows {
		if se.backend() != "tmux" {
			continue
		}
		live := legacy[tmuxName(se.ID)]
		if !live && se.Keep && se.Device != "" {
			live = s.remoteKeepLive(se)
		}
		if live {
			out = append(out, terminalLiveRow{termSession: se, Live: true})
		}
	}
	writeJSON(w, map[string]any{"enabled": true, "sessions": out, "connectivity": connectivity})
}
func (s *Server) handleTermLiveClose(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); o != "" && !sameOrigin(o, r.Host) {
		http.Error(w, "cross-origin refused", http.StatusForbidden)
		return
	}
	if s.terminal == nil || s.terminal.herdr == nil {
		http.Error(w, "herdr unavailable", 503)
		return
	}
	var b struct {
		Handle string `json:"handle"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	id, err := parseTerminalHandle(b.Handle)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	se, err := s.terminalForHandle(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	release, allowed := s.guardTerminalShare(w, se)
	if !allowed {
		return
	}
	defer release()

	if err = s.terminal.herdr.Close(r.Context(), id); err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func agentSessionHandle(se termSession, tmux string) string {
	if se.backend() == "herdr" {
		return terminalHandle(se.Runtime)
	}
	return "tmux:" + tmux
}

// Old agent-session callers retain the original tmux field by default. New
// clients explicitly select herdr and use its qualified handle; no tmux name
// is fabricated for a herdr pane. Keep this compatibility lane until drained.
func (s *Server) createAgentWithBackend(ctx context.Context, kind, cwd, name, model, backend string) (termSession, string, error) {
	if backend == "" || backend == "tmux" {
		return s.createAgentTermSession(kind, cwd, name)
	}
	if backend != "herdr" {
		return termSession{}, "", errors.New("backend must be tmux or herdr")
	}
	if kind != "shell" && !isCodingAgent(kind) {
		return termSession{}, "", errors.New("unsupported agent kind")
	}
	var bits [16]byte
	if _, err := rand.Read(bits[:]); err != nil {
		return termSession{}, "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	se := termSession{ID: hex.EncodeToString(bits[:8]), Kind: kind, Cwd: cwd, Name: name, CreatedAt: now, LastUsed: now}
	if se.Name == "" {
		se.Name = kind
	}
	if isCodingAgent(kind) {
		se.Model, _ = codingModel(kind, model)
	}
	if kind == "claude" {
		se.ResumeID = fmt.Sprintf("%x-%x-%x-%x-%x", bits[:4], bits[4:6], bits[6:8], bits[8:10], bits[10:])
	}
	se, err := s.launchHerdr(ctx, se)
	return se, "", err
}
