package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// terminalRuntime owns process plumbing and advisory observations only. A screen,
// socket, or working/blocked/done/idle label is never durable task truth: only
// CLI JSONL supplies transcripts and validated result files establish board
// completion. Observations must never release the checkout writer gate.
// Connectivity and process existence are independent of agent state. An error
// means unknown, not stopped. All control requests target explicit identities.
type terminalRuntime interface {
	Create(context.Context, termSession) (terminalIdentity, error)
	Attach(context.Context, terminalIdentity) (*exec.Cmd, error)
	Inspect(context.Context, terminalIdentity) (terminalObservation, error)
	List(context.Context) ([]terminalObservation, error)
	Screen(context.Context, terminalIdentity) ([]string, error)
	SendText(context.Context, terminalIdentity, string) error
	SendKey(context.Context, terminalIdentity, string) error
	Close(context.Context, terminalIdentity) error
	Subscribe(context.Context) (<-chan terminalObservation, error)
	Wait(context.Context, terminalIdentity, terminalWait) (terminalObservation, error)
	// Prompt establishes the observation boundary atomically with submission.
	// A timeout is unobserved completion; callers must never replay the send.
	Prompt(context.Context, terminalIdentity, string, terminalWait) (terminalObservation, error)
}

type terminalIdentity struct {
	ManifestID   string `json:"manifestId,omitempty"`
	Backend      string `json:"backend"`
	Host         string `json:"host,omitempty"`
	Generation   string `json:"generation,omitempty"`
	Session      string `json:"session,omitempty"`
	Workspace    string `json:"workspace,omitempty"`
	Pane         string `json:"pane,omitempty"`
	Occupant     string `json:"occupant,omitempty"`
	AgentSession string `json:"agentSession,omitempty"`
}

type terminalObservation struct {
	Identity     terminalIdentity `json:"identity"`
	AgentState   string           `json:"agentState"`
	Connectivity string           `json:"connectivity"`
	Process      string           `json:"process"` // running, stopped, unknown
	ObservedAt   time.Time        `json:"observedAt"`
	Revision     uint64           `json:"revision,omitempty"`
}

type terminalWait struct {
	State   string
	Timeout time.Duration
}

var errTerminalUnsupported = errors.New("terminal runtime does not support this operation")

func terminalUnknown(id terminalIdentity) terminalObservation {
	return terminalObservation{Identity: id, AgentState: "unknown", Connectivity: "unavailable", Process: "unknown", ObservedAt: time.Now().UTC()}
}

// Legacy sessions retain their tmux socket and remote keep launch wrappers.
// tmux does not expose agent detection, so it never reports idle or completion.
type tmuxTerminalRuntime struct{ server *Server }

var _ terminalRuntime = (*tmuxTerminalRuntime)(nil)

func (t *tmuxTerminalRuntime) run(ctx context.Context, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c := t.server.terminal
	if c.run != nil {
		return c.run(args...)
	}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+c.tmuxTmp)
	return cmd.Output()
}

func (t *tmuxTerminalRuntime) Create(ctx context.Context, se termSession) (terminalIdentity, error) {
	id := terminalIdentity{ManifestID: se.ID, Backend: "tmux", Host: se.Device, Session: tmuxName(se.ID)}
	args, err := t.server.termLaunchArgs(se, false)
	if err != nil {
		return id, err
	}
	if err = ctx.Err(); err != nil {
		return id, err
	}
	// Preserve the established independent user scope and shared launch builder.
	if t.server.terminal.run == nil && tmuxScopeAvailable() {
		scoped := append([]string{"--user", "--scope", "--collect", "--quiet", "--description=manifest terminal session", "--setenv=TMUX_TMPDIR=" + t.server.terminal.tmuxTmp, "--", "tmux"}, args...)
		cmd := exec.CommandContext(ctx, "systemd-run", scoped...)
		cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+t.server.terminal.tmuxTmp, "XDG_RUNTIME_DIR="+xdgRuntimeDir())
		if err = cmd.Run(); err != nil {
			return id, err
		}
	} else {
		_, err = t.run(ctx, args...)
	}
	return id, err
}

func (t *tmuxTerminalRuntime) Attach(ctx context.Context, id terminalIdentity) (*exec.Cmd, error) {
	if _, err := t.checked(ctx, id); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "tmux", "attach-session", "-t", id.Session)
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+t.server.terminal.tmuxTmp, "TERM=xterm-256color")
	return cmd, nil
}

func (t *tmuxTerminalRuntime) List(ctx context.Context) ([]terminalObservation, error) {
	data, err := t.run(ctx, "list-panes", "-a", "-F", "#{session_name}\t#{pane_id}\t#{pane_dead}\t#{pane_pid}")
	if err != nil {
		return nil, err
	}
	out := []terminalObservation{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			if line == "" {
				continue
			}
			return nil, fmt.Errorf("invalid tmux inventory")
		}
		if fields[0] == "" || fields[1] == "" || fields[3] == "" || (fields[2] != "0" && fields[2] != "1") {
			return nil, errors.New("invalid tmux pane identity or process state")
		}
		id := terminalIdentity{Backend: "tmux", Session: fields[0], Pane: fields[1], Occupant: fields[3]}
		if strings.HasPrefix(id.Session, "manifest_") {
			id.ManifestID = strings.TrimPrefix(id.Session, "manifest_")
		}
		ob := terminalUnknown(id)
		ob.Connectivity = "connected"
		ob.Process = "running"
		if fields[2] == "1" {
			ob.Process = "stopped"
		}
		out = append(out, ob)
	}
	return out, nil
}

func (t *tmuxTerminalRuntime) Inspect(ctx context.Context, id terminalIdentity) (terminalObservation, error) {
	out := terminalUnknown(id)
	if id.Backend != "" && id.Backend != "tmux" {
		return out, errors.New("terminal backend mismatch")
	}
	if id.Session == "" {
		return out, errors.New("explicit tmux session required")
	}
	all, err := t.List(ctx)
	if err != nil {
		return out, err
	}
	for _, ob := range all {
		if ob.Identity.Session != id.Session || (id.Pane != "" && ob.Identity.Pane != id.Pane) {
			continue
		}
		if id.Occupant != "" && ob.Identity.Occupant != id.Occupant {
			return out, errors.New("terminal occupant changed")
		}
		ob.Identity.ManifestID = id.ManifestID
		ob.Identity.Host = id.Host
		return ob, nil
	}
	out.Connectivity = "connected"
	out.Process = "stopped"
	return out, nil
}

func (t *tmuxTerminalRuntime) checked(ctx context.Context, id terminalIdentity) (terminalObservation, error) {
	ob, err := t.Inspect(ctx, id)
	if err == nil && ob.Process != "running" {
		err = errors.New("terminal process is not running")
	}
	return ob, err
}
func tmuxRuntimeTarget(id terminalIdentity) string {
	if id.Pane != "" {
		return id.Pane
	}
	return id.Session
}

func (t *tmuxTerminalRuntime) Screen(ctx context.Context, id terminalIdentity) ([]string, error) {
	ob, err := t.checked(ctx, id)
	if err != nil {
		return nil, err
	}
	data, err := t.run(ctx, "capture-pane", "-p", "-J", "-S", "-12", "-t", tmuxRuntimeTarget(ob.Identity))
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n"), nil
}
func (t *tmuxTerminalRuntime) SendText(ctx context.Context, id terminalIdentity, text string) error {
	lines, err := t.Screen(ctx, id)
	if err != nil {
		return err
	}
	if why := termBlockingDialog(lines); why != "" {
		return errors.New(why)
	}
	text = strings.TrimRight(text, "\r\n")
	if strings.Contains(text, "\n") {
		text = "\x1b[200~" + text + "\x1b[201~"
	}
	if err = t.SendKey(ctx, id, text); err != nil {
		return err
	}
	_, err = t.run(ctx, "send-keys", "-t", tmuxRuntimeTarget(id), "Enter")
	return err
}
func (t *tmuxTerminalRuntime) SendKey(ctx context.Context, id terminalIdentity, key string) error {
	ob, err := t.checked(ctx, id)
	if err != nil {
		return err
	}
	_, err = t.run(ctx, "send-keys", "-t", tmuxRuntimeTarget(ob.Identity), "-l", "--", key)
	return err
}
func (t *tmuxTerminalRuntime) Close(ctx context.Context, id terminalIdentity) error {
	if _, err := t.checked(ctx, id); err != nil {
		return err
	}
	_, err := t.run(ctx, "kill-session", "-t", id.Session)
	return err
}
func (*tmuxTerminalRuntime) Subscribe(context.Context) (<-chan terminalObservation, error) {
	return nil, errTerminalUnsupported
}
func (*tmuxTerminalRuntime) Wait(_ context.Context, id terminalIdentity, _ terminalWait) (terminalObservation, error) {
	return terminalUnknown(id), errTerminalUnsupported
}
func (*tmuxTerminalRuntime) Prompt(_ context.Context, id terminalIdentity, _ string, _ terminalWait) (terminalObservation, error) {
	return terminalUnknown(id), errTerminalUnsupported
}
