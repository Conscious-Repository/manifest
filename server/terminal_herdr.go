package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// herdrTerminalRuntime is pinned to the locally proven 0.9.0/protocol 22.
// The daemon must be independently supervised. Never start a replacement daemon
// or replay an uncertain request when its socket disappears.
type herdrTerminalRuntime struct {
	Socket, Session, Host string
	server                *Server
}

var _ terminalRuntime = (*herdrTerminalRuntime)(nil)

type herdrAgentSession struct {
	Agent string `json:"agent"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func (a *herdrAgentSession) identity() string {
	if a == nil || a.Agent == "" || a.Kind == "" || a.Value == "" {
		return ""
	}
	return a.Agent + ":" + a.Kind + ":" + a.Value
}

type herdrPane struct {
	Cwd          string             `json:"cwd"`
	Label        string             `json:"label"`
	Title        string             `json:"title"`
	Revision     uint64             `json:"revision"`
	AgentSession *herdrAgentSession `json:"agent_session"`
	Pane         string             `json:"pane_id"`
	Workspace    string             `json:"workspace_id"`
	Terminal     string             `json:"terminal_id"`
	State        string             `json:"agent_status"`
	Agent        string             `json:"agent"`
}
type herdrResult struct {
	ProcessInfo struct {
		Processes []struct {
			PID  int    `json:"pid"`
			Name string `json:"name"`
		} `json:"foreground_processes"`
	} `json:"process_info"`
	Type  string    `json:"type"`
	Root  herdrPane `json:"root_pane"`
	Pane  herdrPane `json:"pane"`
	Agent herdrPane `json:"agent"`
	Read  struct {
		Text string `json:"text"`
	} `json:"read"`
	Snapshot struct {
		Version  string      `json:"version"`
		Protocol int         `json:"protocol"`
		Panes    []herdrPane `json:"panes"`
	} `json:"snapshot"`
}
type herdrResponse struct {
	ID     string      `json:"id"`
	Result herdrResult `json:"result"`
	Error  *herdrError `json:"error"`
}
type herdrError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *herdrError) Error() string { return "herdr " + e.Code + ": " + e.Message }

func (h *herdrTerminalRuntime) generation() (string, error) {
	st, err := os.Stat(h.Socket)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeSocket == 0 || st.Mode().Perm()&0077 != 0 {
		return "", errors.New("herdr socket must be private to its owner")
	}
	return fmt.Sprintf("%d:%d", st.Sys().(*syscall.Stat_t).Ino, st.ModTime().UnixNano()), nil
}
func (h *herdrTerminalRuntime) connect(ctx context.Context) (net.Conn, error) {
	if _, err := h.generation(); err != nil {
		return nil, err
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", h.Socket)
}
func herdrRequest(c net.Conn, method string, params any) error {
	return json.NewEncoder(c).Encode(map[string]any{"id": "manifest", "method": method, "params": params})
}
func (h *herdrTerminalRuntime) call(ctx context.Context, method string, params any) (herdrResult, error) {
	return h.callGeneration(ctx, method, params, "")
}
func (h *herdrTerminalRuntime) callGeneration(ctx context.Context, method string, params any, generation string) (herdrResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	c, err := h.connect(ctx)
	if err != nil {
		return herdrResult{}, err
	}
	defer c.Close()
	if generation != "" {
		current, e := h.generation()
		if e != nil {
			return herdrResult{}, e
		}
		if current != generation {
			return herdrResult{}, errors.New("herdr daemon changed before request; nothing sent")
		}
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if d, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(d)
	}
	if err = herdrRequest(c, method, params); err != nil {
		return herdrResult{}, err
	}
	var reply herdrResponse
	if err = json.NewDecoder(c).Decode(&reply); err != nil {
		return herdrResult{}, err
	}
	if reply.ID != "manifest" {
		return herdrResult{}, errors.New("herdr response identity mismatch")
	}
	if reply.Error != nil {
		return herdrResult{}, reply.Error
	}
	return reply.Result, nil
}
func (h *herdrTerminalRuntime) observation(p herdrPane, gen string) terminalObservation {
	ob := terminalUnknown(terminalIdentity{Backend: "herdr", Host: h.Host, Session: h.Session, Generation: gen, Workspace: p.Workspace, Pane: p.Pane, Occupant: p.Terminal, AgentSession: p.AgentSession.identity()})
	ob.Revision = p.Revision
	ob.Kind = p.Agent
	ob.Cwd = p.Cwd
	ob.Label = p.Label
	if ob.Label == "" {
		ob.Label = p.Title
	}
	ob.Connectivity = "connected"
	ob.Process = "running"
	if p.Agent != "" {
		switch p.State {
		case "idle", "working", "blocked", "done":
			ob.AgentState = p.State
		}
	}
	return ob
}
func (h *herdrTerminalRuntime) List(ctx context.Context) ([]terminalObservation, error) {
	gen, err := h.generation()
	if err != nil {
		return nil, err
	}
	r, err := h.call(ctx, "session.snapshot", map[string]any{})
	if err != nil {
		return nil, err
	}
	if r.Snapshot.Version != "0.9.0" || r.Snapshot.Protocol != 22 {
		return nil, errors.New("unsupported herdr version; require 0.9.0 protocol 22")
	}
	after, err := h.generation()
	if err != nil {
		return nil, err
	}
	if after != gen {
		return nil, errors.New("herdr daemon changed during snapshot")
	}
	out := []terminalObservation{}
	for _, p := range r.Snapshot.Panes {
		out = append(out, h.observation(p, gen))
	}
	return out, nil
}
func (h *herdrTerminalRuntime) Inspect(ctx context.Context, id terminalIdentity) (terminalObservation, error) {
	unknown := terminalUnknown(id)
	if id.Backend != "herdr" || id.Pane == "" || id.Occupant == "" || id.Generation == "" || id.Session != h.Session || id.Host != h.Host {
		return unknown, errors.New("explicit herdr identity required")
	}
	all, err := h.List(ctx)
	if err != nil {
		return unknown, err
	}
	gen, err := h.generation()
	if err != nil {
		return unknown, err
	}
	if gen != id.Generation {
		return unknown, errors.New("herdr daemon generation changed")
	}
	for _, ob := range all {
		if ob.Identity.Pane == id.Pane {
			if ob.Identity.Occupant != id.Occupant || ob.Identity.Workspace != id.Workspace || (id.AgentSession != "" && ob.Identity.AgentSession != id.AgentSession) {
				return unknown, errors.New("herdr pane occupant changed")
			}
			ob.Identity.ManifestID = id.ManifestID
			return ob, nil
		}
	}
	unknown.Connectivity = "connected"
	unknown.Process = "stopped"
	return unknown, nil
}
func (h *herdrTerminalRuntime) checked(ctx context.Context, id terminalIdentity) error {
	ob, err := h.Inspect(ctx, id)
	if err != nil {
		return err
	}
	if ob.Process != "running" {
		return errors.New("herdr pane is absent")
	}
	return nil
}
func (h *herdrTerminalRuntime) Create(ctx context.Context, se termSession) (terminalIdentity, error) {
	id, err := h.allocate(ctx, se)
	if err != nil {
		return id, err
	}
	return id, h.launch(ctx, id, se)
}
func (h *herdrTerminalRuntime) allocate(ctx context.Context, se termSession) (terminalIdentity, error) {
	if se.Device != "" {
		return terminalIdentity{}, errors.New("remote herdr launch is not supported")
	}
	if _, err := h.List(ctx); err != nil {
		return terminalIdentity{}, err
	}
	cwd := se.Cwd
	if cwd == "" {
		cwd = h.server.terminal.defaultWd
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return terminalIdentity{}, errors.New("terminal cwd does not exist")
	}
	gen, err := h.generation()
	if err != nil {
		return terminalIdentity{}, err
	}
	r, err := h.callGeneration(ctx, "workspace.create", map[string]any{"cwd": cwd, "label": se.Name}, gen)
	if err != nil {
		return terminalIdentity{}, err
	}
	id := h.observation(r.Root, gen).Identity
	id.ManifestID = se.ID
	if id.Pane == "" || id.Occupant == "" || id.Workspace == "" {
		return terminalIdentity{}, errors.New("herdr allocation omitted exact identity; outcome unknown")
	}
	return id, nil
}
func (h *herdrTerminalRuntime) launch(ctx context.Context, id terminalIdentity, se termSession) error {
	if err := h.checked(ctx, id); err != nil {
		return err
	}
	inner, ok := h.server.termInner(se)
	if !ok {
		return errors.New("terminal launch configuration invalid")
	}
	_, err := h.callGeneration(ctx, "pane.send_input", map[string]any{"pane_id": id.Pane, "text": "stty cols 120 rows 32; exec bash -lc " + shQuote(inner), "keys": []string{"enter"}}, id.Generation)
	return err
}

func (h *herdrTerminalRuntime) Attach(ctx context.Context, id terminalIdentity) (*exec.Cmd, error) {
	if err := h.checked(ctx, id); err != nil {
		return nil, err
	}
	// This client renders the exact terminal over a PTY; it does not focus a pane
	// in a shared workspace or resolve display labels. PTY resize drives SIGWINCH.
	binary, err := herdrExecutable()
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, binary, "--session", h.Session, "terminal", "attach", id.Occupant), nil
}

// User services may omit ~/.local/bin even though the independently supervised
// daemon uses the standard user installation there.
func herdrExecutable() (string, error) {
	if binary, err := exec.LookPath("herdr"); err == nil {
		return binary, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return exec.LookPath(filepath.Join(home, ".local", "bin", "herdr"))
}
func (h *herdrTerminalRuntime) Screen(ctx context.Context, id terminalIdentity) ([]string, error) {
	if err := h.checked(ctx, id); err != nil {
		return nil, err
	}
	r, err := h.callGeneration(ctx, "pane.read", map[string]any{"pane_id": id.Pane, "source": "visible", "strip_ansi": true}, id.Generation)
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(r.Read.Text, "\n"), "\n"), nil
}
func (h *herdrTerminalRuntime) guardPrompt(ctx context.Context, id terminalIdentity) error {
	lines, err := h.Screen(ctx, id)
	if err != nil {
		return err
	}
	if why := termBlockingDialog(lines); why != "" {
		return errors.New(why)
	}
	return nil
}
func (h *herdrTerminalRuntime) SendText(ctx context.Context, id terminalIdentity, text string) error {
	if err := h.guardPrompt(ctx, id); err != nil {
		return err
	}
	_, err := h.callGeneration(ctx, "agent.prompt", map[string]any{"target": id.Pane, "text": text}, id.Generation)
	return err
}
func (h *herdrTerminalRuntime) SendKey(ctx context.Context, id terminalIdentity, key string) error {
	if err := h.checked(ctx, id); err != nil {
		return err
	}
	keys := map[string]string{"\x03": "ctrl+c", "\x1b": "esc", "\r": "enter", "\n": "enter", "\x1b[A": "up", "\x1b[B": "down", "\x1b[C": "right", "\x1b[D": "left", "y": "y", "n": "n"}
	k, ok := keys[key]
	if !ok {
		return errors.New("unsupported terminal key")
	}
	_, err := h.callGeneration(ctx, "pane.send_keys", map[string]any{"pane_id": id.Pane, "keys": []string{k}}, id.Generation)
	return err
}
func (h *herdrTerminalRuntime) Close(ctx context.Context, id terminalIdentity) error {
	if err := h.checked(ctx, id); err != nil {
		return err
	}
	_, err := h.callGeneration(ctx, "pane.close", map[string]any{"pane_id": id.Pane}, id.Generation)
	return err
}
func (h *herdrTerminalRuntime) wait(ctx context.Context, id terminalIdentity, text *string, w terminalWait) (terminalObservation, error) {
	ob := terminalUnknown(id)
	if w.Timeout <= 0 || w.Timeout > 30*time.Second {
		return ob, errors.New("wait must be bounded to 0–30 seconds")
	}
	if w.State != "idle" && w.State != "done" && w.State != "blocked" && w.State != "working" && w.State != "settled" {
		return ob, errors.New("invalid wait state")
	}
	initial, err := h.Inspect(ctx, id)
	if err != nil {
		return ob, err
	}
	if initial.Process != "running" || initial.Identity.AgentSession == "" {
		return ob, errors.New("supervised wait requires a running, identified agent session; nothing sent")
	}
	id.AgentSession = initial.Identity.AgentSession
	until := []string{w.State}
	if w.State == "settled" {
		until = []string{"idle", "done", "blocked"}
	}
	params := map[string]any{"target": id.Pane, "until": until, "timeout_ms": w.Timeout.Milliseconds()}
	method := "agent.wait"
	if text != nil {
		if err := h.guardPrompt(ctx, id); err != nil {
			return ob, err
		}
		method = "agent.prompt"
		params = map[string]any{"target": id.Pane, "text": *text, "wait": map[string]any{"until": until, "timeout_ms": w.Timeout.Milliseconds()}}
	}
	r, err := h.callGeneration(ctx, method, params, id.Generation)
	if err != nil {
		return ob, fmt.Errorf("completion unobserved (do not replay): %w", err)
	}
	if r.Agent.Terminal != id.Occupant || r.Agent.AgentSession.identity() != id.AgentSession {
		return ob, errors.New("wait occupant changed")
	}
	observed, err := h.Inspect(ctx, id)
	if err != nil {
		return ob, err
	}
	return observed, nil
}
func (h *herdrTerminalRuntime) Wait(ctx context.Context, id terminalIdentity, w terminalWait) (terminalObservation, error) {
	return h.wait(ctx, id, nil, w)
}
func (h *herdrTerminalRuntime) Prompt(ctx context.Context, id terminalIdentity, text string, w terminalWait) (terminalObservation, error) {
	return h.wait(ctx, id, &text, w)
}

// Subscribe acknowledges first, then snapshots while the socket buffers events.
// Consumers reconnect and resnapshot when this bounded stream closes. No agent
// label is converted to process death. Dropped/overflowed streams invalidate all
// observations instead of leaving stale working indicators behind.
func (h *herdrTerminalRuntime) Subscribe(ctx context.Context) (<-chan terminalObservation, error) {
	generation, err := h.generation()
	if err != nil {
		return nil, err
	}
	c, err := h.connect(ctx)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	fail := func() { stop(); c.Close() }
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	// Enumeration only: the authoritative bootstrap still follows subscription ACK.
	preliminary, err := h.List(ctx)
	if err != nil {
		fail()
		return nil, err
	}
	subscribed := map[string]bool{}
	subs := []map[string]string{}
	for _, s := range []string{"pane.created", "pane.updated", "pane.closed", "pane.exited"} {
		subs = append(subs, map[string]string{"type": s})
	}
	// Per-pane agent_status_changed subs are omitted deliberately: a stale
	// pane_id in such a sub makes the whole events.subscribe ACK an error,
	// dropping the stream. The generic pane events are wakeups anyway — the
	// goroutine resnapshots via List() on every event, which carries the real
	// agent state. So generic-only subscribe reliably returns
	// subscription_started and keeps the socket stable.
	for _, ob := range preliminary {
		subscribed[ob.Identity.Pane] = true
	}
	if err = herdrRequest(c, "events.subscribe", map[string]any{"subscriptions": subs}); err != nil {
		fail()
		return nil, err
	}
	reader := bufio.NewReader(c)
	dec := json.NewDecoder(reader)
	var ack herdrResponse
	if err = dec.Decode(&ack); err != nil {
		fail()
		return nil, err
	}
	if ack.Error != nil {
		// A stale per-pane agent_status_changed sub (the pane id no longer
		// exists in the live layout) rejects that one sub, but the GENERIC
		// pane.created/updated/closed/exited subscription — which carries the
		// agent state and is what actually drives the stream — is still live.
		// Treat a benign pane_not_found as "subscription established; that
		// pane's fine-grained status sub just won't fire." Any other error is
		// fatal.
		if ack.Error.Code == "pane_not_found" {
			// proceed — the generic stream is authoritative for state.
		} else {
			fail()
			return nil, ack.Error
		}
	}
	if ack.ID != "manifest" || (ack.Result.Type != "subscription_started" && ack.Error == nil) {
		fail()
		return nil, errors.New("herdr subscription not acknowledged")
	}
	all, err := h.List(ctx)
	if err != nil {
		fail()
		return nil, err
	}
	for _, ob := range all {
		if !subscribed[ob.Identity.Pane] {
			fail()
			return nil, errors.New("pane set changed during subscribe; resubscribe required")
		}
	}
	currentGeneration, err := h.generation()
	if err != nil || currentGeneration != generation {
		fail()
		return nil, errors.New("herdr daemon changed during subscription bootstrap")
	}
	_ = c.SetDeadline(time.Time{})
	out := make(chan terminalObservation, len(all)+128)
	for _, ob := range all {
		out <- ob
	}
	go func() {
		defer close(out)
		defer fail()
		known := map[string]terminalObservation{}
		for _, ob := range all {
			known[ob.Identity.Pane] = ob
		}
		for {
			var ev struct {
				Event string `json:"event"`
			}
			if dec.Decode(&ev) != nil {
				return
			}
			switch ev.Event {
			case "pane_created", "pane_updated", "pane_closed", "pane_exited", "pane.agent_status_changed":
			default:
				continue
			}
			current, e := h.generation()
			if e != nil || current != generation {
				return
			}
			// Pane revision tracks screen/layout changes, not agent status in
			// 0.9.0. Events are wakeups: resnapshot instead of replaying stale
			// buffered labels or dropping real state changes at equal revision.
			fresh, e := h.List(ctx)
			if e != nil {
				return
			}
			next := make(map[string]terminalObservation, len(fresh))
			changed := []terminalObservation{}
			for _, ob := range fresh {
				if ob.Identity.Generation != generation {
					return
				}
				next[ob.Identity.Pane] = ob
				prev, exists := known[ob.Identity.Pane]
				if !exists || prev.Identity != ob.Identity || prev.AgentState != ob.AgentState || prev.Process != ob.Process || prev.Connectivity != ob.Connectivity || prev.Label != ob.Label || prev.Cwd != ob.Cwd || prev.Kind != ob.Kind {
					changed = append(changed, ob)
				}
			}
			for pane, prev := range known {
				if _, exists := next[pane]; !exists {
					prev.Process = "stopped"
					prev.AgentState = "unknown"
					prev.ObservedAt = time.Now().UTC()
					changed = append(changed, prev)
				}
			}
			// 0.9.0 has no wildcard state subscription. Reconnect after topology
			// changes to include new panes, retaining one active subscription.
			topologyChanged := len(next) != len(subscribed)
			for pane := range next {
				if !subscribed[pane] {
					topologyChanged = true
				}
			}
			known = next
			for _, ob := range changed {
				select {
				case out <- ob:
				case <-ctx.Done():
					return
				default:
					return
				}
			}
			if topologyChanged {
				return
			}
		}
	}()
	return out, nil
}
