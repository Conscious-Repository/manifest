package fundraising

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sweep adopts person notes for registry rows and opportunity people
// that already have one (emails land on the note), auto-links an opportunity
// named exactly like a person, and leaves the rest pending; resolving a
// pending name rewrites every opportunity to the chosen person and drains
// the registry file.
func TestSweepAdoptsNotesAndPendingResolves(t *testing.T) {
	s, root := testStore(t)
	if err := s.UpsertRegistry(RegistryPerson{Key: "daisy wolf", Display: "daisy wolf", Emails: []string{"dwolf@a16z.com"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertRegistry(RegistryPerson{Key: "chris", Display: "Chris"}); err != nil {
		t.Fatal(err)
	}
	a16z, err := s.Create("a16z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPerson(a16z.ID, PersonRef{Key: "daisy wolf", Display: "daisy wolf"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPerson(a16z.ID, PersonRef{Key: "chris", Display: "Chris"}); err != nil {
		t.Fatal(err)
	}
	adam, err := s.Create("Adam Gries")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(adam.ID, map[string]any{"unlinkedPeople": []string{"Typed Person"}}); err != nil {
		t.Fatal(err)
	}
	solo, err := s.Create("Chris Leiter")
	if err != nil {
		t.Fatal(err)
	}

	notes := map[string]string{"daisy wolf": "daisy wolf.md", "chris leiter": "chris leiter.md"}
	adopted := map[string][]string{}
	res, err := s.Sweep(func(key string) (string, bool) { rel, ok := notes[key]; return rel, ok },
		func(rel string, emails []string) error { adopted[rel] = append(adopted[rel], emails...); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if res.Adopted != 1 || res.Relinked != 1 || res.AutoLinks != 1 || res.Pending != 1 {
		t.Fatalf("sweep = %+v", res)
	}
	if got := adopted["daisy wolf.md"]; len(got) != 1 || got[0] != "dwolf@a16z.com" {
		t.Fatalf("registry email should land on the note: %v", adopted)
	}
	got, _ := s.Get(a16z.ID)
	if got.People[0].NotePath != "daisy wolf.md" || got.People[1].NotePath != "" {
		t.Fatalf("a16z people = %+v", got.People)
	}
	got, _ = s.Get(solo.ID)
	if len(got.People) != 1 || got.People[0].Key != "chris leiter" || got.People[0].NotePath != "chris leiter.md" {
		t.Fatalf("an opportunity named like a person links that person: %+v", got.People)
	}
	got, _ = s.Get(adam.ID)
	if len(got.People) != 0 {
		t.Fatalf("a pending Sheet name blocks the auto-link: %+v", got.People)
	}
	if reg := s.RegistryPeople(); len(reg) != 1 || reg[0].Key != "chris" {
		t.Fatalf("registry keeps only the unresolved row: %+v", reg)
	}

	pending := s.Pending()
	if len(pending) != 2 || pending[0].Origin != "sheet" || pending[0].Key != "typed person" || pending[1].Key != "chris" {
		t.Fatalf("pending = %+v", pending)
	}
	if len(pending[1].Opportunities) != 1 || pending[1].Opportunities[0].Firm != "a16z" {
		t.Fatalf("a registry name lists where it appears: %+v", pending[1])
	}

	// the Sheet name becomes a person on its opportunity only
	if err := s.ResolvePending("typed person", "sheet", adam.ID, PersonRef{Key: "typed person", Display: "Typed Person", NotePath: "typed person.md"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(adam.ID)
	if len(got.UnlinkedPeople) != 0 || len(got.People) != 1 || got.People[0].NotePath != "typed person.md" {
		t.Fatalf("resolved sheet name = %+v / %+v", got.People, got.UnlinkedPeople)
	}
	// the registry name is rewritten to the chosen contact everywhere and the file goes
	removed := ""
	s.UseRegistryRemover(func(abs string) error { removed = abs; return os.Remove(abs) })
	if err := s.ResolvePending("chris", "registry", "", PersonRef{Key: "chris leiter", Display: "Chris Leiter", NotePath: "chris leiter.md"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(a16z.ID)
	var rewritten *PersonRef
	for i := range got.People {
		if got.People[i].Key == "chris leiter" {
			rewritten = &got.People[i]
		}
	}
	if len(got.People) != 2 || rewritten == nil || rewritten.NotePath != "chris leiter.md" || hasPerson(got.People, "chris") {
		t.Fatalf("registry name rewritten = %+v", got.People)
	}
	if removed == "" || !strings.HasSuffix(filepath.ToSlash(removed), "system/crm/contacts.md") {
		t.Fatalf("registry file should be removed once empty: %q", removed)
	}
	if _, err := os.Stat(filepath.Join(root, "system", "crm", "contacts.md")); !os.IsNotExist(err) {
		t.Fatal("registry file still exists")
	}
	if len(s.Pending()) != 0 {
		t.Fatalf("nothing should be pending: %+v", s.Pending())
	}
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "system", "crm", "contacts.md")); !os.IsNotExist(err) {
		t.Fatal("Ensure must not re-seed the registry")
	}
}

// Latest wins for the last touch; the next touch is the soonest date still
// ahead, else the most recent overdue one.
func TestMergeTouchesLatestWinsAndSoonestAhead(t *testing.T) {
	auto := &Touch{Date: "2026-09-15", Kind: "met", Person: "Ethan"}
	if got := MergeLast(auto, "2026-09-23"); got == nil || got.Kind != "manual" || got.Date != "2026-09-23" {
		t.Fatalf("a newer manual date wins: %+v", got)
	}
	if got := MergeLast(auto, "2026-09-01"); got == nil || got.Kind != "met" {
		t.Fatalf("an older manual date loses: %+v", got)
	}
	if got := MergeLast(auto, "2026-09-15"); got == nil || got.Kind != "met" {
		t.Fatalf("a tie goes to the evidence: %+v", got)
	}
	if got := MergeLast(nil, ""); got != nil {
		t.Fatalf("nothing → nil: %+v", got)
	}
	up := &Touch{Date: "2026-10-01", Kind: "upcoming", Title: "call"}
	if got := MergeNext(up, "2026-09-28", "2026-09-25"); got == nil || got.Kind != "manual" {
		t.Fatalf("the soonest date ahead wins: %+v", got)
	}
	if got := MergeNext(up, "2026-10-05", "2026-09-25"); got == nil || got.Kind != "upcoming" {
		t.Fatalf("the calendar event is sooner: %+v", got)
	}
	if got := MergeNext(up, "2026-09-20", "2026-09-25"); got == nil || got.Kind != "upcoming" {
		t.Fatalf("an overdue manual date yields to something ahead: %+v", got)
	}
	if got := MergeNext(nil, "2026-09-20", "2026-09-25"); got == nil || got.Kind != "manual" || got.Date != "2026-09-20" {
		t.Fatalf("an overdue manual date stays visible when nothing is ahead: %+v", got)
	}
	picked := PickLast([]Touch{{Date: "2026-09-01", Kind: "note"}, {Date: "2026-09-10", Kind: "email"}, {Date: "2026-09-10", Kind: "met"}})
	if picked == nil || picked.Kind != "met" {
		t.Fatalf("pick last = %+v", picked)
	}
	if n := PickNext([]Touch{{Date: "2026-10-09"}, {Date: "2026-10-02"}}); n == nil || n.Date != "2026-10-02" {
		t.Fatalf("pick next = %+v", n)
	}
}

// A name a collaborator types into the Sheet stays plain text on the
// opportunity and the row's Sync cell says it is pending; the winning
// touch dates are what the Sheet receives.
func TestSheetPendingNamesAndWinningDates(t *testing.T) {
	store := syncTestStore(t)
	backend := &fakeSheetBackend{}
	syncer := NewSheetSync(store, backend, filepath.Join(t.TempDir(), "state.json"), "", func() []Opportunity {
		ops, _ := store.List()
		for i := range ops {
			ops[i].LastTouch = MergeLast(&Touch{Date: "2026-09-15", Kind: "met"}, ops[i].LastTouchpointDate)
		}
		return ops
	})
	if _, err := syncer.Initialize(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	backend.data.Rows = append(backend.data.Rows, SyncSheetRow{Row: 1, Record: SharedOpportunity{Firm: "New Fund", People: []string{"Typed Person"}, Status: StatusProspect}})
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(backend.data.Rows[0].Sync, "pending: Typed Person") {
		t.Fatalf("sync cell = %q", backend.data.Rows[0].Sync)
	}
	if backend.data.Rows[0].Record.LastTouchpointDate != "2026-09-15" {
		t.Fatalf("the Sheet receives the winning date: %+v", backend.data.Rows[0].Record)
	}
	ops, _ := store.List()
	if len(ops) != 1 || len(ops[0].UnlinkedPeople) != 1 || len(ops[0].People) != 0 {
		t.Fatalf("a Sheet name is not a person yet: %+v", ops)
	}
	if p := store.Pending(); len(p) != 1 || p[0].Origin != "sheet" || p[0].Opportunities[0].ID != ops[0].ID {
		t.Fatalf("pending = %+v", p)
	}
	// the owner types a later date in the Sheet: it pulls in as manual and wins
	backend.data.Rows[0].Record.LastTouchpointDate = "2026-09-20"
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(ops[0].ID)
	if got.LastTouchpointDate != "2026-09-20" {
		t.Fatalf("manual date should be pulled: %+v", got)
	}
	if backend.data.Rows[0].Record.LastTouchpointDate != "2026-09-20" {
		t.Fatalf("latest wins on the Sheet too: %+v", backend.data.Rows[0].Record)
	}
}
