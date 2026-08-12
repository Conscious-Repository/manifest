package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/realestate"
	"manifest/record"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

// rePublishFixture: a vault with assumptions.md at seed values + a re-portal
// checkout (cloned from a bare remote) whose deals.json holds one template
// deal with no vault record and whose defaults.js is the live portal shape.
func rePublishFixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	vault := t.TempDir()
	dataDir := t.TempDir()
	scratch := t.TempDir()
	remote := filepath.Join(scratch, "remote.git")
	checkout := filepath.Join(scratch, "oodagroup")

	if err := os.MkdirAll(filepath.Join(vault, "system", "realestate"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "system", "realestate", "assumptions.md"),
		[]byte(realestate.SeedAssumptions()), 0o644); err != nil {
		t.Fatal(err)
	}

	git(t, scratch, "-c", "init.defaultBranch=main", "init", "--bare", remote)
	git(t, scratch, "clone", remote, checkout)
	git(t, checkout, "checkout", "-b", "main")
	// deals.json: exactly the recompose's own formatting so a no-edit render
	// is byte-identical (production files are written the same way)
	var tmpl []any
	_ = json.Unmarshal([]byte(`[{"slug":"template-only-deal","name":"Template Only","properties":[{"purchase_price":100000}]}]`), &tmpl)
	pretty, _ := json.MarshalIndent(tmpl, "", "  ")
	if err := os.MkdirAll(filepath.Join(checkout, "src", "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "src", "data", "deals.json"), append(pretty, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	defaults := "export const defaults = {\n" +
		"  vacancy_rate: 0.08,\n  opex_rate: 0.35,\n  rent_growth: 0.04,\n  opex_growth: 0.02,\n" +
		"  capex_per_unit_year: 350,\n  construction_period_months: 10,\n  hold_years: 5,\n" +
		"  exit_cap_rate: 0.0725,\n  perm_interest_rate: 0.0625,\n  perm_amort_years: 25,\n  perm_ltv: 0.75,\n" +
		"  construction_interest_rate: 0.10,\n  construction_loan_ltc: 0.70,\n  selling_cost_pct: 0.015,\n  contingency_pct: 0.05,\n" +
		"};\n\nexport const default_opex_items = {\n  property_tax_rate: 0.10,\n};\n"
	if err := os.MkdirAll(filepath.Join(checkout, "src", "engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "src", "engine", "defaults.js"), []byte(defaults), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, checkout, "add", "-A")
	git(t, checkout, "commit", "-m", "init")
	git(t, checkout, "push", "-u", "origin", "main")

	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	vw := vaultwriter.New(vault).WithZoneRoots("system", "extrinsic").Grant(
		vaultwriter.Capability{Name: "realestate", Zone: record.ZoneSystem,
			Pattern: "system/realestate/**", Actor: vaultwriter.ActorUserAction},
	)
	srv := &Server{index: ix, aionDataDir: dataDir}
	srv.UseVault(vw)
	srv.realestate = realestate.New(ix)
	srv.UseRePortal(checkout)
	return srv, checkout, remote
}

func TestRePublishEndToEnd(t *testing.T) {
	srv, checkout, remote := rePublishFixture(t)

	// clean fixture: both contract files render byte-identical → nothing dirty
	code, prev := doJSON(t, srv.handleRePublishPreview, "GET", "/api/re/publish/preview", "")
	if code != 200 {
		t.Fatalf("preview: %d %v", code, prev)
	}
	if b, _ := prev["blockers"].([]any); len(b) != 0 {
		t.Fatalf("blockers: %v", prev["blockers"])
	}
	for _, f := range prev["files"].([]any) {
		if f.(map[string]any)["status"] != "unchanged" {
			t.Fatalf("clean fixture not byte-stable: %v", f)
		}
	}
	code, res := doJSON(t, srv.handleRePublish, "POST", "/api/re/publish", `{"hash":"`+prev["hash"].(string)+`"}`)
	if code == 200 && res["ok"] == true {
		t.Fatalf("published with nothing to publish: %v", res)
	}

	// an assumptions edit dirties ONLY defaults.js
	a := srv.loadAssumptions()
	if err := a.SetAssumption("vacancy_rate", 0.1); err != nil {
		t.Fatal(err)
	}
	vaultAssump := filepath.Join(srv.vault.VaultRoot(), "system", "realestate", "assumptions.md")
	if err := os.WriteFile(vaultAssump, []byte(realestate.EmitAssumptions(a)), 0o644); err != nil {
		t.Fatal(err)
	}
	code, prev = doJSON(t, srv.handleRePublishPreview, "GET", "/api/re/publish/preview", "")
	if code != 200 {
		t.Fatalf("preview2: %d %v", code, prev)
	}
	var changed []string
	for _, f := range prev["files"].([]any) {
		m := f.(map[string]any)
		if m["status"] != "unchanged" {
			changed = append(changed, m["path"].(string))
		}
	}
	if len(changed) != 1 || changed[0] != "src/engine/defaults.js" {
		t.Fatalf("changed = %v, want only defaults.js", changed)
	}

	// stale hash → 409
	req := httptest.NewRequest("POST", "/api/re/publish", strings.NewReader(`{"hash":"stale"}`))
	rec := httptest.NewRecorder()
	srv.handleRePublish(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d", rec.Code)
	}

	// confirm: one commit lands on the bare remote; the module keeps the
	// untouched literals (0.10) and carries the edit
	code, res = doJSON(t, srv.handleRePublish, "POST", "/api/re/publish", `{"hash":"`+prev["hash"].(string)+`"}`)
	if code != 200 || res["ok"] != true {
		t.Fatalf("publish: %d %v", code, res)
	}
	commit, _ := res["commit"].(string)
	if remoteHead := git(t, remote, "rev-parse", "refs/heads/main"); remoteHead != commit {
		t.Fatalf("remote head %s != %s", remoteHead, commit)
	}
	def, _ := os.ReadFile(filepath.Join(checkout, "src", "engine", "defaults.js"))
	if !strings.Contains(string(def), "  vacancy_rate: 0.1,") ||
		!strings.Contains(string(def), "  construction_interest_rate: 0.10,") {
		t.Fatalf("published module wrong:\n%s", def)
	}
	// the working tree is clean afterwards — the effector committed what it wrote
	if status := git(t, checkout, "status", "--porcelain"); status != "" {
		t.Fatalf("checkout left dirty:\n%s", status)
	}

	// a hand-edited contract file is a hard preflight block
	if err := os.WriteFile(filepath.Join(checkout, "src", "data", "deals.json"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, prev = doJSON(t, srv.handleRePublishPreview, "GET", "/api/re/publish/preview", "")
	if b, _ := prev["blockers"].([]any); len(b) == 0 {
		t.Fatal("hand-edited deals.json did not block")
	}
}
