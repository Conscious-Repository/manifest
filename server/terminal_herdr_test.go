package server

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type herdrFixtureRequest struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

func herdrFixture(t *testing.T, handle func(net.Conn, herdrFixtureRequest)) *herdrTerminalRuntime {
	t.Helper()
	socketDir, err := os.MkdirTemp("/tmp", "manifest-herdr-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "h.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	conns := []net.Conn{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				var req herdrFixtureRequest
				if json.NewDecoder(c).Decode(&req) == nil {
					handle(c, req)
				}
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return &herdrTerminalRuntime{Socket: socket, Session: "fixture", Host: "local"}
}
func herdrFixtureReply(c net.Conn, result any) {
	_ = json.NewEncoder(c).Encode(map[string]any{"id": "manifest", "result": result})
}
func herdrFixturePane(state string, revision int) map[string]any {
	return map[string]any{"pane_id": "p_1", "workspace_id": "w_1", "terminal_id": "t_1", "agent": "claude", "agent_session": map[string]any{"agent": "claude", "kind": "id", "value": "conversation_1"}, "agent_status": state, "revision": revision}
}
func herdrFixtureSnapshot(c net.Conn, state string, revision int) {
	herdrFixtureReply(c, map[string]any{"type": "session_snapshot", "snapshot": map[string]any{"version": "0.9.0", "protocol": 22, "panes": []any{herdrFixturePane(state, revision)}}})
}
func herdrFixtureID(t *testing.T, h *herdrTerminalRuntime) terminalIdentity {
	t.Helper()
	gen, e := h.generation()
	if e != nil {
		t.Fatal(e)
	}
	return terminalIdentity{Backend: "herdr", Host: h.Host, Session: h.Session, Generation: gen, Workspace: "w_1", Pane: "p_1", Occupant: "t_1"}
}
func TestHerdrRuntimeOutageIsUnknown(t *testing.T) {
	h := &herdrTerminalRuntime{Socket: filepath.Join(t.TempDir(), "absent"), Session: "fixture", Host: "local"}
	ob, err := h.Inspect(context.Background(), terminalIdentity{Backend: "herdr", Session: h.Session, Host: h.Host, Generation: "old", Pane: "p_1", Occupant: "t_1"})
	if err == nil || ob.Process != "unknown" || ob.Connectivity != "unavailable" || ob.AgentState != "unknown" {
		t.Fatalf("%+v %v", ob, err)
	}
}
func TestHerdrRuntimeIdentityMismatchRefusesSend(t *testing.T) {
	for _, which := range []string{"generation", "occupant", "workspace", "session", "host"} {
		t.Run(which, func(t *testing.T) {
			var writes atomic.Int32
			h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
				if r.Method == "session.snapshot" {
					herdrFixtureSnapshot(c, "idle", 1)
				} else {
					writes.Add(1)
					herdrFixtureReply(c, map[string]any{})
				}
			})
			id := herdrFixtureID(t, h)
			switch which {
			case "generation":
				id.Generation = "other"
			case "occupant":
				id.Occupant = "other"
			case "workspace":
				id.Workspace = "other"
			case "session":
				id.Session = "other"
			case "host":
				id.Host = "other"
			}
			if err := h.SendKey(context.Background(), id, "y"); err == nil {
				t.Fatal("accepted mismatched identity")
			}
			if writes.Load() != 0 {
				t.Fatal("mutated replacement identity")
			}
		})
	}
}
func TestHerdrRuntimeDialogRefusesPrompt(t *testing.T) {
	var writes atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 1)
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "Do you trust the files in this folder?\n❯ No, exit"}})
		default:
			writes.Add(1)
			herdrFixtureReply(c, map[string]any{})
		}
	})
	if err := h.SendText(context.Background(), herdrFixtureID(t, h), "hello"); err == nil {
		t.Fatal("accepted dialog")
	}
	if writes.Load() != 0 {
		t.Fatal("prompted dialog")
	}
}
func TestHerdrRuntimePromptTimeoutIsUnobservedAndNeverResent(t *testing.T) {
	var prompts atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 1)
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "❯"}})
		case "agent.prompt":
			prompts.Add(1)
			if r.Params["wait"] == nil {
				t.Error("prompt was not atomic with wait")
			}
			_ = json.NewEncoder(c).Encode(map[string]any{"id": "manifest", "error": map[string]any{"code": "timeout", "message": "status not observed"}})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	ob, err := h.Prompt(context.Background(), herdrFixtureID(t, h), "hello", terminalWait{State: "idle", Timeout: time.Millisecond * 50})
	if err == nil || !strings.Contains(err.Error(), "unobserved") || ob.AgentState != "unknown" {
		t.Fatalf("%+v %v", ob, err)
	}
	if prompts.Load() != 1 {
		t.Fatalf("sent %d times", prompts.Load())
	}
}
func TestHerdrRuntimeSubscribeBootstrapsAndReconnects(t *testing.T) {
	var subscribed atomic.Bool
	var snapshots atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "events.subscribe":
			subscribed.Store(true)
			herdrFixtureReply(c, map[string]any{"type": "subscription_started"})
		case "session.snapshot":
			snapshots.Add(1)
			if !subscribed.Swap(false) {
				herdrFixtureSnapshot(c, "idle", 1)
			} else {
				herdrFixtureSnapshot(c, "working", 2)
			}
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		ch, err := h.Subscribe(ctx)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		ob, ok := <-ch
		if !ok || ob.AgentState != "working" {
			cancel()
			t.Fatalf("missing bootstrap: %+v", ob)
		}
		select {
		case _, ok = <-ch:
			if ok {
				t.Error("extra event")
			}
		case <-ctx.Done():
			t.Error("disconnect did not close stream")
		}
		cancel()
	}
	if snapshots.Load() != 4 {
		t.Fatalf("reconnect did not resnapshot: %d", snapshots.Load())
	}
}

func TestHerdrRuntimeBootstrapDiscardsStaleEventsAndAcceptsSameRevisionState(t *testing.T) {
	var snapshots atomic.Int32
	release := make(chan struct{})
	defer close(release)
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "events.subscribe":
			herdrFixtureReply(c, map[string]any{"type": "subscription_started"})
			enc := json.NewEncoder(c)
			_ = enc.Encode(map[string]any{"event": "pane_updated", "data": map[string]any{"pane": herdrFixturePane("idle", 1)}})
			_ = enc.Encode(map[string]any{"event": "pane_updated", "data": map[string]any{"pane": herdrFixturePane("blocked", 2)}})
			<-release
		case "session.snapshot":
			if snapshots.Add(1) <= 2 {
				herdrFixtureSnapshot(c, "working", 2)
			} else {
				herdrFixtureSnapshot(c, "blocked", 2)
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch, err := h.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"working", "blocked"} {
		select {
		case ob := <-ch:
			if ob.AgentState != state {
				t.Fatalf("bootstrap regressed: got %s want %s", ob.AgentState, state)
			}
		case <-ctx.Done():
			t.Fatal("missing event")
		}
	}
}
func TestHerdrRuntimeSupervisedSendRequiresAgentSession(t *testing.T) {
	var prompts atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		if r.Method == "session.snapshot" {
			pane := herdrFixturePane("idle", 1)
			delete(pane, "agent_session")
			herdrFixtureReply(c, map[string]any{"snapshot": map[string]any{"version": "0.9.0", "protocol": 22, "panes": []any{pane}}})
		} else {
			prompts.Add(1)
			herdrFixtureReply(c, map[string]any{})
		}
	})
	ob, err := h.Prompt(context.Background(), herdrFixtureID(t, h), "hello", terminalWait{State: "idle", Timeout: time.Second})
	if err == nil || ob.AgentState != "unknown" || prompts.Load() != 0 {
		t.Fatalf("unidentified occupant prompted: %+v %v calls=%d", ob, err, prompts.Load())
	}
}
func TestHerdrRuntimeAgentSessionMismatchRefusesSend(t *testing.T) {
	var writes atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		if r.Method == "session.snapshot" {
			herdrFixtureSnapshot(c, "idle", 1)
		} else {
			writes.Add(1)
			herdrFixtureReply(c, map[string]any{})
		}
	})
	id := herdrFixtureID(t, h)
	id.AgentSession = "claude:id:old_conversation"
	if err := h.SendKey(context.Background(), id, "y"); err == nil || writes.Load() != 0 {
		t.Fatalf("replacement conversation received input: %v calls=%d", err, writes.Load())
	}
}
func TestHerdrRuntimePinnedRequestRefusesChangedGeneration(t *testing.T) {
	var requests atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) { requests.Add(1); herdrFixtureReply(c, map[string]any{}) })
	if _, err := h.callGeneration(context.Background(), "agent.prompt", map[string]any{"target": "p_1", "text": "hello"}, "stale"); err == nil {
		t.Fatal("sent to replacement daemon")
	}
	if requests.Load() != 0 {
		t.Fatal("generation checked after sending")
	}
}

func TestHerdrRuntimeSubscriptionDoesNotRelabelOldStream(t *testing.T) {
	release := make(chan struct{})
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "events.subscribe":
			herdrFixtureReply(c, map[string]any{"type": "subscription_started"})
			<-release
			_ = json.NewEncoder(c).Encode(map[string]any{"event": "pane_updated", "data": map[string]any{"pane": herdrFixturePane("idle", 3)}})
		case "session.snapshot":
			herdrFixtureSnapshot(c, "working", 2)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch, err := h.Subscribe(ctx)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	<-ch
	// A changed socket generation must invalidate an existing subscription;
	// events from its old connection cannot acquire the new generation.
	now := time.Now().Add(time.Second)
	err = os.Chtimes(h.Socket, now, now)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ob, ok := <-ch:
		if ok {
			t.Fatalf("old stream relabeled: %+v", ob)
		}
	case <-ctx.Done():
		t.Fatal("changed daemon stream remained available")
	}
}
