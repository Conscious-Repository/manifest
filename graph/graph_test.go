package graph

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The aion triad through the graph: a task depends on a decision, produced
// an artifact, and a heuristic supports the decision; a person owns the task.
func triadEdges() []Edge {
	return []Edge{
		{From: R(KindTask, "inbox/gutters"), To: R(KindDecision, "aion-bl/pick-the-vendor"), Kind: EdgeDependsOn,
			Basis: "the vendor call gates the order", Source: "owner", Confidence: "0.90"},
		{From: R(KindTask, "inbox/gutters"), To: R(KindArtifact, "1f2e3d4c5b6a7980"), Kind: EdgeProduced,
			Basis: "outputs field on the task line", Source: "tasks"},
		{From: R(KindHeuristic, "h1a2b3c4"), To: R(KindDecision, "aion-bl/pick-the-vendor"), Kind: EdgeSupports,
			Basis: "solve the read problem first", Source: "owner", Confidence: "0.80"},
		{From: R(KindTask, "inbox/gutters"), To: R(KindPerson, "aion-net/ben-anderson"), Kind: EdgeOwnedBy,
			Basis: "owner field on the line", Source: "tasks"},
		{From: R(KindTask, "inbox/paint"), To: R(KindTask, "inbox/gutters"), Kind: EdgeDependsOn,
			Basis: "paint after the gutters", Source: "owner", Inferred: true, Confidence: "0.60"},
	}
}

func TestValidateRefusesClaimsWithoutBasis(t *testing.T) {
	v := Default()
	good := Edge{From: R(KindTask, "a"), To: R(KindDecision, "d"), Kind: EdgeDependsOn, Basis: "x", Source: "owner"}
	if err := Validate(good, v); err != nil {
		t.Fatalf("well-formed edge refused: %v", err)
	}
	cases := map[string]struct {
		mut  func(*Edge)
		want string
	}{
		"no from":         {func(e *Edge) { e.From.ID = "" }, "both endpoints"},
		"no to":           {func(e *Edge) { e.To = Ref{} }, "both endpoints"},
		"kindless ref":    {func(e *Edge) { e.To.Kind = "" }, "entity kind"},
		"unknown kind":    {func(e *Edge) { e.From.Kind = "idea" }, "entity kind \"idea\""},
		"bad edge kind":   {func(e *Edge) { e.Kind = "friend" }, "edge kind \"friend\" is not in the closed set"},
		"no basis":        {func(e *Edge) { e.Basis = "  " }, "needs a basis"},
		"no source":       {func(e *Edge) { e.Source = "" }, "needs the source"},
		"confidence > 1":  {func(e *Edge) { e.Confidence = "1.5" }, "between 0 and 1"},
		"confidence text": {func(e *Edge) { e.Confidence = "high" }, "between 0 and 1"},
	}
	for name, c := range cases {
		e := good
		c.mut(&e)
		err := Validate(e, v)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", name, c.want, err)
		}
	}
}

func TestVocabularyIsClosedButExtensible(t *testing.T) {
	base := Default()
	idea := Edge{From: R("idea", "x"), To: R(KindTask, "t"), Kind: "sparked", Basis: "b", Source: "s"}
	if Validate(idea, base) == nil {
		t.Fatal("a kind outside the default vocabulary must be refused")
	}
	ext := base.Extend([]string{"idea"}, []string{"sparked", EdgeDependsOn})
	if err := Validate(idea, ext); err != nil {
		t.Fatalf("extended vocabulary refused its own kind: %v", err)
	}
	if !ext.ValidEntityKind(KindTask) || !ext.ValidEdgeKind(EdgeCoauthor) {
		t.Fatal("extension must keep the base set")
	}
	if n := len(ext.EdgeKinds); n != len(base.EdgeKinds)+1 {
		t.Fatalf("extension must dedupe: %d edge kinds, want %d", n, len(base.EdgeKinds)+1)
	}
	if Validate(idea, base) == nil {
		t.Fatal("Extend must not mutate the base vocabulary")
	}
	// a domain composing its own closed set from scratch (recruiting's shape)
	people := Vocabulary{EntityKinds: []string{KindPerson}, EdgeKinds: []string{EdgeCoauthor}}
	if Validate(Edge{From: R(KindPerson, "a"), To: R(KindPerson, "b"), Kind: EdgeCoauthor, Basis: "b", Source: "s"}, people) != nil {
		t.Fatal("own vocabulary must accept its own kinds")
	}
	if Validate(Edge{From: R(KindTask, "a"), To: R(KindPerson, "b"), Kind: EdgeCoauthor, Basis: "b", Source: "s"}, people) == nil {
		t.Fatal("own vocabulary must refuse kinds outside it")
	}
}

func TestRefWireForm(t *testing.T) {
	r := ParseRef("task:aion:123")
	if r != (Ref{Kind: "task", ID: "aion:123"}) || r.String() != "task:aion:123" {
		t.Fatalf("the first colon splits: %+v", r)
	}
	if got := ParseRef(" bare "); got.Kind != "" || got.ID != "bare" {
		t.Fatalf("kindless ref: %+v", got)
	}
	if (Ref{}).String() != "" {
		t.Fatal("zero ref renders empty")
	}
}

// Fact vs inference is a STORED bit, and every provenance field survives
// the edges.md round trip; a hand-edited row (extra field, reordered keys)
// is byte-identical after parse → serialize.
func TestEdgesDocRoundTripKeepsFactsAndInferences(t *testing.T) {
	doc := ParseEdges(SeedFiles[EdgesFile])
	v := Default()
	for _, e := range triadEdges() {
		e.Evidence = "ev-" + e.Kind
		e.Observed = "2026-09-05"
		if _, err := doc.Add(e, v); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := doc.Add(Edge{From: R(KindTask, "a"), To: R(KindTask, "b"), Kind: EdgeDependsOn, Source: "owner"}, v); err == nil {
		t.Fatal("Add must refuse a basis-less claim")
	}
	out := SerializeEdges(doc)
	if !strings.Contains(out, "[from:: task:inbox/gutters] [to:: decision:aion-bl/pick-the-vendor] [kind:: depends_on]") ||
		!strings.Contains(out, "[inferred:: true]") || !strings.Contains(out, "[inferred:: false]") {
		t.Fatalf("serialized rows:\n%s", out)
	}
	back := ParseEdges(out).Edges()
	if len(back) != 5 {
		t.Fatalf("want 5 edges back, got %d", len(back))
	}
	facts, inferred := 0, 0
	for i, e := range back {
		want := triadEdges()[i]
		if e.From != want.From || e.To != want.To || e.Kind != want.Kind || e.Basis != want.Basis ||
			e.Confidence != want.Confidence || e.Inferred != want.Inferred || e.Source != want.Source ||
			e.Evidence != "ev-"+want.Kind || e.Observed != "2026-09-05" {
			t.Fatalf("edge %d changed in the round trip:\n got %+v\nwant %+v", i, e, want)
		}
		if e.Inferred {
			inferred++
		} else {
			facts++
		}
	}
	if facts != 4 || inferred != 1 {
		t.Fatalf("facts/inferences: %d/%d", facts, inferred)
	}
	if again := SerializeEdges(ParseEdges(out)); again != out {
		t.Fatal("serialize is not a fixpoint")
	}
	// a hand-edited row: reordered keys and an unknown field round-trip verbatim
	hand := out + "- [to:: person:x] [from:: task:y] [note:: by hand] [kind:: owned_by] [basis:: typed] [source:: owner]\n"
	if got := SerializeEdges(ParseEdges(hand)); got != hand {
		t.Fatalf("hand-authored row rewritten:\n%s", got)
	}
	last := ParseEdges(hand).Edges()[5]
	if last.From != R(KindTask, "y") || len(last.Unknown) != 1 || last.Unknown[0].Key != "note" {
		t.Fatalf("hand row projection: %+v", last)
	}
}

func TestEntitiesDocRoundTrip(t *testing.T) {
	doc := ParseEntities("")
	v := Default()
	if _, err := doc.Add(Entity{ID: "aion-bl/pick-the-vendor", Kind: KindDecision, Title: "Pick the vendor", Source: "aion", Added: "2026-09-05"}, v); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.Add(Entity{ID: "aion-bl/pick-the-vendor", Kind: KindDecision}, v); err == nil {
		t.Fatal("a second registration of the same ref must be refused")
	}
	if _, err := doc.Add(Entity{ID: "x", Kind: "idea"}, v); err == nil {
		t.Fatal("an entity kind outside the set must be refused")
	}
	out := SerializeEntities(doc)
	want := "- [id:: aion-bl/pick-the-vendor] [kind:: decision] [title:: Pick the vendor] [source:: aion] [added:: 2026-09-05]\n"
	if out != want {
		t.Fatalf("entities.md:\n%s\nwant:\n%s", out, want)
	}
	if e, ok := ParseEntities(out).Find(R(KindDecision, "aion-bl/pick-the-vendor")); !ok || e.Title != "Pick the vendor" {
		t.Fatalf("find: %+v %v", e, ok)
	}
}

func TestNeighborsDegreeAndReverseEdges(t *testing.T) {
	g := Build(triadEdges(), Default())
	if g.Size() != 5 {
		t.Fatalf("size %d", g.Size())
	}
	gutters := R(KindTask, "inbox/gutters")
	if d := g.Degree(gutters); d != (Degree{In: 1, Out: 3, Total: 4}) {
		t.Fatalf("degree %+v", d)
	}
	// outgoing first (sorted by neighbour), then incoming
	var got []string
	for _, h := range g.Neighbors(gutters, NeighborFilter{}) {
		got = append(got, string(h.Direction)+" "+h.Edge.Kind+" "+h.Node.String())
	}
	want := []string{
		"out produced artifact:1f2e3d4c5b6a7980",
		"out depends_on decision:aion-bl/pick-the-vendor",
		"out owned_by person:aion-net/ben-anderson",
		"in depends_on task:inbox/paint",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("neighbors:\n got %v\nwant %v", got, want)
	}
	// filters: direction, edge kind, node kind, facts only
	if n := g.Neighbors(gutters, NeighborFilter{Direction: In}); len(n) != 1 || n[0].Node != R(KindTask, "inbox/paint") {
		t.Fatalf("incoming filter: %+v", n)
	}
	if n := g.Neighbors(gutters, NeighborFilter{Kinds: []string{EdgeProduced}}); len(n) != 1 || n[0].Node.Kind != KindArtifact {
		t.Fatalf("kind filter: %+v", n)
	}
	if n := g.Neighbors(gutters, NeighborFilter{NodeKind: KindPerson}); len(n) != 1 {
		t.Fatalf("node-kind filter: %+v", n)
	}
	if n := g.Neighbors(gutters, NeighborFilter{Facts: true}); len(n) != 3 {
		t.Fatalf("facts-only must drop the inferred paint edge: %+v", n)
	}

	// the reverse edge: what depends on the decision (transitively) vs what
	// the decision depends on (nothing)
	decision := R(KindDecision, "aion-bl/pick-the-vendor")
	down := g.Downstream(decision, nil, 0)
	got = nil
	for _, r := range down {
		got = append(got, r.Node.String()+"@"+itoa(r.Depth))
	}
	if want := []string{"task:inbox/gutters@1", "task:inbox/paint@2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("downstream:\n got %v\nwant %v", got, want)
	}
	if up := g.Upstream(decision, nil, 0); len(up) != 0 {
		t.Fatalf("a decision depends on nothing here: %+v", up)
	}
	up := g.Upstream(R(KindTask, "inbox/paint"), nil, 1)
	if len(up) != 1 || up[0].Node != gutters || up[0].Via.Inferred != true {
		t.Fatalf("upstream depth 1: %+v", up)
	}
	if up = g.Upstream(R(KindTask, "inbox/paint"), nil, 0); len(up) != 2 || up[1].Node != decision {
		t.Fatalf("upstream unbounded: %+v", up)
	}
	// following a different kind: supports — who supports the decision
	if in := g.Neighbors(decision, NeighborFilter{Direction: In, Kinds: []string{EdgeSupports}}); len(in) != 1 || in[0].Node.Kind != KindHeuristic {
		t.Fatalf("supports: %+v", in)
	}
	if g.Has(R(KindTask, "nope")) || len(g.Neighbors(R(KindTask, "nope"), NeighborFilter{})) != 0 {
		t.Fatal("an unknown node has no neighbours")
	}
}

func TestBuildSkipsInvalidAndSelfLoops(t *testing.T) {
	edges := append(triadEdges(),
		Edge{From: R(KindTask, "a"), To: R(KindTask, "b"), Kind: EdgeDependsOn, Source: "owner"}, // no basis
		Edge{From: R(KindTask, "a"), To: R(KindTask, "a"), Kind: EdgeDependsOn, Basis: "b", Source: "owner"},
	)
	if g := Build(edges, Default()); g.Size() != 5 || g.Has(R(KindTask, "a")) {
		t.Fatalf("invalid rows must not be traversed: size %d", g.Size())
	}
}

// Recruiting's fixture, on typed refs: paths rank shortest first, then by
// cumulative confidence; an inferred hop marks the path; seeds are never
// passed through; the enumeration is stable.
func TestPathsRankAndMarkInference(t *testing.T) {
	p := func(id string) Ref { return R(KindPerson, id) }
	edges := []Edge{
		{From: p("aion-net/ben-anderson"), To: p("aion-net/dana-advisor"), Kind: EdgeDirectKnown, Basis: "ben says", Confidence: "0.95", Source: "owner"},
		{From: p("aion-net/dana-advisor"), To: p("cand/avery-quill"), Kind: EdgeAdvisor, Basis: "advised Avery's thesis", Confidence: "0.90", Source: "public_profile"},
		{From: p("aion-net/ben-anderson"), To: p("aion-net/kim-collab"), Kind: EdgeCoauthor, Basis: "paper 2021", Confidence: "0.80", Source: "openalex"},
		{From: p("cand/avery-quill"), To: p("aion-net/kim-collab"), Kind: EdgeSameLab, Basis: "same department 2019", Confidence: "0.60", Inferred: true, Source: "public_profile"},
	}
	g := Build(edges, Default())
	seeds := []Ref{p("aion-net/ben-anderson"), p("aion-net/rj-tevonian")}
	got := g.Paths(seeds, p("cand/avery-quill"), PathOptions{TopN: 5})
	if len(got) != 2 {
		t.Fatalf("want 2 paths, got %+v", got)
	}
	if got[0].String() != "person:aion-net/ben-anderson > person:aion-net/dana-advisor > person:cand/avery-quill" ||
		FormatConfidence(got[0].Confidence) != "0.85" || got[0].Inferred || len(got[0].Edges) != 2 {
		t.Fatalf("best path: %+v", got[0])
	}
	if got[1].String() != "person:aion-net/ben-anderson > person:aion-net/kim-collab > person:cand/avery-quill" ||
		FormatConfidence(got[1].Confidence) != "0.48" || !got[1].Inferred {
		t.Fatalf("second path: %+v", got[1])
	}
	// a direct hop outranks a stronger two-hop; topN caps after ranking
	edges = append(edges, Edge{From: p("aion-net/rj-tevonian"), To: p("cand/avery-quill"), Kind: EdgeDirectKnown, Basis: "rj knows Avery", Confidence: "0.70", Source: "owner"})
	g = Build(edges, Default())
	got = g.Paths(seeds, p("cand/avery-quill"), PathOptions{TopN: 5})
	if len(got) != 3 || got[0].Hops() != 1 || FormatConfidence(got[0].Confidence) != "0.70" {
		t.Fatalf("direct hop first: %+v", got)
	}
	if capped := g.Paths(seeds, p("cand/avery-quill"), PathOptions{TopN: 1}); len(capped) != 1 || capped[0].String() != got[0].String() {
		t.Fatalf("topN: %+v", capped)
	}
	// a seed is never passed through: ben → rj → avery is not offered even
	// though the edges exist
	edges = append(edges, Edge{From: p("aion-net/ben-anderson"), To: p("aion-net/rj-tevonian"), Kind: EdgeDirectKnown, Basis: "cofounders", Confidence: "0.99", Source: "owner"})
	g = Build(edges, Default())
	for _, path := range g.Paths(seeds, p("cand/avery-quill"), PathOptions{TopN: -1}) {
		if strings.Contains(path.String(), "rj-tevonian > person:cand") && path.Hops() > 1 {
			t.Fatalf("path passes through a seed: %s", path)
		}
	}
	// unknown target / unknown seed / no route → empty, never nil
	if r := g.Paths(seeds, p("nobody"), PathOptions{}); r == nil || len(r) != 0 {
		t.Fatalf("unknown target: %v", r)
	}
	// directed: avery has no outgoing route back to ben
	if r := g.Paths([]Ref{p("cand/avery-quill")}, p("aion-net/ben-anderson"), PathOptions{Directed: true}); len(r) != 0 {
		t.Fatalf("directed search must respect edge direction: %+v", r)
	}
	if r := g.Paths([]Ref{p("cand/avery-quill")}, p("aion-net/ben-anderson"), PathOptions{}); len(r) == 0 {
		t.Fatal("undirected search must find the route back")
	}
	// kind filter: only coauthor hops → no route from ben to avery
	if r := g.Paths(seeds, p("cand/avery-quill"), PathOptions{Kinds: []string{EdgeCoauthor}}); len(r) != 0 {
		t.Fatalf("kind-filtered search: %+v", r)
	}
}

func TestPathsAcrossDomains(t *testing.T) {
	g := Build(triadEdges(), Default())
	// heuristic → decision ← task: an undirected route from the heuristic to
	// the artifact the task produced, through the decision and the task
	got := g.Paths([]Ref{R(KindHeuristic, "h1a2b3c4")}, R(KindArtifact, "1f2e3d4c5b6a7980"), PathOptions{})
	if len(got) != 1 || got[0].String() != "heuristic:h1a2b3c4 > decision:aion-bl/pick-the-vendor > task:inbox/gutters > artifact:1f2e3d4c5b6a7980" {
		t.Fatalf("cross-domain path: %+v", got)
	}
}

func TestBridges(t *testing.T) {
	p := func(id string) Ref { return R(KindPerson, id) }
	edges := []Edge{
		{From: p("a"), To: p("x"), Kind: EdgeCoworker, Basis: "b", Source: "s"},
		{From: p("x"), To: p("b"), Kind: EdgeCoworker, Basis: "b", Source: "s"},
		{From: p("a"), To: p("y"), Kind: EdgeCoworker, Basis: "b", Source: "s"},
		{From: p("b"), To: p("y"), Kind: EdgeCoworker, Basis: "b", Source: "s"}, // y bridges too, edge pointing the other way
		{From: p("a"), To: p("z"), Kind: EdgeCoworker, Basis: "b", Source: "s"},
		{From: p("z"), To: p("w"), Kind: EdgeCoworker, Basis: "b", Source: "s"},
		{From: p("w"), To: p("b"), Kind: EdgeCoworker, Basis: "b", Source: "s"}, // longer: not a bridge
	}
	g := Build(edges, Default())
	var got []string
	for _, br := range g.Bridges(p("a"), p("b"), false) {
		got = append(got, br.Node.ID+":"+itoa(br.FromA)+"/"+itoa(br.FromB)+"/"+itoa(br.Distance))
	}
	if want := []string{"x:1/1/2", "y:1/1/2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bridges: %v", got)
	}
	// directed: only x sits on a forward route a → b
	if br := g.Bridges(p("a"), p("b"), true); len(br) != 1 || br[0].Node.ID != "x" {
		t.Fatalf("directed bridges: %+v", br)
	}
	if br := g.Bridges(p("a"), p("x"), false); len(br) != 0 {
		t.Fatalf("adjacent nodes have no bridge: %+v", br)
	}
	if br := g.Bridges(p("a"), p("nobody"), false); br == nil || len(br) != 0 {
		t.Fatalf("unconnected: %+v", br)
	}
}

func TestStoreWritesThroughInjectedFuncAndIsIdempotent(t *testing.T) {
	vault := t.TempDir()
	var writes []string
	write := func(abs string, data []byte) error {
		writes = append(writes, filepath.ToSlash(strings.TrimPrefix(abs, vault)))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, data, 0o644)
	}
	s := NewStore(vault, "system/graph", write)
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 || writes[0] != "/system/graph/entities.md" || writes[1] != "/system/graph/edges.md" {
		t.Fatalf("seed writes: %v", writes)
	}
	if err := s.Ensure(); err != nil || len(writes) != 2 {
		t.Fatalf("Ensure must never overwrite: %v %v", err, writes)
	}
	e := triadEdges()[0]
	got, added, err := s.AddEdge(e)
	if err != nil || !added || got.Key() != e.Key() {
		t.Fatalf("add: %+v %v %v", got, added, err)
	}
	if _, added, err = s.AddEdge(e); err != nil || added {
		t.Fatalf("replay must be a no-op: %v %v", added, err)
	}
	if len(writes) != 3 {
		t.Fatalf("a replay must not write: %v", writes)
	}
	if _, _, err = s.AddEdge(Edge{From: R(KindTask, "a"), To: R(KindTask, "b"), Kind: EdgeDependsOn, Source: "owner"}); err == nil {
		t.Fatal("store must refuse a basis-less claim")
	}
	// a nil writer fails loudly
	nowrite := NewStore(vault, "system/graph-nowrite", nil)
	if _, _, err := nowrite.AddEdge(e); err == nil || !strings.Contains(err.Error(), "no vault writer") {
		t.Fatalf("nil writer: %v", err)
	}
	// the file is the truth: a fresh store over the same root reads the claim
	fresh := NewStore(vault, "system/graph", write)
	if es := fresh.LoadEdges().Edges(); len(es) != 1 || es[0].To != e.To {
		t.Fatalf("reload: %+v", es)
	}
	// entities, same discipline
	ent := Entity{ID: "inbox/gutters", Kind: KindTask, Title: "gutters"}
	if _, added, err := s.AddEntity(ent); err != nil || !added {
		t.Fatalf("add entity: %v %v", added, err)
	}
	if _, added, err := s.AddEntity(ent); err != nil || added {
		t.Fatalf("entity replay: %v %v", added, err)
	}
	if _, _, err := s.AddEntity(Entity{ID: "x", Kind: "idea"}); err == nil {
		t.Fatal("entity kind outside the set must be refused")
	}
	// a hand-edited invalid row blocks the SAVE, not the load
	bad := "- [from:: task:a] [to:: task:b] [kind:: depends_on] [source:: owner]\n"
	if err := os.WriteFile(s.Path(EdgesFile), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if es := s.LoadEdges().Edges(); len(es) != 1 {
		t.Fatalf("load is total: %+v", es)
	}
	if err := s.SaveEdges(s.LoadEdges()); err == nil || !strings.Contains(err.Error(), "needs a basis") {
		t.Fatalf("save must refuse the invalid row: %v", err)
	}
	// Graph merges derived edges under the stored ones (stored key wins)
	if err := os.WriteFile(s.Path(EdgesFile), []byte(SeedFiles[EdgesFile]), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddEdge(e); err != nil {
		t.Fatal(err)
	}
	derived := []Edge{
		{From: R(KindTask, "a"), To: R(KindTask, "b"), Kind: EdgeDependsOn, Basis: "derived", Source: "tasks"},
		{From: R(KindTask, "b"), To: R(KindArtifact, "c"), Kind: EdgeProduced, Basis: "derived", Source: "tasks"},
	}
	g := s.Graph(derived...)
	if g.Size() != 3 {
		t.Fatalf("merged graph size %d", g.Size())
	}
	merged := Merge([]Edge{{From: R(KindTask, "a"), To: R(KindTask, "b"), Kind: EdgeDependsOn, Basis: "stated", Source: "owner"}}, derived)
	if len(merged) != 2 || merged[0].Basis != "stated" {
		t.Fatalf("stored claim must win: %+v", merged)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
