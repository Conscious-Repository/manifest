package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineCommandIsolation(t *testing.T) {
	if run(nil) == nil {
		t.Fatal("default invocation accepted")
	}
	fixture := t.TempDir()
	dir := filepath.Join(fixture, "system/aion")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"backlog", "people", "heuristics"} {
		if e := os.WriteFile(filepath.Join(dir, name+".md"), []byte("sensitive prose"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	out := filepath.Join(t.TempDir(), "report.json")
	vault := filepath.Join(t.TempDir(), "never-opened")
	args := []string{"-copied-fixture", "-fixture-root", fixture, "-excluded-vault", vault, "-ritual", "aion", "-report", out}
	if e := run(args); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(out)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{fixture, vault, "sensitive prose", "backlog.md"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("leaked private data")
		}
	}
	if !strings.Contains(string(b), "contextPartitionUnavailable") {
		t.Fatal("source readiness invented")
	}
	if e := run(args); e == nil {
		t.Fatal("overwrote report")
	}
	entries, e := os.ReadDir(fixture)
	if e != nil || len(entries) != 1 || entries[0].Name() != "system" {
		t.Fatal("fixture write")
	}
	for _, name := range []string{"backlog", "people", "heuristics"} {
		b, e := os.ReadFile(filepath.Join(dir, name+".md"))
		if e != nil || string(b) != "sensitive prose" {
			t.Fatal("fixture mutated")
		}
	}
	if _, e := os.Stat(vault); !os.IsNotExist(e) {
		t.Fatal("vault side effect")
	}
	// No model executable can be launched by this diagnostic.
	t.Setenv("PATH", "")
	args[len(args)-1] = filepath.Join(t.TempDir(), "second.json")
	if e := run(args); e != nil {
		t.Fatal(e)
	}
}
