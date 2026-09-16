package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitModesAndNoWrite(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"system/aion/backlog.md", "system/aion/people.md", "system/aion/heuristics.md", "log/note.md"} {
		p := filepath.Join(root, name)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte("private fixture"), 0400); e != nil {
			t.Fatal(e)
		}
	}
	config, err := filepath.Abs("../../docs/excalibur-retirement/aion-successor-config.json")
	if err != nil {
		t.Fatal(err)
	}
	base := []string{"-config", config, "-source", "log/note.md"}
	for _, mode := range [][]string{
		{"-fixture-root", root, "-copied-fixture", "-vault-root", "/excluded/vault"},
		{"-live-read", "-no-write", "-vault-root", root}, // Synthetic live mode only.
	} {
		var out bytes.Buffer
		args := append(append([]string{}, base...), mode...)
		err := run(args, &out)
		if err == nil || err.Error() != "provider-capacity-unverified" || !strings.Contains(out.String(), `"invocationPerformed":false`) || strings.Contains(out.String(), "private fixture") {
			t.Fatal(out.String(), err)
		}
	}
	for _, args := range [][]string{
		{}, {"-live-read", "-vault-root", root},
		{"-fixture-root", root, "-vault-root", root, "-copied-fixture"},
		{"-fixture-root", root, "-vault-root", "/excluded/vault"},
		{"-ritual", "ooda-email"}, {"-fallback", "claude"},
	} {
		var out bytes.Buffer
		if run(args, &out) == nil || out.Len() != 0 {
			t.Fatal("unsafe flags accepted")
		}
	}
}
