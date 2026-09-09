package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestHerdrLiveCodexFileIdentity(t *testing.T) {
	session := os.Getenv("MANIFEST_HERDR_TEST_SESSION")
	want := os.Getenv("MANIFEST_HERDR_TEST_CODEX_ID")
	if session == "" || want == "" {
		t.Skip("requires isolated live Codex pane and its known conversation ID")
	}
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	cwd := os.Getenv("MANIFEST_HERDR_TEST_CWD")
	s := &Server{terminal: &termCfg{defaultWd: cwd}}
	h := &herdrTerminalRuntime{server: s, Host: host, Session: session, Socket: filepath.Join(home, ".config", "herdr", "sessions", session, "herdr.sock")}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	all, err := h.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatal("use a scratch daemon with exactly one known Codex pane")
	}
	se := termSession{Kind: "codex", Cwd: cwd, Runtime: all[0].Identity}
	got, err := h.codexProcessRollout(ctx, se)
	if err != nil || got != want {
		t.Fatalf("identity=%q want=%q err=%v", got, want, err)
	}
	se.ResumeID = got
	if p := s.terminal.transcriptPath(se); p == "" {
		t.Fatal("discovered ID did not resolve exact rollout")
	}
}

func TestHerdrLiveChatRestart(t *testing.T) {
	session := os.Getenv("MANIFEST_HERDR_TEST_SESSION")
	if session == "" {
		t.Skip("requires isolated herdr daemon and authenticated Codex")
	}
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	cwd := os.Getenv("MANIFEST_HERDR_TEST_CWD")
	if cwd == "" {
		t.Fatal("set trusted scratch cwd")
	}
	reg := filepath.Join(t.TempDir(), "terminals.json")
	fresh := func() *Server {
		s := &Server{terminal: &termCfg{regPath: reg, defaultWd: cwd}}
		s.terminal.herdr = &herdrTerminalRuntime{server: s, Host: host, Session: session, Socket: filepath.Join(home, ".config", "herdr", "sessions", session, "herdr.sock")}
		return s
	}
	s := fresh()
	req := httptest.NewRequest("POST", "/api/terminal/session", strings.NewReader(`{"kind":"codex","model":"gpt-6-astra","name":"migration-chat-probe"}`))
	w := httptest.NewRecorder()
	s.handleTermCreate(w, req)
	if w.Code != 200 {
		t.Fatalf("create %d: %s", w.Code, w.Body.String())
	}
	var se termSession
	if err := json.Unmarshal(w.Body.Bytes(), &se); err != nil {
		t.Fatal(err)
	}
	defer func() { s.closeTerm(context.Background(), se) }()
	input := httptest.NewRequest("POST", "/api/terminal/session/"+se.ID+"/input", strings.NewReader(`{"text":"Reply only CHAT_RESTART_OK. Do not use tools or change files."}`))
	input.SetPathValue("id", se.ID)
	iw := httptest.NewRecorder()
	s.handleTermInput(iw, input)
	if iw.Code != 200 {
		t.Fatalf("input %d: %s", iw.Code, iw.Body.String())
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		r := httptest.NewRequest("GET", "/api/terminal/session/"+se.ID+"/transcript", nil)
		r.SetPathValue("id", se.ID)
		tw := httptest.NewRecorder()
		s.handleTermTranscript(tw, r)
		if strings.Contains(tw.Body.String(), "CHAT_RESTART_OK") {
			break
		}
		time.Sleep(time.Second)
	}
	persisted, ok := s.terminal.find(se.ID)
	if !ok || persisted.ResumeID == "" {
		t.Fatal("exact Codex conversation not persisted")
	}
	se = persisted
	s = fresh()
	after, ok := s.terminal.find(se.ID)
	if !ok || after.Runtime != se.Runtime || after.ResumeID != se.ResumeID {
		t.Fatal("restart changed identity")
	}
	ob, err := s.observeTerm(context.Background(), after)
	if err != nil || ob.Process != "running" {
		t.Fatalf("restart observation %+v %v", ob, err)
	}
	if err = s.closeTerm(context.Background(), after); err != nil {
		t.Fatal(err)
	}
	resumed, yes, err := s.ensureHerdrInput(context.Background(), after)
	if err != nil || !yes || resumed.ID != se.ID || resumed.ResumeID != se.ResumeID || resumed.Model != se.Model {
		t.Fatalf("exact resume %+v resumed=%v err=%v", resumed, yes, err)
	}
	se = resumed
}

func TestHerdrLiveStateSubscription(t *testing.T) {
	session := os.Getenv("MANIFEST_HERDR_TEST_SESSION")
	if session == "" {
		t.Skip("requires isolated live daemon and authenticated Codex")
	}
	cwd := os.Getenv("MANIFEST_HERDR_TEST_CWD")
	if cwd == "" {
		t.Fatal("set trusted scratch cwd")
	}
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	s := &Server{terminal: &termCfg{defaultWd: cwd, regPath: filepath.Join(t.TempDir(), "terminals.json")}}
	h := &herdrTerminalRuntime{server: s, Host: host, Session: session, Socket: filepath.Join(home, ".config", "herdr", "sessions", session, "herdr.sock")}
	s.terminal.herdr = h
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	se := termSession{ID: "abcdef12", Kind: "codex", Cwd: cwd, Model: "gpt-6-astra", Name: "migration-event-probe"}
	id, err := h.Create(ctx, se)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(context.Background(), id)
	se.Runtime = id
	se.Backend = "herdr"
	if err = s.herdrPromptReady(ctx, se); err != nil {
		t.Fatal(err)
	}
	stream, err := h.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.SendText(ctx, id, "Reply only EVENTS_PROBE_OK. Do not use tools or change any files."); err != nil {
		t.Fatal(err)
	}
	working := false
	for {
		select {
		case ob, ok := <-stream:
			if !ok {
				t.Fatal("subscription closed unexpectedly")
			}
			t.Logf("observed %s", ob.AgentState)
			if ob.AgentState == "working" {
				working = true
			}
			if working && (ob.AgentState == "idle" || ob.AgentState == "done") {
				return
			}
		case <-ctx.Done():
			t.Fatal("did not observe actual working and settled transition")
		}
	}
}

func TestHerdrLiveBoardJournalAndSocketOutage(t *testing.T) {
	session := os.Getenv("MANIFEST_HERDR_TEST_SESSION")
	if session == "" {
		t.Skip("requires isolated live daemon and authenticated Codex")
	}
	if !strings.HasPrefix(session, "manifest-migration-") {
		t.Fatal("socket outage test requires a manifest-migration-* scratch daemon")
	}
	cwd := os.Getenv("MANIFEST_HERDR_TEST_CWD")
	home, _ := os.UserHomeDir()
	host, _ := os.Hostname()
	dir := t.TempDir()
	s := &Server{terminal: &termCfg{defaultWd: cwd, regPath: filepath.Join(dir, "terminals.json")}}
	h := &herdrTerminalRuntime{server: s, Host: host, Session: session, Socket: filepath.Join(home, ".config", "herdr", "sessions", session, "herdr.sock")}
	s.terminal.herdr = h
	brief := filepath.Join(dir, "work", "run-probe", "brief.md")
	if err := boardWrite(brief, []byte("Runtime journal probe only: reply BOARD_JOURNAL_OK; do not change repository files or commit. No result file is requested.")); err != nil {
		t.Fatal(err)
	}
	se, err := s.createBoardHerdrSession("codex", cwd, "migration-board-journal", brief, "gpt-6-astra")
	if err != nil {
		t.Fatal(err)
	}
	defer s.closeTerm(context.Background(), se)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(brief), "session"))
	if err != nil || string(raw) != se.ID || se.LaunchPhase != "active" {
		t.Fatal("board journal incomplete")
	}
	moved := h.Socket + ".outage"
	if err = os.Rename(h.Socket, moved); err != nil {
		t.Fatal(err)
	}
	ob, inspectErr := s.observeTerm(context.Background(), se)
	restoreErr := os.Rename(moved, h.Socket)
	if restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if inspectErr == nil || ob.Process != "unknown" || ob.AgentState != "unknown" {
		t.Fatal("socket outage classified process death")
	}
	ob, err = s.observeTerm(context.Background(), se)
	if err != nil || ob.Process != "running" {
		t.Fatalf("process did not survive API outage: %+v %v", ob, err)
	}
	t.Log("live process survived socket outage; unavailable observation remained unknown")
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(filepath.Dir(brief), "exit")); err == nil {
			if string(b) != "0" {
				t.Fatalf("exit %s", b)
			}
			t.Log("board helper durable exit=0")
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("board helper did not exit")
}
