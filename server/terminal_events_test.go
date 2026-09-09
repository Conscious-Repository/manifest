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
	var c net.Conn
	select {
	case c = <-socket:
	case <-time.After(4 * time.Second):
		t.Fatal("subscription not opened")
	}
	if subscriptions.Load() != 1 {
		t.Fatalf("%d subscriptions for two clients", subscriptions.Load())
	}
	c.Close()
	// Reconnection must not emit unavailable or duplicate unchanged snapshots.
	select {
	case <-socket:
	case <-time.After(4 * time.Second):
		t.Fatal("did not resubscribe after disconnect")
	}
	select {
	case snap := <-a:
		t.Fatalf("unchanged state was republished: %+v", snap)
	case <-time.After(2200 * time.Millisecond):
	}
	hub.mu.Lock()
	snap := hub.snapshotLocked()
	hub.mu.Unlock()
	if !snap.Connected || !working(snap) {
		t.Fatalf("subscription drop invalidated reachable daemon: %+v", snap)
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
	if got := h.snapshotLocked().Sessions[0]; got.Process != "unknown" || got.AgentState != "unknown" || got.Connectivity != "connected" {
		t.Fatal("adopted replacement by pane")
	}
	delete(h.latest, id.Pane)
	if got := h.snapshotLocked().Sessions[0]; got.Connectivity != "connected" || got.Process != "unknown" || got.AgentState != "unknown" {
		t.Fatalf("absent occupant changed daemon connectivity: %+v", got)
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

// Even an unacknowledged subscription must not delay snapshots or outage detection.
func TestTerminalEventsPollWhileSubscriptionStallsAndListFails(t *testing.T) {
	var down, idle atomic.Bool
	socket := make(chan net.Conn, 1)
	rt := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			if down.Load() {
				return
			} // EOF: daemon snapshot unavailable.
			state := "working"
			if idle.Load() {
				state = "idle"
			}
			herdrFixtureSnapshot(c, state, 1)
		case "events.subscribe":
			socket <- c
			var b [1]byte
			c.Read(b[:]) // Never ACK; List must progress independently.
		default:
			t.Errorf("unexpected %s", r.Method)
		}
	})
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), herdr: rt}}
	rt.server = s
	if err := s.terminal.upsertChecked(termSession{ID: "abcdef12", Backend: "herdr", Version: 1, Runtime: herdrFixtureID(t, rt)}); err != nil {
		t.Fatal(err)
	}
	hub := s.terminalHub()
	ch, leave := hub.join()
	defer leave()
	select {
	case snap := <-ch:
		if !snap.Connected || len(snap.Sessions) != 1 || snap.Sessions[0].AgentState != "working" {
			t.Fatalf("first snapshot preceded List: %+v", snap)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("initial List blocked by subscription")
	}
	select {
	case <-socket:
	case <-time.After(4 * time.Second):
		t.Fatal("subscription not attempted")
	}
	idle.Store(true)
	awaitTerminalSnapshot(t, ch, func(s terminalEventSnapshot) bool { return s.Connected && s.Sessions[0].AgentState == "idle" })
	down.Store(true)
	snap := awaitTerminalSnapshot(t, ch, func(s terminalEventSnapshot) bool { return !s.Connected })
	if ob := snap.Sessions[0]; ob.Connectivity != "unavailable" || ob.AgentState != "unknown" || ob.Process != "unknown" {
		t.Fatalf("outage retained stale state: %+v", ob)
	}
	hub.mu.Lock()
	remaining := len(hub.latest)
	hub.mu.Unlock()
	if remaining != 0 {
		t.Fatal("failed List retained observations")
	}
	down.Store(false)
	awaitTerminalSnapshot(t, ch, func(s terminalEventSnapshot) bool { return s.Connected && s.Sessions[0].AgentState == "idle" })
	leave()
	hub.mu.Lock()
	if hub.ready || hub.cancel != nil {
		t.Error("last consumer retained poller state")
	}
	hub.mu.Unlock()
}

func TestTerminalEventsSubscriptionFailuresDoNotInvalidateSnapshots(t *testing.T) {
	var subscriptions atomic.Int32
	var idle atomic.Bool
	rt := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			state := "working"
			if idle.Load() {
				state = "idle"
			}
			herdrFixtureSnapshot(c, state, 1)
		case "events.subscribe":
			subscriptions.Add(1)
			// Close without an ACK: Subscribe fails on every attempt.
		default:
			t.Errorf("unexpected %s", r.Method)
		}
	})
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), herdr: rt}}
	rt.server = s
	if err := s.terminal.upsertChecked(termSession{ID: "abcdef12", Backend: "herdr", Version: 1, Runtime: herdrFixtureID(t, rt)}); err != nil {
		t.Fatal(err)
	}
	ch, leave := s.terminalHub().join()
	defer leave()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		select {
		case snap := <-ch:
			if !snap.Connected || len(snap.Sessions) != 1 || snap.Sessions[0].Connectivity != "connected" {
				t.Fatalf("subscription failure invalidated snapshot: %+v", snap)
			}
			if snap.Sessions[0].AgentState == "idle" {
				if subscriptions.Load() < 2 {
					t.Fatal("subscription was not retried")
				}
				return
			}
			idle.Store(true)
		case <-timer.C:
			t.Fatal("poll did not refresh state after subscription failures")
		}
	}
}
