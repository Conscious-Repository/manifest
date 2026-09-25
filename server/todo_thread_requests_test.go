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

// The one deliberate replay (4fbea1c): a task-thread turn the process died on
// is re-dispatched by hermesTurnSweep. This pins the current count — three
// attempts in all (the original plus two re-dispatches), then a visible
// give-up — and makes each re-dispatch its own supervision run and ledger
// entry. Changing hermesTurnRetries must change this test deliberately.
func TestHermesTurnSweepRedispatchIsVisibleAndPinned(t *testing.T) {
	if hermesTurnRetries != 3 {
		t.Fatalf("hermesTurnRetries is %d; the retry count is an owner decision — update this pin deliberately", hermesTurnRetries)
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
	sv := first.taskThreadSupervision(id)
	if len(sv.Runs) != 1 || sv.Runs[0].State != supervisionRunning || sv.Runs[0].Attempt != 0 {
		t.Fatalf("original turn: %+v", sv.Runs)
	}
	// two restarts, each dying mid-turn: the sweep re-dispatches twice
	for attempt := 2; attempt <= 3; attempt++ {
		srv := boot()
		sweep(srv)
		if n := privateCount(srv, id, actTurnOpen); n != attempt {
			t.Fatalf("attempt %d: want %d turn-opens, got %d", attempt, attempt, n)
		}
		sv := srv.taskThreadSupervision(id)
		if len(sv.Runs) != attempt {
			t.Fatalf("each re-dispatch is its own run: %+v", sv.Runs)
		}
		last := sv.Runs[attempt-1]
		if last.Attempt != attempt || last.ReplayOf != sv.Runs[0].RequestID || last.State != supervisionRunning || !strings.Contains(last.Evidence, "re-dispatch attempt") {
			t.Fatalf("re-dispatch run: %+v", last)
		}
		if prev := sv.Runs[attempt-2]; prev.State != supervisionDisconnected {
			t.Fatalf("the interrupted attempt must read disconnected, not running or done: %+v", prev)
		}
	}
	// third restart: the cap is reached, nothing is re-sent, the give-up is visible
	last := boot()
	sweep(last)
	sweep(last)
	if n := privateCount(last, id, actTurnOpen); n != 3 {
		t.Fatalf("the cap allows three attempts in all, got %d", n)
	}
	if n := privateCount(last, id, actTurnRedispatch); n != 2 {
		t.Fatalf("two re-dispatch records, got %d", n)
	}
	sv = last.taskThreadSupervision(id)
	if len(sv.Runs) != 3 || sv.Runs[2].State != supervisionFailed || sv.State != supervisionFailed {
		t.Fatalf("abandoned chain: %+v", sv)
	}
	posts := agentPosts(last, id)
	if len(posts) != 1 || !strings.Contains(posts[0].Text, "interrupted 3 time(s)") {
		t.Fatalf("give-up note: %+v", posts)
	}
	logged := 0
	_ = filepath.Walk(ledgerDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			raw, _ := os.ReadFile(p)
			logged += strings.Count(string(raw), "run.redispatched")
		}
		return nil
	})
	if logged != 2 {
		t.Fatalf("each re-dispatch is a ledger entry, got %d", logged)
	}
	// the GET route carries the projection
	w := httptest.NewRecorder()
	last.handleTaskThreadGet(w, httptest.NewRequest("GET", "/api/tasks/thread?id="+id, nil))
	var got struct{ Supervision chatSupervision }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Supervision.Runs) != 3 || got.Supervision.Capabilities.Retry != "restart-redispatch" {
		t.Fatalf("GET supervision: %s", w.Body.String())
	}
}
