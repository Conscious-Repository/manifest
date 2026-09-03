package server

import (
	"strings"
	"testing"
)

// A kept (☕ caffeinated) remote session wraps its launch in a tmux ON THE
// TARGET BOX — create-or-attach under the same manifest_<id> name, so an ssh
// drop or metis restart reattaches instead of killing the work.
func TestRemoteKeepWrapsInTargetTmux(t *testing.T) {
	se := termSession{ID: "abcd1234", Kind: "claude", Device: "lab-apps", Cwd: "~/src", Keep: true}
	inner := remoteInner(se)
	for _, want := range []string{
		"exec tmux",
		"new-session -A -s manifest_abcd1234",
		"history-limit 10000",
		"set-clipboard on",
		"command -v claude", // tool guard rides inside the wrap
	} {
		if !strings.Contains(inner, want) {
			t.Fatalf("kept remote inner missing %q:\n%s", want, inner)
		}
	}
	// unkept: no tmux on the remote side
	se.Keep = false
	if inner := remoteInner(se); strings.Contains(inner, "tmux") {
		t.Fatalf("unkept remote inner must not touch tmux:\n%s", inner)
	}
}

// The tool guard drops to a visible shell when claude/codex isn't installed;
// plain shells launch bare.
func TestExecLaunchToolGuard(t *testing.T) {
	c := termSession{Kind: "claude"}
	if s := c.execLaunch(); !strings.Contains(s, "command -v claude") || !strings.Contains(s, "exec bash -l") {
		t.Fatalf("claude launch lacks the tool guard: %s", s)
	}
	sh := termSession{Kind: "shell"}
	if s := sh.execLaunch(); s != "exec bash -l" {
		t.Fatalf("shell launch = %q", s)
	}
}

// sshArgs keeps -tt FIRST (one-shot callers strip it via args[1:]) and
// carries the keepalive options that let a dropped path die fast.
func TestSSHArgsKeepalive(t *testing.T) {
	c := &devCfg{knownHosts: "/tmp/kh"}
	args := c.sshArgs(TermDevice{Name: "x", Host: "x.ts", User: "u"})
	if args[0] != "-tt" {
		t.Fatalf("-tt must stay first: %v", args)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"ServerAliveInterval=15", "ServerAliveCountMax=3"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("sshArgs missing %q: %v", want, args)
		}
	}
}

// autoName applies only to minted placeholder names and refuses junk titles.
func TestCleanAutoName(t *testing.T) {
	if n := cleanAutoName("✳ ✶ fix the parser  "); n != "fix the parser" {
		t.Fatalf("spinner strip: %q", n)
	}
	for _, junk := range []string{"claude", "Claude Code", "bash", "  ", "codex"} {
		if n := cleanAutoName(junk); n != "" {
			t.Fatalf("junk title %q accepted as %q", junk, n)
		}
	}
	if !termPlaceholderRe.MatchString("cc3") || termPlaceholderRe.MatchString("my session") {
		t.Fatal("placeholder regex wrong")
	}
}
