package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/daily"
	"manifest/tasks"
)

// TestReBacklogSync — owner report 2026-08-15: RE backlog tasks (the AION
// mirror at system/realestate/backlog.md) were absent from the unified board
// and had no re: write routing. This locks the parity in.
func TestReBacklogSync(t *testing.T) {
	srv, vault := panelFixture(t)
	dir := t.TempDir()
	st := tasks.NewStore(dir, "to do.md", testWriteAbs)
	if err := os.WriteFile(st.Path(), []byte("# To Do\n\n## Inbox\n- [ ] personal thing [added:: 2026-08-14]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.tasksStore = st

	mk := func(root string) *aion.Store {
		if err := os.MkdirAll(filepath.Join(vault, filepath.FromSlash(root)), 0o755); err != nil {
			t.Fatal(err)
		}
		return aion.NewStore(vault, root, testWriteAbs)
	}
	srv.aion = mk("system/aion")
	srv.re = mk("system/realestate")
	if err := srv.aion.AddItem(&aion.BacklogItem{Kind: aion.KindTask, Text: "aion task", Status: aion.StatusOpen, Captured: "2026-08-14"}); err != nil {
		t.Fatal(err)
	}
	if err := srv.re.AddItem(&aion.BacklogItem{Kind: aion.KindTask, Text: "re task", Status: aion.StatusOpen, Captured: "2026-08-14"}); err != nil {
		t.Fatal(err)
	}

	doc, _ := srv.tasksStore.Load()
	rows := srv.unifiedRows(doc, time.Now())
	var reRow, aionRow *unifiedRow
	for i := range rows {
		if strings.HasPrefix(rows[i].ID, "re:") {
			reRow = &rows[i]
		}
		if strings.HasPrefix(rows[i].ID, "aion:") {
			aionRow = &rows[i]
		}
	}
	if aionRow == nil || reRow == nil {
		t.Fatalf("both backlogs must project; rows=%+v", rows)
	}
	if reRow.Source != "realestate" || reRow.Container.Name != "Real Estate" || reRow.Container.Kind != "re" {
		t.Fatalf("re row shape: %+v", reRow)
	}

	// write routing parity: pin resolves, owner patch lands, check closes
	if _, ok := srv.pinTaskID(reRow.ID); !ok {
		t.Fatal("re: id must pin/resolve")
	}
	if err := srv.setTaskOwner(reRow.ID, "agent:hermes"); err != nil {
		t.Fatalf("owner patch: %v", err)
	}
	bare := strings.TrimPrefix(reRow.ID, "re:")
	if it := srv.re.LoadBacklog().Find(bare); it == nil || it.Owner != "agent:hermes" {
		t.Fatalf("owner not in RE backlog: %+v", it)
	}
	store, b2, ok := srv.backlogStoreFor(reRow.ID)
	if !ok || b2 != bare || store != srv.re {
		t.Fatal("backlogStoreFor must route re: to s.re")
	}
	if err := store.UpdateItem(bare, map[string]string{"status": aion.StatusDone}, time.Now()); err != nil {
		t.Fatal(err)
	}
	doc, _ = srv.tasksStore.Load()
	for _, r := range srv.unifiedRows(doc, time.Now()) {
		if r.ID == reRow.ID {
			t.Fatalf("done RE item must leave the board: %+v", r)
		}
	}
	// thread routing: re: ids land in the shared RE store
	if srv.threadKind(reRow.ID) != "re" {
		t.Fatal("re: threads must route to the shared RE store")
	}
}

func TestDailyRETickDoesNotCreatePersonalTasksMiss(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "system", "realestate"), 0o755); err != nil {
		t.Fatal(err)
	}
	reStore := aion.NewStore(vault, "system/realestate", testWriteAbs)
	it := &aion.BacklogItem{Kind: aion.KindTask, Text: "Questions answered for OPG", Status: aion.StatusOpen, Captured: "2026-08-17"}
	if err := reStore.AddItem(it); err != nil {
		t.Fatal(err)
	}
	tasksStore := tasks.NewStore(vault, "tasks.md", testWriteAbs)
	if err := os.WriteFile(tasksStore.Path(), []byte("# Tasks\n\n## Inbox\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &Server{tasksStore: tasksStore, re: reStore, approvals: approvals.NewStore(t.TempDir())}
	tick := daily.Task{Text: it.Text, TaskID: "re:" + it.ID, Done: true}
	srv.syncTaskTasks([]daily.Task{tick})
	srv.syncAionTasks([]daily.Task{tick})
	if pending := srv.approvals.List("pending"); len(pending) != 0 {
		t.Fatalf("RE tick created a false tasks.md miss: %+v", pending)
	}
	if got := reStore.LoadBacklog().Find(it.ID); got == nil || got.Status != aion.StatusDone {
		t.Fatalf("RE tick did not route to backlog: %+v", got)
	}

	// A genuine personal miss still produces a useful notice using the
	// configured tasks filename, while other composite ids stay excluded.
	srv.syncTaskTasks([]daily.Task{
		{Text: "Missing personal task", TaskID: "inbox/missing", Done: true},
		{Text: "Projected property task", TaskID: "prop:house/roof", Done: true},
	})
	pending := srv.approvals.List("pending")
	if len(pending) != 1 || !strings.Contains(pending[0].Body, "tasks.md") || strings.Contains(pending[0].Body, "to do.md") {
		t.Fatalf("personal miss notice=%+v", pending)
	}
}
