package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/graph"
	"manifest/ledger"
	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// Enrichment Phase 3 through the real mux: accepting an enriched draft
// derives person → topic expertise edges into the general graph (readable at
// /api/graph/edges) with their provenance in the ledger under the candidate;
// a queued draft carries the intro paths the network derives for it; and a
// pass can be undone through its own route, ledgered beside the pass.

// topicAdapter is a scholarly source that names topics for the one person it
// returns — what openalex does, without the network.
type topicAdapter struct{}

func (topicAdapter) ID() string         { return "topical" }
func (topicAdapter) Kind() sources.Kind { return sources.KindScholarly }
func (topicAdapter) Scope() []sources.ScopeField {
	return []sources.ScopeField{{Key: "query", Label: "query", Required: true}}
}
func (topicAdapter) Search(_ context.Context, _ sources.Scope) ([]sources.CandidateDraft, error) {
	return []sources.CandidateDraft{{
		SourceID: "topical", ExternalID: "A77", Name: "Avery Quill", Org: "Example Lab", Title: "research engineer",
		Links:  []string{"https://orcid.org/0000-0001-2345-6789", "https://avery.example"},
		Topics: []string{"Low-Field MRI", "Coil Design", "Compressed Sensing", "Signal Processing", "Optics"},
		Evidence: []sources.Evidence{{
			SourceID: "topical", URLOrFile: "https://openalex.org/A77", RetrievedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			Snippet: "works_count: 9 · topics: Low-Field MRI; Coil Design", Kind: sources.EvidencePublication, Trust: sources.TrustMedium,
		}},
	}}, nil
}
func (topicAdapter) Enrich(_ context.Context, d sources.CandidateDraft) (sources.CandidateDraft, error) {
	return d, nil
}
func (topicAdapter) GraphEdges(_ context.Context, d sources.CandidateDraft) ([]sources.EdgeClaim, error) {
	return d.Edges, nil
}

// testRecruitingPhase3Server is the sources server plus the topical adapter,
// a graph store in the same vault, and a ledger.
func testRecruitingPhase3Server(t *testing.T) (*Server, http.Handler, *graph.Store, *ledger.Store) {
	t.Helper()
	s, _ := testRecruitingSourcesServer(t)
	s.recruitingRuns.Register(topicAdapter{})
	vault := s.recruiting.Path("../../..") // system/aion/recruiting → the vault root
	write := func(abs string, data []byte) error {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, data, 0o644)
	}
	gs := graph.NewStore(vault, "system/graph", write)
	if err := gs.Ensure(); err != nil {
		t.Fatal(err)
	}
	s.UseGraph(gs)
	led, err := ledger.New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	s.UseLedger(led)
	return s, s.Handler(), gs, led
}

const topicalRunBody = `{"source":"topical","role":"role/mri-engineer","query":"low-field mri"}`

func ledgerKinds(t *testing.T, led *ledger.Store, kind, id string) []string {
	t.Helper()
	h, err := led.History(kind, id)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range h.Entries {
		kinds = append(kinds, e.Kind)
	}
	return kinds
}

func TestRecruitingSourceAcceptDerivesKnowledgeEdges(t *testing.T) {
	_, mux, gs, led := testRecruitingPhase3Server(t)
	run := startRun(t, mux, topicalRunBody)

	p := decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+run.ID+"/d1", ""))
	if p.Candidate == nil || p.Candidate.ID == "" {
		t.Fatalf("accept answered without a candidate: %s", string(p.Raw["candidate"]))
	}
	cand := *p.Candidate
	if _, has := p.Raw["knowledgeError"]; has {
		t.Fatalf("knowledge derivation failed: %s", string(p.Raw["knowledgeError"]))
	}
	if !strings.Contains(string(p.Raw["knowledge"]), `"addedEdges":[{`) {
		t.Fatalf("the payload should report what landed: %s", string(p.Raw["knowledge"]))
	}

	// the general graph now answers "what does this person know": four
	// inferred expertise edges (the fifth topic is past the cap), stored, not
	// derived-on-read
	code, r := artifactsDo(t, &Server{graphStore: gs}, "GET", "/api/graph/edges?ref=person:"+cand.ID+"&kind=expertise", "")
	if code != 200 {
		t.Fatalf("graph edges: %d %+v", code, r)
	}
	edges := r["edges"].([]any)
	if len(edges) != recruiting.MaxKnowledgeTopics {
		t.Fatalf("want %d expertise edges, got %d: %+v", recruiting.MaxKnowledgeTopics, len(edges), edges)
	}
	var topics []string
	for _, e := range edges {
		m := e.(map[string]any)
		to := m["to"].(map[string]any)
		topics = append(topics, to["id"].(string))
		if to["kind"] != graph.KindTopic || m["inferred"] != true || m["derived"] != false || m["source"] != "topical" || m["confidence"] != recruiting.KnowledgeConfidence {
			t.Errorf("edge shape: %+v", m)
		}
		if !strings.Contains(m["basis"].(string), "https://openalex.org/A77") || !strings.Contains(m["evidence"].(string), "https://openalex.org/A77") {
			t.Errorf("edge provenance should name the work: %+v", m)
		}
	}
	if strings.Join(topics, ",") != "low field mri,coil design,compressed sensing,signal processing" {
		t.Errorf("topics: %v", topics)
	}
	// the person is registered rich: record path + classified links
	ent, ok := gs.LoadEntities().Find(graph.R(graph.KindPerson, cand.ID))
	if !ok || ent.Title != "Avery Quill" || !strings.HasSuffix(ent.Ref, "/candidates/"+cand.Slug+".md") || ent.Links["orcid"] != "https://orcid.org/0000-0001-2345-6789" || ent.Links["homepage"] != "https://avery.example" {
		t.Fatalf("person entity: %+v", ent)
	}
	// the record itself still carries its own fields — nothing moved
	if len(cand.Evidence) == 0 || cand.Profile["website"] != "https://avery.example" {
		t.Errorf("the record lost its fields: %+v", cand.Profile)
	}

	// provenance: the accept and every derived claim, under the candidate
	kinds := ledgerKinds(t, led, graph.KindPerson, cand.ID)
	n := map[string]int{}
	for _, k := range kinds {
		n[k]++
	}
	if n["recruiting.draft.accepted"] != 1 || n["graph.edge.derived"] != recruiting.MaxKnowledgeTopics || n["graph.entity.added"] != 1 {
		t.Fatalf("ledger under %s: %v", cand.ID, kinds)
	}
	if h, _ := led.History(graph.KindPerson, cand.ID); h.Entries[len(h.Entries)-1].Meta["path"] != nil {
		t.Errorf("no network edge, so the accept event names no path: %+v", h.Entries[len(h.Entries)-1].Meta)
	}
	// each topic node was registered under itself
	if k := ledgerKinds(t, led, graph.KindTopic, "coil design"); len(k) != 1 || k[0] != "graph.entity.added" {
		t.Errorf("topic registration event: %v", k)
	}

	// a second person naming two of the same topics joins the same nodes —
	// idempotent registration, new edges only
	before := len(gs.LoadEntities().Entities())
	if _, err := recruiting.ApplyKnowledge(gs, recruiting.DeriveKnowledge(sources.CandidateDraft{
		SourceID: "topical", Name: "Kim Collab", Topics: []string{"Coil Design", "Optics"},
	}, "cand/kim-collab", "", time.Now())); err != nil {
		t.Fatal(err)
	}
	if after := len(gs.LoadEntities().Entities()); after != before+2 { // kim + optics
		t.Errorf("entities before %d after %d", before, after)
	}
	code, r = artifactsDo(t, &Server{graphStore: gs}, "GET", "/api/graph/neighbors?ref=topic:coil%20design&nodeKind=person", "")
	if code != 200 || r["count"].(float64) != 2 {
		t.Errorf("who knows coil design: %d %+v", code, r)
	}
}

// Without a graph store the accept still lands the record and says plainly
// that nothing was derived.
func TestRecruitingSourceAcceptWithoutGraphStoreSaysSo(t *testing.T) {
	s, mux := testRecruitingSourcesServer(t)
	s.recruitingRuns.Register(topicAdapter{})
	mux = s.Handler()
	run := startRun(t, mux, topicalRunBody)
	p := decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+run.ID+"/d1", ""))
	if p.Candidate == nil || !strings.Contains(string(p.Raw["knowledgeError"]), "not configured") {
		t.Fatalf("accept without a graph store: cand=%v err=%s", p.Candidate != nil, string(p.Raw["knowledgeError"]))
	}
	if !strings.Contains(string(p.Raw["knowledge"]), `"addedEdges":[]`) {
		t.Errorf("nothing should claim to have landed: %s", string(p.Raw["knowledge"]))
	}
}

func TestRecruitingSourceUnrejectRoute(t *testing.T) {
	s, mux, _, led := testRecruitingPhase3Server(t)
	run := startRun(t, mux, topicalRunBody)
	obj := run.ID + "/d1"

	// undo before any pass is refused
	if w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/unreject/"+run.ID+"/d1", ""); w.Code == http.StatusOK {
		t.Fatalf("un-passed a draft that was never passed: %s", w.Body.String())
	}
	passed := decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/reject/"+run.ID+"/d1", ""))
	if passed.Run.Drafts[0].Status != recruiting.DraftRejected || passed.Run.ExpiresAt.IsZero() {
		t.Fatalf("after pass: %+v", passed.Run.RunState)
	}
	back := decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/unreject/"+run.ID+"/d1", ""))
	d := back.Run.Drafts[0]
	if d.Status != recruiting.DraftNew || !d.DecidedAt.IsZero() || back.Run.Counts.Rejected != 0 || !back.Run.ExpiresAt.IsZero() {
		t.Fatalf("after undo: %+v %+v", d, back.Run.RunState)
	}
	if back.View != nil {
		t.Error("an undo wrote no record, so no board view should ride along")
	}
	if boardCandidates(t, mux); len(boardCandidates(t, mux)) != 0 {
		t.Error("an undo put someone on the board")
	}
	// the pass and its undo read as a pair under the draft
	if kinds := ledgerKinds(t, led, "draft", obj); strings.Join(kinds, ",") != "recruiting.draft.passed,recruiting.draft.unpassed" {
		t.Errorf("draft history: %v", kinds)
	}
	// and the draft is decidable again
	if w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+run.ID+"/d1", ""); w.Code != http.StatusOK {
		t.Fatalf("accept after undo: %d %s", w.Code, w.Body.String())
	}
	if got, _ := s.recruitingRuns.Get(run.ID); got.Drafts[0].Status != recruiting.DraftAccepted {
		t.Errorf("draft after accept: %+v", got.Drafts[0])
	}
}

// A queued draft the network already names by an external key carries a
// real route on the wire, before anyone accepts it; the record carries the
// same route after.
func TestRecruitingSourceDraftsCarryDerivedPaths(t *testing.T) {
	s, mux, _, led := testRecruitingPhase3Server(t)
	const orcid = "ext/orcid/0000-0001-2345-6789"
	owner := ""
	for _, p := range s.recruiting.LoadNetworkPeople().People() {
		if p.Consent == "owner" {
			owner = p.ID
			break
		}
	}
	if owner == "" {
		if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{ID: "aion-net/ben-anderson", Name: "Benjamin Anderson", Consent: "owner"}); err != nil {
			t.Fatal(err)
		}
		owner = "aion-net/ben-anderson"
	}
	edges := s.recruiting.LoadEdges()
	if _, err := edges.Add(recruiting.Edge{From: owner, To: orcid, Kind: "coauthor", Basis: "paper 2024", Confidence: "0.80", Source: "openalex", Observed: "2026-06-01"}); err != nil {
		t.Fatal(err)
	}
	if err := s.recruiting.SaveEdges(edges); err != nil {
		t.Fatal(err)
	}
	run := startRun(t, mux, topicalRunBody)
	if p := run.Drafts[0].Paths; len(p) != 1 || p[0].Path != owner+" > "+orcid || p[0].Kind != recruiting.PathKindDerived || p[0].Observed != "2026-06-01" || p[0].Weakest == "" {
		t.Fatalf("queued draft paths: %+v", p)
	}
	acc := decodeSources(t, sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/sources/accept/"+run.ID+"/d1", ""))
	if p := acc.Run.Drafts[0].Paths; len(p) != 1 || p[0].Path != owner+" > "+acc.Candidate.ID {
		t.Fatalf("accepted draft paths: %+v", p)
	}
	if h, _ := led.History(graph.KindPerson, acc.Candidate.ID); len(h.Entries) == 0 || h.Entries[len(h.Entries)-1].Meta["path"] != owner+" > "+acc.Candidate.ID {
		t.Errorf("the accept event should name the best path: %+v", h.Entries)
	}
}
