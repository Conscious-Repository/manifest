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
// durable turn-open/turn-dispatched/turn-closed record and the sweep that
// settles it. Owner decision D4 (2026-09-27): a turn reaches Hermes once; a
// turn interrupted after its hand-off is closed visibly, never re-sent, and
// only a turn that never reached the runner is re-dispatched.

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
	old.UseHermes(hermesStub(t, hang), "web")
	// The simulated dead process still has a real goroutine in this test.
	// Release and join its final write before TempDir removes the shared files.
	t.Cleanup(func() {
		closed := privateCount(old, id, actTurnClosed)
		_ = os.Remove(hang)
		waitFor(t, "the simulated old worker to finish cleanup", func() bool {
			old.hermes.mu.Lock()
			_, running := old.hermes.running[id]
			old.hermes.mu.Unlock()
			return !running && privateCount(old, id, actTurnClosed) > closed
		})
	})
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
	waitFor(t, "the hand-off record", func() bool { return privateCount(old, id, actTurnDispatched) == 1 })
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
	// D4: the turn had been handed to Hermes, so it may already have acted.
	// It is not re-sent; the thread says so and the owner asks again.
	if n := privateCount(srv, id, actTurnOpen); n != 1 {
		t.Fatalf("a handed-off turn must not be re-sent, got %d opens", n)
	}
	if n := privateCount(srv, id, actTurnClosed); n != 1 {
		t.Fatalf("the interrupted turn must be closed, got %d closes", n)
	}
	posts := agentPosts(srv, id)
	if len(posts) != 1 || !strings.Contains(posts[0].Text, "interrupted after it was handed over") || !strings.Contains(posts[0].Text, "ask again") {
		t.Fatalf("the owner must be told once: %+v", posts)
	}
	// and the record stays settled: further sweeps neither re-send nor re-open
	sweep(srv)
	sweep(srv)
	time.Sleep(100 * time.Millisecond) // a re-sent stub would have answered by now
	if n := privateCount(srv, id, actTurnOpen); n != 1 {
		t.Fatalf("a closed turn must not be re-dispatched, got %d opens", n)
	}
	if got := len(agentPosts(srv, id)); got != 1 {
		t.Fatalf("nothing further may post, got %d", got)
	}
}

// A turn the process died on before its hand-off never reached Hermes, so
// re-sending it is effect-free: the sweep re-dispatches it once and the
// reply lands once.
func TestHermesTurnNeverDeliveredIsRedispatchedOnce(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	srv.UseHermes(hermesStub(t, filepath.Join(t.TempDir(), "never")), "web")
	if _, ok := srv.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	srv.hermesTurnMark(id, actTurnOpen, map[string]any{
		"agent": "agent:alfred", "phase": "comment", "intent": "info", "text": "what is parcel 12 zoned?", "dispatchMarked": true})
	sweep(srv)
	waitFor(t, "the re-dispatched turn to close", func() bool { return privateCount(srv, id, actTurnClosed) == 1 })
	if n := privateCount(srv, id, actTurnOpen); n != 2 {
		t.Fatalf("want the original open and one re-dispatch, got %d", n)
	}
	if n := privateCount(srv, id, actTurnDispatched); n != 1 {
		t.Fatalf("the re-dispatch is the only hand-off, got %d", n)
	}
	sweep(srv)
	sweep(srv)
	posts := agentPosts(srv, id)
	if len(posts) != 1 || !strings.Contains(posts[0].Text, "parcel 12 is zoned R-1") {
		t.Fatalf("want exactly one reply: %+v", posts)
	}
	if rec := srv.readPlanRecord(id); strings.TrimSpace(rec.Plan) != "" {
		t.Fatalf("a recovered Ask must not write a plan: %q", rec.Plan)
	}
}

// An open written before hand-offs were recorded (no dispatchMarked) cannot
// be proven undelivered, so it is treated as delivered: closed, not re-sent.
func TestHermesTurnUnmarkedOpenIsNotResent(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	srv.UseHermes(hermesStub(t, filepath.Join(t.TempDir(), "never")), "web")
	if _, ok := srv.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	srv.hermesTurnMark(id, actTurnOpen, map[string]any{"agent": "agent:alfred", "phase": "comment", "intent": "info", "text": "zoned?"})
	sweep(srv)
	time.Sleep(100 * time.Millisecond)
	if n := privateCount(srv, id, actTurnOpen); n != 1 || privateCount(srv, id, actTurnDispatched) != 0 {
		t.Fatalf("an unprovable turn was re-sent: %d opens", n)
	}
	posts := agentPosts(srv, id)
	if len(posts) != 1 || !strings.Contains(posts[0].Text, "not re-sent") {
		t.Fatalf("the owner must be told: %+v", posts)
	}
}

// A process that outlived its restart must not hand off a turn the newer
// process already settled: it stands down without dispatching, without
// running, and without writing a close that would settle the newer chain.
func TestHermesTurnSupersededBeforeHandOffStandsDown(t *testing.T) {
	srv := loopFixture(t)
	id := "inbox/research-zoning"
	srv.UseHermes(hermesStub(t, filepath.Join(t.TempDir(), "never")), "web")
	if _, ok := srv.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	open, err := srv.threads.private.Add(threads.Identity{ID: "system", Name: "system"}, id, actTurnOpen, "", nil, nil,
		map[string]any{"marker": true, "agent": "agent:alfred", "phase": "comment", "dispatchMarked": true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	srv.hermesTurnMark(id, actTurnClosed, map[string]any{"agent": "agent:alfred", "phase": "comment", "abandoned": true})
	srv.hermes.mu.Lock()
	srv.hermes.running[id] = hermesTurn{Phase: "comment", Agent: "agent:alfred", Since: time.Now()}
	srv.hermes.mu.Unlock()
	srv.runHermesTurn(id, open.ID, "agent:alfred", "comment", "info", "what is parcel 12 zoned?")
	if n := privateCount(srv, id, actTurnDispatched); n != 0 {
		t.Fatalf("a superseded turn was handed off")
	}
	if n := privateCount(srv, id, actTurnClosed); n != 1 {
		t.Fatalf("a superseded turn wrote a close, got %d", n)
	}
	if got := len(agentPosts(srv, id)); got != 0 {
		t.Fatalf("a superseded turn ran: %d posts", got)
	}
	srv.hermes.mu.Lock()
	_, running := srv.hermes.running[id]
	srv.hermes.mu.Unlock()
	if running {
		t.Fatal("the in-flight record must clear")
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
	// three attempts that each died before the hand-off (dispatchMarked, no
	// turn-dispatched): effect-free to repeat, but bounded
	for i := 0; i < hermesTurnRetries; i++ {
		srv.hermesTurnMark(id, actTurnOpen, map[string]any{
			"agent": "agent:alfred", "phase": "comment", "intent": "info", "text": "again?", "dispatchMarked": true})
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
	if len(posts) != 1 || !strings.Contains(posts[0].Text, "interrupted 3 time(s)") {
		t.Fatalf("giving up must be visible in the thread: %+v", posts)
	}
}
