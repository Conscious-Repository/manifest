// Package extractorcompare retains offline evidence of comparison refusal.
// It deliberately cannot certify parity: native extractor artifacts do not bind
// a complete dependency, category, reference and rendered write-set manifest.
package extractorcompare

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const Unrun = "comparison-unrun"

var ErrRefused = errors.New("comparison refused; semantic review and complete bound structural evidence required")
var errInput = errors.New("comparison input refused; require private opaque staging, explicit hashes and regular bounded artifacts")

// Request names only explicitly staged artifacts; there is no discovery or live
// store access. Each digest selects <side>-<digest>.artifact in Directory.
type Request struct {
	Directory string
	Vault     string
	Ritual    string
	Legacy    []string
	Successor []string
}
type Evidence struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Report struct {
	Version                       int        `json:"version"`
	Duty                          string     `json:"duty"`
	Status                        string     `json:"status"`
	Readiness                     string     `json:"comparisonReadiness"`
	InputHashesVerified           bool       `json:"inputHashesVerified"`
	StructuralComparisonPerformed bool       `json:"structuralComparisonPerformed"`
	SemanticValidationPerformed   bool       `json:"semanticValidationPerformed"`
	Migrated                      bool       `json:"migrated"`
	Replay                        bool       `json:"replay"`
	Legacy                        []Evidence `json:"legacy"`
	Successor                     []Evidence `json:"successor"`
	MissingEvidence               []string   `json:"missingEvidence"`
}

func hashOK(s string) bool {
	return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == ""
}
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// Check writes only a new report.json in the caller-prepared staging directory.
// Success means a durable refusal report, and still returns ErrRefused. All
// earlier errors are fixed messages: private file names or contents never escape.
// This command intentionally has no successful comparison branch.
func Check(q Request) (Report, error) {
	r := Report{Version: 1, Status: "refused", Readiness: Unrun}
	if q.Ritual != "aion" && q.Ritual != "real-estate" && q.Ritual != "ooda-email" {
		return r, errInput
	}
	r.Duty = "extractor/" + q.Ritual
	// An opaque staging namespace is necessary to retain exact evidence paths
	// without leaking names/addresses from native artifact filenames. No heuristic
	// PII scanner is treated as a proof of redaction.
	prefix := "/var/tmp/extractor-comparison-"
	if !strings.HasPrefix(q.Directory, prefix) || !hashOK(strings.TrimPrefix(q.Directory, prefix)) || !filepath.IsAbs(q.Vault) {
		return r, errInput
	}
	vault, err := filepath.EvalSymlinks(q.Vault)
	if err != nil {
		return r, errInput
	}
	actual, err := filepath.EvalSymlinks(q.Directory)
	if err != nil || actual != q.Directory || within(vault, actual) || within(actual, vault) {
		return r, errInput
	}
	before, err := os.Lstat(q.Directory)
	if err != nil || !before.IsDir() {
		return r, errInput
	}
	root, err := os.OpenRoot(q.Directory)
	if err != nil {
		return r, errInput
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil || info.Mode().Perm() != 0700 || !os.SameFile(before, info) {
		return r, errInput
	}
	readSet := func(side string, hashes []string) ([]Evidence, error) {
		if len(hashes) == 0 || len(hashes) > 64 {
			return nil, errInput
		}
		refs := make([]Evidence, 0, len(hashes))
		seen := map[string]bool{}
		for _, digest := range hashes {
			if !hashOK(digest) || seen[digest] {
				return nil, errInput
			}
			seen[digest] = true
			name := side + "-" + digest + ".artifact"
			f, e := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
			if e != nil {
				return nil, errInput
			}
			st, e := f.Stat()
			if e != nil || !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > 8<<20 {
				f.Close()
				return nil, errInput
			}
			h := sha256.New()
			n, e := io.Copy(h, io.LimitReader(f, (8<<20)+1))
			ce := f.Close()
			if e != nil || ce != nil || n > 8<<20 || hex.EncodeToString(h.Sum(nil)) != digest {
				return nil, errInput
			}
			refs = append(refs, Evidence{Path: filepath.Join(q.Directory, name), SHA256: digest})
		}
		return refs, nil
	}
	r.Legacy, err = readSet("legacy", q.Legacy)
	if err != nil {
		return r, err
	}
	r.Successor, err = readSet("successor", q.Successor)
	if err != nil {
		return r, err
	}
	r.InputHashesVerified = true
	r.MissingEvidence = []string{
		"complete run-to-output membership and explicit zero-output receipt",
		"source store identity and immutable source bytes bound to each output",
		"explicit replay disposition bound to both runs",
		"category namespace revision and complete reference identities including absent targets",
		"proposed note metadata and complete purely rendered write set with capabilities",
		"versioned adapters validating supported proposal types and all above bindings",
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return r, errInput
	}
	raw = append(raw, '\n')
	// O_EXCL prevents overwrites, including existing symlinks and prior reports.
	f, err := root.OpenFile("report.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return r, errors.New("comparison report destination refused")
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil || ce != nil {
		root.Remove("report.json")
		return r, errors.New("comparison report persistence failed")
	}
	d, err := root.Open(".")
	if err != nil {
		return r, errors.New("comparison report persistence uncertain")
	}
	err = d.Sync()
	ce = d.Close()
	if err != nil || ce != nil {
		return r, errors.New("comparison report persistence uncertain")
	}
	return r, ErrRefused
}
