package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/approvals"
	"manifest/hermes"
	"manifest/realestate"
	"manifest/record"
	"manifest/reintake"
	"manifest/spirits"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

func intakeFixture(t *testing.T, enabled bool) (*Server, string, string) {
	t.Helper()
	vault, data, harness := t.TempDir(), t.TempDir(), t.TempDir()
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	vw := vaultwriter.New(vault).Grant(vaultwriter.Capability{Name: "re-files", Zone: record.ZoneSystem, Pattern: "system/realestate/files/**", Actor: vaultwriter.ActorUserAction})
	s := New(nil, nil, nil)
	s.UseVault(vw)
	s.UseIndex(ix)
	s.UseRealestate(realestate.New(ix), "system/realestate", data)
	s.reFiles = realestate.NewFileStore(vault, "system/realestate", func(p string, b []byte) error { return vw.WriteCap("re-files", p, b) })
	s.UseApprovals(approvals.NewStore(filepath.Join(harness, "artifacts")))
	s.UseSpirits(spirits.NewStore(harness))
	zero := 0.0
	a := hermes.DutyAuthority{Provider: reintake.Provider, Model: reintake.Model, Endpoint: hermes.LocalEndpoint, ProviderBinding: hermes.LocalProviderBinding, CostPolicy: hermes.LocalCostPolicy, Tools: []string{"none"}, MCP: "no_mcp", MaxSteps: 1, TimeoutSeconds: 120, CeilingUSD: &zero}
	s.UseReIntake(reintake.Config{ProductionEnabled: enabled, OwnerBoundary: reintake.OwnerBoundary}, data, a)
	// These stand for every forbidden side-effect substrate. The tests also
	// inspect new files in vault/harness rather than relying only on sentinels.
	for _, p := range []string{"connector/cursor.json", "vessel/state/cursor", "ledger/run.jsonl"} {
		abs := filepath.Join(harness, p)
		os.MkdirAll(filepath.Dir(abs), 0700)
		os.WriteFile(abs, []byte("unchanged"), 0600)
	}
	return s, vault, harness
}
func postIntake(s *Server, name, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/realestate/intake?name="+name, strings.NewReader(body)))
	return w
}
func assertIntakeProtected(t *testing.T, s *Server, vault, harness string) {
	t.Helper()
	for _, p := range []string{"connector/cursor.json", "vessel/state/cursor", "ledger/run.jsonl"} {
		b, err := os.ReadFile(filepath.Join(harness, p))
		if err != nil || string(b) != "unchanged" {
			t.Fatal(p, err)
		}
	}
	filepath.WalkDir(harness, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(harness, p)
			rel = filepath.ToSlash(rel)
			if rel != "connector/cursor.json" && rel != "vessel/state/cursor" && rel != "ledger/run.jsonl" && !strings.HasPrefix(rel, "artifacts/approvals/pending/") {
				t.Error("unexpected harness write", rel)
			}
		}
		return nil
	})
	files, _ := filepath.Glob(filepath.Join(harness, "vessel/spool/*"))
	if len(files) != 0 {
		t.Fatal("spooled", files)
	}
	for _, status := range []string{"approved", "rejected"} {
		if len(s.approvals.List(status)) != 0 {
			t.Fatal("decision written")
		}
	}
	filepath.WalkDir(vault, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(vault, p)
			if !strings.HasPrefix(filepath.ToSlash(rel), "system/realestate/files/") {
				t.Error("non-source vault write", rel)
			}
		}
		return nil
	})
}

func TestREIntakeDefaultOffPreservesCanonicalSpool(t *testing.T) {
	s, vault, harness := intakeFixture(t, false)
	s.reIntakeRun = func(context.Context, string, reintake.Config, hermes.DutyAuthority, reintake.ProductionContract) (approvals.Proposal, reintake.ProductionReceipt, error) {
		t.Fatal("successor called while off")
		return approvals.Proposal{}, reintake.ProductionReceipt{}, nil
	}
	w := postIntake(s, "bid.txt", "synthetic bid")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"spooled":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	files, _ := filepath.Glob(filepath.Join(harness, "vessel/spool/*"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	b, _ := os.ReadFile(files[0])
	if !strings.Contains(string(b), "contractor records") || !strings.Contains(string(b), "existing contract records") {
		t.Fatal(string(b))
	}
	files, _ = filepath.Glob(filepath.Join(vault, "system/realestate/files/*"))
	if len(files) != 3 {
		t.Fatal(files)
	}
	if reintake.PilotStatus(s.reIntakeDataDir) != "unused; one document maximum" {
		t.Fatal("off route burnt pilot")
	}
	if len(s.approvals.List("pending")) != 0 {
		t.Fatal("off created proposal")
	}
	s.spirits = nil
	w = postIntake(s, "next.txt", "next")
	if w.Code != 503 || !strings.Contains(w.Body.String(), "no engine harness configured") {
		t.Fatal(w.Body.String())
	}
}

// This helper simulates the adapter RETURN boundary, not DutyVerified or a live
// model. reintake tests exercise parsing/receipts; hermes tests prove isolation.
func syntheticIntakeCandidate(t *testing.T, dir string, c reintake.ProductionContract) (approvals.Proposal, reintake.ProductionReceipt) {
	t.Helper()
	payload := approvals.ReContractPayload{Kind: "bid", ContractorCreate: "Synthetic contractor", Name: "Synthetic bid", Total: 100, Doc: c.Source, Allocations: []approvals.ReContractAllocation{{Property: "synthetic", Node: "work", Amount: 100}}}
	b, _ := json.Marshal(payload)
	p := approvals.Proposal{Type: approvals.TypeReContract, Agent: "extractor", Ritual: "re-intake", Action: "Review re-intake candidate", ApplyPath: c.ApplyPath, Body: "````re-contract\n" + string(b) + "\n````\n"}
	raw, _ := json.Marshal(p)
	receipt := reintake.ProductionReceipt{State: "verified", CandidateSHA256: fmt.Sprintf("%x", sha256.Sum256(raw))}
	final, _ := json.Marshal(receipt)
	if err := os.WriteFile(filepath.Join(dir, reintake.ProductionPath, "run.jsonl"), append([]byte("{\"state\":\"uncertain\"}\n"), append(final, '\n')...), 0600); err != nil {
		t.Fatal(err)
	}
	return p, receipt
}

func TestREIntakeProductionPendingOnlyAndOneShot(t *testing.T) {
	s, vault, harness := intakeFixture(t, true)
	calls := 0
	s.reIntakeRun = func(_ context.Context, dir string, cfg reintake.Config, a hermes.DutyAuthority, c reintake.ProductionContract) (approvals.Proposal, reintake.ProductionReceipt, error) {
		calls++
		if err := reintake.ValidateProductionRoute(cfg, a, c); err != nil {
			t.Fatal(err)
		}
		if c.Owner != "owner" || len(c.Documents) != 1 || c.Source != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("synthetic bid"))) {
			t.Fatal(c)
		}
		b, err := os.ReadFile(filepath.Join(dir, reintake.ProductionPath, "staging", c.TextSource[7:]+".txt"))
		if err != nil || string(b) != "synthetic bid" {
			t.Fatal(string(b), err)
		}
		for _, text := range []string{"contractor records", "active properties", "existing contract records", "Existing work nodes"} {
			if !strings.Contains(c.Context, text) {
				t.Fatal(text)
			}
		}
		files, _ := filepath.Glob(filepath.Join(vault, "system/realestate/files/*"))
		if len(files) != 3 {
			t.Fatal("model preceded source writer", files)
		}
		p, r := syntheticIntakeCandidate(t, dir, c)
		return p, r, nil
	}
	w := postIntake(s, "bid.txt", "synthetic bid")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"pending"`) || !strings.Contains(w.Body.String(), `"spooled":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	ps := s.approvals.List("pending")
	if len(ps) != 1 || ps[0].Auto != "" || ps[0].Status != "pending" {
		t.Fatal(ps)
	}
	if n, _ := s.approvals.AutoApplyAppends(nil); n != 0 {
		t.Fatal("auto applied")
	}
	// Rewiring emulates a restart: the durable reservation, not process memory,
	// refuses a second upload before source artifacts or the adapter.
	s.UseReIntake(s.reIntakeConfig, s.reIntakeDataDir, s.reIntakeAuthority)
	w = postIntake(s, "second.txt", "different document")
	if w.Code != 503 || !strings.Contains(w.Body.String(), "STOP") || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	files, _ := filepath.Glob(filepath.Join(vault, "system/realestate/files/*"))
	if len(files) != 3 {
		t.Fatal("second source ingested", files)
	}
	assertIntakeProtected(t, s, vault, harness)
}

func TestREIntakeProductionRefusalsNeverSpoolProposeOrRetry(t *testing.T) {
	for _, kind := range []string{"authority", "boundary", "empty-name", "no-text", "oversize-text", "writer", "index", "uncertain", "malformed", "receipt-lost", "receipt-drift", "filing"} {
		t.Run(kind, func(t *testing.T) {
			s, vault, harness := intakeFixture(t, true)
			calls := 0
			name, body := "bid.txt", "synthetic bid"
			switch kind {
			case "authority":
				s.reIntakeAuthority.Model = "other"
			case "boundary":
				s.reIntakeConfig.OwnerBoundary = ""
			case "empty-name":
				name = ""
			case "no-text":
				name = "image.png"
			case "oversize-text":
				body = strings.Repeat("x", 16001)
			case "writer":
				s.vault = vaultwriter.New(vault)
			case "index":
				s.index.Close()
			}
			s.reIntakeRun = func(_ context.Context, dir string, _ reintake.Config, _ hermes.DutyAuthority, c reintake.ProductionContract) (approvals.Proposal, reintake.ProductionReceipt, error) {
				calls++
				if kind == "uncertain" {
					return approvals.Proposal{}, reintake.ProductionReceipt{}, errors.New("timeout")
				}
				p, r := syntheticIntakeCandidate(t, dir, c)
				switch kind {
				case "malformed":
					p.Auto = "yes"
				case "receipt-lost":
					os.Remove(filepath.Join(dir, reintake.ProductionPath, "run.jsonl"))
				case "receipt-drift":
					r.CandidateSHA256 = "wrong"
				case "filing":
					os.Remove(filepath.Join(harness, "artifacts/approvals/pending"))
				}
				return p, r, nil
			}
			w := postIntake(s, name, body)
			if w.Code != 503 || !strings.Contains(w.Body.String(), `"pageOwnerRequired":true`) {
				t.Fatal(w.Code, w.Body.String())
			}
			if len(s.approvals.List("pending")) != 0 {
				t.Fatal("failed with proposal")
			}
			again := postIntake(s, name, body)
			if again.Code != 503 || calls > 1 {
				t.Fatal("retried", calls, again.Code)
			}
			assertIntakeProtected(t, s, vault, harness)
		})
	}
}

func TestREIntakeExactRouteAndOwnerPortalBoundary(t *testing.T) {
	s, _, _ := intakeFixture(t, true)
	h := s.Handler()
	for _, route := range []string{"/api/realestate/intake/other", "/api/reintake", "/api/realestate/intake/"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, route, strings.NewReader("doc")))
		if w.Code == 200 {
			t.Fatal(route)
		}
	}
	// Public/team listeners have explicit route mounts, never this owner handler.
	for _, file := range []string{"portal.go", "olga.go"} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "handleREIntake") {
			t.Fatal("owner route exposed", file)
		}
	}
}

func TestREIntakeProductionCanonicalDocxExtraction(t *testing.T) {
	s, vault, harness := intakeFixture(t, true)
	s.spirits = nil // the successor never requires the Excalibur engine handle
	var doc bytes.Buffer
	z := zip.NewWriter(&doc)
	f, err := z.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Synthetic extracted bid</w:t></w:r></w:p></w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	original := fmt.Sprintf("sha256:%x", sha256.Sum256(doc.Bytes()))
	s.reIntakeRun = func(_ context.Context, dir string, _ reintake.Config, _ hermes.DutyAuthority, c reintake.ProductionContract) (approvals.Proposal, reintake.ProductionReceipt, error) {
		if c.Source != original || c.TextSource == original {
			t.Fatal("original/extract identity conflated", c)
		}
		text, err := os.ReadFile(filepath.Join(dir, reintake.ProductionPath, "staging", c.TextSource[7:]+".txt"))
		if err != nil || !strings.Contains(string(text), "Synthetic extracted bid") {
			t.Fatal(string(text), err)
		}
		extract, err := os.ReadFile(filepath.Join(vault, "system/realestate/files", original[7:]+".extract.md"))
		if err != nil || !bytes.Contains(extract, text) || !strings.Contains(string(extract), original) {
			t.Fatal("canonical extract mismatch", err)
		}
		p, r := syntheticIntakeCandidate(t, dir, c)
		return p, r, nil
	}
	w := postIntake(s, "bid.docx", doc.String())
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	payload, ok := approvals.ParseReContractPayload(s.approvals.List("pending")[0].Body)
	if !ok || payload.Doc != original {
		t.Fatal(payload)
	}
	assertIntakeProtected(t, s, vault, harness)
}
