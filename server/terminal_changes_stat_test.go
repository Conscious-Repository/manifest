package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The +N −M chip's numbers: tracked lines added and removed against HEAD
// (staged and unstaged alike), files changed, binary files counted without
// line numbers, a rename counted once, and untracked files by count.
func TestWorkingChangesStat(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("a.txt", "one\ntwo\nthree\n")
	write("old name.txt", "keep\nthis\nfile\nlong\nenough\n")
	write("logo.bin", "\x00\x01\x02")
	git("add", ".")
	git("commit", "-qm", "base")
	write("a.txt", "one\nTWO\nthree\nfour\n") // +2 −1
	git("mv", "old name.txt", "new name.txt")
	write("new name.txt", "keep\nthis\nfile\nlong\nenough\nplus\n") // +1
	write("logo.bin", "\x00\x01\x03\x04")                           // binary
	write("staged.txt", "s\n")
	git("add", "staged.txt") // +1, staged only
	write("loose.txt", "u\n")
	st, err := workingChangesStat(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Added != 4 || st.Removed != 1 || st.Files != 4 || st.Binary != 1 || st.Untracked != 1 || len(st.Head) != 40 {
		t.Fatalf("stat = %+v", st)
	}
	clean := t.TempDir()
	if _, err := workingChangesStat(context.Background(), clean); err == nil {
		t.Fatal("a folder that is not a repository must say the review is unavailable")
	}
}
