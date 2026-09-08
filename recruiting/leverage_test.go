package recruiting

import (
	"math"
	"strings"
	"testing"

	"manifest/graph"
)

// Domain leverage (leverage.go): the ranking is a projection over expertise
// edges and person ties, every component visible, honest at zero.

func expertise(person, topic, conf, source string) graph.Edge {
	return graph.Edge{
		From: PersonRef(person), To: TopicRef(topic), Kind: graph.EdgeExpertise,
		Basis: "attributed works https://doi.org/10.1/" + person, Confidence: conf, Inferred: true,
		Source: source, Evidence: "https://doi.org/10.1/" + person + ", https://openalex.org/A-" + person,
	}
}

func tie(a, b, kind, conf string, inferred bool) Edge {
	return Edge{From: a, To: b, Kind: kind, Basis: "on file", Confidence: conf, Inferred: inferred, Source: "openalex", Evidence: "https://doi.org/10.1/" + a}
}

func leverageFixture() ([]graph.Edge, []Edge, func(string) (string, bool)) {
	knowledge := []graph.Edge{
		expertise("cand/a", "Low-Field MRI", "0.60", "openalex"),
		expertise("cand/b", "low field mri", "0.60", "openalex"),
		expertise("cand/c", "Low-Field MRI", "0.35", "deepseek"),
		expertise("cand/a", "Optics", "0.60", "openalex"),
		// a stray expertise edge from something that is not a person
		{From: graph.R(graph.KindOrg, "x"), To: TopicRef("Low-Field MRI"), Kind: graph.EdgeExpertise, Basis: "b", Source: "s"},
	}
	ties := []Edge{
		tie("cand/a", "cand/b", "coauthor", "0.55", false),
		tie("cand/b", "cand/c", "same_lab", "0.45", true),
		tie("cand/b", "cand/d", "coauthor", "0.55", false),
		tie("cand/c", "aion-net/e", "coauthor", "0.55", false),
		tie("cand/a", "ext/orcid/0000-0009-9999-9999", "coauthor", "0.55", false),
		// an invalid row (no basis) is skipped, never walked
		{From: "cand/a", To: "cand/c", Kind: "coauthor", Confidence: "0.99"},
	}
	names := map[string]string{"cand/a": "Avery", "cand/b": "Blake", "cand/c": "Casey", "cand/d": "Dana", "aion-net/e": "Eli"}
	display := func(id string) (string, bool) {
		n, ok := names[id]
		if !ok {
			return id, false
		}
		return n, true
	}
	return knowledge, ties, display
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.0005 }

func TestRankLeverageOrdersByKnowledgeTimesReach(t *testing.T) {
	knowledge, ties, display := leverageFixture()
	res := RankLeverage("low-field mri", knowledge, ties, display, LeverageOptions{})
	if !res.Known || res.Experts != 3 || res.TopicID != "low field mri" || res.Hops != DefaultLeverageHops || res.Formula == "" {
		t.Fatalf("header: %+v", res)
	}
	var ids []string
	for _, p := range res.People {
		ids = append(ids, p.ID)
	}
	// B: 0.60 × (1 + 0.55 + 0.45 + 0.55 + 0.55·0.5) = 0.60 × 2.825 = 1.695
	// A: 0.60 × (1 + 0.55 + 0.45·0.5 + 0.55·0.5)   = 0.60 × 2.05  = 1.230
	// C: 0.35 × (1 + 0.45 + 0.55 + 0.55·0.5 + 0.55·0.5) = 0.35 × 2.55 = 0.8925
	// D, E: adjacent to an expert, no expertise → 0, ordered by reach
	if strings.Join(ids, ",") != "cand/b,cand/a,cand/c,cand/d,aion-net/e" {
		t.Fatalf("order: %v", ids)
	}
	b, a, c, d := res.People[0], res.People[1], res.People[2], res.People[3]
	if !near(b.Leverage, 1.695) || !near(b.Knowledge, 0.60) || !near(b.Connectivity, 1.825) || b.Role != "expert" || b.Name != "Blake" {
		t.Fatalf("B: %+v", b)
	}
	if !near(a.Leverage, 1.23) || !near(a.Connectivity, 1.05) || a.External != 1 || a.TieCount != 3 {
		t.Fatalf("A: %+v", a)
	}
	if math.Abs(c.Leverage-0.8925) > 0.001 || c.Expertise == nil || c.Expertise.Source != "deepseek" || c.Expertise.Confidence != "0.35" {
		t.Fatalf("C: %+v", c)
	}
	if d.Role != "adjacent" || d.Leverage != 0 || d.Knowledge != 0 || d.Expertise != nil || !near(d.Connectivity, 1.05) {
		t.Fatalf("ties but no expertise ranks at zero, with the reach visible: %+v", d)
	}
	// the basis is in the open: A's expertise names its works; A's ties name
	// the edge, the hop, and the node the walk came through
	if len(a.Expertise.Works) != 2 || a.Expertise.Works[0] != "https://doi.org/10.1/cand/a" {
		t.Fatalf("works: %+v", a.Expertise.Works)
	}
	byPerson := map[string]LeverageTie{}
	for _, x := range a.Ties {
		byPerson[x.Person] = x
	}
	if x := byPerson["cand/b"]; x.Hop != 1 || x.Kind != "coauthor" || !near(x.Contribution, 0.55) || x.Name != "Blake" || !x.Known || x.Basis == "" || x.Evidence == "" {
		t.Fatalf("A–B: %+v", x)
	}
	if x := byPerson["cand/d"]; x.Hop != 2 || x.Via != "cand/b" || !near(x.Contribution, 0.275) {
		t.Fatalf("A–D via B: %+v", x)
	}
	if x := byPerson["cand/c"]; x.Hop != 2 || x.Kind != "same_lab" || !x.Inferred || !near(x.Contribution, 0.225) {
		t.Fatalf("A–C via B (the invalid direct row is never walked): %+v", x)
	}
	if x, ok := byPerson["ext/orcid/0000-0009-9999-9999"]; !ok || x.Known || x.Contribution != 0 {
		t.Fatalf("an external tie is listed, unweighted: %+v %v", x, ok)
	}
	// the same input again is the same answer, and the limit truncates
	// after the sort (Total says how many there were)
	again := RankLeverage("Low-Field MRI", knowledge, ties, display, LeverageOptions{Limit: 2})
	if again.Total != 5 || len(again.People) != 2 || again.People[0].ID != "cand/b" || again.People[1].ID != "cand/a" {
		t.Fatalf("limit: %+v", again)
	}
	// one hop only: A's reach is B alone
	one := RankLeverage("low field mri", knowledge, ties, display, LeverageOptions{Hops: 1})
	for _, p := range one.People {
		if p.ID == "cand/a" && (!near(p.Connectivity, 0.55) || p.TieCount != 1) {
			t.Fatalf("hops=1: %+v", p)
		}
	}
	// knowledge with no ties is knowledge alone
	solo := RankLeverage("optics", knowledge, nil, display, LeverageOptions{})
	if len(solo.People) != 1 || !near(solo.People[0].Leverage, 0.60) || solo.People[0].Connectivity != 0 || len(solo.People[0].Ties) != 0 {
		t.Fatalf("expertise without ties: %+v", solo.People)
	}
}

func TestRankLeverageIsHonestWhenNobodyKnowsTheTopic(t *testing.T) {
	knowledge, ties, display := leverageFixture()
	res := RankLeverage("magnetic field", knowledge, ties, display, LeverageOptions{})
	if res.Known || res.Experts != 0 || len(res.People) != 0 || res.Total != 0 {
		t.Fatalf("an unknown topic ranks nobody: %+v", res)
	}
	if !strings.Contains(res.Note, "no expertise edge") {
		t.Fatalf("say why: %q", res.Note)
	}
	if strings.Join(res.Suggestions, ",") != "low field mri" {
		t.Fatalf("offer the topics that share a word: %v", res.Suggestions)
	}
	if r := RankLeverage("   ", knowledge, ties, display, LeverageOptions{}); r.Known || r.Note == "" {
		t.Fatalf("no topic: %+v", r)
	}
	// nothing on file at all
	if r := RankLeverage("optics", nil, nil, nil, LeverageOptions{}); r.Known || len(r.Suggestions) != 0 {
		t.Fatalf("empty graph: %+v", r)
	}
}

// PersonTies is the inverse adapter: only person ↔ person rows in the
// network vocabulary come back; knowledge and authorship never do.
func TestPersonTiesProjectsOnlyRelationshipRows(t *testing.T) {
	edges := []graph.Edge{
		{From: PersonRef("cand/a"), To: PersonRef("cand/b"), Kind: graph.EdgeCoauthor, Basis: "paper", Confidence: "0.55", Source: "openalex", Evidence: "https://doi.org/x"},
		expertise("cand/a", "Optics", "0.60", "openalex"),
		{From: PersonRef("cand/a"), To: graph.R(graph.KindPaper, "doi/x"), Kind: graph.EdgeAuthored, Basis: "b", Source: "s"},
		{From: PersonRef("cand/a"), To: PersonRef("cand/c"), Kind: graph.EdgeRelated, Basis: "b", Source: "s"},
	}
	got := PersonTies(edges)
	if len(got) != 1 || got[0].From != "cand/a" || got[0].To != "cand/b" || got[0].Kind != "coauthor" || got[0].Evidence != "https://doi.org/x" || got[0].Weight() != 0.55 {
		t.Fatalf("projection: %+v", got)
	}
	if TieKey(got[0]) != TieKey(Edge{From: "cand/b", To: "cand/a", Kind: "coauthor"}) {
		t.Fatal("the tie key is undirected")
	}
}
