package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rig: a bare "remote" + two clones (machine A, machine B), each with a syncer.
func rig(t *testing.T) (a, b *syncer) {
	t.Helper()
	dir := t.TempDir()
	bare := filepath.Join(dir, "remote.git")
	run(t, dir, "git", "init", "-q", "--bare", "-b", "main", bare)
	mk := func(name string) *syncer {
		clone := filepath.Join(dir, name)
		run(t, dir, "git", "clone", "-q", bare, clone)
		run(t, clone, "git", "config", "user.name", name)
		run(t, clone, "git", "config", "user.email", name+"@test")
		return &syncer{spec: rootSpec{Name: name, Path: clone},
			stateDir: filepath.Join(dir, "state-"+name), host: name,
			debounce: time.Second, interval: time.Second, kick: make(chan struct{}, 1)}
	}
	a = mk("a")
	// seed shared history by hand (an empty bare has no upstream ref yet —
	// the real deployments always start from an existing remote branch)
	os.WriteFile(filepath.Join(a.spec.Path, "note.md"), []byte("hello\n"), 0o644)
	run(t, a.spec.Path, "git", "add", "-A")
	run(t, a.spec.Path, "git", "commit", "-q", "-m", "seed")
	run(t, a.spec.Path, "git", "push", "-q", "-u", "origin", "main")
	b = mk("b")
	for _, s := range []*syncer{a, b} {
		if err := os.MkdirAll(s.stateDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return a, b
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, _ := os.ReadFile(path)
	return string(b)
}

// The happy path: A writes → cycle → B cycles → B sees it; then the reverse.
func TestSyncConverges(t *testing.T) {
	a, b := rig(t)
	os.WriteFile(filepath.Join(a.spec.Path, "note.md"), []byte("from a\n"), 0o644)
	a.cycle()
	b.cycle()
	if got := read(t, filepath.Join(b.spec.Path, "note.md")); got != "from a\n" {
		t.Fatalf("b did not converge: %q", got)
	}
	os.WriteFile(filepath.Join(b.spec.Path, "reply.md"), []byte("from b\n"), 0o644)
	b.cycle()
	a.cycle()
	if got := read(t, filepath.Join(a.spec.Path, "reply.md")); got != "from b\n" {
		t.Fatalf("a did not converge: %q", got)
	}
}

// Divergent edits to DIFFERENT files rebase cleanly — no park.
func TestSyncRebasesDisjointEdits(t *testing.T) {
	a, b := rig(t)
	os.WriteFile(filepath.Join(a.spec.Path, "a-file.md"), []byte("a\n"), 0o644)
	a.cycle()
	os.WriteFile(filepath.Join(b.spec.Path, "b-file.md"), []byte("b\n"), 0o644)
	b.cycle() // commits b, rebases over a's push, pushes
	if b.parked {
		t.Fatal("disjoint edits must not park")
	}
	a.cycle()
	if read(t, filepath.Join(a.spec.Path, "b-file.md")) != "b\n" {
		t.Fatal("a missing b's file after converge")
	}
}

// A true content conflict: STOP, PARK, MARK — tree restored, state file
// written, no further cycles do anything; resolving by hand resumes.
func TestConflictParksAndResumes(t *testing.T) {
	a, b := rig(t)
	// both edit the SAME line of the same file
	os.WriteFile(filepath.Join(a.spec.Path, "note.md"), []byte("A version\n"), 0o644)
	a.cycle()
	os.WriteFile(filepath.Join(b.spec.Path, "note.md"), []byte("B version\n"), 0o644)
	b.cycle() // commit B → pull --rebase hits the conflict → park

	if !b.parked {
		t.Fatal("conflicting rebase must park")
	}
	st := read(t, b.conflictFile())
	if !strings.Contains(st, "note.md") {
		t.Fatalf("conflict state missing path: %s", st)
	}
	// tree restored: B's version intact, no rebase in progress
	if got := read(t, filepath.Join(b.spec.Path, "note.md")); got != "B version\n" {
		t.Fatalf("tree not restored after abort: %q", got)
	}
	if b.rebaseInProgress() {
		t.Fatal("rebase still in progress after park")
	}
	// parked cycles are no-ops (still parked, no state change)
	b.cycle()
	if !b.parked {
		t.Fatal("parked root must stay parked until resolved")
	}

	// the human resolves REBASE-STYLE (the doctrine's flow: pull --rebase →
	// fix the file → add → rebase --continue). Mid-rebase, cycles stay parked.
	cmd := exec.Command("git", "pull", "--rebase", "-q")
	cmd.Dir = b.spec.Path
	_ = cmd.Run() // conflicts — rebase left in progress, on purpose
	if !b.rebaseInProgress() {
		t.Fatal("expected an in-progress rebase for the human to finish")
	}
	b.cycle()
	if !b.parked {
		t.Fatal("mid-resolution (rebase in progress) must stay parked")
	}
	os.WriteFile(filepath.Join(b.spec.Path, "note.md"), []byte("B version\n"), 0o644)
	run(t, b.spec.Path, "git", "add", "note.md")
	cont := exec.Command("git", "-c", "core.editor=true", "rebase", "--continue")
	cont.Dir = b.spec.Path
	if out, err := cont.CombinedOutput(); err != nil {
		t.Fatalf("rebase --continue: %v\n%s", err, out)
	}

	b.cycle() // resume check passes → conflict cleared → converge + push
	if b.parked {
		t.Fatal("resolved root must resume")
	}
	if _, err := os.Stat(b.conflictFile()); !os.IsNotExist(err) {
		t.Fatal("conflict file must be cleared on resume")
	}
	a.cycle()
	if got := read(t, filepath.Join(a.spec.Path, "note.md")); got != "B version\n" {
		t.Fatalf("post-resolution content did not converge to a: %q", got)
	}
}
