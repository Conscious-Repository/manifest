package reintake

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"manifest/hermes"
)

func productionFixture(t *testing.T) (string, Config, hermes.DutyAuthority, ProductionContract, string) {
	t.Helper()
	dir := t.TempDir()
	text := []byte("synthetic confidential document sentinel")
	source := fmtSource(text)
	c := ProductionContract{Owner: "owner", Actor: "extractor", Source: source, Documents: []string{source}, Target: "system/realestate/contracts/synthetic.md", ApplyPath: "system/realestate/contracts/synthetic.md"}
	zero := 0.0
	a := hermes.DutyAuthority{Provider: Provider, Model: Model, Endpoint: hermes.LocalEndpoint, ProviderBinding: hermes.LocalProviderBinding, CostPolicy: hermes.LocalCostPolicy, Tools: []string{"none"}, MCP: "no_mcp", MaxSteps: 1, TimeoutSeconds: 120, CeilingUSD: &zero}
	stage := filepath.Join(dir, ProductionPath, "staging")
	if err := os.MkdirAll(stage, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, source[7:]+".txt"), text, 0600); err != nil {
		t.Fatal(err)
	}
	reply := `{"type":"re-contract","actor":"extractor","source":"` + source + `","target":"` + c.Target + `","applyPath":"` + c.ApplyPath + `","payload":{"kind":"bid","contractor":"synthetic","name":"private response sentinel","total":100,"doc":"` + source + `","allocations":[{"property":"synthetic","node":"work","amount":100}]}}`
	return dir, Config{ProductionEnabled: true}, a, c, reply
}
func fmtSource(b []byte) string {
	sum := sha256.Sum256(b)
	const hex = "0123456789abcdef"
	out := "sha256:"
	for _, v := range sum {
		out += string([]byte{hex[v>>4], hex[v&15]})
	}
	return out
}
func readReceipt(t *testing.T, dir string) []ProductionReceipt {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ProductionPath, "run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sentinel", "sha256:", "synthetic.md", "Bearer", "password"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("raw data leaked", string(b))
		}
	}
	var rs []ProductionReceipt
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var r ProductionReceipt
		if json.Unmarshal([]byte(line), &r) != nil {
			t.Fatal(string(b))
		}
		rs = append(rs, r)
	}
	info, _ := os.Stat(filepath.Join(dir, ProductionPath, "run.jsonl"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	return rs
}

func TestProductionPrelaunchRefusals(t *testing.T) {
	changes := map[string]func(*Config, *hermes.DutyAuthority, *ProductionContract){
		"default-off":       func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { *cfg = Config{} },
		"missing-authority": func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { *a = hermes.DutyAuthority{} },
		"owner":             func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { c.Owner = "agent:owner" },
		"actor":             func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { c.Actor = "other" },
		"zero-documents":    func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { c.Documents = nil },
		"two-documents": func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) {
			c.Documents = append(c.Documents, c.Source)
		},
		"source":          func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { c.Source = "https://example.com" },
		"source-mismatch": func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { c.Documents[0] = "other" },
		"target":          func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { c.Target += "other" },
		"apply-path": func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) {
			c.ApplyPath = "system/realestate/contracts/../bad.md"
			c.Target = c.ApplyPath
		},
		"model":    func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { a.Model = "other" },
		"provider": func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { a.Provider = "claude-sub" },
		"endpoint": func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { a.Endpoint = "http://localhost/v1" },
		"binding":  func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { a.ProviderBinding = "" },
		"policy":   func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { a.CostPolicy = "" },
		"tools":    func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { a.Tools = []string{"web"} },
		"mcp":      func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { a.MCP = "vault" },
		"steps":    func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { a.MaxSteps = 2 },
		"timeout":  func(cfg *Config, a *hermes.DutyAuthority, c *ProductionContract) { a.TimeoutSeconds = 121 },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			dir, cfg, a, c, _ := productionFixture(t)
			change(&cfg, &a, &c)
			calls := 0
			p, r, err := runStaged(context.Background(), dir, cfg, a, c, func(context.Context, hermes.DutyAuthority, string) (boundedCompletion, error) {
				calls++
				return boundedCompletion{}, nil
			})
			if err == nil || calls != 0 || p.Body != "" || r.State != "refused" || !r.PageOwnerRequired {
				t.Fatalf("%+v %v calls=%d", r, err, calls)
			}
			rs := readReceipt(t, dir)
			if len(rs) != 2 || rs[0].State != "uncertain" || rs[1].State != "refused" {
				t.Fatal(rs)
			}
		})
	}
}

func TestProductionStagingContainment(t *testing.T) {
	for _, kind := range []string{"symlink-file", "symlink-stage", "symlink-ancestor", "hardlink", "fifo", "oversize", "hash", "utf8", "traversal", "url", "absolute"} {
		t.Run(kind, func(t *testing.T) {
			dir, cfg, a, c, _ := productionFixture(t)
			stage := filepath.Join(dir, ProductionPath, "staging")
			file := filepath.Join(stage, c.Source[7:]+".txt")
			switch kind {
			case "symlink-file":
				os.Remove(file)
				os.Symlink(filepath.Join(t.TempDir(), "secret"), file)
			case "symlink-stage":
				os.Rename(stage, stage+"-old")
				os.Symlink(stage+"-old", stage)
			case "symlink-ancestor":
				base := filepath.Join(dir, "excalibur-retirement")
				os.Rename(base, base+"-old")
				os.Symlink(base+"-old", base)
			case "hardlink":
				os.Link(file, filepath.Join(t.TempDir(), "linked"))
			case "fifo":
				os.Remove(file)
				syscall.Mkfifo(file, 0600)
			case "oversize":
				os.WriteFile(file, []byte(strings.Repeat("a", maxDocumentBytes+1)), 0600)
			case "hash":
				os.WriteFile(file, []byte("changed"), 0600)
			case "utf8":
				os.WriteFile(file, []byte{255}, 0600)
			case "traversal":
				c.Source = "../../secret"
				c.Documents = []string{c.Source}
			case "url":
				c.Source = "https://example.com/doc"
				c.Documents = []string{c.Source}
			case "absolute":
				c.Source = file
				c.Documents = []string{c.Source}
			}
			_, _, err := runStaged(context.Background(), dir, cfg, a, c, func(context.Context, hermes.DutyAuthority, string) (boundedCompletion, error) {
				t.Fatal("unsafe staging reached provider")
				return boundedCompletion{}, nil
			})
			if err == nil {
				t.Fatal("accepted unsafe staging")
			}
		})
	}
}

func TestProductionReceiptAndLatch(t *testing.T) {
	for _, state := range []string{"verified", "uncertain", "unverified", "invalid-output", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			dir, cfg, a, c, reply := productionFixture(t)
			protected := []string{"approvals/pending/keep", "vaultwriter/audit", "connectors/state", "cursors/state", "spool/queued", "ledger/events", "portal/state"}
			for _, name := range protected {
				full := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte("preserve existing state"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			p, r, err := runStaged(context.Background(), dir, cfg, a, c, func(ctx context.Context, got hermes.DutyAuthority, prompt string) (boundedCompletion, error) {
				calls++
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 120*time.Second {
					t.Fatal("unbounded execution")
				}
				if got.Validate() != nil || got.Provider != Provider || !strings.Contains(prompt, c.Source) {
					t.Fatal("contract not preserved")
				}
				switch state {
				case "uncertain":
					return boundedCompletion{}, errors.New("Bearer password sentinel")
				case "unverified":
					return boundedCompletion{reply: reply}, nil
				case "invalid-output":
					reply += "{}"
				case "cancelled":
					return boundedCompletion{}, context.Canceled
				}
				return boundedCompletion{reply: reply, verified: true, usage: &hermes.TokenUsage{}}, nil
			})
			want := "uncertain"
			if state == "verified" {
				want = "verified"
			}
			if state == "invalid-output" {
				want = "refused"
			}
			if calls != 1 || r.State != want || (err == nil) != (want == "verified") || (p.Body != "") != (want == "verified") {
				t.Fatalf("%+v %v", r, err)
			}
			if want == "verified" && (p.Proposed != "" || p.Auto != "" || p.ID != "" || r.CandidateSHA256 == "") {
				t.Fatal("not candidate-only")
			}
			rs := readReceipt(t, dir)
			if len(rs) != 2 || rs[1].State != want {
				t.Fatal(rs)
			}
			before, _ := os.ReadFile(filepath.Join(dir, ProductionPath, "run.jsonl"))
			_, _, err = runStaged(context.Background(), dir, cfg, a, c, func(context.Context, hermes.DutyAuthority, string) (boundedCompletion, error) {
				t.Fatal("retry/fallback")
				return boundedCompletion{}, nil
			})
			after, _ := os.ReadFile(filepath.Join(dir, ProductionPath, "run.jsonl"))
			if err == nil || string(before) != string(after) {
				t.Fatal("latch changed")
			}
			// Adapter writes exactly one derived receipt; source is unchanged. No other
			// dataDir subtree (approvals, vaultwriter audit, connector/cursor/spool/ledger)
			// can be created or modified by any terminal outcome.
			var files []string
			filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if !d.IsDir() {
					rel, _ := filepath.Rel(dir, path)
					files = append(files, rel)
				}
				return nil
			})
			for _, name := range protected {
				b, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || string(b) != "preserve existing state" {
					t.Fatal("protected state changed", name, err)
				}
			}
			if len(files) != 2+len(protected) {
				t.Fatal(files)
			}
		})
	}
}

func TestProductionStrictCandidate(t *testing.T) {
	_, _, _, c, reply := productionFixture(t)
	for _, bad := range []string{strings.Replace(reply, `"type":`, `"Type":`, 1), strings.Replace(reply, `"total":100`, `"total":100,"TOTAL":100`, 1), `[` + reply + `]`, reply + reply, strings.Replace(reply, `"type":"re-contract"`, `"type":"re-backlog"`, 1), strings.Replace(reply, `"actor":"extractor"`, `"actor":"extractor","auto":"yes"`, 1), strings.Replace(reply, `"total":100`, `"total":100,"total":100`, 1), strings.Replace(reply, `"total":100`, `"total":99`, 1), strings.Replace(reply, `"kind":"bid"`, `"kind":"other"`, 1), strings.Replace(reply, `"doc":"sha256:`, `"doc":"sha257:`, 1), strings.Replace(reply, c.ApplyPath, "system/other.md", 1), strings.Replace(reply, `"amount":100`, `"amount":100,"unknown":1`, 1), "```json\n" + reply + "\n```"} {
		if p, err := parseProductionCandidate(c, bad); err == nil || p.Body != "" {
			t.Fatal("accepted malformed proposal", bad)
		}
	}
}

func TestProductionRouteAlwaysDisabled(t *testing.T) {
	_, cfg, a, c, _ := productionFixture(t)
	if ValidateProductionRoute(cfg, a, c) == nil {
		t.Fatal("enabled without source integration")
	}
	cfg.ProductionEnabled = false
	if ValidateProductionRoute(cfg, a, c) == nil {
		t.Fatal("default-on route")
	}
}
