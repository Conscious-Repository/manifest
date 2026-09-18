package domainextract

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	syncatomic "sync/atomic"
	"testing"
	"time"
)

type redirectSparks struct{ target *url.URL }

func (r redirectSparks) RoundTrip(req *http.Request) (*http.Response, error) {
	c := req.Clone(req.Context())
	u := *req.URL
	c.URL = &u
	c.URL.Scheme = r.target.Scheme
	c.URL.Host = r.target.Host
	return http.DefaultTransport.RoundTrip(c)
}
func sparksEvidence(t *testing.T, s AionInput) (SparksCapability, SparksAccounting) {
	t.Helper()
	cap, e := LoadSparksCapability(aionConfig(), []byte(`{"data":[{"id":"deepseek-v4.1-flash","owned_by":"vllm","max_model_len":1048576}]}`))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := s.SparksRequest(aionConfig(), 4096)
	if e != nil {
		t.Fatal(e)
	}
	return cap, SparksAccounting{RequestSHA256: byteHash(raw), CapabilitySHA256: reducerDigest(cap), TokenizerSHA256: strings.Repeat("a", 64), TemplateSHA256: strings.Repeat("b", 64), ProbeSHA256: strings.Repeat("c", 64), SerializedTokens: 10000, PromptTokens: 9000, TokenMargin: 256, ByteMargin: 1024, VerifiedRequestBytes: len(raw) + 2048, ReservedOutput: 4096}
}
func responseSparks() string {
	return `{"id":"fixture","object":"chat.completion","created":1,"model":"deepseek-v4.1-flash","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[],"content":"{\"summary\":\"No commitments.\",\"candidates\":[]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":9000,"completion_tokens":10,"total_tokens":9010}}`
}
func TestSparksPreflightAndNoStateWrites(t *testing.T) {
	root, s := aionSnapshot(t)
	putPlan(t, root, "approvals/sentinel.json", "unchanged")
	snapshot := func() string {
		files := map[string]string{}
		if err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
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
		}); err != nil {
			t.Fatal(err)
		}
		return reducerDigest(files)
	}
	before := snapshot()
	var calls syncatomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write([]byte(responseSparks())) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	cap, a := sparksEvidence(t, s)
	for _, change := range []func(*AionSuccessorConfig, *SparksCapability, *SparksAccounting){
		func(c *AionSuccessorConfig, p *SparksCapability, a *SparksAccounting) { c.Model = "wrong" },
		func(c *AionSuccessorConfig, p *SparksCapability, a *SparksAccounting) { c.Tools = []string{"web"} },
		func(c *AionSuccessorConfig, p *SparksCapability, a *SparksAccounting) { c.MCP = "enabled" },
		func(c *AionSuccessorConfig, p *SparksCapability, a *SparksAccounting) { c.Fallback = true },
		func(c *AionSuccessorConfig, p *SparksCapability, a *SparksAccounting) {
			p.MaxModelLen = 8192
			a.CapabilitySHA256 = reducerDigest(*p)
		},
		func(c *AionSuccessorConfig, p *SparksCapability, a *SparksAccounting) { a.VerifiedRequestBytes = 1 },
		func(c *AionSuccessorConfig, p *SparksCapability, a *SparksAccounting) { a.TemplateSHA256 = "" },
		func(c *AionSuccessorConfig, p *SparksCapability, a *SparksAccounting) {
			a.RequestSHA256 = strings.Repeat("0", 64)
		},
	} {
		c, p, b := aionConfig(), cap, a
		change(&c, &p, &b)
		r := s.runSparks(context.Background(), c, p, b, redirectSparks{u}, time.Second)
		if r.Error == "" || r.Invoked || calls.Load() != 0 {
			t.Fatal(r)
		}
	}
	r := s.runSparks(context.Background(), aionConfig(), cap, a, redirectSparks{u}, time.Second)
	if r.Error != "" || !r.Invoked || r.Usage == nil || r.Finish != "stop" || r.ResponseSHA256 != byteHash([]byte(responseSparks())) || r.RequestSHA256 != a.RequestSHA256 {
		t.Fatal(r)
	}
	h := r.ReceiptSHA256
	r.ReceiptSHA256 = ""
	if reducerDigest(r) != h {
		t.Fatal("receipt not bound")
	}
	r.Usage.Completion++
	if reducerDigest(r) == h {
		t.Fatal("usage not bound")
	}
	if snapshot() != before {
		t.Fatal("vault or approval tree changed")
	}
	if e := s.Revalidate(root); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(root, "approvals/sentinel.json"))
	if string(b) != "unchanged" {
		t.Fatal("approval modified")
	}
}
func TestSparksResponsesAndTimeout(t *testing.T) {
	_, s := aionSnapshot(t)
	cap, a := sparksEvidence(t, s)
	for _, body := range []string{"not JSON", strings.Replace(responseSparks(), "deepseek-v4.1-flash", "other", 1), strings.Replace(responseSparks(), `"stop"`, `"length"`, 1), strings.Replace(responseSparks(), `"tool_calls":[]`, `"tool_calls":[{"type":"function"}]`, 1), strings.Replace(responseSparks(), "No commitments.", "AKIAIOSFODNN7EXAMPLE", 1), strings.Replace(responseSparks(), `"prompt_tokens":9000`, `"prompt_tokens":9001`, 1)} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		u, _ := url.Parse(srv.URL)
		r := s.runSparks(context.Background(), aionConfig(), cap, a, redirectSparks{u}, time.Second)
		srv.Close()
		raw, _ := json.Marshal(r)
		if r.Error == "" || strings.Contains(string(raw), "AKIA") || r.ResponseSHA256 != byteHash([]byte(body)) {
			t.Fatal(r)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(50 * time.Millisecond) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	r := s.runSparks(context.Background(), aionConfig(), cap, a, redirectSparks{u}, 10*time.Millisecond)
	if r.Error != "transport-failed-or-timeout" {
		t.Fatal(r)
	}
}
func TestSparksLargeInputRequiresMeasuredTokens(t *testing.T) {
	root, _ := aionSnapshot(t)
	putPlan(t, root, "system/aion/backlog.md", strings.Repeat("record ", 15000))
	s, e := BuildAionInput(root, []string{"log/source.md"}, "")
	if e != nil {
		t.Fatal(e)
	}
	if s.input.Validate() != nil {
		t.Fatal("bounded Hermes context rejected")
	}
	cap, a := sparksEvidence(t, s)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(responseSparks())) }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	accepted := s.runSparks(context.Background(), aionConfig(), cap, a, redirectSparks{u}, time.Second)
	if accepted.Error != "" || !accepted.Invoked {
		t.Fatal(accepted)
	}
	a.SerializedTokens = cap.MaxModelLen
	r := s.runSparks(context.Background(), aionConfig(), cap, a, nil, time.Second)
	if r.Invoked || r.Error != "complete-request-capacity-refused" {
		t.Fatal(r)
	}
}
func TestSparksDiscoveryAccountingAndReceipt(t *testing.T) {
	_, s := aionSnapshot(t)
	for _, raw := range []string{`{}`, `{"data":[{"id":"other","max_model_len":1048576}]}`, `{"data":[{"id":"deepseek-v4.1-flash","owned_by":"vllm","max_model_len":0}]}`} {
		if _, e := LoadSparksCapability(aionConfig(), []byte(raw)); e == nil {
			t.Fatal(raw)
		}
	}
	_, a := sparksEvidence(t, s)
	raw, _ := json.Marshal(a)
	if _, e := DecodeSparksAccounting(raw, byteHash(raw)); e != nil {
		t.Fatal(e)
	}
	if _, e := DecodeSparksAccounting(raw, strings.Repeat("0", 64)); e == nil {
		t.Fatal("unreviewed accounting")
	}
	vault := t.TempDir()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "receipt.json")
	f, e := OpenSparksReceipt(path, vault, vault)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.NewEncoder(f).Encode(SparksReceipt{State: "comparison-unrun"}); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	if _, e := OpenSparksReceipt(path, vault, vault); e == nil {
		t.Fatal("overwrote")
	}
	if _, e := OpenSparksReceipt(filepath.Join(vault, "receipt"), vault, vault); e == nil {
		t.Fatal("vault write")
	}
}

func TestSparksExactRequestHTTPErrorAndNoRedirect(t *testing.T) {
	_, s := aionSnapshot(t)
	cap, a := sparksEvidence(t, s)
	var calls syncatomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, e := io.ReadAll(r.Body)
		if e != nil || byteHash(raw) != a.RequestSHA256 || r.URL.Path != "/v1/chat/completions" || r.Method != "POST" || r.Header.Get("Authorization") != "" {
			t.Error("request drift")
		}
		w.Header().Set("Location", "/fallback")
		w.WriteHeader(307)
		w.Write([]byte("AKIAIOSFODNN7EXAMPLE"))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	r := s.runSparks(context.Background(), aionConfig(), cap, a, redirectSparks{u}, time.Second)
	raw, _ := json.Marshal(r)
	if r.Error != "provider-http-error" || calls.Load() != 1 || strings.Contains(string(raw), "AKIA") || r.ResponseSHA256 == "" {
		t.Fatal(r)
	}
}
