package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"manifest/approvals"
)

func ownerReconciliation(o options, out io.Writer) error {
	if o.Report || o.Stage || o.DataDir == "" {
		return fmt.Errorf("owner reconciliation requires dataDir and cannot combine with report or stage")
	}
	artifacts := filepath.Join(o.Root, "artifacts")
	r, _, err := approvals.PreviewOwnerReconciliation(artifacts, o.Source)
	if err != nil {
		return err
	}
	hash := approvals.EvidenceHash(string(approvals.OwnerReconciliationBytes(r)))
	if o.Apply {
		if _, err = approvals.WriteOwnerReconciliation(o.DataDir, artifacts, o.Source, o.ExpectReconciliation); err != nil {
			return err
		}
	}
	return json.NewEncoder(out).Encode(map[string]any{"record": r, "reconciliationHash": hash, "applied": o.Apply, "ownership": "excalibur", "activation": "blocked"})
}
