package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCodexExactTranscriptDiscovery(t *testing.T) {
	const wanted = "01a084b2-78a3-73e0-82a1-150d710697a7"
	const other = "01a084b0-317d-7e10-b2e3-819b87c3e9c5"
	root := t.TempDir()
	cwd := t.TempDir()
	c := &termCfg{codexSessions: root, defaultWd: cwd}
	write := func(name, id, dir string) string {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{"id": id, "cwd": dir}})
		if err := os.WriteFile(p, append(b, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("2026/09/09/rollout-newest-"+other+".jsonl", other, cwd)
	row := termSession{Kind: "codex", ResumeID: wanted, Cwd: cwd}
	if p := c.transcriptPath(row); p != "" {
		t.Fatalf("missing identity selected %s", p)
	}
	wrong := write("2026/09/08/rollout-wrong-"+wanted+".jsonl", other, cwd)
	if p := c.transcriptPath(row); p != "" {
		t.Fatalf("wrong metadata selected %s", p)
	}
	_ = os.Remove(wrong)
	exact := write("2026/09/08/rollout-exact-"+wanted+".jsonl", wanted, cwd)
	if p := c.transcriptPath(row); p != exact {
		t.Fatalf("got %s want %s", p, exact)
	}
	row.Cwd = t.TempDir()
	if p := c.transcriptPath(row); p != "" {
		t.Fatalf("wrong cwd selected %s", p)
	}
	row.Cwd = cwd
	row.ResumeID = ""
	if p := c.transcriptPath(row); p != "" {
		t.Fatalf("unknown identity guessed %s", p)
	}
	row.ResumeID = wanted
	row.Device = "remote"
	if p := c.transcriptPath(row); p != "" {
		t.Fatalf("remote read local %s", p)
	}
	row.Device = ""
	write("2026/09/09/rollout-copy-"+wanted+".jsonl", wanted, cwd)
	if p := c.transcriptPath(row); p != "" {
		t.Fatalf("ambiguous identity selected %s", p)
	}
}

func TestCodexRolloutRejectsPartialAndConflictingIdentity(t *testing.T) {
	for _, data := range []string{`{"type":"session_meta"`, `{"type":"session_meta","payload":{"id":"abcdef12","session_id":"abcdef13","cwd":"/tmp"}}`, `{"type":"session_meta","payload":{"id":"abcdef12","cwd":"relative"}}`} {
		p := filepath.Join(t.TempDir(), "rollout.jsonl")
		_ = os.WriteFile(p, []byte(data), 0600)
		if id, _ := codexRolloutIdentity(p); id != "" {
			t.Fatalf("accepted %s", data)
		}
	}
}

func TestCodexOpenFileIdentity(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	id := "01a084b2-78a3-73e0-82a1-150d710697a7"
	path := filepath.Join(root, "rollout-probe-"+id+".jsonl")
	raw, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{"id": id, "cwd": cwd}})
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	// A real open descriptor proves exact-file association, not matching names.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fd := fmt.Sprintf("/proc/self/fd/%d", f.Fd())
	if _, err := os.Stat(fd); err != nil {
		t.Skip("requires Linux /proc")
	}
	if got := codexOpenRolloutIdentity(root, path, fd, cwd); got != id {
		t.Fatalf("got %q", got)
	}
	if got := codexOpenRolloutIdentity(t.TempDir(), path, fd, cwd); got != "" {
		t.Fatal("accepted outside root")
	}
	if got := codexOpenRolloutIdentity(root, path, fd, t.TempDir()); got != "" {
		t.Fatal("accepted wrong cwd")
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if got := codexOpenRolloutIdentity(root, path, fd, cwd); got != "" {
		t.Fatal("accepted replaced path for old descriptor")
	}
}
