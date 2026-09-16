package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportDeterministicRedactedReadOnlyAndBlocked(t *testing.T) {
	o := fixture(t)
	o.Report = true
	o.ExpectState = strings.Repeat("0", 64)
	before, _ := os.ReadFile(o.EmailState)
	var a, b bytes.Buffer
	if err := run(o, &a); err != nil {
		t.Fatal(err)
	}
	if err := run(o, &b); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatal("nondeterministic")
	}
	var r report
	if err := json.Unmarshal(a.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Ready || !strings.Contains(a.String(), "state-drift") {
		t.Fatal(a.String())
	}
	if strings.Contains(a.String(), o.Account) || strings.Contains(a.String(), o.Root) {
		t.Fatal("leaked input")
	}
	after, _ := os.ReadFile(o.EmailState)
	if !bytes.Equal(before, after) {
		t.Fatal("changed source")
	}
	entries, _ := os.ReadDir(o.DataDir)
	if len(entries) != 0 {
		t.Fatal("wrote state")
	}
	for _, apply := range []bool{false, true} {
		o.Stage = !apply
		o.Apply = apply
		if run(o, &b) == nil {
			t.Fatal("accepted mutating option")
		}
	}
}

func TestReportIndexFailureSurvivesApprovalFailure(t *testing.T) {
	o := fixture(t)
	o.Report = true
	o.Source = "granola"
	o.Index = filepath.Join(t.TempDir(), "frozen.sqlite")
	os.WriteFile(o.Index, []byte("corrupt"), 0600)
	os.WriteFile(o.Index+"-wal", []byte("private-token"), 0600)
	os.Remove(filepath.Join(o.Root, "artifacts", "approvals", "pending"))
	var out bytes.Buffer
	if err := run(o, &out); err != nil {
		t.Fatal(err)
	}
	for _, class := range []string{"inventory-unavailable", "index-wal-invalid", "state-invalid", "strict-reconciliation-unresolved"} {
		if !strings.Contains(out.String(), class) {
			t.Fatal("missing", class)
		}
	}
	if strings.Contains(out.String(), "private-token") {
		t.Fatal("leaked")
	}
	entries, _ := os.ReadDir(filepath.Dir(o.Index))
	if len(entries) != 2 {
		t.Fatal("created sidecars")
	}
	os.Remove(o.Index + "-wal")
	if inspectIndex(o.Index, o.Source) == nil {
		t.Fatal("corrupt index accepted")
	}
}
