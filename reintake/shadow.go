// Package reintake implements only the offline, owner-triggered Phase 3 shadow
// comparison. It has no runner, scheduler, portal, approval store or vault handle.
package reintake

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"time"

	"manifest/approvals"
	"manifest/hermes"
	"manifest/ledger"
	"manifest/mdfm"
)

const Duty = "extractor/re-intake"
const Provider = "deepseek-local"
const Model = "deepseek-v4.1-flash"
const ShadowPath = "excalibur-retirement/shadow/re-intake"
const StopAndPage = "STOP: shadow lane frozen; page owner with local evidence; wait for explicit owner action; no retry or fallback"

// Config has no production mode. A true shadowEnabled permits embedded fixture
// replay only. Missing authority never inherits interactive Hermes defaults.
type Config struct {
	ShadowEnabled bool `json:"shadowEnabled"`
}

//go:embed fixtures/single.json fixtures/split.json
var fixtures embed.FS

type request struct {
	OwnerTriggered bool     `json:"ownerTriggered"`
	Documents      []string `json:"documents"`
	PortalVisible  bool     `json:"portalVisible"`
}
type structure struct {
	Source    string                      `json:"source"`
	Actor     string                      `json:"actor"`
	Target    string                      `json:"target"`
	ApplyPath string                      `json:"applyPath"`
	Type      string                      `json:"type"`
	Payload   approvals.ReContractPayload `json:"payload"`
}
type fixture struct {
	Request   request              `json:"request"`
	Proposals []approvals.Proposal `json:"proposals"`
	Usage     json.RawMessage      `json:"usage"`
	Expected  structure            `json:"expected"`
}

// Report is evidence, never an approvals.Proposal or a live usage receipt.
// It uses the existing run ledger entry vocabulary, privately beneath shadow.
type Report struct {
	Status            string       `json:"status"`
	StructuredParity  bool         `json:"structuredParity"`
	Prose             string       `json:"prose"`
	LiveUsageVerified bool         `json:"liveUsageVerified"`
	ProductionRouted  bool         `json:"productionRouted"`
	PortalVisible     bool         `json:"portalVisible"`
	FixtureSHA256     string       `json:"fixtureSHA256"`
	Entry             ledger.Entry `json:"entry"`
}

func refusal(reason string) error { return &hermes.Refusal{Reason: reason + "; " + StopAndPage} }

func validateAuthority(a hermes.DutyAuthority) error {
	if err := a.Validate(); err != nil {
		return refusal("invalid explicit successor authority")
	}
	if a.Model != Model || a.Provider != Provider {
		return refusal("lane model/provider pin differs")
	}
	if len(a.Tools) != 1 || a.Tools[0] != "none" || a.MCP != "no_mcp" {
		return refusal("shadow requires explicit tool-free authority")
	}
	if a.MaxSteps != 1 || a.TimeoutSeconds > 120 || *a.CeilingUSD != 0 {
		return refusal("lane bounds exceed contract")
	}
	return nil
}

// Replay accepts only named, compiled-in redacted fixtures. No request body,
// document path, arbitrary completion, provider URL or output path is accepted.
// Authority validation is shared with Hermes; synthetic usage can never mint its
// private live verification receipt. Subscription options remain unsupported/unverified.
func Replay(dataDir string, cfg Config, duties map[string]hermes.DutyAuthority, id string) (Report, error) {
	if !cfg.ShadowEnabled {
		return Report{}, refusal("shadow flag off")
	}
	var raw []byte
	switch id {
	case "single", "split":
		raw, _ = fixtures.ReadFile("fixtures/" + id + ".json")
	default:
		// Unknown input is uncertainty too; latch the same local stop record.
		raw = nil
	}
	return replay(dataDir, duties, raw)
}

func replay(dataDir string, duties map[string]hermes.DutyAuthority, raw []byte) (Report, error) {
	root, err := shadowRoot(dataDir)
	if err != nil {
		return Report{}, refusal("shadow evidence path unavailable")
	}
	defer root.Close()
	lock, lockErr := root.OpenFile(".replay-lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if lockErr != nil {
		return Report{}, refusal("shadow busy or interrupted; owner review required")
	}
	lock.Close()
	defer root.Remove(".replay-lock")
	if _, err = root.Lstat("STOP.json"); !os.IsNotExist(err) {
		return Report{}, refusal("prior uncertainty requires owner review")
	}
	report := Report{Status: "shadow-compared", Prose: "semantic-review-only; original prose withheld; no byte parity claim", FixtureSHA256: fmt.Sprintf("%x", sha256.Sum256(raw))}
	// Deterministic fixture timestamp, not a claim about real execution time.
	report.Entry = ledger.Entry{TS: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Source: "run", Kind: "run.completed", Actor: "extractor", Harness: "manifest-shadow", Object: ledger.Object{Kind: ledger.ObjRun, ID: report.FixtureSHA256}, Text: "frozen structural comparison only", Meta: map[string]any{"duty": Duty, "itemsWritten": 0, "modelPin": Model, "providerPin": Provider, "authorityChoice": "primary", "usageEvidence": "synthetic fixture only", "fallback": false, "productionRouted": false}}
	var f fixture
	a, ok := duties[Duty]
	report.Entry.Meta["authority"] = a
	if !ok {
		err = refusal("missing duty authority")
	} else {
		err = validateAuthority(a)
	}
	if err == nil {
		err = decodeStrict(raw, &f)
	}
	if err == nil {
		err = compare(a, f)
	}
	if err != nil {
		report.Status = "refused"
		report.Entry.Kind = "run.refused"
		report.Entry.Text = StopAndPage
		report.Entry.Meta["pageOwnerRequired"] = true
		// Fixed reason only: never persist rejected payload, provider text or paths.
		if writeErr := writeEvidence(root, "STOP.json", report); writeErr != nil {
			return Report{}, refusal("cannot persist stop evidence; owner page required")
		}
		return report, refusal("authority, usage or fixture comparison uncertain")
	}
	report.StructuredParity = true
	report.Entry.Meta["authority"] = a
	if err = writeEvidence(root, report.FixtureSHA256+".json", report); err != nil {
		report.Status = "refused"
		report.StructuredParity = false
		report.Entry.Kind = "run.refused"
		report.Entry.Text = StopAndPage
		report.Entry.Meta["pageOwnerRequired"] = true
		_ = writeEvidence(root, "STOP.json", report)
		return Report{}, refusal("comparison evidence unavailable")
	}
	return report, nil
}

func compare(a hermes.DutyAuthority, f fixture) error {
	if !f.Request.OwnerTriggered || len(f.Request.Documents) != 1 || f.Request.PortalVisible || len(f.Proposals) != 1 {
		return refusal("owner-only one-document proposal scope required")
	}
	// Shared strict usage parser checks completion, exact pins, finite cost and
	// duplicate fields. This is acceptance simulation, never verified execution.
	res, err := hermes.VerifyDutyUsage(a, hermes.Result{}, f.Usage)
	if err != nil || res.DutyVerified() {
		return refusal("usage not accepted")
	}
	var usage struct {
		Policy    string             `json:"cost_policy"`
		Telemetry string             `json:"cost_telemetry"`
		Binding   string             `json:"provider_binding"`
		Tokens    *hermes.TokenUsage `json:"usage"`
		Model     string             `json:"model"`
		Provider  string             `json:"provider"`
		Completed bool               `json:"completed"`
		Cost      *float64           `json:"cost_usd"`
		Steps     *int               `json:"steps"`
	}
	if decodeStrict(f.Usage, &usage) != nil || usage.Steps == nil || *usage.Steps != 1 || *usage.Steps > a.MaxSteps {
		return refusal("invalid fixture step evidence")
	}
	p := f.Proposals[0]
	if strings.TrimSpace(p.Action) == "" || p.Type != approvals.TypeReContract || p.Agent != "extractor" || p.Ritual != "re-intake" || !approvals.ReContractPathAllowed(p.ApplyPath) || p.Proposed != "" || p.Section != "" || p.ErrandText != "" || p.Auto != "" {
		return refusal("proposal contract differs")
	}
	if strings.Count(p.Body, "````re-contract\n") != 1 {
		return refusal("ambiguous payload fence")
	}
	if extra, _ := mdfm.ExtractFencedBlock(p.Body, "proposed"); strings.TrimSpace(extra) != "" {
		return refusal("proposed content must be empty")
	}
	payload, ok := approvals.ParseReContractPayload(p.Body)
	raw, found := mdfm.ExtractFencedBlock(p.Body, approvals.ReContractFence)
	var strict approvals.ReContractPayload
	if !ok || !found || decodeStrict([]byte(raw), &strict) != nil || payload.Validate() != nil {
		return refusal("invalid re-contract payload")
	}
	if payload.Doc == "" || payload.Doc != f.Request.Documents[0] {
		return refusal("document source differs")
	}
	got := structure{Source: payload.Doc, Actor: p.Agent, Target: p.ApplyPath, ApplyPath: p.ApplyPath, Type: p.Type, Payload: payload}
	if !reflect.DeepEqual(structural(got), structural(f.Expected)) {
		return refusal("structured fixture mismatch")
	}
	return nil
}

func structural(s structure) structure {
	// Keep field/cardinality and relationship comparisons; prose values require
	// owner semantic review and cannot be established by this deterministic replay.
	p := s.Payload
	p.Name = ""
	p.Allocations = append([]approvals.ReContractAllocation(nil), p.Allocations...)
	for i := range p.Allocations {
		p.Allocations[i].Reason = ""
	}
	p.Tasks = append([]approvals.ReContractTask(nil), p.Tasks...)
	for i := range p.Tasks {
		p.Tasks[i].Text = ""
	}
	blank := func(in []string) []string {
		if in == nil {
			return nil
		}
		return make([]string, len(in))
	}
	p.Terms = blank(p.Terms)
	p.Exclusions = blank(p.Exclusions)
	p.RiskItems = blank(p.RiskItems)
	s.Payload = p
	return s
}

// decodeStrict rejects unknown and duplicate fields, including nested objects.
func decodeStrict(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var walk func() error
	walk = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		seen := map[string]bool{}
		for d.More() {
			if delim == '{' {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return errors.New("ambiguous fixture")
				}
				seen[key] = true
			}
			if err := walk(); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing fixture data")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// Open each component without following symlinks. os.Root confines subsequent
// writes even if a parent pathname is replaced; exclusive files prevent links
// and existing files from redirecting or being overwritten by shadow evidence.
func shadowRoot(dataDir string) (*os.Root, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("confined shadow directory implementation unavailable")
	}
	if !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) != dataDir {
		return nil, errors.New("explicit absolute dataDir required")
	}
	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	components := append(strings.Split(strings.TrimPrefix(dataDir, "/"), "/"), strings.Split(ShadowPath, "/")...)
	for i, part := range components {
		if part == "" {
			continue
		}
		if i >= len(components)-3 {
			if err = root.Mkdir(part, 0700); err != nil && !os.IsExist(err) {
				root.Close()
				return nil, err
			}
		}
		info, e := root.Lstat(part)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, errors.New("unsafe shadow ancestor")
		}
		// O_NOFOLLOW closes the Lstat/OpenRoot rename race. Pin the opened
		// directory descriptor before turning it into an os.Root.
		dir, e := root.OpenFile(part, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		root.Close()
		if e != nil {
			return nil, e
		}
		next, e := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", dir.Fd()))
		dir.Close()
		if e != nil {
			return nil, e
		}
		root = next
	}
	return root, nil
}

func writeEvidence(root *os.Root, name string, v Report) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	store, err := ledger.NewInRoot(root, time.UTC)
	if err != nil {
		return err
	}
	return store.Append(v.Entry)
}

// CutoverEvidence is a FUTURE review checklist, never a runtime grant. Even a
// complete record does not enable production or bypass the runner's refusal.
type CutoverEvidence struct {
	ManifestFlagOff         bool   `json:"manifestFlagOff"`
	OldEnginePaused         bool   `json:"oldEnginePaused"`
	PauseRecord             string `json:"pauseRecord"`
	SnapshotSHA256          string `json:"snapshotSHA256"`
	OwnershipRecord         string `json:"ownershipRecord"`
	NoQueuedOrRunning       bool   `json:"noQueuedOrRunning"`
	OwnerComparisonApproved bool   `json:"ownerComparisonApproved"`
}

func (e CutoverEvidence) Check() error {
	if !e.OldEnginePaused || strings.TrimSpace(e.PauseRecord) == "" {
		return refusal("old engine authority not recorded paused")
	}
	if !e.ManifestFlagOff || len(e.SnapshotSHA256) != 64 || strings.Trim(e.SnapshotSHA256, "0123456789abcdef") != "" || strings.TrimSpace(e.OwnershipRecord) == "" || !e.NoQueuedOrRunning || !e.OwnerComparisonApproved {
		return refusal("cutover evidence incomplete")
	}
	return nil
}
