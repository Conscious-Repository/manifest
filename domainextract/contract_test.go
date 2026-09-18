package domainextract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/hermes"
)

func inputFixture() Input {
	return Input{Ritual: "aion", Documents: []Document{{Name: "log/2026-09-12 fixture.md", Text: "Jane: I will review the draft."}}, Context: map[string]string{"system/aion/backlog.md": "", "system/aion/people.md": "", "system/aion/heuristics.md": ""}}
}

const replyFixture = `{"summary":"Jane committed to review.","candidates":[{"type":"aion-backlog","applyPath":"system/aion/backlog.md","source":"log/2026-09-12 fixture.md","payload":{"kind":"task","title":"Review draft","quote":"I will review the draft.","sources":["log/2026-09-12 fixture.md"],"confidence":0.8}}]}`

func TestContractBoundaries(t *testing.T) {
	input := inputFixture()
	p, e := ValidateReply(input, replyFixture)
	if e != nil || len(p) != 1 || p[0].Proposed != "" || p[0].Auto != "" {
		t.Fatal(p, e)
	}
	for _, raw := range []string{
		strings.Replace(replyFixture, `"type":`, `"Type":`, 1),
		strings.Replace(replyFixture, `"type":`, `"type":"aion-backlog","type":`, 1),
		strings.Replace(replyFixture, "system/aion/backlog.md", "system/realestate/backlog.md", 1),
		strings.Replace(replyFixture, "I will review the draft.", "Invented quote.", 1),
		strings.Replace(replyFixture, `"kind":"task"`, `"kind":"heuristic"`, 1),
		strings.Replace(replyFixture, `"confidence":0.8`, `"confidence":0.8,"auto":true`, 1),
		replyFixture + "{}",
	} {
		if _, e := ValidateReply(input, raw); e == nil {
			t.Fatal("invalid contract accepted", raw)
		}
	}
	changed := input
	changed.Context = map[string]string{"system/aion/backlog.md": "new context"}
	if input.ID() != changed.ID() {
		t.Fatal("context changes duplicated accepted source identity")
	}
}
func writeInput(t *testing.T, vault string, input Input) {
	t.Helper()
	for name, text := range input.Context {
		path := filepath.Join(vault, name)
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, []byte(text), 0600)
	}
	for _, d := range input.Documents {
		path := filepath.Join(vault, d.Name)
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, []byte(d.Text), 0600)
	}
}
func TestRecoverVerifiedBatchWithoutModelOrDecisionReplay(t *testing.T) {
	dir, vault := t.TempDir(), t.TempDir()
	input := inputFixture()
	writeInput(t, vault, input)
	ap := approvals.NewStore(filepath.Join(dir, "artifacts"))
	harness := prepareHandoff(t, dir, "aion", 1)
	s := New(context.Background(), dir, vault, harness, Config{Aion: true}, nil, ap)
	candidates, e := ValidateReply(input, replyFixture)
	if e != nil {
		t.Fatal(e)
	}
	// Prior publication succeeded and owner rejected; checkpoint acknowledgement
	// was lost. A verified batch resumes filing only, with no model invocation.
	if _, e = ap.ProposeOnce(candidates[0]); e != nil {
		t.Fatal(e)
	}
	if e = ap.Reject(candidates[0].ID, "reviewed"); e != nil {
		t.Fatal(e)
	}
	j := Job{Version: 1, OwnershipRevision: 1, ID: input.ID(), Input: input, State: "verified", Started: time.Now(), Candidates: candidates}
	if e = s.save(j); e != nil {
		t.Fatal(e)
	}
	s.sweep()
	raw, _ := os.ReadFile(filepath.Join(s.dir, j.ID+".json"))
	json.Unmarshal(raw, &j)
	if j.State != "completed" || j.Published != 1 || len(ap.List("pending")) != 0 || len(ap.List("rejected")) != 1 {
		t.Fatal(j.State, j.Published)
	}
	for name, before := range input.Context {
		after, err := os.ReadFile(filepath.Join(vault, name))
		if err != nil || string(after) != before {
			t.Fatal("publication changed vault context", name, err)
		}
	}
	for _, doc := range input.Documents {
		after, err := os.ReadFile(filepath.Join(vault, doc.Name))
		if err != nil || string(after) != doc.Text {
			t.Fatal("publication changed source", err)
		}
	}
}
func TestInterruptedAndStaleDoNotExecute(t *testing.T) {
	for _, state := range []string{"running", "verified"} {
		t.Run(state, func(t *testing.T) {
			dir, vault := t.TempDir(), t.TempDir()
			input := inputFixture()
			writeInput(t, vault, input)
			ap := approvals.NewStore(filepath.Join(dir, "artifacts"))
			harness := prepareHandoff(t, dir, "aion", 1)
			s := New(context.Background(), dir, vault, harness, Config{Aion: true}, nil, ap)
			candidates, _ := ValidateReply(input, replyFixture)
			j := Job{Version: 1, OwnershipRevision: 1, ID: input.ID(), Input: input, State: state, Candidates: candidates}
			s.save(j)
			if state == "verified" {
				os.WriteFile(filepath.Join(vault, input.Documents[0].Name), []byte("changed"), 0600)
			}
			s.sweep()
			raw, _ := os.ReadFile(filepath.Join(s.dir, j.ID+".json"))
			json.Unmarshal(raw, &j)
			if (j.State != "uncertain" && j.State != "refused") || len(ap.List("pending")) != 0 {
				t.Fatal(j.State)
			}
		})
	}
}
func TestPreservesModelBeforeAccepting(t *testing.T) {
	zero := 0.0
	r := hermes.NewRunner(hermes.Config{Enabled: true, Duties: map[string]hermes.DutyAuthority{"extractor/aion": {Provider: "deepseek-local", Model: "deepseek-v4.1-flash", Endpoint: hermes.LocalEndpoint, ProviderBinding: hermes.LocalProviderBinding, CostPolicy: hermes.LocalCostPolicy, Tools: []string{"none"}, MCP: "no_mcp", TimeoutSeconds: 120, MaxSteps: 1, CeilingUSD: &zero}}})
	ap := approvals.NewStore(t.TempDir())
	s := New(context.Background(), t.TempDir(), t.TempDir(), "", Config{Aion: true}, r, ap)
	if _, e := s.Submit(inputFixture()); e == nil {
		t.Fatal("changed existing model")
	}
}

func TestReadInputRejectsDisguisedExcludedSources(t *testing.T) {
	vault := t.TempDir()
	input := inputFixture()
	input.Context["system/realestate/backlog.md"] = "Private real estate record"
	input.Context["extrinsic/private.md"] = "Excluded source"
	writeInput(t, vault, input)
	for _, name := range []string{"./system/realestate/backlog.md", "./extrinsic/private.md", "log/../system/realestate/backlog.md"} {
		if got, err := ReadInput(vault, "aion", []Document{{Name: name}}); err == nil {
			t.Errorf("excluded source accepted: %+v", got.Documents)
		}
	}
}

func TestDisabledExtractionAndInvalidInputs(t *testing.T) {
	s := New(context.Background(), t.TempDir(), t.TempDir(), "", Config{}, nil, approvals.NewStore(t.TempDir()))
	if _, err := s.Submit(inputFixture()); err == nil {
		t.Fatal("disabled service accepted work")
	}
	if _, err := os.Stat(s.dir); !os.IsNotExist(err) {
		t.Fatal("disabled service created job state", err)
	}
	for _, mode := range []string{"empty", "duplicate", "missing-name", "domain"} {
		input := inputFixture()
		switch mode {
		case "empty":
			input.Documents[0].Text = " \n"
		case "duplicate":
			input.Documents = append(input.Documents, input.Documents[0])
		case "missing-name":
			input.Documents[0].Name = ""
		case "domain":
			input.Ritual = "unknown"
		}
		if _, err := input.Prompt(); err == nil {
			t.Fatal("invalid input accepted", mode)
		}
	}
}

func TestSnapshotBindsContextWithoutChangingReplayIdentity(t *testing.T) {
	i := inputFixture()
	before, err := ValidateReply(i, replyFixture)
	if err != nil {
		t.Fatal(err)
	}
	i.Context["system/aion/backlog.md"] = "changed"
	after, err := ValidateReply(i, replyFixture)
	if err != nil {
		t.Fatal(err)
	}
	if before[0].ID != after[0].ID {
		t.Fatal("context drift must not create a replay identity")
	}
	if before[0].ExtractionSnapshot == after[0].ExtractionSnapshot {
		t.Fatal("context absent from snapshot")
	}
}

func TestOodaArtifactProvenance(t *testing.T) {
	source := "Builder offers renovation for ten dollars."
	hash := "sha256:" + approvals.EvidenceHash(source)
	i := Input{Ritual: "ooda-email", Documents: []Document{{Name: hash, Text: source}}, Context: map[string]string{
		"system/realestate/people.md":              "",
		"system/realestate/properties/home.md":     "---\ncategories: [property]\n---\n## rocks\n- [ ] Renovation\n",
		"system/realestate/contractors/builder.md": "---\ncategories: [contractor]\n---\n",
	}}
	reply := `{"summary":"Bid found","candidates":[{"type":"re-contract","applyPath":"system/realestate/contracts/bid.md","source":"` + hash + `","payload":{"kind":"bid","contractor":"builder","name":"Bid","total":10,"doc":"` + hash + `","allocations":[{"property":"home","node":"renovation","amount":10}]}}]}`
	ps, err := ValidateReply(i, reply)
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := approvals.ParseReContractPayload(ps[0].Body)
	if !ok || payload.Doc != hash || !strings.Contains(ps[0].Body, "Source: portal email "+hash) || ps[0].ExtractionSnapshot == "" {
		t.Fatal("lost exact provenance")
	}
	i.Documents[0].Text += " drift"
	if _, err := ValidateReply(i, reply); err == nil {
		t.Fatal("accepted mismatched source hash")
	}
}

func TestHermesCompleteInputBound(t *testing.T) {
	input := inputFixture()
	input.Context["system/aion/backlog.md"] = strings.Repeat("x", 200000)
	if prompt, err := input.Prompt(); err != nil || !strings.Contains(prompt, input.Context["system/aion/backlog.md"]) {
		t.Fatal("complete production-sized context rejected or truncated", err)
	}
	input.Context["system/aion/backlog.md"] = strings.Repeat("x", hermes.ExtractionPromptLimit)
	if _, err := input.Prompt(); err == nil {
		t.Fatal("oversized context accepted")
	}
	input.Context["system/aion/backlog.md"] = strings.Repeat("<", 100000)
	if _, err := input.Prompt(); err == nil {
		t.Fatal("JSON expansion bypassed bound")
	}
}
