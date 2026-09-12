package reintake

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"manifest/approvals"
)

// ReserveUpload burns the pilot after read-only upload eligibility checks and
// before writing source artifacts. It is independent of the model latch and
// survives crashes/restarts.
// An owner-reviewed offline reset must preserve both latches and all receipts.
func ReserveUpload(dataDir string) error {
	root, err := productionRoot(dataDir)
	if err != nil {
		return productionRefusal("pilot storage unavailable")
	}
	defer root.Close()
	for _, name := range []string{"invoked", "run.jsonl"} {
		if _, err := root.Lstat(name); !os.IsNotExist(err) {
			return productionRefusal("prior or uncertain invocation")
		}
	}
	if root.Mkdir("upload-reserved", 0700) != nil || syncProductionRoot(root) != nil {
		return productionRefusal("pilot already reserved or uncertain")
	}
	return nil
}

// StageExtract copies only canonical extract.Doc output, never another uploader
// or format conversion. Source remains the original CAS identity in the contract.
func StageExtract(dataDir, text string) (string, error) {
	if len(text) > maxDocumentBytes || !utf8.ValidString(text) || strings.TrimSpace(text) == "" || strings.ContainsRune(text, 0) {
		return "", productionRefusal("extract unavailable or exceeds pilot bound")
	}
	root, err := productionRoot(dataDir)
	if err != nil {
		return "", productionRefusal("staging unavailable")
	}
	defer root.Close()
	if err := root.Mkdir("staging", 0700); err != nil && !os.IsExist(err) {
		return "", productionRefusal("staging unavailable")
	}
	dir, err := root.OpenFile("staging", os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", productionRefusal("staging unavailable")
	}
	defer dir.Close()
	stage, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", dir.Fd()))
	if err != nil {
		return "", productionRefusal("staging unavailable")
	}
	defer stage.Close()
	source := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(text)))
	f, err := stage.OpenFile(source[7:]+".txt", os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return "", productionRefusal("staging already exists or unavailable")
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		return "", productionRefusal("staging persistence failed")
	}
	if f.Sync() != nil || syncProductionRoot(stage) != nil || syncProductionRoot(root) != nil {
		return "", productionRefusal("staging persistence failed")
	}
	return source, nil
}

// CheckCandidateReceipt checks the exact candidate digest against discoverable,
// durable terminal evidence immediately before the existing pending-store call.
func CheckCandidateReceipt(dataDir string, c ProductionContract, p approvals.Proposal, receipt ProductionReceipt) error {
	raw, _ := json.Marshal(p)
	if receipt.State != "verified" || receipt.PageOwnerRequired || receipt.Fallback || receipt.ItemsWritten != 0 || receipt.CandidateSHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) {
		return productionRefusal("candidate receipt mismatch")
	}
	// Reconstruct the fixed envelope through the same parser; no caller-supplied
	// auto/status/ID/extra fields can ride through even if a digest is supplied.
	payload, ok := approvals.ParseReContractPayload(p.Body)
	if !ok {
		return productionRefusal("candidate refused")
	}
	reply, _ := json.Marshal(map[string]any{"type": p.Type, "actor": p.Agent, "source": c.Source, "target": c.Target, "applyPath": p.ApplyPath, "payload": payload})
	expected, err := parseProductionCandidate(c, string(reply))
	expectedRaw, _ := json.Marshal(expected)
	if err != nil || string(expectedRaw) != string(raw) {
		return productionRefusal("candidate refused")
	}
	root, err := productionRoot(dataDir)
	if err != nil {
		return productionRefusal("receipt unavailable")
	}
	defer root.Close()
	f, err := root.OpenFile("run.jsonl", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return productionRefusal("receipt unavailable")
	}
	defer f.Close()
	if !productionReceiptPresent(root, f) {
		return productionRefusal("receipt unavailable")
	}
	info, err := f.Stat()
	if err != nil || info.Size() > 16000 {
		return productionRefusal("receipt unavailable")
	}
	evidence, err := io.ReadAll(io.LimitReader(f, 16001))
	if err != nil || len(evidence) > 16000 {
		return productionRefusal("receipt unavailable")
	}
	lines := strings.Split(strings.TrimSpace(string(evidence)), "\n")
	var initial, final ProductionReceipt
	if len(lines) != 2 || decodeStrict([]byte(lines[0]), &initial) != nil || decodeStrict([]byte(lines[1]), &final) != nil || initial.State != "uncertain" {
		return productionRefusal("receipt uncertain")
	}
	a, _ := json.Marshal(final)
	b, _ := json.Marshal(receipt)
	if string(a) != string(b) {
		return productionRefusal("receipt mismatch")
	}
	return nil
}

// PilotStatus is advisory and read-only. Even missing receipts after reservation
// mean stopped; it never interprets candidate verification as an owner decision.
func PilotStatus(dataDir string) string {
	if dataDir == "" {
		return "unconfigured"
	}
	for _, name := range []string{"upload-reserved", "invoked", "run.jsonl"} {
		_, err := os.Lstat(filepath.Join(dataDir, ProductionPath, name))
		if err == nil {
			return "stopped; owner review/reset required"
		}
		if !os.IsNotExist(err) {
			return "unknown; stop and page owner"
		}
	}
	return "unused; one document maximum"
}
