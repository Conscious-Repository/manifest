package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// The intake through the real mux: resolve says what a paste is, preview
// fills the scaffold from the source's own record, and commit writes it where
// the (possibly corrected) resolution says. Nothing here may write a contact
// field, and a preview leaves no run behind.

// stubPreview is an adapter that answers one reference, so the intake's
// preview path is exercised without leaving the process.
type stubPreview struct {
	sources.Manual
	facts sources.PreviewFacts
	err   error
	calls int
}

func (s *stubPreview) ID() string { return "openalex" }
func (s *stubPreview) Preview(_ context.Context, ref string) (sources.PreviewFacts, error) {
	s.calls++
	if s.err != nil {
		return sources.PreviewFacts{}, s.err
	}
	f := s.facts
	f.Ref = ref
	return f, nil
}

func testIntakeServer(t *testing.T, prev *stubPreview) (*Server, http.Handler) {
	t.Helper()
	s, _, _, _ := testRecruitingServer(t)
	rs, err := recruiting.NewRunStore(filepath.Join(t.TempDir(), "recruiting", "runs"), s.recruiting)
	if err != nil {
		t.Fatal(err)
	}
	rs.Register(sources.Manual{Owner: "benjamin"})
	if prev != nil {
		rs.Register(prev)
	}
	s.UseRecruitingRuns(rs)
	return s, s.Handler()
}

func intakeJSON(t *testing.T, mux http.Handler, path, body string) map[string]json.RawMessage {
	t.Helper()
	w := sourcesDo(t, mux, http.MethodPost, path, body)
	if w.Code != http.StatusOK {
		t.Fatalf("%s → %d: %s", path, w.Code, w.Body.String())
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The resolution rides on /intake/preview — one route that says what the
// paste is AND looks it up, rather than two that can answer differently.
func TestIntakeResolveSaysWhatAPasteIs(t *testing.T) {
	_, mux := testIntakeServer(t, nil)
	got := intakeJSON(t, mux, "/api/aion/recruiting/intake/preview",
		`{"text":"https://github.com/numpy/numpy"}`)
	var res recruiting.Resolution
	if err := json.Unmarshal(got["resolution"], &res); err != nil {
		t.Fatal(err)
	}
	if res.Kind != "github-repo" || res.Class != recruiting.SeedRepo || res.Dest != recruiting.DestSeed {
		t.Fatalf("resolution: %+v", res)
	}
	// the "I know them · via …" picker reads its connectors from the view, not
	// from here — the old /resolve echoed them back and the client never looked
	if w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/intake/preview", `{"text":"  "}`); w.Code != http.StatusOK {
		t.Fatalf("an empty paste resolves to nothing rather than erroring: %d", w.Code)
	}
}

func TestIntakePreviewFillsTheScaffoldFromTheSource(t *testing.T) {
	prev := &stubPreview{facts: sources.PreviewFacts{
		Kind: "work", Name: "Array programming with NumPy", Org: "Nature",
		URL:   "https://doi.org/10.1038/s41586-020-2649-2",
		Total: 26,
		Facts: []sources.PreviewFact{{Field: "name", Value: "Array programming with NumPy",
			Source: "openalex", URL: "https://doi.org/10.1038/s41586-020-2649-2"}},
		People: []sources.PreviewPerson{{Name: "K. Jarrod Millman", Key: "ext/orcid/0000-0002-5263-5070"}},
	}}
	_, mux := testIntakeServer(t, prev)
	got := intakeJSON(t, mux, "/api/aion/recruiting/intake/preview",
		`{"text":"10.1038/s41586-020-2649-2"}`)
	if got["preview"] == nil {
		t.Fatalf("no preview: %v", got)
	}
	var facts sources.PreviewFacts
	if err := json.Unmarshal(got["preview"], &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Name != "Array programming with NumPy" || facts.Total != 26 {
		t.Fatalf("facts: %+v", facts)
	}
	if len(facts.Facts) == 0 || facts.Facts[0].Source != "openalex" || facts.Facts[0].URL == "" {
		t.Fatal("every filled field names where it came from")
	}
	if prev.calls != 1 {
		t.Fatalf("one lookup, not %d", prev.calls)
	}
	// a preview is not a run: nothing is cached
	runs := intakeJSONGet(t, mux, "/api/aion/recruiting/sources/runs")
	if strings.Contains(string(runs["runs"]), "openalex") {
		t.Fatalf("a preview left a run behind: %s", runs["runs"])
	}
}

func TestIntakePreviewSaysWhyWhenThereIsNothingToLookUp(t *testing.T) {
	_, mux := testIntakeServer(t, nil)
	for _, tc := range []struct{ paste, want string }{
		{`https://x.com/someone`, "profiles"},
		{`Jane Q Smith`, "search"},
		// a lab page CAN be looked up (one page, no crawl) — but only when the
		// web source is wired, and this server registers none
		{`https://bme.washu.edu/people`, "web source is not wired"},
	} {
		got := intakeJSON(t, mux, "/api/aion/recruiting/intake/preview", `{"text":"`+tc.paste+`"}`)
		note := string(got["note"])
		if !strings.Contains(note, tc.want) {
			t.Fatalf("%s: note %q should explain (%q)", tc.paste, note, tc.want)
		}
	}
}

// A failed lookup is said out loud; the scaffold still opens so the owner can
// name the thing by hand.
func TestIntakePreviewFailureIsSaidNotSwallowed(t *testing.T) {
	prev := &stubPreview{err: errBadRequest("openalex: GET /works returned HTTP 404")}
	_, mux := testIntakeServer(t, prev)
	got := intakeJSON(t, mux, "/api/aion/recruiting/intake/preview", `{"text":"10.9999/nope"}`)
	if got["error"] == nil || !strings.Contains(string(got["error"]), "404") {
		t.Fatalf("the failure must reach the scaffold: %v", got)
	}
	if got["resolution"] == nil {
		t.Fatal("the resolution still stands when the lookup fails")
	}
}

func TestIntakeCommitsToEachDestination(t *testing.T) {
	s, mux := testIntakeServer(t, nil)

	// a seed, with the url that makes the row a link and sweepable
	intakeJSON(t, mux, "/api/aion/recruiting/intake",
		`{"dest":"seed","class":"lab","name":"WashU BME","url":"https://bme.washu.edu","text":"https://bme.washu.edu"}`)
	seeds := s.recruiting.View().Seeds
	last := seeds[len(seeds)-1]
	if last.Class != "lab" || last.URL != "https://bme.washu.edu" || last.Added == "" {
		t.Fatalf("seed: %+v", last)
	}

	// a media seed keeps its feed
	intakeJSON(t, mux, "/api/aion/recruiting/intake",
		`{"dest":"seed","class":"media","name":"The Imaging Podcast","url":"https://ex.test/show","feed":"https://ex.test/rss","text":"https://ex.test/rss"}`)
	seeds = s.recruiting.View().Seeds
	media := seeds[len(seeds)-1]
	var feed string
	for _, f := range media.Unknown {
		if f.Key == "feed" {
			feed = f.Value
		}
	}
	if media.Class != "media" || feed != "https://ex.test/rss" {
		t.Fatalf("media seed: %+v", media)
	}

	// someone the owner knows lands in the network, not on the board
	before := len(s.recruiting.View().Candidates)
	intakeJSON(t, mux, "/api/aion/recruiting/intake", `{"dest":"network","name":"Dana Fox","org":"Hyperfine"}`)
	if n := len(s.recruiting.View().Candidates); n != before {
		t.Fatalf("a network person is not a candidate: %d → %d", before, n)
	}
	people := s.recruiting.View().Network.People
	if people[len(people)-1].Name != "Dana Fox" {
		t.Fatalf("people: %+v", people)
	}

	// a candidate, with the profile link recorded in its own slot
	got := intakeJSON(t, mux, "/api/aion/recruiting/intake",
		`{"dest":"candidate","name":"Kai Osei","url":"https://x.com/kaiosei","profile":"x","text":"https://x.com/kaiosei"}`)
	var created struct{ Kind, ID, Name string }
	if err := json.Unmarshal(got["created"], &created); err != nil {
		t.Fatal(err)
	}
	if created.Kind != "candidate" || created.ID == "" {
		t.Fatalf("created: %+v", created)
	}
	for _, c := range s.recruiting.View().Candidates {
		if c.ID != created.ID {
			continue
		}
		if c.Profile["x"] != "https://x.com/kaiosei" {
			t.Fatalf("the X link is recorded on the person: %+v", c.Profile)
		}
		if c.Profile["email"] != "" || c.Profile["phone"] != "" {
			t.Fatalf("D15: intake filled a contact field: %+v", c.Profile)
		}
	}
}

// "I know them" is the only human-relationship edge in the system, and it is
// worth exactly as much as the person asserting it — so it must name one.
func TestIntakeKnownNeedsTheAsserter(t *testing.T) {
	s, mux := testIntakeServer(t, nil)
	w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/intake",
		`{"dest":"candidate","name":"Kai Osei","known":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a known-person claim with no asserter: %d %s", w.Code, w.Body.String())
	}
	via := s.recruiting.View().Network.People[0].ID
	intakeJSON(t, mux, "/api/aion/recruiting/intake",
		`{"dest":"candidate","name":"Kai Osei","known":true,"knownVia":"`+via+`","text":"met at ISMRM"}`)
	edges := s.recruiting.View().Network.Edges
	if len(edges) == 0 {
		t.Fatal("the owner's own word is an edge — the only one that starts a path")
	}
	e := edges[len(edges)-1]
	if e.From != via || e.Kind != "direct_known" || e.Basis == "" {
		t.Fatalf("edge: %+v", e)
	}
	if !strings.HasPrefix(e.To, "cand/") {
		t.Fatalf("the edge lands on the record: %+v", e)
	}
}

func intakeJSONGet(t *testing.T, mux http.Handler, path string) map[string]json.RawMessage {
	t.Helper()
	w := sourcesDo(t, mux, http.MethodGet, path, "")
	var out map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
