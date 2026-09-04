package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// seedStore writes a three-day fixture: two tasks, one chat session, one run,
// mixed actors, in a deliberate order so ordering assertions mean something.
func seedStore(t *testing.T) (*Store, []Entry) {
	t.Helper()
	st, err := New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	d1 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	seed := []Entry{
		{TS: d1, Source: "thread", Kind: "thread.comment", Actor: "owner", Object: Object{Kind: ObjTask, ID: "inbox/a"}, Task: "inbox/a", Text: "first"},
		{TS: d1.Add(time.Minute), Source: "chat", Kind: "chat.user", Actor: "owner", Object: Object{Kind: ObjSession, ID: "s1"}, Session: "s1", Text: "hi"},
		{TS: d1.Add(2 * time.Minute), Source: "chat", Kind: "chat.assistant", Actor: "agent:alfred", Object: Object{Kind: ObjSession, ID: "s1"}, Session: "s1", Text: "hello"},
		{TS: d2, Source: "thread", Kind: "thread.assign", Actor: "owner", Object: Object{Kind: ObjTask, ID: "inbox/b"}, Task: "inbox/b"},
		{TS: d2.Add(time.Minute), Source: "run", Kind: "run.completed", Actor: "hermes", Object: Object{Kind: ObjRun, ID: "r1"}, Run: "r1", Task: "inbox/a"},
		{TS: d3, Source: "plan", Kind: "plan.materialized", Actor: "agent:hermes", Object: Object{Kind: ObjTask, ID: "inbox/a"}, Task: "inbox/a", Ref: "plans/a.md"},
		{TS: d3.Add(time.Minute), Source: "chat", Kind: "chat.promoted", Actor: "owner", Object: Object{Kind: ObjSession, ID: "s1"}, Session: "s1", Task: "inbox/b"},
	}
	for _, e := range seed {
		if err := st.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	return st, seed
}

func kinds(es []Entry) string {
	var out []string
	for _, e := range es {
		out = append(out, e.Kind)
	}
	return strings.Join(out, ",")
}

func TestObjectRoundTrip(t *testing.T) {
	st, err := New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	if err := st.Append(Entry{TS: ts, Source: "thread", Kind: "thread.comment", Actor: "owner",
		Object: Object{Kind: ObjTask, ID: "aion:42"}, Task: "aion:42", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Day("2026-09-04")
	if err != nil || len(got) != 1 {
		t.Fatalf("day: %v %+v", err, got)
	}
	if got[0].Object != (Object{Kind: ObjTask, ID: "aion:42"}) || got[0].Task != "aion:42" {
		t.Fatalf("object round trip: %+v", got[0])
	}
	// the wire form: an explicit object key, the legacy todo key untouched
	b, _ := os.ReadFile(filepath.Join(st.Dir(), "2026-09-04.jsonl"))
	if !strings.Contains(string(b), `"object":{"kind":"task","id":"aion:42"}`) || !strings.Contains(string(b), `"todo":"aion:42"`) {
		t.Fatalf("wire form: %s", b)
	}
}

func TestAppendStampsDerivedObject(t *testing.T) {
	st, err := New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	// a writer that only set the legacy fields: task wins over session over run
	_ = st.Append(Entry{TS: ts, Source: "run", Kind: "run.completed", Actor: "hermes", Task: "inbox/x", Run: "r9"})
	_ = st.Append(Entry{TS: ts, Source: "chat", Kind: "chat.user", Actor: "owner", Session: "s9", Run: "r9"})
	_ = st.Append(Entry{TS: ts, Source: "run", Kind: "run.failed", Actor: "hermes", Run: "r9"})
	_ = st.Append(Entry{TS: ts, Source: "run", Kind: "run.failed", Actor: "hermes"}) // nothing derivable
	got, _ := st.Day("2026-09-04")
	want := []Object{{ObjTask, "inbox/x"}, {ObjSession, "s9"}, {ObjRun, "r9"}, {}}
	for i, e := range got {
		if e.Object != want[i] {
			t.Errorf("entry %d object = %+v, want %+v", i, e.Object, want[i])
		}
	}
	// an untagged line omits the key entirely — no `"object":{}` noise
	b, _ := os.ReadFile(filepath.Join(st.Dir(), "2026-09-04.jsonl"))
	if strings.Contains(string(b), `"object":{"kind":"","id":""}`) {
		t.Fatalf("empty object must be omitted: %s", b)
	}
}

func TestLegacyLinesStayReadableAndQueryable(t *testing.T) {
	// lines written before the object tag existed, byte-for-byte as the old
	// writers produced them
	dir := t.TempDir()
	legacy := `{"ts":"2026-08-15T05:00:00Z","source":"run","kind":"run.completed","actor":"hermes","todo":"inbox/research-zoning","run":"r1","harness":"hermes","text":"work the zoning memo"}
{"ts":"2026-08-15T12:05:01Z","source":"chat","kind":"chat.assistant","actor":"concierge","session":"20260815-120000-ab12","text":"the full answer"}
{"ts":"2026-08-15T13:00:00Z","source":"thread","kind":"thread.comment","actor":"owner","todo":"inbox/research-zoning","text":"a real comment"}
`
	if err := os.WriteFile(filepath.Join(dir, "2026-08-15.jsonl"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := New(dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	day, err := st.Day("2026-08-15")
	if err != nil || len(day) != 3 {
		t.Fatalf("legacy day: %v %+v", err, day)
	}
	if !day[0].Object.IsZero() || day[0].Task != "inbox/research-zoning" {
		t.Fatalf("legacy line must decode with an unset object and its fields intact: %+v", day[0])
	}
	// derived refs: the run line names the task AND the run
	if objs := day[0].Objects(); len(objs) != 2 || objs[0] != (Object{ObjTask, "inbox/research-zoning"}) || objs[1] != (Object{ObjRun, "r1"}) {
		t.Fatalf("derived objects: %+v", objs)
	}
	// object-scoped query finds legacy lines through the derived refs
	es, err := st.Events(Query{Object: "inbox/research-zoning", ObjectKind: ObjTask})
	if err != nil || kinds(es) != "run.completed,thread.comment" {
		t.Fatalf("legacy task query: %v %s", err, kinds(es))
	}
	if es, _ := st.Events(Query{Object: "r1"}); kinds(es) != "run.completed" {
		t.Fatalf("legacy run query: %s", kinds(es))
	}
	if es, _ := st.Events(Query{Object: "20260815-120000-ab12", ObjectKind: ObjSession}); kinds(es) != "chat.assistant" {
		t.Fatalf("legacy session query: %s", kinds(es))
	}
	// and the file is untouched — reads never rewrite
	b, _ := os.ReadFile(filepath.Join(dir, "2026-08-15.jsonl"))
	if string(b) != legacy {
		t.Fatalf("read rewrote the day file:\n%s", b)
	}
}

func TestEventsUnfilteredIsEveryLineInOrder(t *testing.T) {
	st, seed := seedStore(t)
	es, err := st.Events(Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != len(seed) {
		t.Fatalf("want %d entries, got %d", len(seed), len(es))
	}
	for i := range es {
		if es[i].Kind != seed[i].Kind || !es[i].TS.Equal(seed[i].TS) {
			t.Fatalf("order broke at %d: %+v vs %+v", i, es[i], seed[i])
		}
		if i > 0 && es[i].TS.Before(es[i-1].TS) {
			t.Fatalf("not ascending at %d", i)
		}
	}
}

func TestEventsObjectScoped(t *testing.T) {
	st, _ := seedStore(t)
	cases := []struct {
		name string
		q    Query
		want string
	}{
		{"task a explicit + run's related ref", Query{Object: "inbox/a", ObjectKind: ObjTask}, "thread.comment,run.completed,plan.materialized"},
		{"task b incl. the promotion that created it", Query{Object: "inbox/b", ObjectKind: ObjTask}, "thread.assign,chat.promoted"},
		{"session s1", Query{Object: "s1", ObjectKind: ObjSession}, "chat.user,chat.assistant,chat.promoted"},
		{"any kind by id", Query{Object: "r1"}, "run.completed"},
		{"wrong kind, right id", Query{Object: "r1", ObjectKind: ObjTask}, ""},
		{"kind only: every task-scoped line", Query{ObjectKind: ObjTask}, "thread.comment,thread.assign,run.completed,plan.materialized,chat.promoted"},
		{"kind filter", Query{Kinds: []string{"chat.user", "chat.assistant"}}, "chat.user,chat.assistant"},
		{"source filter", Query{Source: "thread"}, "thread.comment,thread.assign"},
		{"actor filter", Query{Actor: "agent:hermes"}, "plan.materialized"},
		{"actor + object", Query{Actor: "owner", Object: "s1"}, "chat.user,chat.promoted"},
		{"unknown object", Query{Object: "nope"}, ""},
	}
	for _, c := range cases {
		es, err := st.Events(c.q)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := kinds(es); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestEventsWindowAndLimit(t *testing.T) {
	st, _ := seedStore(t)
	d2 := time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)
	// since is inclusive: the d2 09:00 line rides
	if es, _ := st.Events(Query{Since: d2}); kinds(es) != "thread.assign,run.completed,plan.materialized,chat.promoted" {
		t.Fatalf("since: %s", kinds(es))
	}
	// until is exclusive: the d2 09:00 line does not
	if es, _ := st.Events(Query{Until: d2}); kinds(es) != "thread.comment,chat.user,chat.assistant" {
		t.Fatalf("until: %s", kinds(es))
	}
	// half-open window on one day
	if es, _ := st.Events(Query{Since: d2, Until: d2.Add(24 * time.Hour)}); kinds(es) != "thread.assign,run.completed" {
		t.Fatalf("window: %s", kinds(es))
	}
	// limit keeps the most recent n, still in file order
	if es, _ := st.Events(Query{Limit: 2}); kinds(es) != "plan.materialized,chat.promoted" {
		t.Fatalf("limit: %s", kinds(es))
	}
	if es, _ := st.Events(Query{Object: "inbox/a", Limit: 1}); kinds(es) != "plan.materialized" {
		t.Fatalf("limit+object: %s", kinds(es))
	}
}

func TestEventsWindowUsesLocalCalendar(t *testing.T) {
	chi, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("no tzdata")
	}
	st, err := New(t.TempDir(), chi)
	if err != nil {
		t.Fatal(err)
	}
	// 23:30 Chicago Sep 1 = 04:30 UTC Sep 2 → files under 2026-09-01
	late := time.Date(2026, 9, 1, 23, 30, 0, 0, chi)
	_ = st.Append(Entry{TS: late, Source: "chat", Kind: "chat.user", Actor: "owner", Session: "s"})
	// a since of Sep 2 00:00 UTC (still Sep 1 in Chicago) must open the Sep 1 file and find it
	es, err := st.Events(Query{Since: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil || len(es) != 1 {
		t.Fatalf("local-day window: %v %+v", err, es)
	}
}

func TestHistoryReconstructsOneObject(t *testing.T) {
	st, _ := seedStore(t)
	h, err := st.History(ObjTask, "inbox/a")
	if err != nil {
		t.Fatal(err)
	}
	if kinds(h.Entries) != "thread.comment,run.completed,plan.materialized" {
		t.Fatalf("entries: %s", kinds(h.Entries))
	}
	if h.Object != (Object{ObjTask, "inbox/a"}) {
		t.Fatalf("object: %+v", h.Object)
	}
	if !h.First.Equal(time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)) || !h.Last.Equal(time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("first/last: %v %v", h.First, h.Last)
	}
	if strings.Join(h.Actors, ",") != "owner,hermes,agent:hermes" {
		t.Fatalf("actors: %v", h.Actors)
	}
	if strings.Join(h.Kinds, ",") != "thread.comment,run.completed,plan.materialized" {
		t.Fatalf("kinds: %v", h.Kinds)
	}
	// unknown object: an empty, well-formed history — not an error
	h, err = st.History(ObjTask, "never")
	if err != nil || len(h.Entries) != 0 || !h.First.IsZero() || h.Actors == nil || h.Kinds == nil {
		t.Fatalf("empty history: %v %+v", err, h)
	}
	if _, err := st.History(ObjTask, " "); err == nil {
		t.Fatal("empty id must error")
	}
	// the JSON shape: a zero first/last is omitted, lists never null
	b, _ := json.Marshal(h)
	if strings.Contains(string(b), `"first"`) || strings.Contains(string(b), `null`) {
		t.Fatalf("history json: %s", b)
	}
}

func TestReadsNeverWriteAndDirHoldsOnlyTheLog(t *testing.T) {
	st, _ := seedStore(t)
	before := map[string]string{}
	entries, _ := os.ReadDir(st.Dir())
	for _, f := range entries {
		b, _ := os.ReadFile(filepath.Join(st.Dir(), f.Name()))
		before[f.Name()] = string(b)
	}
	_, _ = st.Events(Query{Object: "inbox/a"})
	_, _ = st.Events(Query{Kinds: []string{"chat.user"}, Limit: 1})
	_, _ = st.History(ObjSession, "s1")
	_, _ = st.Day("2026-09-01")
	after, _ := os.ReadDir(st.Dir())
	if len(after) != len(before) {
		t.Fatalf("a read created a file: %v", after)
	}
	for _, f := range after {
		if !strings.HasSuffix(f.Name(), ".jsonl") || !validDate(strings.TrimSuffix(f.Name(), ".jsonl")) {
			t.Fatalf("non-log file in the ledger dir: %s", f.Name())
		}
		b, _ := os.ReadFile(filepath.Join(st.Dir(), f.Name()))
		if string(b) != before[f.Name()] {
			t.Fatalf("a read rewrote %s", f.Name())
		}
	}
	// and an append only ever grows the day file
	_ = st.Append(Entry{TS: time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC), Source: "thread", Kind: "thread.comment", Actor: "owner", Task: "inbox/a"})
	b, _ := os.ReadFile(filepath.Join(st.Dir(), "2026-09-03.jsonl"))
	if !strings.HasPrefix(string(b), before["2026-09-03.jsonl"]) || len(b) <= len(before["2026-09-03.jsonl"]) {
		t.Fatal("append must extend the existing bytes, never rewrite them")
	}
}

func TestConcurrentAppendsAndQueries(t *testing.T) {
	st, err := New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = st.Append(Entry{TS: ts, Source: "thread", Kind: "thread.comment", Actor: "owner", Task: "inbox/c"})
		}()
		go func() {
			defer wg.Done()
			if _, err := st.Events(Query{Object: "inbox/c"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	es, err := st.Events(Query{Object: "inbox/c", ObjectKind: ObjTask})
	if err != nil || len(es) != 20 {
		t.Fatalf("want 20 entries after concurrent append+query, got %d (%v)", len(es), err)
	}
}
