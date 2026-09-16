package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIRefusesDefaultsAndRedacts(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "private-fixture.json")
	if err := os.WriteFile(p, []byte(`{"private-prose":"sensitive"}`), 0400); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"-fixture", p}, {"-copied-fixture", "-fixture", p}, {"-unknown-private-flag"}, {"-fixture", "/missing/private-name", "-copied-fixture"}} {
		var out bytes.Buffer
		if run(args, &out) != 1 {
			t.Fatal("accepted invalid fixture")
		}
		for _, s := range []string{"private", "sensitive", p} {
			if strings.Contains(out.String(), s) {
				t.Fatal("leaked input")
			}
		}
	}
	entries, _ := os.ReadDir(dir)
	raw, _ := os.ReadFile(p)
	if len(entries) != 1 || string(raw) != `{"private-prose":"sensitive"}` {
		t.Fatal("wrote fixture")
	}
}
