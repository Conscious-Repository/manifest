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

func TestAccountingProbePrivateReceipt(t *testing.T) {
	root := t.TempDir()
	vault := t.TempDir()
	evidenceDir := t.TempDir()
	if err := os.Chmod(evidenceDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"system/aion/backlog.md", "system/aion/people.md", "system/aion/heuristics.md", "log/note.md"} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("private fixture"), 0400); err != nil {
			t.Fatal(err)
		}
	}
	config, _ := filepath.Abs("../../docs/excalibur-retirement/aion-successor-config.json")
	models := filepath.Join(evidenceDir, "models.json")
	if err := os.WriteFile(models, []byte(`{"data":[{"id":"deepseek-v4.1-flash","owned_by":"vllm","max_model_len":1048576}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(evidenceDir, "probe.json")
	args := []string{"-config", config, "-fixture-root", root, "-copied-fixture", "-vault-root", vault, "-source", "log/note.md", "-models", models, "-accounting-probe", "-receipt", receipt}
	var out bytes.Buffer
	err := run(args, &out)
	if err == nil || err.Error() != "tokenizer-template-accounting-required" {
		t.Fatal(err)
	}
	b, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(receipt)
	if st.Mode().Perm() != 0600 || !bytes.Equal(b, out.Bytes()) || strings.Contains(string(b), "private fixture") || strings.Contains(string(b), root) || !strings.Contains(string(b), "endpoint-inspection-required") {
		t.Fatal("unsafe receipt")
	}
	for _, extra := range [][]string{{}, {"-canary"}, {"-probe-live-read"}} {
		out.Reset()
		if run(append(append([]string{}, args...), extra...), &out) == nil || out.Len() != 0 {
			t.Fatal("unsafe mode accepted")
		}
	}
	after, _ := os.ReadFile(receipt)
	if !bytes.Equal(b, after) {
		t.Fatal("receipt overwritten")
	}
}
