package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/recruiting"
)

// A graph with a centre, a ring of people the owner knows, and a ring of
// candidates behind them — the shape the ego view is for.
func testGraphServer(t *testing.T) (*Server, http.Handler, string) {
	t.Helper()
	s, _, vault, _ := testRecruitingServer(t)
	// two people to route through. The owner is ALREADY a connector in the
	// seeded records — that row is what ownerNode() matches, and standing
	// somewhere else would not be the ego view.
	for _, name := range []string{"Dana Fox", "Kai Ito"} {
		if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{
			Name: name, Source: "owner", Consent: "owner"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Priya Raman", "Tom Levin"} {
		if _, err := s.recruiting.AddCandidate(recruiting.QuickAdd{Text: name, Name: name}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	ids := map[string]string{}
	for _, p := range s.recruiting.Connectors() {
		ids[p.Name] = p.ID
		if strings.Contains(strings.ToLower(p.Name), "benjamin") {
			ids["YOU"] = p.ID
		}
	}
	for _, c := range s.recruiting.Identities() {
		ids[c.Name] = c.ID
	}
	doc := s.recruiting.LoadEdges()
	add := func(from, to, kind, conf string) {
		if _, err := doc.Add(recruiting.Edge{From: ids[from], To: ids[to], Kind: kind,
			Basis: "they were on the same call", Source: "calendar", Confidence: conf, Inferred: true}); err != nil {
			t.Fatal(err)
		}
	}
	add("YOU", "Dana Fox", "same_meeting", "0.70")
	add("YOU", "Kai Ito", "same_meeting", "0.70")
	add("Dana Fox", "Priya Raman", "co_mentioned", "0.40")
	add("Kai Ito", "Tom Levin", "same_meeting", "0.70")
	if err := s.recruiting.SaveEdges(doc); err != nil {
		t.Fatal(err)
	}
	return s, s.Handler(), vault
}

// graphSnapshot is every file under the recruiting records, by content — the
// cheapest possible proof that a read stayed a read.
func graphSnapshot(t *testing.T, vault string) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join(vault, "system/aion/recruiting")
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[rel] = string(b)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func graphGet(t *testing.T, mux http.Handler, query string) graphReply {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/aion/recruiting/graph"+query, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s → %d: %s", query, w.Code, w.Body.String())
	}
	var out graphReply
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func graphLabels(g graphReply) []string {
	var out []string
	for _, n := range g.Nodes {
		out = append(out, n.Label)
	}
	return out
}

// ⚠ THE GUARDRAIL, and the whole reason this is an EGO view: nothing past the
// chosen degree is SENT, so a hairball is unreachable by construction rather
// than hidden by a slider. One hop is who you know; two adds who they reach.
func TestGraphIsBoundedByDegree(t *testing.T) {
	_, mux, _ := testGraphServer(t)

	one := graphGet(t, mux, "?degree=1")
	if got := strings.Join(graphLabels(one), ","); got != "Benjamin Anderson,Dana Fox,Kai Ito" {
		t.Fatalf("one hop is the people you know: %q", got)
	}
	for _, e := range one.Edges {
		for _, n := range []string{e.From, e.To} {
			found := false
			for _, node := range one.Nodes {
				if node.ID == n {
					found = true
				}
			}
			if !found {
				t.Fatalf("an edge reaches outside the drawn set: %+v", e)
			}
		}
	}

	two := graphGet(t, mux, "?degree=2")
	if len(two.Nodes) != 5 {
		t.Fatalf("two hops adds the candidates behind them: %v", graphLabels(two))
	}
	// and the ceiling holds
	if g := graphGet(t, mux, "?degree=9"); g.Degree != graphMaxDegree {
		t.Fatalf("degree ceiling: %d", g.Degree)
	}
}

// A node says what you are DOING about the person, not just how many lines
// touch it — the answer to the standing critique that a graph view encodes
// topology and no operational state.
func TestGraphNodesCarryOperationalState(t *testing.T) {
	_, mux, _ := testGraphServer(t)
	g := graphGet(t, mux, "?degree=2")
	kinds := map[string]string{}
	for _, n := range g.Nodes {
		kinds[n.Label] = n.Kind
	}
	if kinds["Benjamin Anderson"] != "you" {
		t.Fatalf("the centre is you: %v", kinds)
	}
	if kinds["Dana Fox"] != "connector" || kinds["Kai Ito"] != "connector" {
		t.Fatalf("the people you would ask: %v", kinds)
	}
	if kinds["Priya Raman"] != "considering" || kinds["Tom Levin"] != "considering" {
		t.Fatalf("the people on the board: %v", kinds)
	}
	for _, n := range g.Nodes {
		if n.Kind == "considering" && n.Stage == "" {
			t.Fatalf("a candidate node must carry its stage: %+v", n)
		}
	}
}

// The kind chips filter what is WALKED. Their counts describe the whole
// graph, though, so a chip says how much it would add rather than how much is
// left after it.
func TestGraphFiltersByEdgeKind(t *testing.T) {
	_, mux, _ := testGraphServer(t)
	all := graphGet(t, mux, "?degree=2")
	counts := map[string]int{}
	for _, k := range all.Kinds {
		counts[k.Kind] = k.Count
	}
	if counts["same_meeting"] != 3 || counts["co_mentioned"] != 1 {
		t.Fatalf("kind counts: %v", counts)
	}
	meetings := graphGet(t, mux, "?degree=2&kind=same_meeting")
	for _, e := range meetings.Edges {
		if e.Kind != "same_meeting" {
			t.Fatalf("a filtered graph drew %q", e.Kind)
		}
	}
	// Priya is only reachable through a co-mention, so filtering it out drops
	// her from the picture entirely
	for _, l := range graphLabels(meetings) {
		if l == "Priya Raman" {
			t.Fatal("filtering the only edge that reaches her must drop her")
		}
	}
	// and the counts still describe the whole graph
	for _, k := range meetings.Kinds {
		if k.Kind == "co_mentioned" && k.Count != 1 {
			t.Fatalf("a chip must say what it would ADD: %+v", k)
		}
	}
}

// Search is the entry point, not the canvas (van Ham & Perer step one): it
// answers who, and drawing happens only once you have chosen a centre.
func TestGraphSearchAnswersWithoutRedrawing(t *testing.T) {
	_, mux, _ := testGraphServer(t)
	g := graphGet(t, mux, "?q=fox")
	if len(g.Search) != 1 || g.Search[0].Label != "Dana Fox" {
		t.Fatalf("search: %+v", g.Search)
	}
	if g.Search[0].Kind != "connector" {
		t.Fatalf("a match says what it is: %+v", g.Search[0])
	}
	// centring on her puts her at hop 0
	c := graphGet(t, mux, "?degree=1&center="+g.Search[0].ID)
	if c.Center != g.Search[0].ID {
		t.Fatalf("centre: %+v", c.Center)
	}
	for _, n := range c.Nodes {
		if n.ID == c.Center && n.Hop != 0 {
			t.Fatalf("the centre is hop 0: %+v", n)
		}
	}
}

// The empty state is the design: with nothing to draw, the answer NAMES the
// gestures that would fill it.
func TestGraphEmptyStateNamesTheGestures(t *testing.T) {
	s, _, _, _ := testRecruitingServer(t)
	mux := s.Handler()
	g := graphGet(t, mux, "?degree=2")
	if len(g.Edges) != 0 {
		t.Fatalf("there is nothing to draw: %+v", g.Edges)
	}
	if len(g.Nodes) > 1 {
		t.Fatalf("with no edges the picture is you alone: %v", graphLabels(g))
	}
	if len(g.Missing) == 0 {
		t.Fatal("an empty picture must say what would fill it")
	}
	if !strings.Contains(strings.Join(g.Missing, " | "), "sweep") {
		t.Fatalf("the gesture that fills an edgeless graph is a sweep: %q", g.Missing)
	}
	// and with nobody to route through, the OTHER gesture is named
	for _, p := range s.recruiting.Connectors() {
		if _, err := s.recruiting.UpdateNetworkPerson(p.ID, map[string]string{"archived": "2026-09-05"}); err != nil {
			t.Fatal(err)
		}
	}
	g = graphGet(t, s.Handler(), "?degree=2")
	if !strings.Contains(strings.Join(g.Missing, " | "), "mark someone") {
		t.Fatalf("with nobody to ask, say so: %q", g.Missing)
	}
}

// ⚠ DRAWING IS NOT DECIDING. The whole layer is a read: loading the graph, at
// any degree, from any centre, must leave the vault byte-identical.
func TestGraphWritesNothing(t *testing.T) {
	s, _, vault := testGraphServer(t)
	before := graphSnapshot(t, vault)
	for _, q := range []string{"?degree=1", "?degree=3", "?q=fox", "?degree=2&kind=same_meeting"} {
		graphGet(t, s.Handler(), q)
	}
	after := graphSnapshot(t, vault)
	if len(before) != len(after) {
		t.Fatalf("loading the graph changed the file set: %d → %d", len(before), len(after))
	}
	for name, want := range before {
		if after[name] != want {
			t.Fatalf("loading the ego graph rewrote %s", name)
		}
	}
}
