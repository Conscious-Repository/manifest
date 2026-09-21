package sources

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBriefClaimsRequireExactEvidenceAndStaySeparate(t *testing.T) {
	d := CandidateDraft{Name: "Ada Example", Evidence: []Evidence{{URLOrFile: "https://ada.example/bio", Snippet: "Ada Example earned a PhD at Example University in 2018 and worked at Example Lab."}}}
	valid := map[string]any{"value": "PhD, Example University (2018)", "confidence": 0.95, "evidence": 0, "quote": "earned a PhD at Example University in 2018"}
	bad := map[string]any{"value": "CEO at Other Lab", "confidence": 0.99, "evidence": 0, "quote": "CEO at Other Lab"}
	raw, _ := json.Marshal(map[string]any{"brief": map[string]any{"education": []any{valid}, "experience": []any{bad}, "unknown": []any{valid}}})
	hits, err := deepseekDraft(d, string(raw))
	if err != nil || len(hits) != 1 || hits[0].Brief == nil {
		t.Fatalf("%+v %v", hits, err)
	}
	brief := hits[0].Brief
	if len(brief.Items) != 1 || brief.Items[0].Section != "education" || brief.Items[0].URL != d.Evidence[0].URLOrFile || len(brief.Evidence) != 1 {
		t.Fatalf("%+v", brief)
	}
	if hits[0].Note != "" || len(hits[0].Evidence) != 0 {
		t.Fatal("generated prose contaminated source text")
	}
	for _, mutation := range []struct {
		key   string
		value any
	}{{"evidence", 999}, {"confidence", 0.3}, {"quote", "invented evidence"}, {"value", strings.Repeat("x", 601)}} {
		c := map[string]any{}
		for k, v := range valid {
			c[k] = v
		}
		c[mutation.key] = mutation.value
		raw, _ := json.Marshal(map[string]any{"brief": map[string]any{"education": []any{c}}})
		hits, err := deepseekDraft(d, string(raw))
		if err != nil || len(hits) != 0 {
			t.Fatalf("accepted invalid %s: %+v %v", mutation.key, hits, err)
		}
	}
}

func TestBriefContextBoundCountsUnicode(t *testing.T) {
	d := CandidateDraft{}
	for i := 0; i < 100; i++ {
		d.Evidence = append(d.Evidence, Evidence{Snippet: strings.Repeat("界", 10000)})
	}
	input := deepseekContext(d)
	total := 0
	for _, e := range input.Evidence {
		total += len([]rune(e.Snippet))
	}
	if total > 24000 || len(input.Evidence) > 24 {
		t.Fatalf("unbounded: %d", total)
	}
}
