package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/hermes"
	"manifest/threads"
)

// The owner's bug (2026-09-04 17:45): an Ask was accepted as a Hermes turn,
// manifest restarted 30s later (autodeploy), and the reply never came — the
// turn lived only in the in-memory running map. These tests cover the
// durable turn-open/turn-closed record and the sweep that re-drives it.

// hermesStub writes a fake `hermes` CLI. While hangFile exists the stub
// blocks (a turn the process will die on); otherwise it answers.
func hermesStub(t *testing.T, hangFile string) *hermes.Runner {
	t.Helper()
	script := "#!/bin/sh\nwhile [ -e '" + hangFile + "' ]; do sleep 0.05; done\n" +
		"printf 'ANSWER: parcel 12 is zoned R-1'\n"
	stub := filepath.Join(t.TempDir(), "hermes")
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return hermes.NewRunner(hermes.Config{Enabled: true, Bin: stub})
}

// agentPosts counts the visible agent comments on a thread.
func agentPosts(srv *Server, id string) []threads.Comment {
	var out []threads.Comment
	for _, c := range srv.listThread(id) {
		if strings.HasPrefix(c.Author, "agent:") {
			out = append(out, c)
		}
	}
	return out
}

func privateCount(srv *Server, id, action string) int {
	n := 0
	for _, c := range srv.threads.private.Thread(id) {
		if c.Action == action {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestHermesTurnSurvivesRestart(t *testing.T) {
	dirs := loopDirs{t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()}
	old := loopFixtureAt(t, dirs)
	id := "inbox/research-zoning"
	hang := filepath.Join(t.TempDir(), "hang")
	if err := os.WriteFile(hang, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(hang) }) // let the orphaned stub exit
	old.UseHermes(hermesStub(t, hang), "web")
	if _, ok := old.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	if err := old.setPlanAssignee(id, "agent:hermes"); err != nil {
		t.Fatal(err)
	}
	// the Ask is ACCEPTED (not refused) — the turn is in flight
	if _, err := old.postAndDispatch(id, "ask", "", nil, nil, "what is parcel 12 zoned?"); err != nil {
		t.Fatal(err)
	}
	if n := privateCount(old, id, actTurnOpen); n != 1 {
		t.Fatalf("an accepted turn must leave one turn-open marker, got %d", n)
	}
	old.hermes.mu.Lock()
	_, inFlight := old.hermes.running[id]
	old.hermes.mu.Unlock()
	if !inFlight {
		t.Fatal("turn should be running")
	}
	// while the turn is in flight the sweep leaves it alone
	sweep(old)
	if n := privateCount(old, id, actTurnOpen); n != 1 {
		t.Fatalf("sweep must not re-dispatch an in-flight turn, got %d opens", n)
	}
	// RESTART: a fresh process over the same files — empty running map, a
	// fresh runner (this one answers). The old process's goroutine is gone
	// with it (here: parked on the hang file, never touching the new server).
	srv := loopFixtureAt(t, dirs)
	srv.UseHermes(hermesStub(t, filepath.Join(t.TempDir(), "never")), "web")
	if got := len(agentPosts(srv, id)); got != 0 {
		t.Fatalf("no reply should exist before recovery: %d", got)
	}
	sweep(srv)
	if n := privateCount(srv, id, actTurnOpen); n != 2 {
		t.Fatalf("the sweep must re-dispatch the orphaned turn once, got %d opens", n)
	}
	waitFor(t, "the recovered reply", func() bool { return len(agentPosts(srv, id)) >= 1 })
	waitFor(t, "the turn to close", func() bool { return privateCount(srv, id, actTurnClosed) >= 1 })
	posts := agentPosts(srv, id)
	if len(posts) != 1 || !strings.Contains(posts[0].Text, "parcel 12 is zoned R-1") {
		t.Fatalf("want exactly one recovered reply: %+v", posts)
	}
	// an Ask never writes ## plan — recovery keeps the Ask contract
	if rec := srv.readPlanRecord(id); strings.TrimSpace(rec.Plan) != "" {
		t.Fatalf("recovered Ask must not write a plan: %q", rec.Plan)
	}
	// and the record is closed: further sweeps neither re-send nor re-open
	sweep(srv)
	sweep(srv)
	if n := privateCount(srv, id, actTurnOpen); n != 2 {
		t.Fatalf("a closed turn must not be re-dispatched, got %d opens", n)
	}
	if got := len(agentPosts(srv, id)); got != 1 {
		t.Fatalf("reply must post exactly once, got %d", got)
	}
}

// A crash between "reply posted" and "marker closed" must not double-post:
// the sweep sees the agent's reply after the open and closes the record.
func TestHermesTurnAnsweredIsNotResent(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	srv.UseHermes(hermesStub(t, filepath.Join(t.TempDir(), "never")), "web")
	if _, ok := srv.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	srv.hermesTurnMark(id, actTurnOpen, map[string]any{
		"agent": "agent:alfred", "phase": "comment", "intent": "info", "text": "what is it zoned?"})
	time.Sleep(5 * time.Millisecond)
	if _, err := srv.addThreadEntry(agentTokenIdentity("agent:alfred"), id, threads.ActComment,
		"ANSWER: already said R-1", nil, nil, map[string]any{"hermes": true}); err != nil {
		t.Fatal(err)
	}
	sweep(srv)
	if n := privateCount(srv, id, actTurnOpen); n != 1 {
		t.Fatalf("an answered turn must not be re-dispatched, got %d opens", n)
	}
	if n := privateCount(srv, id, actTurnClosed); n != 1 {
		t.Fatalf("an answered turn must be closed in place, got %d closes", n)
	}
	time.Sleep(100 * time.Millisecond) // a re-dispatched stub would have answered by now
	if got := len(agentPosts(srv, id)); got != 1 {
		t.Fatalf("reply must stay single, got %d", got)
	}
}

// A turn interrupted on every attempt gives up visibly instead of looping.
func TestHermesTurnRetryCap(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	srv.UseHermes(hermesStub(t, filepath.Join(t.TempDir(), "never")), "web")
	if _, ok := srv.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	for i := 0; i < hermesTurnRetries; i++ {
		srv.hermesTurnMark(id, actTurnOpen, map[string]any{
			"agent": "agent:alfred", "phase": "comment", "intent": "info", "text": "again?"})
		time.Sleep(2 * time.Millisecond)
	}
	sweep(srv)
	if n := privateCount(srv, id, actTurnOpen); n != hermesTurnRetries {
		t.Fatalf("the cap must stop re-dispatch, got %d opens", n)
	}
	if n := privateCount(srv, id, actTurnClosed); n != 1 {
		t.Fatalf("the abandoned turn must be closed, got %d closes", n)
	}
	posts := agentPosts(srv, id)
	if len(posts) != 1 || !strings.Contains(posts[0].Text, "interrupted") {
		t.Fatalf("giving up must be visible in the thread: %+v", posts)
	}
}
