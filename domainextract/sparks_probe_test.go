package domainextract

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const probeModels = `{"data":[{"id":"deepseek-v4.1-flash","owned_by":"vllm","max_model_len":1048576}]}`
const probeAPI = `{"paths":{"/tokenize":{"post":{"responses":{"200":{"content":{"application/json":{"schema":{}}}}}}}}}`

func TestSparksProbeRefusesUnsupportedAccounting(t *testing.T) {
	root, s := aionSnapshot(t)
	snapshot := func() string {
		files := map[string]string{}
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() {
				b, e := os.ReadFile(p)
				if e != nil {
					return e
				}
				files[p] = byteHash(b)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return reducerDigest(files)
	}
	before := snapshot()
	cap, _ := sparksEvidence(t, s)
	raw, _ := s.SparksRequest(aionConfig(), 4096)
	for _, tc := range []struct {
		name, models, api, reason string
		status                    int
		delay                     time.Duration
	}{
		{name: "undocumented", models: probeModels, api: probeAPI, reason: "tokenizer-response-contract-undocumented"},
		{name: "endpoint", models: probeModels, api: `{"paths":{}}`, reason: "tokenizer-endpoint-unsupported"},
		{name: "model", models: strings.Replace(probeModels, "deepseek-v4.1-flash", "other", 1), reason: "model-capability-mismatch"},
		{name: "capacity", models: strings.Replace(probeModels, "1048576", "8192", 1), reason: "model-capability-mismatch"},
		{name: "token-template-mismatch", models: probeModels, api: strings.Replace(probeAPI, `"schema":{}`, `"schema":{"count":32,"template":"different"}`, 1), reason: "tokenizer-template-contract-unsupported"},
		{name: "malformed-count", models: strings.Replace(probeModels, "1048576", "1.5", 1), reason: "model-capability-mismatch"},
		{name: "secret", models: probeModels, api: `{"credential":"AKIAIOSFODNN7EXAMPLE"}`, reason: "metadata-content-refused"},
		{name: "oversized", models: probeModels, api: strings.Repeat("x", (1<<20)+1), reason: "metadata-body-refused"},
		{name: "duplicate", models: probeModels, api: `{"paths":{},"paths":{}}`, reason: "metadata-content-refused"},
		{name: "redirect", status: 302, reason: "metadata-endpoint-refused"},
		{name: "timeout", delay: 50 * time.Millisecond, reason: "metadata-transport-failed-or-timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || (r.URL.Path != "/v1/models" && r.URL.Path != "/openapi.json") {
					t.Error("unexpected request")
					w.WriteHeader(500)
					return
				}
				if r.ContentLength > 0 {
					t.Error("private data transmitted")
				}
				if tc.delay > 0 {
					time.Sleep(tc.delay)
				}
				if tc.status != 0 {
					w.Header().Set("Location", "/chat/completions")
					w.WriteHeader(tc.status)
					return
				}
				if r.URL.Path == "/v1/models" {
					w.Write([]byte(tc.models))
				} else {
					w.Write([]byte(tc.api))
				}
			}))
			defer srv.Close()
			u, _ := url.Parse(srv.URL)
			timeout := time.Second
			if tc.delay > 0 {
				timeout = 5 * time.Millisecond
			}
			r := s.probeSparks(context.Background(), aionConfig(), cap, raw, 4096, true, redirectSparks{u}, timeout)
			if r.Reason != tc.reason || r.State != "tokenizer-template-accounting-required" {
				t.Fatalf("%+v", r)
			}
			if r.SerializedTokens != 0 || r.PromptTokens != 0 || r.VerifiedRequestBytes != 0 || r.TemplateSHA256 != "" || r.TokenizerSHA256 != "" {
				t.Fatal("fabricated accounting")
			}
			if r.RequestBytes != len(raw) || r.RequestSHA256 != byteHash(raw) || r.InputSHA256 != s.digest || r.ConfigSHA256 != reducerDigest(aionConfig()) || r.CapabilitySHA256 != reducerDigest(cap) {
				t.Fatal("binding")
			}
			digest := r.ProbeSHA256
			r.ProbeSHA256 = ""
			if digest != reducerDigest(r) {
				t.Fatal("probe digest")
			}
			b, _ := json.Marshal(r)
			for _, secret := range []string{root, "Jane", "AKIA", "different", "credential"} {
				if strings.Contains(string(b), secret) {
					t.Fatal("unredacted receipt")
				}
			}
		})
	}
	if before != snapshot() {
		t.Fatal("input/state modified")
	}
}

func TestSparksProbeOfflineAndBinding(t *testing.T) {
	_, s := aionSnapshot(t)
	cap, _ := sparksEvidence(t, s)
	raw, _ := s.SparksRequest(aionConfig(), 4096)
	r := s.probeSparks(context.Background(), aionConfig(), cap, raw, 4096, false, nil, time.Second)
	if r.Reason != "endpoint-inspection-required" {
		t.Fatal(r)
	}
	for _, modified := range [][]byte{append(append([]byte{}, raw...), '\n'), []byte(`{"model":"other"}`)} {
		r = s.probeSparks(context.Background(), aionConfig(), cap, modified, 4096, true, nil, time.Second)
		if r.Reason != "accounting-request-mismatch" {
			t.Fatal(r)
		}
	}
	small, _ := s.SparksRequest(aionConfig(), 1)
	r = s.probeSparks(context.Background(), aionConfig(), cap, small, 1, true, nil, time.Second)
	if r.Reason != "insufficient-output-reserve" {
		t.Fatal(r)
	}
	c := aionConfig()
	c.Endpoint = "http://proxy/v1"
	r = s.probeSparks(context.Background(), c, cap, raw, 4096, true, nil, time.Second)
	if r.Reason != "invalid-request-input" {
		t.Fatal(r)
	}
}
