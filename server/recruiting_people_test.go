package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/recruiting"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ⚠ THE RECURSION. View() derives every candidate's intro paths from
// networkEdges(); networkEdges() asks the injected derivation; the derivation
// asks who is on the board. If it asks by calling View(), the first page load
// recurses until the process dies. This test wires a derivation that reads the
// board the way the real one does and then loads the view — it hangs or
// crashes if that loop is ever reintroduced.
func TestDerivedEdgesDoNotRecurseThroughTheView(t *testing.T) {
	s, _, _, _ := testRecruitingServer(t)
	// a board with somebody on it and somebody to route through — the shape the
	// derivation actually reads
	if _, err := s.recruiting.AddCandidate(recruiting.QuickAdd{Text: "Kai Okonkwo", Name: "Kai Okonkwo"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{
		Name: "Dana Fox", Source: "owner", Consent: "owner"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.recruiting.UseDerivedEdges(func() []recruiting.Edge {
		calls++
		if calls > 4 {
			t.Fatal("the derivation is being re-entered — View is calling itself through the edges")
		}
		var out []recruiting.Edge
		ids := s.recruiting.Identities() // the read the real derivation uses
		conns := s.recruiting.Connectors()
		if len(ids) > 0 && len(conns) > 0 {
			out = append(out, recruiting.Edge{
				From: conns[0].ID, To: ids[0].ID, Kind: "same_meeting",
				Basis: "both on a meeting", Source: "calendar", Confidence: "0.70", Inferred: true,
			})
		}
		return out
	})

	done := make(chan recruiting.View, 1)
	go func() { done <- s.recruiting.View() }()
	select {
	case v := <-done:
		if len(v.Network.Edges) == 0 {
			t.Fatal("the derived edge never reached the view")
		}
		if !v.Network.Edges[len(v.Network.Edges)-1].Derived {
			t.Fatal("a derived edge must say it was derived")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("View did not return — the derivation recursed")
	}
}

// A derived edge is a READ. It must never reach the vault, however many times
// the view is loaded, because a derivation that gets written becomes a fact
// nobody can correct at the source.
func TestDerivedEdgesAreNeverWritten(t *testing.T) {
	s, _, vault, _ := testRecruitingServer(t)
	s.recruiting.UseDerivedEdges(func() []recruiting.Edge {
		return []recruiting.Edge{{
			From: "contact/dana fox", To: "contact/kai okonkwo", Kind: "co_mentioned",
			Basis: "written about together in a note", Source: "notes", Confidence: "0.40", Inferred: true,
		}}
	})
	_ = s.recruiting.View()
	_ = s.recruiting.View()
	raw := readFile(t, filepath.Join(vault, "system/aion/recruiting/network/edges.md"))
	if strings.Contains(raw, "co_mentioned") || strings.Contains(raw, "dana fox") {
		t.Fatalf("a derived edge was written to the vault:\n%s", raw)
	}
	// and a REAL write still works, without picking the derived one up
	doc := s.recruiting.LoadEdges()
	if _, err := doc.Add(recruiting.Edge{From: "a", To: "b", Kind: "direct_known",
		Basis: "the owner says so", Source: "owner", Confidence: "0.95"}); err != nil {
		t.Fatal(err)
	}
	if err := s.recruiting.SaveEdges(doc); err != nil {
		t.Fatal(err)
	}
	raw = readFile(t, filepath.Join(vault, "system/aion/recruiting/network/edges.md"))
	if strings.Contains(raw, "co_mentioned") {
		t.Fatalf("saving a real edge swept a derived one into the file:\n%s", raw)
	}
	if !strings.Contains(raw, "direct_known") {
		t.Fatalf("the real edge did not land:\n%s", raw)
	}
}

// Marking a vault contact is what creates a path origin, and it is the ONE
// thing here that writes: `consent: owner` (or no path can start from them)
// plus the contact key, so the same human is not a second record.
func TestMarkingAKnownPersonWritesOneConnector(t *testing.T) {
	s, _, _, _ := testRecruitingServer(t)
	w := recruitingPost(t, s, s.handleRecruitingMarkKnown, "/api/aion/recruiting/network/mark", "",
		`{"key":"dana fox","name":"Dana Fox"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	people := s.recruiting.Connectors()
	var dana *recruiting.NetworkPerson
	for i := range people {
		if people[i].Name == "Dana Fox" {
			dana = &people[i]
		}
	}
	if dana == nil {
		t.Fatalf("no connector written: %+v", people)
	}
	if dana.Consent != "owner" {
		t.Fatalf("without consent:owner no path can start from them: %+v", dana)
	}
	if dana.Ref != "dana fox" {
		t.Fatalf("the contact key must ride along so this is not a second record: %+v", dana)
	}

	// marking twice is not two people
	before := len(s.recruiting.Connectors())
	w = recruitingPost(t, s, s.handleRecruitingMarkKnown, "/api/aion/recruiting/network/mark", "", `{"key":"dana fox"}`)
	var out map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["already"] == nil {
		t.Fatalf("a second mark must say it was already marked: %s", w.Body.String())
	}
	if n := len(s.recruiting.Connectors()); n != before {
		t.Fatalf("marking twice wrote %d rows", n-before+1)
	}
}
