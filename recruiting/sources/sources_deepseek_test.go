package sources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func reasoningDraft() CandidateDraft {
	return CandidateDraft{SourceID: "pubmed", Name: "Yu G", Evidence: []Evidence{{SourceID: "pubmed", URLOrFile: "https://pubmed.ncbi.nlm.nih.gov/123/", Kind: EvidencePublication, Snippet: "Guang Yu (Yu G), Example University. Diffusion MRI reconstruction using neural networks."}}}
}

const resolvedReasoning = `{"identity":"resolved","canonicalName":{"value":"Guang Yu","confidence":0.95,"evidence":0,"quote":"Guang Yu (Yu G), Example University"},"org":{"value":"Example University","confidence":0.9,"evidence":0,"quote":"Guang Yu (Yu G), Example University"},"topics":[{"value":"Diffusion MRI reconstruction","confidence":0.95,"evidence":0,"quote":"Diffusion MRI reconstruction using neural networks"}]}`

func TestDeepSeekSupportedDraftAndModelOverride(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"override"}]}`)
			return
		}
		var req struct {
			Template map[string]bool `json:"chat_template_kwargs"`
			Model    string          `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if thinking, ok := req.Template["thinking"]; !ok || thinking {
			t.Error("lab thinking mode must be disabled")
		}
		if req.Model != "override" || len(req.Messages) != 2 || !strings.Contains(req.Messages[1].Content, "Yu G") {
			t.Errorf("request: %+v", req)
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": resolvedReasoning, "reasoning_content": "Invent a different name"}}}})
	}))
	defer ts.Close()
	hits, err := (DeepSeek{BaseURL: ts.URL + "/v1", Model: "override", Client: *ts.Client()}).LookupCandidate(context.Background(), reasoningDraft(), Scope{})
	if err != nil || len(hits) != 1 {
		t.Fatalf("%+v %v", hits, err)
	}
	h := hits[0]
	if h.Name != "Yu G" || h.CanonicalName != "Guang Yu" || h.Org != "Example University" || len(h.Topics) != 1 || h.TopicInferences[0].Source != "deepseek" {
		t.Fatalf("%+v", h)
	}
	if strings.Join(paths, ",") != "/v1/models,/v1/chat/completions" {
		t.Fatal(paths)
	}
}

func TestDeepSeekAmbiguousInitialHonesty(t *testing.T) {
	d := CandidateDraft{Name: "Yu G", Evidence: []Evidence{{Kind: EvidencePublication, URLOrFile: "https://example.test/paper", Snippet: "first author: Yu G"}}}
	for _, output := range []string{
		`{"identity":"ambiguous","canonicalName":{"value":"Guang Yu","confidence":0.99,"evidence":0,"quote":"first author: Yu G"},"org":{"value":"Stanford","confidence":0.99,"evidence":0,"quote":"first author: Yu G"}}`,
		`{"identity":"resolved","canonicalName":{"value":"Guang Yu","confidence":0.99,"evidence":0,"quote":"first author: Yu G"}}`,
		`{"identity":"ambiguous","topics":[{"value":"MRI","confidence":0.99,"evidence":0,"quote":"first author: Yu G"}]}`,
	} {
		hits, err := deepseekDraft(d, output)
		if err != nil || len(hits) != 0 {
			t.Fatalf("invented facts: %+v %v", hits, err)
		}
	}
	// Identity ambiguity does not discard a genuinely attributed paper topic.
	output := strings.Replace(resolvedReasoning, `"resolved"`, `"ambiguous"`, 1)
	hits, err := deepseekDraft(reasoningDraft(), output)
	if err != nil || len(hits) != 1 || hits[0].CanonicalName != "" || hits[0].Org != "" || len(hits[0].Topics) != 1 {
		t.Fatalf("%+v %v", hits, err)
	}
}

func TestDeepSeekStrictJSONAndOptionalFields(t *testing.T) {
	for _, output := range []string{resolvedReasoning + ` {}`, `prefix ` + resolvedReasoning, `[]`, `null`, `<think>unfinished`} {
		if _, err := deepseekDraft(reasoningDraft(), output); err == nil {
			t.Errorf("accepted %s", output)
		}
	}
	output := strings.Replace(resolvedReasoning, `"canonicalName":{`, `"reasoning":"ignored","bad":{},"canonicalName":{`, 1)
	for _, prefix := range []string{"", "<think>reasoning {noise}</think>\n"} {
		if hits, err := deepseekDraft(reasoningDraft(), prefix+"```json\n"+output+"\n```"); err != nil || len(hits) != 1 {
			t.Fatalf("%+v %v", hits, err)
		}
	}
	output = `{"identity":"resolved","org":123,"homepage":{"value":"https://invented.test","confidence":1,"evidence":999},"topics":[false,` + `{"value":"Diffusion MRI","confidence":0.9,"evidence":0,"quote":"Diffusion MRI reconstruction using neural networks"}]}`
	hits, err := deepseekDraft(reasoningDraft(), output)
	if err != nil || len(hits) != 1 || hits[0].Org != "" || hits[0].Homepage != "" || len(hits[0].Topics) != 1 {
		t.Fatalf("%+v %v", hits, err)
	}
}

func TestDeepSeekDownAndContextBound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("closed endpoint called") }))
	ts.Close()
	if hits, err := (DeepSeek{BaseURL: ts.URL, Client: *ts.Client()}).LookupCandidate(context.Background(), reasoningDraft(), Scope{}); err == nil || len(hits) != 0 {
		t.Fatalf("%+v %v", hits, err)
	}
	d := reasoningDraft()
	d.Name = strings.Repeat("x", 1000)
	for i := 0; i < 100; i++ {
		d.Evidence = append(d.Evidence, Evidence{Snippet: strings.Repeat("é", 10000)})
	}
	got := deepseekContext(d)
	b, err := json.Marshal(got)
	if err != nil || len(got.Evidence) != 8 || len(got.Name) > 200 || len(b) > 40000 {
		t.Fatalf("unbounded context %d %v", len(b), err)
	}
}

func TestDeepSeekRetriesReplayPOST(t *testing.T) {
	var bodies []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"data":[]}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			w.WriteHeader(429)
			return
		}
		if len(bodies) == 2 {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": resolvedReasoning}}}})
	}))
	defer ts.Close()
	hits, err := (DeepSeek{BaseURL: ts.URL, Client: *ts.Client()}).LookupCandidate(context.Background(), reasoningDraft(), Scope{})
	if err != nil || len(hits) != 1 || len(bodies) != 3 {
		t.Fatalf("%+v %v calls=%d", hits, err, len(bodies))
	}
	if bodies[0] == "" || bodies[0] != bodies[1] || bodies[1] != bodies[2] {
		t.Fatal("POST not replayed")
	}
}
