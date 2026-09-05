package recruiting

import (
	"strings"
	"testing"
)

// DELETE THE CHEAP THINGS, ARCHIVE THE PEOPLE (owner, 2026-09-05). Places and
// edges were append-only from the UI, so a typo was permanent unless the owner
// opened the markdown by hand. These pin the gestures and, just as important,
// what they leave alone.

func TestPlacesEditAndDelete(t *testing.T) {
	s, _ := testStore(t)
	lab, err := s.AddSeed(Seed{Class: SeedLab, Name: "WashU BME", URL: "https://bme.washu.edu"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddSeed(Seed{Class: SeedCompany, Name: "Hyperfine"}, testNow)
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.UpdateSeed(lab.ID, map[string]string{"name": "WashU Biomedical Engineering", "org": "WashU"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "WashU Biomedical Engineering" || got.Org != "WashU" {
		t.Fatalf("edit: %+v", got)
	}
	if got.Added != lab.Added || got.Source != lab.Source || got.ID != lab.ID {
		t.Fatalf("an edit rewrote identity or provenance: %+v", got)
	}

	// emptying an optional field REMOVES it — a row carrying `[url:: ]` reads
	// as "has a url" to the sweep, which is how a dead sweep button happens
	if _, err := s.UpdateSeed(lab.ID, map[string]string{"url": ""}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.raw("seeds.md"), "[url::") {
		t.Fatalf("an emptied url left a husk: %s", s.raw("seeds.md"))
	}

	if err := s.RemoveSeed(lab.ID); err != nil {
		t.Fatal(err)
	}
	left := s.LoadSeeds().Seeds()
	for _, sd := range left {
		if sd.ID == lab.ID {
			t.Fatal("the deleted place is still there")
		}
	}
	if findSeed(left, other.ID) == nil {
		t.Fatal("deleting one place took another with it")
	}
	if err := s.RemoveSeed(lab.ID); err == nil {
		t.Fatal("deleting nothing reported success — that is how a UI starts lying")
	}
}

func TestPlaceEditRefusesWhatItCannotWrite(t *testing.T) {
	s, _ := testStore(t)
	seed, _ := s.AddSeed(Seed{Class: SeedLab, Name: "A Lab"}, testNow)
	for _, bad := range []map[string]string{
		{"name": ""},
		{"class": "university"},
		{"added": "2020-01-01"},
		{"consent": "public_record"},
	} {
		if _, err := s.UpdateSeed(seed.ID, bad); err == nil {
			t.Fatalf("edit accepted %v", bad)
		}
	}
}

func TestConnectorEditArchiveAndDelete(t *testing.T) {
	s, _ := testStore(t)
	if err := s.AddNetworkPerson(NetworkPerson{Name: "Dana Fox", Source: "owner", Consent: "owner"}); err != nil {
		t.Fatal(err)
	}
	people := s.LoadNetworkPeople().People()
	dana := people[len(people)-1]

	// the editor the MY PEOPLE hint has always promised: org, title, email
	got, err := s.UpdateNetworkPerson(dana.ID, map[string]string{
		"org": "Hyperfine", "title": "VP Engineering", "email": "dana@hyperfine.example", "type": "advisor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Org != "Hyperfine" || got.Title != "VP Engineering" ||
		got.Email != "dana@hyperfine.example" || got.Type != "advisor" {
		t.Fatalf("edit: %+v", got)
	}
	if _, err := s.UpdateNetworkPerson(dana.ID, map[string]string{"type": "wizard"}); err == nil {
		t.Fatal("an unknown person type was accepted")
	}

	// a person ARCHIVES rather than vanishing — the row keeps its history
	if _, err := s.UpdateNetworkPerson(dana.ID, map[string]string{"archived": "2026-09-05"}); err != nil {
		t.Fatal(err)
	}
	if got := findPerson(s.LoadNetworkPeople().People(), dana.ID); got == nil || got.Archived != "2026-09-05" {
		t.Fatalf("archive: %+v", got)
	}
	// …and restoring is emptying the field, not a second concept
	if _, err := s.UpdateNetworkPerson(dana.ID, map[string]string{"archived": ""}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.raw("network/people.md"), "[archived::") {
		t.Fatal("restore left an archived husk")
	}

	if err := s.RemoveNetworkPerson(dana.ID); err != nil {
		t.Fatal(err)
	}
	if findPerson(s.LoadNetworkPeople().People(), dana.ID) != nil {
		t.Fatal("the deleted person is still there")
	}
}

// Deleting a node does not delete the claims about it: an edge is a statement
// that was true when it was made, and removing the row it points at does not
// make it false.
func TestDeletingAConnectorLeavesTheEdgesStanding(t *testing.T) {
	s, _ := testStore(t)
	if err := s.AddNetworkPerson(NetworkPerson{Name: "Dana Fox", Source: "owner", Consent: "owner"}); err != nil {
		t.Fatal(err)
	}
	people := s.LoadNetworkPeople().People()
	dana := people[len(people)-1]

	edges := s.LoadEdges()
	if _, err := edges.Add(Edge{From: dana.ID, To: "cand/kai-okonkwo", Kind: "direct_known",
		Basis: "Benjamin says they know Kai", Source: "owner", Confidence: "0.95"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEdges(edges); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveNetworkPerson(dana.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(s.LoadEdges().Edges()); n != 1 {
		t.Fatalf("deleting a person took %d edges with it", 1-n)
	}

	// an edge cuts by its ENDS, the way the graph addresses it — and in
	// either order, because the claim is undirected
	if err := s.RemoveEdge("cand/kai-okonkwo", dana.ID, "direct_known"); err != nil {
		t.Fatal(err)
	}
	if n := len(s.LoadEdges().Edges()); n != 0 {
		t.Fatalf("the edge survived its delete: %d", n)
	}
	if err := s.RemoveEdge("a", "b", "coauthor"); err == nil {
		t.Fatal("deleting an edge that does not exist reported success")
	}
}

func findSeed(all []Seed, id string) *Seed {
	for i := range all {
		if all[i].ID == id {
			return &all[i]
		}
	}
	return nil
}

func findPerson(all []NetworkPerson, id string) *NetworkPerson {
	for i := range all {
		if all[i].ID == id {
			return &all[i]
		}
	}
	return nil
}
