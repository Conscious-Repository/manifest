package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"manifest/ledger"
	"manifest/threads"
)

func ledgerGET(t *testing.T, srv *Server, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var out map[string]any
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: bad json: %v\n%s", path, err, rec.Body.String())
		}
	}
	return rec.Code, out
}

func entryKinds(v any) []string {
	var out []string
	for _, e := range v.([]any) {
		out = append(out, e.(map[string]any)["kind"].(string))
	}
	return out
}

func TestLedgerEventsEndpoint(t *testing.T) {
	srv, led := ledgerFixture(t)
	base := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	seed := []ledger.Entry{
		{TS: base, Source: "chat", Kind: "chat.user", Actor: "owner", Session: "s1", Text: "hi"},
		{TS: base.Add(time.Minute), Source: "chat", Kind: "chat.assistant", Actor: "agent:alfred", Session: "s1", Text: "hello"},
		{TS: base.Add(24 * time.Hour), Source: "thread", Kind: "thread.comment", Actor: "owner", Task: "inbox/a", Text: "note"},
		{TS: base.Add(25 * time.Hour), Source: "run", Kind: "run.completed", Actor: "hermes", Run: "r1", Task: "inbox/a"},
	}
	for _, e := range seed {
		if err := led.Append(e); err != nil {
			t.Fatal(err)
		}
	}

	code, r := ledgerGET(t, srv, "/api/ledger/events")
	if code != 200 || r["count"].(float64) != 4 {
		t.Fatalf("unfiltered: %d %+v", code, r)
	}
	if k := entryKinds(r["entries"]); k[0] != "chat.user" || k[3] != "run.completed" {
		t.Fatalf("file order: %v", k)
	}
	// the wire carries the object tag Append stamped
	first := r["entries"].([]any)[0].(map[string]any)
	if obj, _ := first["object"].(map[string]any); obj == nil || obj["kind"] != "session" || obj["id"] != "s1" {
		t.Fatalf("object on the wire: %+v", first)
	}

	cases := []struct {
		path string
		want []string
	}{
		{"/api/ledger/events?kind=chat.user", []string{"chat.user"}},
		{"/api/ledger/events?kind=chat.user,run.completed", []string{"chat.user", "run.completed"}},
		{"/api/ledger/events?object=inbox/a&objectKind=task", []string{"thread.comment", "run.completed"}},
		{"/api/ledger/events?object=s1", []string{"chat.user", "chat.assistant"}},
		{"/api/ledger/events?object=s1&objectKind=task", nil},
		{"/api/ledger/events?actor=owner", []string{"chat.user", "thread.comment"}},
		{"/api/ledger/events?source=run", []string{"run.completed"}},
		{"/api/ledger/events?since=2026-09-03", []string{"thread.comment", "run.completed"}},
		{"/api/ledger/events?until=2026-09-02", []string{"chat.user", "chat.assistant"}}, // bare until = through that day
		{"/api/ledger/events?since=2026-09-02T09:00:30Z&until=2026-09-03T09:00:00Z", []string{"chat.assistant"}},
		{"/api/ledger/events?limit=1", []string{"run.completed"}},
		{"/api/ledger/events?object=inbox/a&limit=1", []string{"run.completed"}},
	}
	for _, c := range cases {
		code, r := ledgerGET(t, srv, c.path)
		if code != 200 {
			t.Errorf("%s: %d", c.path, code)
			continue
		}
		got := entryKinds(r["entries"])
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v want %v", c.path, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v want %v", c.path, got, c.want)
				break
			}
		}
	}
	for _, bad := range []string{"/api/ledger/events?since=yesterday", "/api/ledger/events?limit=-1", "/api/ledger/events?limit=x"} {
		if code, _ := ledgerGET(t, srv, bad); code != 400 {
			t.Errorf("%s: want 400, got %d", bad, code)
		}
	}
	// the day view still works beside the query
	if code, r := ledgerGET(t, srv, "/api/ledger?date=2026-09-03"); code != 200 || len(r["entries"].([]any)) != 2 {
		t.Fatalf("day view: %d %+v", code, r)
	}
}

func TestLedgerHistoryEndpoint(t *testing.T) {
	srv, led := ledgerFixture(t)
	id := "inbox/research-zoning"
	// a real writer tags the task
	if _, err := srv.addThreadEntry(srv.ownerIdentity(), id, threads.ActComment, "kick off", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	// and a legacy untagged line (pre-Phase-1 writer) still lands in the same history
	if err := led.Append(ledger.Entry{TS: time.Now().Add(time.Second), Source: "run", Kind: "run.completed", Actor: "hermes", Task: id, Run: "r7"}); err != nil {
		t.Fatal(err)
	}
	code, r := ledgerGET(t, srv, "/api/ledger/history?object="+id+"&objectKind=task")
	if code != 200 {
		t.Fatalf("history: %d", code)
	}
	if k := entryKinds(r["entries"]); len(k) != 2 || k[0] != "thread.comment" || k[1] != "run.completed" {
		t.Fatalf("history entries: %v", k)
	}
	obj := r["object"].(map[string]any)
	if obj["kind"] != "task" || obj["id"] != id {
		t.Fatalf("history object: %+v", obj)
	}
	if a := r["actors"].([]any); len(a) != 2 || a[0] != "owner" || a[1] != "hermes" {
		t.Fatalf("history actors: %v", a)
	}
	if _, ok := r["first"]; !ok {
		t.Fatalf("history first/last missing: %+v", r)
	}
	if code, _ := ledgerGET(t, srv, "/api/ledger/history"); code != 400 {
		t.Fatalf("history without object: %d", code)
	}
	if code, r := ledgerGET(t, srv, "/api/ledger/history?object=nothing"); code != 200 || len(r["entries"].([]any)) != 0 {
		t.Fatalf("unknown object: %d %+v", code, r)
	}
}

func TestLedgerEndpointsWithoutStore(t *testing.T) {
	srv, _ := panelFixture(t)
	if code, r := ledgerGET(t, srv, "/api/ledger/events?kind=chat.user"); code != 200 || r["count"].(float64) != 0 {
		t.Fatalf("no store: %d %+v", code, r)
	}
	if code, r := ledgerGET(t, srv, "/api/ledger/history?object=x"); code != 200 || len(r["entries"].([]any)) != 0 {
		t.Fatalf("no store history: %d %+v", code, r)
	}
}
