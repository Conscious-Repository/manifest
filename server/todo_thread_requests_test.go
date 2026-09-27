package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/ledger"
	"manifest/threads"
)

func postTaskThread(t *testing.T, srv *Server, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	srv.handleTaskThreadPost(w, httptest.NewRequest("POST", "/api/tasks/thread", strings.NewReader(string(raw))))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func ownerComments(srv *Server, id string) []threads.Comment {
	var out []threads.Comment
	for _, c := range srv.listThread(id) {
		if c.Author == srv.ownerIdentity().ID && c.Action == threads.ActComment {
			out = append(out, c)
		}
	}
	return out
}

// ws2 audit defect: POST /api/tasks/thread had no request ID, so a lost
// acknowledgment plus an explicit retry posted a second comment and spent a
// second agent turn. With a request ID the retry recovers the first outcome.
func TestTaskThreadPostRequestIDIsIdempotent(t *testing.T) {
	srv := loopFixture(t)
	srv.UseHermes(hermesStub(t, filepath.Join(t.TempDir(), "never")), "web")
	id := "inbox/research-zoning"
	if _, ok := srv.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	ask := map[string]any{"id": id, "text": "what is parcel 12 zoned?", "mode": "ask", "requestId": "req-thread-0001"}
	code, first := postTaskThread(t, srv, ask)
	if code != 200 || first["replayed"] == true {
		t.Fatal(code, first)
	}
	waitFor(t, "the answer", func() bool { return len(agentPosts(srv, id)) == 1 && privateCount(srv, id, actTurnClosed) == 1 })
	// the acknowledgment was lost; the client retries the same request
	code, again := postTaskThread(t, srv, ask)
	if code != 200 || again["replayed"] != true || again["dispatch"] != "recorded" {
		t.Fatal(code, again)
	}
	if again["comment"].(map[string]any)["id"] != first["comment"].(map[string]any)["id"] {
		t.Fatalf("retry must return the recorded comment: %v vs %v", again["comment"], first["comment"])
	}
	time.Sleep(100 * time.Millisecond) // a second dispatch would have answered by now
	if n := len(ownerComments(srv, id)); n != 1 {
		t.Fatalf("one comment for one request, got %d", n)
	}
	if n := privateCount(srv, id, actTurnOpen); n != 1 {
		t.Fatalf("one agent turn for one request, got %d", n)
	}
	if n := len(agentPosts(srv, id)); n != 1 {
		t.Fatalf("one answer for one request, got %d", n)
	}
	// same ID, different payload: a conflict, nothing posted
	changed := map[string]any{"id": id, "text": "what is parcel 13 zoned?", "mode": "ask", "requestId": "req-thread-0001"}
	if code, out := postTaskThread(t, srv, changed); code != 409 {
		t.Fatal(code, out)
	}
	if n := len(ownerComments(srv, id)); n != 1 {
		t.Fatalf("conflict must not post, got %d comments", n)
	}
	// receipts are markers: no thread view shows them
	for _, c := range srv.listThread(id) {
		if c.Action == actRequestOpen || c.Action == actRequestClosed {
			t.Fatalf("receipt leaked into the thread: %+v", c)
		}
	}
	// a request without an ID keeps the old contract (each post is new)
	for i := 0; i < 2; i++ {
		if code, out := postTaskThread(t, srv, map[string]any{"id": id, "text": "plain note"}); code != 200 {
			t.Fatal(code, out)
		}
	}
	if n := len(ownerComments(srv, id)); n != 3 {
		t.Fatalf("ID-less comments stay independent, got %d", n)
	}
	if code, _ := postTaskThread(t, srv, map[string]any{"id": id, "text": "x", "requestId": "bad id!"}); code != 400 {
		t.Fatal("invalid request ID must be refused", code)
	}
}

// The process died after the comment was written and before the receipt
// closed. The retry finds the comment, reports the dispatch uncertain and
// runs nothing; with no comment on the thread nothing happened, so the
// request proceeds once.
func TestTaskThreadPostReconcilesUnclosedReceipt(t *testing.T) {
	srv := loopFixture(t)
	srv.UseHermes(hermesStub(t, filepath.Join(t.TempDir(), "never")), "web")
	id := "inbox/research-zoning"
	if _, ok := srv.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	body := map[string]any{"id": id, "text": "note one", "requestId": "req-thread-lost"}
	var b taskThreadPost
	raw, _ := json.Marshal(body)
	_ = json.Unmarshal(raw, &b)
	srv.markerAddMeta(id, actRequestOpen, "", map[string]any{"requestId": "req-thread-lost", "fingerprint": b.fingerprint(id)})
	time.Sleep(2 * time.Millisecond)
	if _, err := srv.addThreadEntry(srv.ownerIdentity(), id, threads.ActComment, "note one", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	code, out := postTaskThread(t, srv, body)
	if code != 200 || out["replayed"] != true || out["dispatch"] != "uncertain" {
		t.Fatal(code, out)
	}
	if n := len(ownerComments(srv, id)); n != 1 {
		t.Fatalf("reconciled retry must not re-post, got %d", n)
	}
	// and the reconciliation is durable: a later retry says the same
	if _, again := postTaskThread(t, srv, body); again["dispatch"] != "uncertain" {
		t.Fatal(again)
	}

	body2 := map[string]any{"id": id, "text": "note two", "requestId": "req-thread-none"}
	raw, _ = json.Marshal(body2)
	var b2 taskThreadPost
	_ = json.Unmarshal(raw, &b2)
	srv.markerAddMeta(id, actRequestOpen, "", map[string]any{"requestId": "req-thread-none", "fingerprint": b2.fingerprint(id)})
	if code, out := postTaskThread(t, srv, body2); code != 200 || out["replayed"] == true {
		t.Fatal(code, out)
	}
	if code, out := postTaskThread(t, srv, body2); code != 200 || out["replayed"] != true {
		t.Fatal(code, out)
	}
	if n := len(ownerComments(srv, id)); n != 2 {
		t.Fatalf("want note one + note two once each, got %d", n)
	}
}

// Owner decision D4 (2026-09-27): a task-thread turn reaches Hermes ONCE.
// This test used to pin the sweep's replay count (4fbea1c: three attempts in
// all, the original plus two re-dispatches). The owner rejected that default
// because a repeated send can duplicate work. It now pins the new behaviour:
//   - a turn that was handed to the runner and then interrupted is closed with
//     a visible note and never re-sent, across any number of restarts;
//   - only a turn that provably never reached the runner (a dispatchMarked
//     open with no turn-dispatched after it) is re-dispatched, which is
//     effect-free, as its own visible run and ledger entry, bounded by
//     hermesTurnRetries (3 attempts; TestHermesTurnRetryCap).
// Changing either rule must change this test deliberately.
func TestHermesTurnSweepRedispatchIsVisibleAndPinned(t *testing.T) {
	if hermesTurnRetries != 3 {
		t.Fatalf("hermesTurnRetries is %d; it bounds attempts that never reached the runner — update this pin deliberately", hermesTurnRetries)
	}
	dirs := loopDirs{t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()}
	ledgerDir := t.TempDir()
	id := "inbox/research-zoning"
	var hangs []string
	var servers []*Server
	boot := func() *Server {
		srv := loopFixtureAt(t, dirs)
		hang := filepath.Join(t.TempDir(), "hang")
		if err := os.WriteFile(hang, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		srv.UseHermes(hermesStub(t, hang), "web")
		led, err := ledger.New(ledgerDir, time.UTC)
		if err != nil {
			t.Fatal(err)
		}
		srv.UseLedger(led)
		hangs, servers = append(hangs, hang), append(servers, srv)
		return srv
	}
	t.Cleanup(func() {
		for i, srv := range servers {
			_ = os.Remove(hangs[i])
			waitFor(t, "a simulated dead worker to finish", func() bool {
				srv.hermes.mu.Lock()
				defer srv.hermes.mu.Unlock()
				return len(srv.hermes.running) == 0
			})
		}
		time.Sleep(50 * time.Millisecond)
	})
	ledgerCount := func(kind string) int {
		n := 0
		_ = filepath.Walk(ledgerDir, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				raw, _ := os.ReadFile(p)
				n += strings.Count(string(raw), kind)
			}
			return nil
		})
		return n
	}
	first := boot()
	if _, ok := first.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	if err := first.setPlanAssignee(id, "agent:hermes"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.postAndDispatch(id, "ask", "", nil, nil, "what is parcel 12 zoned?"); err != nil {
		t.Fatal(err)
	}
	// the turn is handed to the runner (and hangs there: the process dies mid-turn)
	waitFor(t, "the hand-off record", func() bool { return privateCount(first, id, actTurnDispatched) == 1 })
	sv := first.taskThreadSupervision(id)
	if len(sv.Runs) != 1 || sv.Runs[0].State != supervisionRunning || sv.Runs[0].Attempt != 0 {
		t.Fatalf("original turn: %+v", sv.Runs)
	}
	// two restarts: a handed-off turn is never re-sent
	var last *Server
	for restart := 1; restart <= 2; restart++ {
		last = boot()
		sweep(last)
		sweep(last)
		if n := privateCount(last, id, actTurnOpen); n != 1 {
			t.Fatalf("restart %d: a handed-off turn was re-sent (%d turn-opens)", restart, n)
		}
		if n := privateCount(last, id, actTurnDispatched); n != 1 {
			t.Fatalf("restart %d: %d hand-offs, want the original one", restart, n)
		}
		if n := privateCount(last, id, actTurnRedispatch); n != 0 {
			t.Fatalf("restart %d: %d re-dispatch records", restart, n)
		}
	}
	sv = last.taskThreadSupervision(id)
	if len(sv.Runs) != 1 || sv.Runs[0].State != supervisionFailed || sv.State != supervisionFailed || !strings.Contains(sv.Runs[0].Evidence, "not re-sent") {
		t.Fatalf("interrupted after hand-off: %+v", sv)
	}
	posts := agentPosts(last, id)
	if len(posts) != 1 || !strings.Contains(posts[0].Text, "was not re-sent because it may already have acted") {
		t.Fatalf("the owner must be told, once: %+v", posts)
	}
	if n := ledgerCount("run.redispatched"); n != 0 {
		t.Fatalf("no re-dispatch may be logged, got %d", n)
	}

	// A turn the process died on BEFORE the hand-off (open written, no
	// dispatch) never reached Hermes: re-sending it is effect-free, so the
	// sweep does, once, as its own visible run and ledger entry.
	if _, err := last.addThreadEntry(threads.Identity{ID: "owner", Name: "Owner"}, id, threads.ActComment, "and parcel 13?", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	last.hermesTurnMark(id, actTurnOpen, map[string]any{"agent": "agent:alfred", "phase": "comment", "intent": "info", "text": "and parcel 13?", "dispatchMarked": true})
	fresh := boot()
	_ = os.Remove(hangs[len(hangs)-1]) // this runner answers
	sweep(fresh)
	waitFor(t, "the re-dispatched turn to close", func() bool { return privateCount(fresh, id, actTurnClosed) == 2 })
	if n := privateCount(fresh, id, actTurnRedispatch); n != 1 {
		t.Fatalf("one re-dispatch record, got %d", n)
	}
	if n := privateCount(fresh, id, actTurnDispatched); n != 2 {
		t.Fatalf("the re-dispatch hands off exactly once more, got %d hand-offs", n)
	}
	sv = fresh.taskThreadSupervision(id)
	if len(sv.Runs) != 3 {
		t.Fatalf("runs: %+v", sv.Runs)
	}
	replay := sv.Runs[2]
	if replay.Attempt != 2 || replay.ReplayOf != sv.Runs[1].RequestID || replay.State != supervisionReady || !strings.Contains(replay.Evidence, "re-dispatch attempt") {
		t.Fatalf("re-dispatch run: %+v", replay)
	}
	if prev := sv.Runs[1]; prev.State != supervisionDisconnected {
		t.Fatalf("the undelivered attempt must read disconnected: %+v", prev)
	}
	if n := ledgerCount("run.redispatched"); n != 1 {
		t.Fatalf("the re-dispatch is one ledger entry, got %d", n)
	}
	sweep(fresh)
	sweep(fresh)
	if n := privateCount(fresh, id, actTurnOpen); n != 3 {
		t.Fatalf("a settled chain is not re-sent, got %d opens", n)
	}
	replies := 0
	for _, p := range agentPosts(fresh, id) {
		if strings.Contains(p.Text, "parcel 12 is zoned R-1") {
			replies++
		}
	}
	if replies != 1 {
		t.Fatalf("the re-dispatched turn answers exactly once, got %d", replies)
	}
	// the GET route carries the projection
	w := httptest.NewRecorder()
	fresh.handleTaskThreadGet(w, httptest.NewRequest("GET", "/api/tasks/thread?id="+id, nil))
	var got struct{ Supervision chatSupervision }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Supervision.Runs) != 3 || got.Supervision.Capabilities.Retry != "restart-redispatch" {
		t.Fatalf("GET supervision: %s", w.Body.String())
	}
}
