package server

import (
	"strings"
	"testing"

	"manifest/aion"
	"manifest/domainextract"
	"manifest/jev"
	"manifest/typesafe"
)

// Which notes may leave the lab while it is down (2026-10-06): the tier map
// decides for a mapped note (open/internal go, held never); Jev's restrictive
// tier advice decides for an unmapped one; anything unsure, unjudged, outside
// log/ or from another extractor waits. Nothing writes the tier map.
func TestExtractionRouteFollowsTiersThenJev(t *testing.T) {
	t.Setenv(typesafe.EnvKey, "")
	tm := aion.TierMap{
		"2026-09-20 rj sync.md": {Tier: aion.TierInternal, Reason: "team sync", Bytes: 1},
		"2026-09-21 comp.md":    {Tier: aion.TierHeld, Reason: "comp", Bytes: 1},
	}
	s, _ := visibilityServer(t, tm)
	route := s.ExtractionRoute([]string{"aion"}, []string{"open", "internal"})
	doc := func(name, text string) domainextract.Input {
		return domainextract.Input{Ritual: "aion", Documents: []domainextract.Document{{Name: "log/" + name, Text: text}}}
	}
	expect := func(in domainextract.Input, ok bool, why string) {
		t.Helper()
		gotOK, gotWhy := route(in)
		if gotOK != ok || !strings.Contains(gotWhy, why) {
			t.Fatalf("route(%s) = %v %q, want %v %q", in.Documents[0].Name, gotOK, gotWhy, ok, why)
		}
	}
	expect(doc("2026-09-20 rj sync.md", "x"), true, "")
	expect(doc("2026-09-21 comp.md", "x"), false, "is held (tier map)")
	expect(doc("2026-10-05 aion sync.md", "We reviewed the assay roadmap."), false, "unmapped and Jev is unavailable")

	// an unmapped note: Jev's confident internal clears it; a held answer does not
	s.jevJudge = &jev.Judge{Eval: tierFakeAnswers("internal", 0.9, map[string]float64{"open": 0.05, "internal": 0.9, "held": 0.05}, nil)}
	expect(doc("2026-10-05 aion sync.md", "We reviewed the assay roadmap."), true, "")
	s.jevJudge = &jev.Judge{Eval: tierFakeAnswers("held", 0.9, map[string]float64{"open": 0.02, "internal": 0.08, "held": 0.9}, nil)}
	expect(doc("2026-10-06 offer letter.md", "We discussed her salary."), false, "is held (Jev)")

	in := doc("2026-09-20 rj sync.md", "x")
	in.Ritual = "real-estate"
	expect(in, false, "real-estate notes stay on the lab model")
	expect(domainextract.Input{Ritual: "aion", Documents: []domainextract.Document{{Name: "intrinsic/2026-10-01.md", Text: "x"}}}, false, "has no transcript tier")
	if _, known := s.aionTierMap().Tier("2026-10-05 aion sync.md"); known {
		t.Fatal("routing must never write the tier map")
	}
}
