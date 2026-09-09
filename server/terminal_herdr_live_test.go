package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt in only against an isolated, independently running scratch daemon. This
// invokes the installed CLIs with a harmless brief; it never edits the checkout.
func TestHerdrLiveBoardLaunch(t *testing.T) {
	session := os.Getenv("MANIFEST_HERDR_TEST_SESSION")
	if session == "" {
		t.Skip("requires isolated live herdr 0.9.0 and authenticated Claude/Codex")
	}
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	s := &Server{terminal: &termCfg{defaultWd: os.Getenv("MANIFEST_HERDR_TEST_CWD")}}
	if s.terminal.defaultWd == "" {
		t.Fatal("set MANIFEST_HERDR_TEST_CWD to a trusted scratch checkout")
	}
	h := &herdrTerminalRuntime{server: s, Host: host, Session: session, Socket: filepath.Join(home, ".config", "herdr", "sessions", session, "herdr.sock")}
	for _, kind := range []string{"codex", "claude"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			brief := filepath.Join(dir, "brief.md")
			if err := os.WriteFile(brief, []byte("This is an isolated runtime protocol probe. Reply only BOARD_PROBE_OK. Do not use tools, write results, or change any files. The enclosing shell records your exit."), 0600); err != nil {
				t.Fatal(err)
			}
			model := "gpt-6-astra"
			resume := ""
			if kind == "claude" {
				model = "sonnet"
				var u [16]byte
				if _, err := rand.Read(u[:]); err != nil {
					t.Fatal(err)
				}
				resume = fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
			}
			se := termSession{ID: "abcdef123456", Kind: kind, Cwd: s.terminal.defaultWd, Name: "migration-board-probe", BoardBrief: brief, Model: model, ResumeID: resume}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			id, err := h.Create(ctx, se)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = h.Close(context.Background(), id) }()
			for {
				if b, err := os.ReadFile(filepath.Join(dir, "exit")); err == nil {
					t.Logf("durable exit=%s", b)
					if string(b) != "0" {
						t.Fatalf("CLI exit %s", b)
					}
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("no durable exit before timeout")
				case <-time.After(time.Second):
				}
				ob, err := h.Inspect(ctx, id)
				t.Logf("advisory process=%s state=%s err=%v", ob.Process, ob.AgentState, err)
			}
		})
	}
}
