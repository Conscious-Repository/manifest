package server

import (
	"context"
	"net"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func awaitTerminalSnapshot(t *testing.T, ch <-chan terminalEventSnapshot, predicate func(terminalEventSnapshot) bool) terminalEventSnapshot {
	t.Helper()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		select {
		case snap := <-ch:
			if predicate(snap) {
				return snap
			}
		case <-timer.C:
			t.Fatal("terminal event not observed")
			return terminalEventSnapshot{}
		}
	}
}
func TestTerminalEventsShareSubscriptionAndInvalidateDisconnect(t *testing.T) {
	var subscriptions atomic.Int32
	socket := make(chan net.Conn, 3)
	rt := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "working", 1)
		case "events.subscribe":
			subscriptions.Add(1)
			herdrFixtureReply(c, map[string]any{"type": "subscription_started"})
			socket <- c
			var b [1]byte
			c.Read(b[:])
		default:
			t.Errorf("unexpected %s", r.Method)
		}
	})
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), herdr: rt}}
	rt.server = s
	se := termSession{ID: "abcdef12", Backend: "herdr", Version: 1, Runtime: herdrFixtureID(t, rt)}
	if err := s.terminal.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	hub := s.terminalHub()
	a, leaveA := hub.join()
	defer leaveA()
	b, leaveB := hub.join()
	defer leaveB()
	working := func(snap terminalEventSnapshot) bool {
		return len(snap.Sessions) == 1 && snap.Sessions[0].AgentState == "working"
	}
	awaitTerminalSnapshot(t, a, working)
	awaitTerminalSnapshot(t, b, working)
	if subscriptions.Load() != 1 {
		t.Fatalf("%d subscriptions for two clients", subscriptions.Load())
	}
	c := <-socket
	c.Close()
	// The event-subscription socket dropped, but the daemon is REACHABLE (the
	// fixture's List() still succeeds). The hub must not lie that the agent is
	// unavailable — it verifies reachability, stays connected, and re-subscribes.
	awaitTerminalSnapshot(t, a, working)
	// Re-subscription happens shortly after the reachability check; wait for it
	// rather than checking synchronously (the 150ms backoff hasn't elapsed yet).
	deadline := time.Now().Add(4 * time.Second)
	for subscriptions.Load() != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("did not resubscribe after disconnect")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
func TestTerminalEventProjectionDoesNotTrustWrongOccupantOrBoardLabel(t *testing.T) {
	c := &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}
	s := &Server{terminal: c}
	id := terminalIdentity{Backend: "herdr", Host: "local", Session: "fixture", Pane: "p", Occupant: "a", Generation: "g"}
	se := termSession{ID: "abcdef12", Version: 1, Backend: "herdr", Runtime: id, BoardBrief: "/work/run-123/brief.md"}
	if err := c.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	h := s.terminalHub()
	h.connected = true
	h.latest[id.Pane] = terminalObservation{Identity: id, AgentState: "done", Process: "running", Connectivity: "connected"}
	snap := h.snapshotLocked()
	if snap.Sessions[0].AgentState != "unknown" || snap.Sessions[0].RunID != "run-123" {
		t.Fatal("board label leaked or run link lost")
	}
	wrong := h.latest[id.Pane]
	wrong.Identity.Occupant = "replacement"
	h.latest[id.Pane] = wrong
	if got := h.snapshotLocked().Sessions[0]; got.Process != "unknown" || got.AgentState != "unknown" {
		t.Fatal("adopted replacement by pane")
	}
}
func TestTerminalEventsSameOriginAndCancellation(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}}
	req := httptest.NewRequest("GET", "http://manifest/api/terminal/events", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.handleTermEvents(w, req)
	if w.Code != 403 {
		t.Fatal("cross-origin SSE accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = httptest.NewRequest("GET", "http://manifest/api/terminal/events", nil).WithContext(ctx)
	s.handleTermEvents(httptest.NewRecorder(), req)
	h := s.terminalHub()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subscribers) != 0 || h.cancel != nil {
		t.Fatal("canceled SSE retained daemon consumer")
	}
}
