package profiles

import "testing"

func TestSaveGetRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	in := Profile{
		Name:        "Domain Scout",
		Model:       "cheap",
		Tools:       []string{"web", "file"},
		Permissions: []string{"read-only"},
		Schedule:    "0 7 * * *",
		Brief:       "Scan for new papers.\n\nEmit JSON.",
	}
	saved, err := s.Save(in)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.Name != "domain-scout" {
		t.Fatalf("name should slugify to domain-scout, got %q", saved.Name)
	}
	got, ok := s.Get("domain-scout")
	if !ok {
		t.Fatal("Get failed")
	}
	if got.Model != "cheap" || got.Schedule != "0 7 * * *" {
		t.Fatalf("round-trip fields: %+v", got)
	}
	if len(got.Tools) != 2 || got.Tools[0] != "web" {
		t.Fatalf("tools: %v", got.Tools)
	}
	if got.Brief != in.Brief {
		t.Fatalf("brief: %q", got.Brief)
	}
}

func TestSaveRejectsEmptyName(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Save(Profile{Name: "  "}); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Delete("nope"); err != nil {
		t.Fatalf("delete missing should be nil, got %v", err)
	}
}

func TestSeedThenNoOverwrite(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Seed(); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	list := s.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 seeded profiles, got %d", len(list))
	}
	// Mutate one, re-seed, confirm it wasn't overwritten.
	ds, _ := s.Get("domain-scout")
	ds.Brief = "MY EDIT"
	if _, err := s.Save(ds); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Get("domain-scout")
	if again.Brief != "MY EDIT" {
		t.Fatal("Seed must not overwrite existing profiles")
	}
}
