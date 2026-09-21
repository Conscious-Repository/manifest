package server

import (
	"context"
	"encoding/json"
	"manifest/hermes"
	"manifest/recruiting"
	"manifest/recruiting/sources"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnhanceHTTPRunsDeepSeekThenPrivateKairos(t *testing.T) {
	s, mux := testRecruitingSourcesServer(t)
	s.recruitingRuns.Register(topicAdapter{})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		content := `{"brief":{"work":[{"value":"Low-Field MRI and Coil Design","confidence":0.9,"evidence":0,"quote":"topics: Low-Field MRI; Coil Design"}]}}`
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	}))
	defer model.Close()
	s.recruitingRuns.Register(sources.DeepSeek{BaseURL: model.URL, Client: *model.Client()})
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := filepath.Join(home, ".hermes/profiles/kairos-private")
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "config.yaml"), []byte("model: fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	reply := `{"competencies":{"text":"Works on low-field MRI and coil design","evidence":[0]},"relevance":{"text":"May support AION MRI engineering","evidence":[0]}}`
	result, _ := json.Marshal(hermes.AnnotationResult{Reply: reply, Model: "kairos-fixture"})
	bin := filepath.Join(home, "python")
	script := "#!/bin/sh\ncat > \"$HERMES_HOME/packet.json\"\nprintf '%s' '" + string(result) + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	s.hermes = &hermesCfg{runner: hermes.NewRunner(hermes.Config{Enabled: true, AnnotationPython: bin})}
	run, err := s.recruitingRuns.Execute(context.Background(), recruiting.RunRequest{Source: "topical", Query: "MRI", Role: "role/mri-engineer"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	out := decodeSources(t, sourcesDo(t, mux, "POST", "/api/aion/recruiting/sources/lookup/"+run.ID+"/d1", "{}"))
	if out.Run == nil || out.Run.Drafts[0].Summary == nil || out.Run.Drafts[0].Summary.Agent != "kairos-private" {
		t.Fatalf("missing summary: %+v", out.Run)
	}
	packet, err := os.ReadFile(filepath.Join(profile, "packet.json"))
	if err != nil || !strings.Contains(string(packet), "Low-Field MRI and Coil Design") {
		t.Fatalf("DeepSeek evidence did not precede Kairos: %v", err)
	}
	if !strings.Contains(out.Run.Drafts[0].Summary.Text, "no mutual connections recorded") {
		t.Fatal("missing deterministic connection field")
	}
}
