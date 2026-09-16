package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/approvals"
)

type report struct {
	Version   int                       `json:"version"`
	Source    string                    `json:"source"`
	Ready     bool                      `json:"ready"`
	Ownership string                    `json:"ownership"`
	StateHash string                    `json:"stateHash,omitempty"`
	IndexHash string                    `json:"indexHash,omitempty"`
	Inventory approvals.InventoryReport `json:"inventory"`
	Blockers  []string                  `json:"blockers"`
}

func reconciliationReport(o options, out io.Writer) error {
	if o.Stage || o.Apply {
		return fmt.Errorf("report cannot be combined with stage or apply")
	}
	r := report{Version: 1, Source: o.Source, Ownership: "excalibur", Blockers: []string{}}
	add := func(s string) {
		for _, v := range r.Blockers {
			if v == s {
				return
			}
		}
		r.Blockers = append(r.Blockers, s)
	}
	r.Inventory = approvals.InspectConnectorInventory(filepath.Join(o.Root, "artifacts"))
	if len(r.Inventory.Issues) > 0 {
		add("approval-history-unresolved")
	}
	statePath := o.EmailState
	if o.Source != "email" {
		statePath = filepath.Join(o.Root, "vessel", "state", o.Source, "watermark")
	}
	state, err := os.ReadFile(statePath)
	if err != nil {
		add("state-invalid")
	} else {
		r.StateHash = approvals.EvidenceHash(string(state))
		if o.Source != "email" {
			if _, err := time.Parse(time.RFC3339, strings.TrimSpace(string(state))); err != nil {
				add("state-invalid")
			}
		} else if !json.Valid(state) {
			add("state-invalid")
		}
	}
	if o.ExpectState != "" && o.ExpectState != r.StateHash || o.ExpectApprovals != "" && o.ExpectApprovals != r.Inventory.Hash {
		add("state-drift")
	}
	var index []byte
	if o.Source != "email" {
		index, err = os.ReadFile(o.Index)
		if err != nil {
			add("index-invalid")
		} else {
			r.IndexHash = approvals.EvidenceHash(string(index))
		}
		if err := inspectIndex(o.Index, o.Source); err != nil {
			add("index-wal-invalid")
		}
	}
	// Keep the strict reconciliation gate as an independent, fail-closed check.
	// Errors are never printed: future parser/driver errors may contain input.
	check := o
	check.Report = false
	check.Stage = false
	check.Apply = false
	var strict bytes.Buffer
	if err := run(check, &strict); err != nil {
		add("strict-reconciliation-unresolved")
	} else {
		var result struct {
			Uncertain int `json:"uncertain"`
		}
		if json.Unmarshal(strict.Bytes(), &result) != nil || result.Uncertain > 0 {
			add("historical-outcome-unresolved")
		}
	}
	again := approvals.InspectConnectorInventory(filepath.Join(o.Root, "artifacts"))
	a, _ := json.Marshal(r.Inventory)
	b, _ := json.Marshal(again)
	latest, e := os.ReadFile(statePath)
	if !bytes.Equal(a, b) || e != nil || !bytes.Equal(state, latest) {
		add("state-drift")
	}
	if o.Source != "email" {
		latest, e := os.ReadFile(o.Index)
		if e != nil || !bytes.Equal(index, latest) {
			add("state-drift")
		}
		if inspectIndex(o.Index, o.Source) != nil {
			add("index-wal-invalid")
		}
	}
	// Even a clean observation is not evidence of dispatch exclusion, account
	// binding, or live continuity. This diagnostic never authorizes activation.
	add("activation-evidence-required")
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func inspectIndex(path, source string) error {
	for _, suffix := range []string{"-wal", "-journal"} {
		if _, err := os.Lstat(path + suffix); !os.IsNotExist(err) {
			return fmt.Errorf("index sidecar")
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("index unavailable")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro&immutable=1"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return fmt.Errorf("index invalid")
	}
	column := "granola_id"
	if source == "pocket" {
		column = "pocket_id"
	}
	rows, err := db.Query("SELECT " + column + ", path FROM notes WHERE " + column + " IS NOT NULL AND " + column + " != ''")
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var id, path string
		if rows.Scan(&id, &path) != nil || id == "" || path == "" || seen[id] {
			return fmt.Errorf("index identity invalid")
		}
		seen[id] = true
	}
	return rows.Err()
}
