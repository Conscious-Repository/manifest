package approvals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

const ReconciledUncertain = "legacy-reconciled-uncertain"
const ReconciledRejected = "legacy-reconciled-rejected"

// OwnerReconciliation records a decision about history, never permission to replay.
// Artifacts retain every original decision and hash; nothing is deduplicated.
type OwnerReconciliation struct {
	Version       int                 `json:"version"`
	Source        string              `json:"source"`
	SourceID      string              `json:"sourceId"`
	Disposition   string              `json:"disposition"`
	Replay        bool                `json:"replay"`
	Owner         string              `json:"owner"`
	Date          string              `json:"date"`
	Authorization string              `json:"authorization"`
	Artifacts     []ConnectorApproval `json:"artifacts"`
}

func ownerCase(source string) (OwnerReconciliation, error) {
	r := OwnerReconciliation{Version: 1, Source: source, Owner: "Benjamin", Date: "2026-09-16", Authorization: "bring them inline with new ones, do what common sense suggests and yes, proceed; preserve history, replay=false"}
	switch source {
	case "granola":
		r.SourceID = "not_vJw8dIUwVUiWDT"
		r.Disposition = ReconciledUncertain
	case "pocket":
		r.SourceID = "72886f85-9810-488e-a70a-b32ef2fd9dd6"
		r.Disposition = ReconciledUncertain
	case "email":
		r.SourceID = "19fdd282744d15a0"
		r.Disposition = ReconciledRejected
	default:
		return r, fmt.Errorf("no owner authorization for source")
	}
	return r, nil
}

// PreviewOwnerReconciliation accepts only the three enumerated owner decisions.
// The narrow duplicate exception is private and the complete group is validated
// before any inventory can escape. Global and ordinary scoped readers stay strict.
func PreviewOwnerReconciliation(artifacts, source string) (OwnerReconciliation, ConnectorInventory, error) {
	r, err := ownerCase(source)
	if err != nil {
		return r, ConnectorInventory{}, err
	}
	scope := source
	if scope == "email" {
		scope = "gmail-thread"
	}
	inv, err := (&Store{dir: filepath.Join(artifacts, "approvals")}).connectorInventoryWithReconciliation(scope, true)
	if err != nil {
		return r, inv, err
	}
	return ownerReconciliationForInventory(source, inv)
}

func ownerReconciliationForInventory(source string, inv ConnectorInventory) (OwnerReconciliation, ConnectorInventory, error) {
	r, err := ownerCase(source)
	if err != nil {
		return r, inv, err
	}
	scope := source
	if source == "email" {
		scope = "gmail-thread"
	}
	expected := map[string]string{}
	status := "approved"
	switch source {
	case "granola":
		expected["58719e5e1d11"] = "2026-06-25 austin.md"
	case "pocket":
		expected["00fcb06ba967"] = "2026-08-25 raise process and communications.md"
	case "email":
		status = "rejected"
		expected["2a71f54cd7b3"] = "2026-08-07 our new project wi-fly.md"
		expected["d75af2b821e6"] = "2026-08-07 our new project wi-fly 4d15a0.md"
	}
	for _, p := range inv.Items {
		if p.SourceID != r.SourceID {
			continue
		}
		path, ok := expected[p.ID]
		if !ok || p.Source != scope || p.Status != status || p.Type != TypeCreateVaultNote || p.Path != path {
			return r, inv, fmt.Errorf("owner reconciliation evidence identity mismatch")
		}
		r.Artifacts = append(r.Artifacts, p)
		delete(expected, p.ID)
	}
	if len(expected) != 0 {
		return r, inv, fmt.Errorf("owner reconciliation evidence missing")
	}
	return r, inv, nil
}

// ValidateOwnerReconciliation rejects unlisted IDs, changed decisions and evidence.
func ValidateOwnerReconciliation(r OwnerReconciliation, inv ConnectorInventory) error {
	expected, _, err := ownerReconciliationForInventory(r.Source, inv)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(r, expected) {
		return fmt.Errorf("invalid owner reconciliation")
	}
	return nil
}

func OwnerReconciliationBytes(r OwnerReconciliation) []byte {
	b, _ := json.MarshalIndent(r, "", "  ")
	return b
}
func ownerRecordPath(dataDir, source string) string {
	return filepath.Join(dataDir, "connector-handoff", "reconciliation", source+".json")
}

// ReadReconciledConnectorInventory validates durable authorization against fresh
// canonical evidence. Absence retains the old strict behavior. Canonical encoding
// rejects omitted replay, duplicate/unknown JSON fields and trailing data.
func ReadReconciledConnectorInventory(artifacts, source, dataDir string) (ConnectorInventory, *OwnerReconciliation, error) {
	if _, err := ownerCase(source); err != nil {
		return ConnectorInventory{}, nil, err
	}
	if dataDir == "" {
		inv, err := ReadConnectorInventoryForSource(artifacts, source)
		return inv, nil, err
	}
	path := ownerRecordPath(dataDir, source)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		inv, e := ReadConnectorInventoryForSource(artifacts, source)
		return inv, nil, e
	}
	if err != nil || !info.Mode().IsRegular() {
		return ConnectorInventory{}, nil, fmt.Errorf("owner reconciliation unavailable")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ConnectorInventory{}, nil, fmt.Errorf("owner reconciliation unreadable")
	}
	r, inv, err := PreviewOwnerReconciliation(artifacts, source)
	if err != nil {
		return inv, nil, err
	}
	if !bytes.Equal(b, OwnerReconciliationBytes(r)) {
		return inv, nil, fmt.Errorf("owner reconciliation tampering or evidence drift")
	}
	return inv, &r, nil
}

// WriteOwnerReconciliation atomically publishes immutable authorization. The
// expected hash must come from the dry run. Only dataDir is written.
func WriteOwnerReconciliation(dataDir, artifacts, source, expected string) (string, error) {
	if dataDir == "" {
		return "", fmt.Errorf("explicit dataDir required")
	}
	r, _, err := PreviewOwnerReconciliation(artifacts, source)
	if err != nil {
		return "", err
	}
	b := OwnerReconciliationBytes(r)
	hash := EvidenceHash(string(b))
	if expected != hash {
		return "", fmt.Errorf("owner reconciliation preview hash required or evidence drift")
	}
	path := ownerRecordPath(dataDir, source)
	dir := filepath.Dir(path)
	if info, e := os.Lstat(path); e == nil && !info.Mode().IsRegular() {
		return "", fmt.Errorf("owner reconciliation is not a regular file")
	} else if e != nil && !os.IsNotExist(e) {
		return "", fmt.Errorf("owner reconciliation unavailable")
	}
	if old, e := os.ReadFile(path); e == nil {
		if !bytes.Equal(old, b) {
			return "", fmt.Errorf("owner reconciliation already exists with different evidence")
		}
		return hash, nil
	} else if !os.IsNotExist(e) {
		return "", fmt.Errorf("owner reconciliation unavailable")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".owner-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	again, _, err := PreviewOwnerReconciliation(artifacts, source)
	if err != nil || !reflect.DeepEqual(r, again) {
		return "", fmt.Errorf("owner reconciliation evidence changed")
	}
	if err = os.Link(f.Name(), path); err != nil {
		return "", err
	}
	d, err := os.Open(dir)
	if err != nil {
		return "", err
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return "", err
	}
	return hash, nil
}
