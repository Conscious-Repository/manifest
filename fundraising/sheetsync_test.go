package fundraising

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSheetBackend struct {
	data   SheetData
	writes []SheetChange
}

func (f *fakeSheetBackend) Read(context.Context) (SheetData, error) { return f.data, nil }
func (f *fakeSheetBackend) Initialize(_ context.Context, records []SharedOpportunity, dry bool) (SheetInitResult, error) {
	result := SheetInitResult{DryRun: dry, BackupTitle: "Legacy import", Rows: len(records)}
	if dry {
		return result, nil
	}
	f.data = SheetData{Initialized: true, RowCount: 100}
	for i, record := range records {
		f.data.Rows = append(f.data.Rows, SyncSheetRow{ID: record.ID, MetadataID: int64(i + 1), Row: i + 1, Record: record, Sync: "synced"})
	}
	return result, nil
}
func (f *fakeSheetBackend) Write(_ context.Context, changes []SheetChange) error {
	f.writes = append(f.writes, changes...)
	byRow := map[int]int{}
	for i, row := range f.data.Rows {
		byRow[row.Row] = i
	}
	for _, change := range changes {
		if change.Clear {
			if i, ok := byRow[change.Row]; ok {
				f.data.Rows = append(f.data.Rows[:i], f.data.Rows[i+1:]...)
				byRow = map[int]int{}
				for j, row := range f.data.Rows {
					byRow[row.Row] = j
				}
			}
			continue
		}
		row := SyncSheetRow{ID: change.ID, MetadataID: change.MetadataID, Row: change.Row, Record: change.Record, Sync: change.Sync}
		if change.AttachMetadata && row.MetadataID == 0 {
			row.MetadataID = int64(1000 + change.Row)
		}
		if i, ok := byRow[change.Row]; ok {
			f.data.Rows[i] = row
		} else {
			f.data.Rows = append(f.data.Rows, row)
			byRow[change.Row] = len(f.data.Rows) - 1
		}
	}
	return nil
}

func syncTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	write := func(path string, b []byte) error { return writeAtomicForTest(path, b) }
	s := NewStore(root, "system/crm/fundraising", "system/crm/contacts.md", write, write)
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	return s
}

func writeAtomicForTest(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func TestSheetSyncPullPushConflictAndRestore(t *testing.T) {
	store := syncTestStore(t)
	op, err := store.Create("Acme")
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeSheetBackend{}
	syncer := NewSheetSync(store, backend, filepath.Join(t.TempDir(), "state.json"), "https://example.test", nil)
	if _, err := syncer.Initialize(context.Background(), false); err != nil {
		t.Fatal(err)
	}

	backend.data.Rows[0].Record.Notes = "sheet note"
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get(op.ID); got.Notes != "sheet note" {
		t.Fatalf("Sheet edit not pulled: %+v", got)
	}

	if _, err := store.Update(op.ID, map[string]any{"nextStep": "manifest next"}); err != nil {
		t.Fatal(err)
	}
	backend.data.Rows[0].Record.NextStep = "sheet next"
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := syncer.Status()
	if len(status.Conflicts) != 1 || status.Conflicts[0].Field != "nextStep" {
		t.Fatalf("conflicts = %+v", status.Conflicts)
	}
	if err := syncer.Resolve(context.Background(), op.ID, "nextStep", "sheet"); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get(op.ID); got.NextStep != "sheet next" {
		t.Fatalf("resolved value = %q", got.NextStep)
	}

	backend.data.Rows = nil // collaborator row removal must not delete Markdown
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(backend.data.Rows) != 1 || backend.data.Rows[0].ID != op.ID {
		t.Fatalf("missing row was not restored: %+v", backend.data.Rows)
	}
}

func TestSheetSyncCreatesPlaintextPeopleAndRejectsInvalidDate(t *testing.T) {
	store := syncTestStore(t)
	backend := &fakeSheetBackend{}
	syncer := NewSheetSync(store, backend, filepath.Join(t.TempDir(), "state.json"), "", nil)
	if _, err := syncer.Initialize(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	backend.data.Rows = append(backend.data.Rows, SyncSheetRow{Row: 1, Record: SharedOpportunity{
		Firm: "New Fund", People: []string{"Unlisted Person"}, Status: StatusProspect, Interest: InterestUnknown, Currency: "USD",
	}})
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	ops, _ := store.List()
	if len(ops) != 1 || len(ops[0].UnlinkedPeople) != 1 || ops[0].UnlinkedPeople[0] != "Unlisted Person" || len(ops[0].People) != 0 {
		t.Fatalf("created opportunity = %+v", ops)
	}
	backend.data.Rows[0].Record.NextStepDue = "tomorrow"
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(ops[0].ID)
	if got.NextStepDue != "" || !strings.HasPrefix(backend.data.Rows[0].Sync, "error:") {
		t.Fatalf("invalid date changed record or lacked status: op=%+v row=%+v", got, backend.data.Rows[0])
	}
}

func TestPlaintextPeopleFrontmatterPreservesUnknownFieldsAndBody(t *testing.T) {
	store := syncTestStore(t)
	op, err := store.CreateShared(SharedOpportunity{Firm: "Plain Fund", People: []string{"External Person"}, Status: StatusProspect, Interest: InterestUnknown, Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	path := store.abs(op.Path)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(b), "---\n\n# Plain Fund", "unknown-field: keep-me\n---\n\n# Plain Fund\n\nprivate body", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	desired := SharedFromOpportunity(op)
	desired.Notes = "updated"
	if _, err := store.SharedUpdate(op.ID, desired, []string{"notes"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	for _, want := range []string{`people-text: ["External Person"]`, "unknown-field: keep-me", "private body"} {
		if !strings.Contains(string(after), want) {
			t.Fatalf("missing %q in\n%s", want, after)
		}
	}
}
