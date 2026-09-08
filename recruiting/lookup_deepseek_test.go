package recruiting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"manifest/recruiting/sources"
)

func TestLookupDeepSeekFallbackOrderAndKnowledge(t *testing.T) {
	for _, exact := range []bool{false, true} {
		t.Run(map[bool]string{false: "near miss", true: "exact"}[exact], func(t *testing.T) {
			draft := sources.CandidateDraft{SourceID: "fake", Name: "Yu G", Evidence: []sources.Evidence{{SourceID: "pubmed", Kind: sources.EvidencePublication, URLOrFile: "https://pubmed.ncbi.nlm.nih.gov/123/", Snippet: "Guang Yu (Yu G), Example University. Diffusion MRI reconstruction."}}}
			rs, _, _ := testRunStore(t, &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{draft}})
			name := "Different Person"
			if exact {
				name = "Yu G"
			}
			rs.Register(&fakeAdapter{id: "openalex", drafts: []sources.CandidateDraft{{Name: name, Org: "Deterministic University"}}})
			calls := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if exact {
					t.Error("reasoning ran despite exact deterministic match")
				}
				if r.Method == http.MethodGet {
					io.WriteString(w, `{"data":[]}`)
					return
				}
				content := `{"identity":"resolved","org":{"value":"Example University","confidence":0.95,"evidence":0,"quote":"Guang Yu (Yu G), Example University"},"topics":[{"value":"Diffusion MRI reconstruction","confidence":0.95,"evidence":0,"quote":"Diffusion MRI reconstruction"}]}`
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
			}))
			defer ts.Close()
			rs.Register(sources.DeepSeek{BaseURL: ts.URL, Client: *ts.Client()})
			run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "MRI", DryRun: true}, testNow)
			if err != nil {
				t.Fatal(err)
			}
			run, res, err := rs.Lookup(context.Background(), run.ID, "d1", testNow)
			if err != nil {
				t.Fatal(err)
			}
			if exact {
				if calls != 0 || strings.Join(res.Asked, ",") != "openalex" || run.Drafts[0].Draft.Org != "Deterministic University" {
					t.Fatalf("%+v", res)
				}
				return
			}
			if strings.Join(res.Asked, ",") != "openalex,deepseek" || strings.Join(res.Matched, ",") != "deepseek" || run.Drafts[0].Draft.Org != "Example University" {
				t.Fatalf("%+v %+v", res, run.Drafts)
			}
			d := run.Drafts[0].Draft
			k := DeriveKnowledge(d, "cand/yu-g", "", testNow)
			if len(k.Edges) != 1 || k.Edges[0].Source != "deepseek" || !k.Edges[0].Inferred || k.Edges[0].Confidence != "0.50" || !strings.Contains(k.Edges[0].Basis, "Diffusion MRI") {
				t.Fatalf("%+v", k)
			}
			run, res, err = rs.Lookup(context.Background(), run.ID, "d1", testNow)
			if err != nil || len(run.Drafts[0].Draft.TopicInferences) != 1 || res.Cites != 0 || len(res.Filled) != 0 {
				t.Fatalf("non-idempotent: %+v %v", res, err)
			}
		})
	}
}

func TestLookupDeepSeekDownSoftSkip(t *testing.T) {
	rs, _, _ := testRunStore(t, &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{{Name: "Yu G"}}})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()
	rs.Register(sources.DeepSeek{BaseURL: ts.URL, Client: *ts.Client()})
	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "MRI", DryRun: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	run, res, err := rs.Lookup(context.Background(), run.ID, "d1", testNow)
	if err != nil || len(res.Failed) != 1 || res.Failed[0] != "deepseek" || run.Drafts[0].LookedUpAt.IsZero() || len(run.Drafts[0].Draft.Topics) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestDeepSeekIsLookupOnly(t *testing.T) {
	rs, _, _ := testRunStore(t, &fakeAdapter{id: "fake"})
	rs.Register(sources.DeepSeek{})
	for _, src := range rs.Sources() {
		if src.ID == "deepseek" {
			t.Fatal("lookup-only source offered as discovery")
		}
	}
	if _, _, err := rs.PrepareScope(RunRequest{Source: "deepseek", Query: "MRI"}); err == nil {
		t.Fatal("lookup-only source started a run")
	}
}
