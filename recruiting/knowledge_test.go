package recruiting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/graph"
	"manifest/recruiting/sources"
)

// The knowledge overlay (Phase 3): an accepted draft's topics become
// person → topic expertise edges in the GENERAL graph — inferred, capped,
// with the works as basis and their URLs as evidence — and applying them
// twice adds nothing. The same store carries the platform's own edges, so a
// "who knows X" query and a task dependency walk run over one graph.

func topicDraft() sources.CandidateDraft {
	d := citedDraft("Avery Quill", "A1")
	d.SourceID = "openalex"
	d.Homepage = "https://avery.example"
	d.Orcid = "https://orcid.org/0000-0001-2345-6789"
	d.Site = "https://lab.example/people/avery"
	d.Topics = []string{"Diffusion MRI Reconstruction", "diffusion-mri reconstruction", "Compressed Sensing",
		"Low-Field MRI", "Coil Design", "Signal Processing", "Optics"}
	d.Evidence = append(d.Evidence, sources.Evidence{
		SourceID: "openalex", URLOrFile: "https://openalex.org/A1", RetrievedAt: testNow,
		Snippet: "works_count: 12 · topics: Diffusion MRI Reconstruction; Compressed Sensing", Kind: sources.EvidencePublication,
	})
	return d
}

func testGraphStore(t *testing.T) (*graph.Store, string) {
	t.Helper()
	vault := t.TempDir()
	write := func(abs string, b []byte) error {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, b, 0o644)
	}
	gs := graph.NewStore(vault, "system/graph", write)
	if err := gs.Ensure(); err != nil {
		t.Fatal(err)
	}
	return gs, vault
}

func TestDeriveKnowledgeCapsDedupesAndMarksInferred(t *testing.T) {
	k := DeriveKnowledge(topicDraft(), "cand/avery-quill", "system/aion/recruiting/candidates/avery-quill.md", testNow)

	p := k.Person
	if p.Kind != graph.KindPerson || p.ID != "cand/avery-quill" || p.Title != "Avery Quill" || p.Source != "openalex" || p.Added != "2026-09-02" {
		t.Fatalf("person entity: %+v", p)
	}
	if p.Ref != "system/aion/recruiting/candidates/avery-quill.md" {
		t.Errorf("person ref should be the record path: %q", p.Ref)
	}
	if p.Links["homepage"] != "https://avery.example" || p.Links["orcid"] != "https://orcid.org/0000-0001-2345-6789" || p.Links["site"] != "https://lab.example/people/avery" {
		t.Errorf("classified links should ride on the entity: %v", p.Links)
	}
	if _, has := p.Links["linkedin"]; has {
		t.Errorf("an empty class is absent, not blank: %v", p.Links)
	}

	// 7 topics named, one a normalized duplicate → 6 distinct, capped at 4
	if len(k.Edges) != MaxKnowledgeTopics || len(k.Topics) != MaxKnowledgeTopics {
		t.Fatalf("want %d capped edges/topics, got %d/%d: %+v", MaxKnowledgeTopics, len(k.Edges), len(k.Topics), k.Edges)
	}
	wantTopics := []string{"diffusion mri reconstruction", "compressed sensing", "low field mri", "coil design"}
	for i, e := range k.Edges {
		if e.To.Kind != graph.KindTopic || e.To.ID != wantTopics[i] {
			t.Errorf("edge %d to %s, want topic:%s", i, e.To, wantTopics[i])
		}
		if e.From != k.Person.AsRef() || e.Kind != graph.EdgeExpertise {
			t.Errorf("edge %d shape: %+v", i, e)
		}
		if !e.Inferred {
			t.Errorf("edge %d must be inferred — a topic list is never a stated fact", i)
		}
		if e.Confidence != KnowledgeConfidence || e.Source != "openalex" || e.Observed != "2026-09-02" {
			t.Errorf("edge %d provenance: conf=%q src=%q obs=%q", i, e.Confidence, e.Source, e.Observed)
		}
		if !strings.HasPrefix(e.Basis, "attributed works ") || !strings.Contains(e.Basis, "https://example.test/paper/A1") || !strings.Contains(e.Basis, "https://openalex.org/A1") {
			t.Errorf("edge %d basis should name the works: %q", i, e.Basis)
		}
		if !strings.Contains(e.Evidence, "https://openalex.org/A1") {
			t.Errorf("edge %d evidence should carry the work URLs: %q", i, e.Evidence)
		}
		if err := graph.Validate(e, graph.Default()); err != nil {
			t.Errorf("edge %d fails the platform validator: %v", i, err)
		}
	}
	// the topic keeps the source's wording; the id is the normalized key
	if k.Topics[0].Title != "Diffusion MRI Reconstruction" || k.Topics[0].ID != "diffusion mri reconstruction" || k.Topics[0].Kind != graph.KindTopic {
		t.Errorf("topic entity: %+v", k.Topics[0])
	}

	// no topics → the person alone, no edge invented
	bare := citedDraft("Kim Collab", "A2")
	if k := DeriveKnowledge(bare, "cand/kim-collab", "", testNow); len(k.Edges) != 0 || len(k.Topics) != 0 || k.Person.ID != "cand/kim-collab" || k.Person.Links != nil {
		t.Errorf("a draft with no topics: %+v", k)
	}
	// no works cited → the basis still says what it rests on
	noWorks := topicDraft()
	noWorks.Evidence = nil
	if k := DeriveKnowledge(noWorks, "cand/x", "", testNow); k.Edges[0].Basis != "attributed works named by openalex" || k.Edges[0].Evidence != "" {
		t.Errorf("basis without works: %+v", k.Edges[0])
	}
	// no candidate → nothing
	if k := DeriveKnowledge(topicDraft(), " ", "", testNow); len(k.Edges) != 0 {
		t.Errorf("no candidate id, no claims: %+v", k)
	}
}

func TestApplyKnowledgeIsIdempotentThroughTheStore(t *testing.T) {
	gs, vault := testGraphStore(t)
	k := DeriveKnowledge(topicDraft(), "cand/avery-quill", "system/aion/recruiting/candidates/avery-quill.md", testNow)

	first, err := ApplyKnowledge(gs, k)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.AddedEntities) != 1+MaxKnowledgeTopics || len(first.AddedEdges) != MaxKnowledgeTopics {
		t.Fatalf("first apply: %d entities, %d edges", len(first.AddedEntities), len(first.AddedEdges))
	}
	before := snapshot(t, vault)

	second, err := ApplyKnowledge(gs, k)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.AddedEntities) != 0 || len(second.AddedEdges) != 0 {
		t.Fatalf("a replay added %d entities / %d edges", len(second.AddedEntities), len(second.AddedEdges))
	}
	if after := snapshot(t, vault); len(after) != len(before) {
		t.Fatalf("replay changed the vault")
	} else {
		for p, b := range before {
			if after[p] != b {
				t.Fatalf("replay rewrote %s", p)
			}
		}
	}
	// a re-accept with one NEW topic adds exactly that edge
	more := topicDraft()
	more.Topics = []string{"Coil Design", "Optics"}
	third, err := ApplyKnowledge(gs, DeriveKnowledge(more, "cand/avery-quill", "", testNow))
	if err != nil {
		t.Fatal(err)
	}
	if len(third.AddedEdges) != 1 || third.AddedEdges[0].To.ID != "optics" || len(third.AddedEntities) != 1 {
		t.Fatalf("incremental apply: %+v / %+v", third.AddedEdges, third.AddedEntities)
	}
	if edges := gs.LoadEdges().Edges(); len(edges) != MaxKnowledgeTopics+1 {
		t.Fatalf("stored edges: %d", len(edges))
	}
	// the stored person row round-trips its links
	if got, ok := gs.LoadEntities().Find(graph.R(graph.KindPerson, "cand/avery-quill")); !ok || got.Links["homepage"] != "https://avery.example" || got.Links["orcid"] == "" {
		t.Fatalf("person entity on file: %+v", got)
	}
	// a nil writer is a no-op, not a panic
	if res, err := ApplyKnowledge(nil, k); err != nil || len(res.AddedEdges) != 0 {
		t.Errorf("nil writer: %+v %v", res, err)
	}
}

// One graph, two machines: the platform's task → artifact claim and
// recruiting's person → topic claim live in the same store and answer the
// same queries — "who knows X" is a neighbour read on the topic node, the
// task's dependency walk is untouched by it, and the recruiting NETWORK
// vocabulary (person-only) still refuses the knowledge kind, so the social
// graph and the knowledge overlay cannot be conflated.
func TestKnowledgeSharesTheCrossDomainGraph(t *testing.T) {
	gs, _ := testGraphStore(t)
	if _, _, err := gs.AddEdge(graph.Edge{From: graph.R(graph.KindTask, "inbox/coil"), To: graph.R(graph.KindArtifact, "1f2e3d4c"),
		Kind: graph.EdgeProduced, Basis: "outputs field", Source: "tasks"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Avery Quill", "Kim Collab"} {
		d := topicDraft()
		d.Name = name
		id := CandidateID(NewCandidateSlug(name, nil))
		if _, err := ApplyKnowledge(gs, DeriveKnowledge(d, id, "", testNow)); err != nil {
			t.Fatal(err)
		}
	}
	g := gs.Graph()
	who := g.Neighbors(TopicRef("Compressed Sensing"), graph.NeighborFilter{Direction: graph.In, NodeKind: graph.KindPerson})
	if len(who) != 2 || who[0].Node.ID != "cand/avery-quill" || who[1].Node.ID != "cand/kim-collab" {
		t.Fatalf("who knows compressed sensing: %+v", who)
	}
	// facts-only hides every inferred expertise claim
	if facts := g.Neighbors(TopicRef("Compressed Sensing"), graph.NeighborFilter{Facts: true}); len(facts) != 0 {
		t.Errorf("an inferred topic edge passed a facts-only read: %+v", facts)
	}
	if up := g.Upstream(graph.R(graph.KindTask, "inbox/coil"), []string{graph.EdgeProduced}, 0); len(up) != 1 || up[0].Node.Kind != graph.KindArtifact {
		t.Errorf("the platform edge still walks: %+v", up)
	}
	// a person's knowledge is a typed read, never mixed into their social hops
	knows := g.Neighbors(PersonRef("cand/avery-quill"), graph.NeighborFilter{Kinds: []string{graph.EdgeExpertise}})
	if len(knows) != MaxKnowledgeTopics {
		t.Errorf("avery's expertise: %+v", knows)
	}
	// the recruiting network vocabulary refuses the knowledge kind
	e := DeriveKnowledge(topicDraft(), "cand/avery-quill", "", testNow).Edges[0]
	if err := graph.Validate(e, EdgeVocabulary()); err == nil {
		t.Error("the person-only network vocabulary accepted an expertise edge")
	}
	if err := graph.Validate(e, graph.Default()); err != nil {
		t.Errorf("the platform vocabulary refused it: %v", err)
	}
}
