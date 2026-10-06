package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// Opt in against an isolated scratch herdr daemon (MANIFEST_HERDR_TEST_SESSION)
// and a scratch folder (MANIFEST_HERDR_TEST_CWD): drives a real Claude Code
// through its folder-trust check, a Bash permission prompt and AskUserQuestion
// using only the chat answer path.
func TestHerdrLiveScreenPrompts(t *testing.T) {
	session := os.Getenv("MANIFEST_HERDR_TEST_SESSION")
	if session == "" {
		t.Skip("requires isolated live herdr 0.9.0 and authenticated Claude")
	}
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	cwd, err := os.MkdirTemp(os.Getenv("MANIFEST_HERDR_TEST_CWD"), "prompt-probe-")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{terminal: &termCfg{defaultWd: cwd}}
	h := &herdrTerminalRuntime{server: s, Host: host, Session: session, Socket: filepath.Join(home, ".config", "herdr", "sessions", session, "herdr.sock")}
	s.terminal.herdr = h
	var u [16]byte
	_, _ = rand.Read(u[:])
	se := termSession{ID: "abcdef654321", Backend: "herdr", Kind: "claude", Cwd: s.terminal.defaultWd, Name: "prompt-probe", Model: "sonnet", Permission: "default",
		ResumeID: fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	id, err := h.Create(ctx, se)
	if err != nil {
		t.Fatal(err)
	}
	se.Runtime = id
	defer func() { _ = h.Close(context.Background(), id) }()

	findPrompt := func(want string, tries int) (*termPrompt, []string) {
		var lines []string
		for i := 0; i < tries; i++ {
			p, screen, err := s.terminalScreenPrompt(ctx, se)
			lines = screen
			if err == nil && p != nil && strings.Contains(strings.ToLower(p.Title), want) {
				return p, nil
			}
			time.Sleep(time.Second)
		}
		return nil, lines
	}
	waitPrompt := func(want string) *termPrompt {
		t.Helper()
		p, lines := findPrompt(want, 120)
		if p == nil {
			t.Fatalf("no prompt %q; screen:\n%s", want, strings.Join(lines, "\n"))
		}
		return p
	}
	choose := func(p *termPrompt, label string) int {
		for _, o := range p.Options {
			if strings.HasPrefix(strings.ToLower(o.Label), strings.ToLower(label)) {
				return o.Index
			}
		}
		t.Fatalf("no option %q in %+v", label, p.Options)
		return -1
	}
	// asked only for a folder not yet trusted (a parent's trust is inherited)
	if p, _ := findPrompt("trust", 20); p != nil {
		if err := s.answerTermPrompt(ctx, se, termPromptAnswer{Revision: p.Revision, Option: choose(p, "Yes, I trust")}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(4 * time.Second)
	if err := h.SendText(ctx, id, "Use the Bash tool to run exactly: echo probe > probe.txt"); err != nil {
		t.Fatal(err)
	}
	p := waitPrompt("proceed")
	// a stale revision is refused and nothing is pressed
	if err := s.answerTermPrompt(ctx, se, termPromptAnswer{Revision: "stale", Option: 0}); err != errPromptChanged {
		t.Fatalf("stale answer: %v", err)
	}
	if err := s.answerTermPrompt(ctx, se, termPromptAnswer{Revision: p.Revision, Option: choose(p, "Yes")}); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		if b, err := os.ReadFile(filepath.Join(se.Cwd, "probe.txt")); err == nil && strings.TrimSpace(string(b)) == "probe" {
			break
		}
		if i > 60 {
			t.Fatal("approved command did not run")
		}
		time.Sleep(time.Second)
	}
	time.Sleep(5 * time.Second)
	if err := h.SendText(ctx, id, "Use AskUserQuestion with two questions: 'Pick a colour?' options red/green, and 'Which pets?' multiSelect options cat/dog/fish. Then reply with only my answers as ANSWERS: <colour>; <pets>"); err != nil {
		t.Fatal(err)
	}
	p = waitPrompt("colour")
	if err := s.answerTermPrompt(ctx, se, termPromptAnswer{Revision: p.Revision, Option: choose(p, "Type something"), Text: "teal"}); err != nil {
		t.Fatal(err)
	}
	p = waitPrompt("pets")
	if !p.Multi {
		t.Fatalf("pets not multi: %+v", p)
	}
	if err := s.answerTermPrompt(ctx, se, termPromptAnswer{Revision: p.Revision, Checked: []int{choose(p, "Cat"), choose(p, "Fish")}}); err != nil {
		t.Fatal(err)
	}
	p = waitPrompt("submit")
	if err := s.answerTermPrompt(ctx, se, termPromptAnswer{Revision: p.Revision, Option: choose(p, "Submit")}); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		lines, _ := h.Screen(ctx, id)
		screen := strings.Join(lines, "\n")
		// the agent's reply, not the instruction that names the format
		if p, _, _ := s.terminalScreenPrompt(ctx, se); p == nil && strings.Contains(strings.ToLower(screen), "answers: teal; cat") && strings.Contains(strings.ToLower(screen), "fish") {
			t.Logf("final screen:\n%s", screen)
			break
		}
		if i > 90 {
			t.Fatalf("answers never arrived:\n%s", screen)
		}
		time.Sleep(time.Second)
	}
}

// The Codex approval chooser, driven the same way (read-only sandbox, so a
// write needs the owner's approval).
func TestHerdrLiveScreenPromptsCodex(t *testing.T) {
	session := os.Getenv("MANIFEST_HERDR_TEST_SESSION")
	if session == "" {
		t.Skip("requires isolated live herdr 0.9.0 and authenticated Codex")
	}
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	cwd, err := os.MkdirTemp(os.Getenv("MANIFEST_HERDR_TEST_CWD"), "prompt-probe-codex-")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{terminal: &termCfg{defaultWd: cwd}}
	h := &herdrTerminalRuntime{server: s, Host: host, Session: session, Socket: filepath.Join(home, ".config", "herdr", "sessions", session, "herdr.sock")}
	s.terminal.herdr = h
	se := termSession{ID: "abcdef654322", Backend: "herdr", Kind: "codex", Cwd: cwd, Name: "prompt-probe-codex", Model: "gpt-6-astra", Permission: "read-only"}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	id, err := h.Create(ctx, se)
	if err != nil {
		t.Fatal(err)
	}
	se.Runtime = id
	defer func() { _ = h.Close(context.Background(), id) }()
	find := func(want string, tries int) (*termPrompt, []string) {
		var lines []string
		for i := 0; i < tries; i++ {
			p, screen, err := s.terminalScreenPrompt(ctx, se)
			lines = screen
			if err == nil && p != nil && strings.Contains(strings.ToLower(p.Title), want) {
				return p, nil
			}
			time.Sleep(time.Second)
		}
		return nil, lines
	}
	if p, _ := find("trust", 20); p != nil {
		if err := s.answerTermPrompt(ctx, se, termPromptAnswer{Revision: p.Revision, Option: 0}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(4 * time.Second)
	}
	if err := h.SendText(ctx, id, "Run this shell command: touch made.txt"); err != nil {
		t.Fatal(err)
	}
	p, lines := find("would you like to run", 120)
	if p == nil {
		t.Fatalf("no approval; screen:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(p.Options[0].Label, "Yes, proceed") {
		t.Fatalf("options %+v", p.Options)
	}
	if err := s.answerTermPrompt(ctx, se, termPromptAnswer{Revision: p.Revision, Option: 0}); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		if _, err := os.Stat(filepath.Join(cwd, "made.txt")); err == nil {
			break
		}
		if i > 90 {
			screen, _ := h.Screen(ctx, id)
			t.Fatalf("approved command did not run:\n%s", strings.Join(screen, "\n"))
		}
		time.Sleep(time.Second)
	}
}

// A short or tiny pane (run the scratch client ~16 rows, or a few cells as a
// collapsed browser panel leaves it): read the chooser at the reading size
// and pick an option Claude scrolled out of view, as the chat card does.
func TestHerdrLiveScreenPromptsShortPane(t *testing.T) {
	session := os.Getenv("MANIFEST_HERDR_TEST_SESSION")
	if session == "" {
		t.Skip("requires isolated live herdr 0.9.0 and authenticated Claude")
	}
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	cwd, err := os.MkdirTemp(os.Getenv("MANIFEST_HERDR_TEST_CWD"), "prompt-short-")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{terminal: &termCfg{defaultWd: cwd}}
	h := &herdrTerminalRuntime{server: s, Host: host, Session: session, Socket: filepath.Join(home, ".config", "herdr", "sessions", session, "herdr.sock")}
	s.terminal.herdr = h
	var u [16]byte
	_, _ = rand.Read(u[:])
	resume := fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
	se := termSession{ID: "abcdef654323", Backend: "herdr", Kind: "claude", Cwd: cwd, Name: "prompt-short", Model: "sonnet", Permission: "default", ResumeID: resume}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	id, err := h.Create(ctx, se)
	if err != nil {
		t.Fatal(err)
	}
	se.Runtime = id
	se.Started = true
	defer func() { _ = h.Close(context.Background(), id) }()
	time.Sleep(6 * time.Second)
	// a phone or collapsed panel that attached and left: the pane keeps its few cells
	// (the browser's own path: a direct attach over a PTY of that size)
	if shrink, err := h.Attach(ctx, id); err == nil {
		if tty, err := pty.StartWithSize(shrink, &pty.Winsize{Cols: 30, Rows: 7}); err == nil {
			go func() { _, _ = io.Copy(io.Discard, tty) }()
			time.Sleep(1500 * time.Millisecond)
			_ = shrink.Process.Kill()
			_ = shrink.Wait()
			_ = tty.Close()
		}
	}
	if err := h.SendText(ctx, id, "Use AskUserQuestion: one question 'Which fruit do you want?' with options apple, pear, plum, kiwi, each with a one-line description. Then reply only PICKED: <fruit>"); err != nil {
		t.Fatal(err)
	}
	var p *termPrompt
	for i := 0; i < 90 && (p == nil || !strings.Contains(p.Title, "fruit")); i++ {
		time.Sleep(time.Second)
		p, _ = s.readablePrompt(ctx, se, true)
	}
	if p == nil {
		lines, _ := h.Screen(ctx, id)
		ob, _ := h.Inspect(ctx, id)
		t.Fatalf("no prompt (agent %s):\n%s", ob.AgentState, strings.Join(lines, "\n"))
	}
	t.Logf("prompt clipped=%v options=%q", p.Clipped, promptLabels(p))
	// (a scratch daemon may restore the size when the attach leaves; the live
	// one keeps it — log which this run exercised)
	small, lines, _ := s.terminalScreenPrompt(ctx, se)
	t.Logf("at the pane's own size: %d lines, readable=%v clipped=%v", len(lines), small != nil, small != nil && small.Clipped)
	full := p
	if p.Clipped {
		if err := s.answerAtReadableSize(ctx, se, p.Revision, func() error {
			var err error
			cur, _, err := s.terminalScreenPrompt(ctx, se)
			if err == nil && cur != nil {
				full, _, err = s.scanTermPrompt(ctx, se, cur)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("scanned options=%q", promptLabels(full))
	target := -1
	for _, o := range full.Options {
		if o.Label == "kiwi" {
			target = o.Index
		}
	}
	if target < 0 {
		t.Fatalf("kiwi not offered: %+v", p.Options)
	}
	a := termPromptAnswer{Revision: p.Revision, Option: target, Label: "kiwi"}
	if err := s.answerAtReadableSize(ctx, se, a.Revision, func() error { return s.answerTermPrompt(ctx, se, a) }); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		lines, _ := h.Screen(ctx, id)
		if strings.Contains(strings.Join(lines, "\n"), "PICKED: kiwi") {
			break
		}
		if i > 60 {
			t.Fatalf("answer never arrived:\n%s", strings.Join(lines, "\n"))
		}
		time.Sleep(time.Second)
	}
}
