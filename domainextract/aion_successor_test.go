package domainextract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/hermes"
)

func aionConfig() AionSuccessorConfig {
	return AionSuccessorConfig{Provider: "lab-sparks", Model: "deepseek-v4.1-flash", Endpoint: hermes.LocalEndpoint, ProviderBinding: hermes.LocalProviderBinding, Tools: []string{"none"}, MCP: "no_mcp", CostTelemetry: "unavailable"}
}
func aionSnapshot(t *testing.T) (string, AionInput) {
	t.Helper()
	root := planFixture(t, "aion")
	putPlan(t, root, "log/source.md", "Jane: I will review the draft.")
	s, e := BuildAionInput(root, []string{"log/source.md"}, "")
	if e != nil {
		t.Fatal(e)
	}
	return root, s
}
func TestAionProviderAndCapacityRefuse(t *testing.T) {
	_, s := aionSnapshot(t)
	for _, change := range []func(*AionSuccessorConfig){
		func(c *AionSuccessorConfig) { c.Provider = "claude-sub" },
		func(c *AionSuccessorConfig) { c.Provider = "deepseek-local" },
		func(c *AionSuccessorConfig) { c.Model = "other" },
		func(c *AionSuccessorConfig) { c.ProviderBinding = "" },
		func(c *AionSuccessorConfig) { c.Endpoint = "http://elsewhere" },
		func(c *AionSuccessorConfig) { c.Fallback = true },
		func(c *AionSuccessorConfig) { c.Tools = []string{"web"} },
		func(c *AionSuccessorConfig) { c.MCP = "mcp-manifest" },
		func(c *AionSuccessorConfig) { c.CostTelemetry = "zero" },
		func(c *AionSuccessorConfig) { c.InputCapacityBytes = -1 },
	} {
		c := aionConfig()
		change(&c)
		if c.validate() == nil || s.Readiness(c).State != "refused" {
			t.Fatal("accepted authority")
		}
	}
	for _, capacity := range []int{0, 1, 56000, 1000000} {
		c := aionConfig()
		c.InputCapacityBytes = capacity
		c.ContextWindowTokens = 1000000
		r := s.Readiness(c)
		if r.State != "refused" || r.InvocationPerformed || r.RequestSHA256 != "" || r.ResultSHA256 != "" || r.UsageState != "not-invoked" {
			t.Fatal(r)
		}
		if capacity == 1 && r.Reason != "complete-input-exceeds-declared-capacity" {
			t.Fatal(r)
		}
		if capacity != 1 && r.Reason != "provider-capacity-unverified" {
			t.Fatal(r)
		}
	}
	raw, _ := json.Marshal(aionConfig())
	if _, e := DecodeAionSuccessorConfig(raw); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{}`, `{"provider":"lab-sparks","provider":"lab-sparks"}`, `{"Provider":"lab-sparks"}`, `{"secret":"private"}`} {
		if _, e := DecodeAionSuccessorConfig([]byte(raw)); e == nil {
			t.Fatal("accepted malformed config")
		}
	}
}
func TestAionCompleteMeasurementAndDrift(t *testing.T) {
	root, _ := aionSnapshot(t)
	body := strings.Repeat("<>&\n", 50000)
	putPlan(t, root, "system/aion/backlog.md", body)
	s, e := BuildAionInput(root, []string{"log/source.md"}, "")
	if e != nil {
		t.Fatal(e)
	}
	r := s.Readiness(aionConfig())
	if s.input.Context["system/aion/backlog.md"] != body || r.RecordCount != 3 || r.SourceCount != 1 || r.SerializedBytes != len(s.serialized) || r.SerializedBytes <= r.LegacyInputBytes || r.LegacyInputBytes <= len(body) {
		t.Fatal(r)
	}
	if _, e = BuildAionInput(root, []string{"log/source.md"}, r.InputSHA256); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"log/source.md", "system/aion/people.md"} {
		putPlan(t, root, name, "drift")
		if s.Revalidate(root) == nil {
			t.Fatal("drift accepted")
		}
		if _, e = BuildAionInput(root, []string{"log/source.md"}, r.InputSHA256); e == nil {
			t.Fatal("pin drift accepted")
		}
	}
	if e = os.Remove(filepath.Join(root, "system/aion/heuristics.md")); e != nil {
		t.Fatal(e)
	}
	if _, e = BuildAionInput(root, []string{"log/source.md"}, ""); e == nil {
		t.Fatal("missing required treated as absence")
	}
}
func TestAionNoWriteRedactionAndCopiedReply(t *testing.T) {
	root, s := aionSnapshot(t)
	before := map[string]string{}
	scan := func() map[string]string {
		result := map[string]string{}
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() {
				b, e := os.ReadFile(p)
				if e != nil {
					return e
				}
				result[p] = byteHash(b)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before = scan()
	r := s.Readiness(aionConfig())
	raw, _ := json.Marshal(r)
	for _, private := range []string{root, "Jane", "source.md", "private prose", "system/aion", "lab-sparks"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("leak: %s", private)
		}
	}
	reply := strings.ReplaceAll(replyFixture, "log/2026-09-12 fixture.md", "log/source.md")
	digest, n, e := s.CheckCopiedReply(aionConfig(), []byte(reply))
	if e != nil || n != 1 || len(digest) != 64 {
		t.Fatal(digest, n, e)
	}
	c := aionConfig()
	c.ContextWindowTokens = 4096
	bound, _, e := s.CheckCopiedReply(c, []byte(reply))
	if e != nil || bound == digest {
		t.Fatal("provider config not bound")
	}
	c.Model = "other"
	if _, _, e := s.CheckCopiedReply(c, []byte(reply)); e == nil {
		t.Fatal("reply accepted for wrong model")
	}
	if _, _, e := s.CheckCopiedReply(aionConfig(), []byte("\xff")); e == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	for _, reply := range []string{`{}`, `{"candidates":[],"summary":"ok","summary":"other"}`, strings.Replace(reply, "Review draft", "AKIAIOSFODNN7EXAMPLE", 1)} {
		h, n, e := s.CheckCopiedReply(aionConfig(), []byte(reply))
		if e == nil || n != 0 || h == digest || strings.Contains(e.Error(), "ghp_") {
			t.Fatal(h, n, e)
		}
	}
	putPlan(t, root, "system/aion/backlog.md", "new snapshot")
	changed, e := BuildAionInput(root, []string{"log/source.md"}, "")
	if e != nil {
		t.Fatal(e)
	}
	other, _, e := changed.CheckCopiedReply(aionConfig(), []byte(reply))
	if e != nil || other == digest {
		t.Fatal("reply not bound to snapshot")
	}
	// Restore the deliberate test mutation, then prove the seam added no files.
	putPlan(t, root, "system/aion/backlog.md", s.input.Context["system/aion/backlog.md"])
	if reducerDigest(before) != reducerDigest(scan()) {
		t.Fatal("read-only seam changed fixture")
	}
}
func TestAionSourceBoundaries(t *testing.T) {
	root, _ := aionSnapshot(t)
	for _, names := range [][]string{nil, {"../escape.md"}, {"system/aion/backlog.md"}, {"extrinsic/note.md"}, {"log/source.md", "log/source.md"}, {"missing.md"}} {
		if _, e := BuildAionInput(root, names, ""); e == nil {
			t.Fatal(names)
		}
	}
	if e := os.Symlink(filepath.Join(root, "log/source.md"), filepath.Join(root, "log/link.md")); e != nil {
		t.Fatal(e)
	}
	if _, e := BuildAionInput(root, []string{"log/link.md"}, ""); e == nil {
		t.Fatal("symlink accepted")
	}
	putPlan(t, root, "log/large.md", strings.Repeat("x", maxAionSourceBytes+1))
	if _, e := BuildAionInput(root, []string{"log/large.md"}, ""); e == nil {
		t.Fatal("oversized source accepted")
	}
}

func TestAionContextSizeAndEncoding(t *testing.T) {
	root, _ := aionSnapshot(t)
	for _, body := range []string{strings.Repeat("x", maxAionSourceBytes+1), "\xff"} {
		putPlan(t, root, "system/aion/backlog.md", body)
		if _, e := BuildAionInput(root, []string{"log/source.md"}, ""); e == nil {
			t.Fatal("invalid context accepted")
		}
	}
	putPlan(t, root, "system/aion/backlog.md", "")
	s, e := BuildAionInput(root, []string{"log/source.md"}, "")
	if e != nil || s.Readiness(aionConfig()).RecordCount != 3 {
		t.Fatal("empty required record lost", e)
	}
}
