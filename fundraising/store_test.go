package fundraising

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	write := func(abs string, b []byte) error {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, b, 0o644)
	}
	s := NewStore(root, "system/crm/fundraising", "system/crm/contacts.md", write, write)
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	return s, root
}

func TestStoreCRUDPreservesUnknownContent(t *testing.T) {
	s, root := testStore(t)
	op, err := s.Create("Acme Ventures")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(op.Path))
	b, _ := os.ReadFile(path)
	raw := strings.Replace(string(b), "status: prospect", "status: prospect\ncustom-field: keep-me", 1) + "\n## private scratch\nkeep this body\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(op.ID, map[string]any{"status": "active", "nextStep": "Send deck", "amount": "250000", "website": "acme.vc"}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	got := string(b)
	for _, want := range []string{"status: active", "next-step: \"Send deck\"", "amount: 250000", "website: \"https://acme.vc\"", "custom-field: keep-me", "## private scratch", "keep this body"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if _, err := s.Archive(op.ID, true); err != nil {
		t.Fatal(err)
	}
	arch, _ := s.Get(op.ID)
	if !arch.Archived {
		t.Fatal("archive state not persisted")
	}
}

func TestOpportunityWebsiteValidationAndClear(t *testing.T) {
	s, root := testStore(t)
	op, err := s.Create("Web Fund")
	if err != nil {
		t.Fatal(err)
	}
	op, err = s.Update(op.ID, map[string]any{"website": "example.com/path"})
	if err != nil || op.Website != "https://example.com/path" {
		t.Fatalf("website=%q err=%v", op.Website, err)
	}
	if _, err := s.Update(op.ID, map[string]any{"website": "javascript:alert(1)"}); err == nil {
		t.Fatal("unsafe website scheme accepted")
	}
	op, err = s.Update(op.ID, map[string]any{"website": ""})
	if err != nil || op.Website != "" {
		t.Fatalf("clear website=%q err=%v", op.Website, err)
	}
	b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(op.Path)))
	if strings.Contains(string(b), "\nwebsite:") {
		t.Fatalf("cleared website remains in frontmatter:\n%s", b)
	}
}

func TestDeleteTombstonesOpportunityWithoutErasingRecord(t *testing.T) {
	s, root := testStore(t)
	op, err := s.Create("Delete Me Capital")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(op.Path))
	b, _ := os.ReadFile(path)
	b = append(b, []byte("\n## private context\nkeep this history\n")...)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	oldTouch := Touch
	Touch = func() time.Time { return time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC) }
	defer func() { Touch = oldTouch }()
	if err := s.Delete(op.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(op.ID); ok {
		t.Fatal("deleted opportunity remains in live CRM")
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("recoverable record was erased: %v", err)
	}
	for _, want := range []string{"categories: [fundraising-deleted]", `deleted: "2026-08-17T20:00:00Z"`, "## private context", "keep this history"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("deleted record missing %q:\n%s", want, b)
		}
	}
}

func TestParserWriterFixpoint(t *testing.T) {
	s, root := testStore(t)
	op, err := s.Create("Stable Capital")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(op.ID, map[string]any{"status": "active", "notes": "keep exactly"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(op.Path))
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(op.ID, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("parse/write cycle changed stable record\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestOpportunitySourceAcceptsTextOrLinkedContact(t *testing.T) {
	s, root := testStore(t)
	op, err := s.Create("Source Capital")
	if err != nil {
		t.Fatal(err)
	}

	op, err = s.Update(op.ID, map[string]any{"source": "cold intro"})
	if err != nil {
		t.Fatal(err)
	}
	if op.Source == nil || op.Source.Text != "cold intro" || op.Source.Contact != nil {
		t.Fatalf("plain source=%+v", op.Source)
	}

	contact := map[string]any{"key": "Jane Doe", "display": "Jane Doe"}
	op, err = s.Update(op.ID, map[string]any{"source": map[string]any{"contact": contact}})
	if err != nil {
		t.Fatal(err)
	}
	if op.Source == nil || op.Source.Contact == nil || op.Source.Contact.Key != "jane doe" || op.Source.Text != "" {
		t.Fatalf("contact source=%+v", op.Source)
	}
	if p, ok := s.Person("jane doe"); !ok || p.Display != "Jane Doe" {
		t.Fatalf("source contact missing from CRM: person=%+v ok=%v", p, ok)
	}
	if got := s.OpportunitiesFor("jane doe"); len(got) != 1 || got[0].ID != op.ID {
		t.Fatalf("source contact opportunities=%+v", got)
	}

	path := filepath.Join(root, filepath.FromSlash(op.Path))
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `source: {"contact":{"key":"jane doe","display":"Jane Doe"}}`) {
		t.Fatalf("linked source not persisted:\n%s", b)
	}

	if _, err := s.Update(op.ID, map[string]any{"source": map[string]any{"text": "DM", "contact": contact}}); err == nil {
		t.Fatal("source accepted both contact and text")
	}
	op, err = s.Update(op.ID, map[string]any{"source": nil})
	if err != nil || op.Source != nil {
		t.Fatalf("clear source: op=%+v err=%v", op.Source, err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "\nsource:") {
		t.Fatalf("cleared source remains in frontmatter:\n%s", b)
	}
}

func TestOpportunitySourceParsesHandEditedFrontmatter(t *testing.T) {
	s, root := testStore(t)
	op, err := s.Create("Hand Edit Fund")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(op.Path))
	b, _ := os.ReadFile(path)
	b = []byte(strings.Replace(string(b), "people: []", "people: []\nsource: {\"text\":\"DM\"}", 1))
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get(op.ID)
	if !ok || got.Source == nil || got.Source.Text != "DM" {
		t.Fatalf("hand-edited source=%+v ok=%v", got.Source, ok)
	}
}

func TestEnsureRemovesLegacyIntroViaAndPreservesItsPeople(t *testing.T) {
	s, root := testStore(t)
	op, err := s.Create("Legacy Fund")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(op.Path))
	b, _ := os.ReadFile(path)
	b = []byte(strings.Replace(string(b), "people: []", "people: []\nintro-via: \"Jane Doe, John Smith\"", 1))
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get(op.ID)
	if !ok || got.Source != nil || len(got.People) != 2 || got.People[0].Key != "jane doe" || got.People[1].Key != "john smith" {
		t.Fatalf("migrated opportunity=%+v ok=%v", got, ok)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "\nintro-via:") || strings.Contains(string(b), "\nsource:") {
		t.Fatalf("legacy field was not replaced:\n%s", b)
	}
}

func TestRepairTextSourcesRestoresMultiplePeopleAndLeavesContactSource(t *testing.T) {
	s, _ := testStore(t)
	bad, err := s.Create("Bad Migration")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(bad.ID, map[string]any{"source": map[string]any{"text": "Jane Doe; John Smith"}}); err != nil {
		t.Fatal(err)
	}
	linked, err := s.Create("Legitimate Source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(linked.ID, map[string]any{"source": map[string]any{"contact": map[string]any{"key": "introducer", "display": "Introducer"}}}); err != nil {
		t.Fatal(err)
	}

	ops, people, err := s.RepairTextSourcesAsPeople(true)
	if err != nil || ops != 1 || people != 2 {
		t.Fatalf("dry-run opportunities=%d people=%d err=%v", ops, people, err)
	}
	stillBad, _ := s.Get(bad.ID)
	if stillBad.Source == nil || len(stillBad.People) != 0 {
		t.Fatalf("dry-run mutated record: %+v", stillBad)
	}
	ops, people, err = s.RepairTextSourcesAsPeople(false)
	if err != nil || ops != 1 || people != 2 {
		t.Fatalf("repair opportunities=%d people=%d err=%v", ops, people, err)
	}
	repaired, _ := s.Get(bad.ID)
	if repaired.Source != nil || len(repaired.People) != 2 {
		t.Fatalf("repaired opportunity=%+v", repaired)
	}
	untouched, _ := s.Get(linked.ID)
	if untouched.Source == nil || untouched.Source.Contact == nil || untouched.Source.Contact.Key != "introducer" {
		t.Fatalf("contact source changed: %+v", untouched.Source)
	}
}

func TestStoreWritesStayInsideCapabilities(t *testing.T) {
	root := t.TempDir()
	frRoot := filepath.Join(root, "system", "crm", "fundraising")
	registry := filepath.Join(root, "system", "crm", "contacts.md")
	guard := func(wantRoot string, exact bool) func(string, []byte) error {
		return func(abs string, b []byte) error {
			clean := filepath.Clean(abs)
			allowed := clean == wantRoot
			if !exact {
				rel, err := filepath.Rel(wantRoot, clean)
				allowed = err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
			}
			if !allowed {
				return os.ErrPermission
			}
			if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
				return err
			}
			return os.WriteFile(clean, b, 0o644)
		}
	}
	s := NewStore(root, "system/crm/fundraising", "system/crm/contacts.md", guard(frRoot, false), guard(registry, true))
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("../../outside"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "outside.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected write outside capability: %v", err)
	}
}

func TestRegistryPeopleAreExplicitAndNoteOptional(t *testing.T) {
	s, _ := testStore(t)
	op, err := s.Create("Acme")
	if err != nil {
		t.Fatal(err)
	}
	op, err = s.AddPerson(op.ID, PersonRef{Key: "jane doe", Display: "Jane Doe"})
	if err != nil {
		t.Fatal(err)
	}
	if len(op.People) != 1 {
		t.Fatalf("people=%+v", op.People)
	}
	p, ok := s.Person("Jane Doe")
	if !ok || p.NotePath != "" {
		t.Fatalf("registry person=%+v ok=%v", p, ok)
	}
	if err := s.AddEmail("jane doe", "Jane@Example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachNote("jane doe", "jane doe.md"); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Person("jane doe")
	if p.NotePath != "jane doe.md" || len(p.Emails) != 1 || p.Emails[0] != "jane@example.com" {
		t.Fatalf("promoted registry=%+v", p)
	}
}
