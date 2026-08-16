package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"manifest/ledger"
	"manifest/spirits"
	"manifest/threads"
)

// ledgerFixture: panelFixture + a two-harness federation (primary with a real
// spirits tree for chat, hermes for runs) + a wired ledger.
func ledgerFixture(t *testing.T) (*Server, *ledger.Store) {
	t.Helper()
	srv, _ := panelFixture(t)
	primary := spirits.NewStore(t.TempDir())
	hermes := spirits.NewStore(t.TempDir())
	srv.UseHarnesses([]Harness{{Name: "excalibur", Spirits: primary}, {Name: "hermes", Spirits: hermes}})
	led, err := ledger.New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	srv.UseLedger(led)
	return srv, led
}

func allLedgerEntries(t *testing.T, led *ledger.Store) []ledger.Entry {
	t.Helper()
	var out []ledger.Entry
	for _, d := range led.Days() {
		es, err := led.Day(d)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, es...)
	}
	return out
}

func countKind(es []ledger.Entry, kind string) int {
	n := 0
	for _, e := range es {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func TestLedgerSweepRunsAndChat(t *testing.T) {
	srv, led := ledgerFixture(t)
	// a completed hermes run report carrying a todo token
	hermes := srv.eachHarness()[1].Spirits
	if err := os.MkdirAll(filepath.Join(hermes.Root(), "artifacts", "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	report := `---
run: r1
spirit: hermes
ritual: delegate
request: "work the zoning memo [todo:: inbox/research-zoning] [phase:: plan]"
started: 2026-08-15T05:00:00Z
finished: 2026-08-15T05:01:00Z
outcome: completed
---
ran
`
	if err := os.WriteFile(filepath.Join(hermes.Root(), "artifacts", "runs", "2026-08-15-hermes-r1.md"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	// an engine-authored chat session with one assistant turn
	primary := srv.eachHarness()[0].Spirits
	sid := "20260815-120000-ab12"
	if err := os.MkdirAll(filepath.Join(primary.Root(), "artifacts", "chats"), 0o755); err != nil {
		t.Fatal(err)
	}
	session := "---\nsession: " + sid + "\nspirit: concierge\ntitle: test\n---\nhello\n"
	if err := os.WriteFile(filepath.Join(primary.Root(), "artifacts", "chats", sid+".md"), []byte(session), 0o644); err != nil {
		t.Fatal(err)
	}
	evDir := filepath.Join(primary.Root(), "vessel", "state", "chat", sid)
	if err := os.MkdirAll(evDir, 0o755); err != nil {
		t.Fatal(err)
	}
	events := `{"seq":1,"ts":"2026-08-15T12:05:00Z","type":"assistant.delta","data":{"text":"par"}}
{"seq":2,"ts":"2026-08-15T12:05:01Z","type":"assistant.message","data":{"text":"the full answer"}}
`
	if err := os.WriteFile(filepath.Join(evDir, "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	srv.ledgerSweep()
	es := allLedgerEntries(t, led)
	if countKind(es, "run.completed") != 1 {
		t.Fatalf("want one run.completed: %+v", es)
	}
	if countKind(es, "chat.assistant") != 1 {
		t.Fatalf("want one chat.assistant (deltas skipped): %+v", es)
	}
	for _, e := range es {
		if e.Kind == "run.completed" && (e.Task != "inbox/research-zoning" || e.Harness != "hermes" || e.Actor != "hermes") {
			t.Fatalf("run entry fields: %+v", e)
		}
		if e.Kind == "chat.assistant" && (e.Session != sid || e.Text != "the full answer" || e.Actor != "concierge") {
			t.Fatalf("chat entry fields: %+v", e)
		}
	}

	// second sweep: the cursor makes it a no-op
	srv.ledgerSweep()
	if got := len(allLedgerEntries(t, led)); got != len(es) {
		t.Fatalf("second sweep appended: %d → %d", len(es), got)
	}
}

func TestLedgerThreadHookSkipsMarkers(t *testing.T) {
	srv, led := ledgerFixture(t)
	id := "inbox/research-zoning"
	if _, err := srv.addThreadEntry(srv.ownerIdentity(), id, threads.ActComment, "a real comment", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	srv.markerAdd(id, threads.ActRelay, "")                                                       // hidden marker via the private store
	_, _ = srv.addThreadEntry(srv.ownerIdentity(), id, threads.ActRelay, "", nil, nil, nil)       // relay action is a marker
	es := allLedgerEntries(t, led)
	if len(es) != 1 || es[0].Kind != "thread.comment" || es[0].Task != id || es[0].Actor != "owner" {
		t.Fatalf("want exactly the one comment line: %+v", es)
	}
}
