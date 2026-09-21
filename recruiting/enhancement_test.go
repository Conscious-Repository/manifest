package recruiting

import (
	"context"
	"encoding/json"
	"manifest/recruiting/sources"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEnhanceBriefEvenWithTopicsPersistsAndRejectsAmbiguity(t *testing.T) {
	d := sources.CandidateDraft{Name: "Ada Example", Topics: []string{"MRI"}, Evidence: []sources.Evidence{{URLOrFile: "https://ada.example/bio", Snippet: "Ada Example earned a PhD at Example University."}}}
	rs, _, _ := testRunStore(t, &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{d}})
	rs.Register(&fakeAdapter{id: "openalex", drafts: []sources.CandidateDraft{{Name: d.Name, ExternalID: "A1", Org: "One"}, {Name: d.Name, ExternalID: "A2", Org: "Two"}}})
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		calls++
		content := `{"brief":{"education":[{"value":"PhD at Example University","confidence":0.95,"evidence":0,"quote":"earned a PhD at Example University"}]}}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	defer ts.Close()
	rs.Register(sources.DeepSeek{BaseURL: ts.URL, Client: *ts.Client(), Model: "test-model"})
	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "MRI", DryRun: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	_, res, err := rs.Lookup(context.Background(), run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := rs.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Drafts[0]
	if calls != 1 || !res.Brief || strings.Join(res.Ambiguous, ",") != "openalex" || got.Draft.Org != "" || got.Enhancement == nil || got.Draft.Brief.Model != "test-model" {
		t.Fatalf("%+v %+v", res, got)
	}
	if !got.Draft.Brief.GeneratedAt.Equal(testNow) {
		t.Fatal("missing timestamp")
	}
}

func TestEnhanceDoesNotBlockDecisionsOrOverwriteThem(t *testing.T) {
	rs, _, _ := testRunStore(t, &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{{Name: "Ada Example"}}})
	entered, release := make(chan struct{}), make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			close(entered)
			<-release
			w.Write([]byte(`{"data":[]}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer ts.Close()
	rs.Register(sources.DeepSeek{BaseURL: ts.URL, Client: *ts.Client()})
	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "Ada"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, _, err := rs.Lookup(context.Background(), run.ID, "d1", testNow); done <- err }()
	<-entered
	rejected := make(chan error, 1)
	go func() { _, err := rs.Reject(run.ID, "d1", "owner decision", testNow); rejected <- err }()
	select {
	case err := <-rejected:
		if err != nil {
			close(release)
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("network held queue lock")
	}
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "changed during enhancement") {
		t.Fatalf("lost-update guard: %v", err)
	}
	got, err := rs.Get(run.ID)
	if err != nil || got.Drafts[0].Status != DraftRejected || got.Drafts[0].Draft.Brief != nil {
		t.Fatalf("%+v %v", got, err)
	}
}
