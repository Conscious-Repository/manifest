package server

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// SESSION LIFETIME. A tmux server manifest starts inherits manifest's cgroup,
// and the unit's default KillMode (control-group) means a restart — every
// autodeploy push — SIGTERMs it and every Claude Code session inside. The
// spawn therefore goes through a transient systemd user scope. These pin the
// seam rather than systemd itself: the fallback must stay exact, because it is
// what runs on a dev Mac and in every other test in this package.

func TestTmuxSpawnFallsBackToAPlainExecWhenInjected(t *testing.T) {
	f := &fakeTmux{name: tmuxName("abc123ff")}
	c := &termCfg{run: f.run, tmuxTmp: t.TempDir(), regPath: t.TempDir() + "/reg.json"}
	s := &Server{terminal: c}
	se := termSession{ID: "abc123ff", Kind: "claude", ResumeID: "9f3c2e11-0000-4000-8000-abcdefabcdef", Started: true}

	if err := s.spawnTermTmux(se); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("one tmux call: %+v", f.calls)
	}
	got := strings.Join(f.calls[0], " ")
	// the injected runner must see TMUX'S OWN argv — no systemd-run wrapper,
	// no scope flags, in the same order the attach path uses
	if strings.Contains(got, "systemd-run") || strings.Contains(got, "--scope") {
		t.Fatalf("the injected runner was handed a wrapper: %q", got)
	}
	for _, want := range []string{"default-terminal", "new-session", "-d", "-s", tmuxName("abc123ff"),
		"claude --resume 9f3c2e11-0000-4000-8000-abcdefabcdef"} {
		if !strings.Contains(got, want) {
			t.Fatalf("spawn argv lost %q: %q", want, got)
		}
	}
}

// The scope probe must never fire on a machine that cannot serve it — a dev
// Mac has no systemd, and a probe that shelled out anyway would log a failure
// on every spawn.
func TestTmuxScopeIsLinuxOnly(t *testing.T) {
	if runtime.GOOS != "linux" && tmuxScopeAvailable() {
		t.Fatal("a scope was claimed on a machine with no systemd")
	}
}

func TestXDGRuntimeDirDerivesFromTheUID(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	want := "/run/user/" + strconv.Itoa(os.Getuid())
	if got := xdgRuntimeDir(); got != want {
		t.Fatalf("derived %q, want %q", got, want)
	}
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/4242")
	if got := xdgRuntimeDir(); got != "/run/user/4242" {
		t.Fatalf("an explicit runtime dir must win: %q", got)
	}
}

// THE TRUST DIALOG. Verified on metis: a Claude Code session opening a folder
// it has not seen draws
//
//	 Security guide
//	 ❯ No, exit
//	   Yes, I trust this folder
//	 Enter to confirm · Esc to cancel
//
// The old detector matched `❯` by prefix and called that a prompt, so the
// relaunch typed the owner's message into a menu and pressed Enter on "No,
// exit" — the CLI quit, the tmux died, the message was lost.
func TestPromptDetectorIsNotFooledByAMenu(t *testing.T) {
	trust := []string{
		"  Security guide",
		" ❯ No, exit",
		"   Yes, I trust this folder",
		" Enter to confirm · Esc to cancel",
	}
	if termPromptShowing(trust) {
		t.Fatal("a menu row is not an input line — this is the bug that killed sessions")
	}
	why := termBlockingDialog(trust)
	if why == "" {
		t.Fatal("a dialog must be named, so the send can refuse in words")
	}
	if !strings.Contains(why, "TERMINAL") {
		t.Fatalf("the refusal must say where to answer it: %q", why)
	}
}

func TestPromptDetectorFindsARealInputLine(t *testing.T) {
	ready := []string{
		"● Tests and build pass.",
		"────────────────────────────",
		"❯ ",
		"────────────────────────────",
		"  ⏵⏵ auto mode on (shift+tab to cycle)",
	}
	if !termPromptShowing(ready) {
		t.Fatal("an empty ❯ inside the input box IS the prompt")
	}
	if termBlockingDialog(ready) != "" {
		t.Fatal("a ready pane is not a dialog")
	}
	// codex and a dropped shell
	if !termPromptShowing([]string{"› "}) {
		t.Fatal("codex's marker")
	}
	if !termPromptShowing([]string{"benjamin@metis:~/src/manifest$ "}) {
		t.Fatal("a shell prompt keeps the prefix rule")
	}
	// a claude turn still running draws no input line
	if termPromptShowing([]string{"● Running 3 shell commands", "  ⎿  go test ./..."}) {
		t.Fatal("mid-turn output is not a prompt")
	}
}
