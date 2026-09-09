package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mappingFixtureHerdr(t *testing.T, s *Server) *atomic.Int32 {
	t.Helper()
	var sends atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input":
			sends.Add(1)
			herdrFixtureReply(c, map[string]any{"type": "pane_input_sent"})
		default:
			t.Errorf("unexpected request %s", r.Method)
			herdrFixtureReply(c, map[string]any{})
		}
	})
	h.server = s
	s.terminal.herdr = h
	if s.terminal.defaultWd == "" {
		s.terminal.defaultWd = t.TempDir()
	}
	return &sends
}
func TestTerminalMappingLegacyImportBacksUpAndPreservesIdentity(t *testing.T) {
	c := &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}
	before := []termSession{{ID: "abcdef12", Kind: "claude", Device: "remote", Cwd: "/exact/cwd", Name: "old label", ResumeID: "01234567-abcd", Started: true, Model: "pinned", BoardBrief: "/work/run/brief.md", Keep: true, Pinned: true}}
	b, _ := json.Marshal(before)
	if err := os.WriteFile(c.regPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.importLegacy(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(c.regPath + ".pre-herdr-v1.bak")
	if err != nil || string(backup) != string(b) {
		t.Fatalf("backup changed: %s %v", backup, err)
	}
	rows, err := c.loadChecked()
	if err != nil || len(rows) != 1 {
		t.Fatalf("%+v %v", rows, err)
	}
	got := rows[0]
	if got.Version != 1 || got.backend() != "tmux" || got.Runtime.Session != tmuxName(got.ID) {
		t.Fatal(got)
	}
	got.Version = 0
	got.Backend = ""
	got.Runtime = terminalIdentity{}
	if !reflect.DeepEqual(got, before[0]) {
		t.Fatalf("legacy metadata lost: %+v", got)
	}
	first, _ := os.ReadFile(c.regPath)
	if err = c.importLegacy(); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(c.regPath)
	if string(first) != string(second) {
		t.Fatal("import is not idempotent")
	}
	restart := &termCfg{regPath: c.regPath}
	row, ok := restart.find("abcdef12")
	if !ok || row.ResumeID != before[0].ResumeID || row.BoardBrief != before[0].BoardBrief {
		t.Fatal("restart lost exact resume/work link")
	}
}
func TestTerminalMappingCorruptionNeverOverwritten(t *testing.T) {
	for _, raw := range []string{"{broken", "null", `[{"id":"abcdef12","version":999}]`, `[{"id":"abcdef12"},{"id":"abcdef12"}]`} {
		t.Run(raw, func(t *testing.T) {
			c := &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}
			if err := os.WriteFile(c.regPath, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if err := c.upsertChecked(termSession{ID: "11223344"}); err == nil {
				t.Fatal("accepted corrupt registry")
			}
			c.upsert(termSession{ID: "55667788"})
			c.remove("abcdef12")
			if err := c.importLegacy(); err == nil {
				t.Fatal("import accepted corruption")
			}
			after, _ := os.ReadFile(c.regPath)
			if string(after) != raw {
				t.Fatalf("corrupt registry overwritten: %s", after)
			}
		})
	}
}
func TestTerminalMappingLaunchPersistsBeforeEverySideEffect(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	var allocations, sends atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "workspace.create":
			row, ok := s.terminal.find("abcdef12")
			if !ok || row.LaunchPhase != "intent" {
				t.Error("allocation before persisted intent")
			}
			allocations.Add(1)
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input":
			row, ok := s.terminal.find("abcdef12")
			if !ok || row.LaunchPhase != "submitted" || !row.Started || row.Runtime.Pane != "p_1" {
				t.Errorf("launch before persisted identity/submission: %+v", row)
			}
			text, _ := r.Params["text"].(string)
			if !strings.Contains(text, "--session-id 01234567-abcd") || !strings.Contains(text, "pinned-model") {
				t.Errorf("launch lost exact resume/model: %s", text)
			}
			sends.Add(1)
			herdrFixtureReply(c, map[string]any{})
		}
	})
	h.server = s
	s.terminal.herdr = h
	se, err := s.launchHerdr(context.Background(), termSession{ID: "abcdef12", Kind: "claude", Cwd: s.terminal.defaultWd, ResumeID: "01234567-abcd", Model: "pinned-model"})
	if err != nil {
		t.Fatal(err)
	}
	if se.LaunchPhase != "active" || se.Runtime.ManifestID != se.ID || !se.Started || allocations.Load() != 1 || sends.Load() != 1 {
		t.Fatalf("%+v", se)
	}
	row, ok := (&termCfg{regPath: s.terminal.regPath}).find(se.ID)
	if !ok || !reflect.DeepEqual(row, se) {
		t.Fatal("restart lost mapping")
	}
}
func TestTerminalMappingRestartUnresolvedStepsNeverReplay(t *testing.T) {
	for _, phase := range []string{"intent", "allocated", "submitted"} {
		t.Run(phase, func(t *testing.T) {
			s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}}
			var requests atomic.Int32
			h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) { requests.Add(1); herdrFixtureReply(c, map[string]any{}) })
			h.server = s
			s.terminal.herdr = h
			se := termSession{Version: 1, Backend: "herdr", ID: "abcdef12", Kind: "claude", ResumeID: "01234567-abcd", Started: phase == "submitted", LaunchPhase: phase, Runtime: herdrFixtureID(t, h)}
			if err := s.terminal.upsertChecked(se); err != nil {
				t.Fatal(err)
			}
			// A fresh Server reads the persisted posture; no in-memory flag authorizes replay.
			restart := &Server{terminal: &termCfg{regPath: s.terminal.regPath, herdr: h}}
			rec := postInput(t, restart, se.ID, map[string]string{"text": "hello"})
			if rec.Code != http.StatusBadGateway || requests.Load() != 0 {
				t.Fatalf("phase %s replayed: %d %s requests %d", phase, rec.Code, rec.Body.String(), requests.Load())
			}
		})
	}
}
func TestTerminalMappingSocketOutageNeverFallbackLaunch(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}}
	s.terminal.run = func(...string) ([]byte, error) { t.Fatal("herdr outage reached tmux fallback"); return nil, nil }
	s.terminal.herdr = &herdrTerminalRuntime{Socket: filepath.Join(t.TempDir(), "absent"), Session: "fixture", Host: "local", server: s}
	se := termSession{Version: 1, Backend: "herdr", ID: "abcdef12", Kind: "claude", ResumeID: "01234567-abcd", Started: true, LaunchPhase: "active", Runtime: terminalIdentity{Backend: "herdr", Session: "fixture", Host: "local", Generation: "g", Pane: "p_1", Workspace: "w_1", Occupant: "t_1"}}
	if err := s.terminal.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	rec := postInput(t, s, se.ID, map[string]string{"text": "hello"})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	ob, err := s.observeTerm(context.Background(), se)
	if err == nil || ob.Process != "unknown" {
		t.Fatalf("%+v %v", ob, err)
	}
	after, _ := s.terminal.find(se.ID)
	if !reflect.DeepEqual(after, se) {
		t.Fatal("outage changed launch identity")
	}
}
func TestTerminalMappingPersistenceFailurePreventsLaunch(t *testing.T) {
	dir := t.TempDir()
	s := &Server{terminal: &termCfg{regPath: dir, defaultWd: t.TempDir()}}
	sends := mappingFixtureHerdr(t, s)
	if _, err := s.launchHerdr(context.Background(), termSession{ID: "abcdef12", Kind: "claude"}); err == nil {
		t.Fatal("reported successful launch without persistent intent")
	}
	if sends.Load() != 0 {
		t.Fatal("launched despite persistence failure")
	}
}
func TestTerminalMappingLostLaunchReplyIsNotResent(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	var sends atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input":
			sends.Add(1) /* lost reply */
		}
	})
	h.server = s
	s.terminal.herdr = h
	se, err := s.launchHerdr(context.Background(), termSession{ID: "abcdef12", Kind: "claude", ResumeID: "01234567-abcd"})
	if err == nil || se.LaunchPhase != "submitted" {
		t.Fatalf("%+v %v", se, err)
	}
	rec := postInput(t, s, se.ID, map[string]string{"text": "hello"})
	if rec.Code != http.StatusBadGateway || sends.Load() != 1 {
		t.Fatalf("uncertain launch replayed: %d %s", rec.Code, rec.Body.String())
	}
}
func TestTerminalMappingBoardForgetPreservesLinkAndProcess(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), run: func(...string) ([]byte, error) { t.Fatal("forget killed work-order process"); return nil, nil }}}
	se := termSession{ID: "abcdef12", BoardBrief: "/run/brief.md"}
	if err := s.terminal.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("DELETE", "/api/terminal/session/abcdef12", nil)
	req.SetPathValue("id", se.ID)
	rec := httptest.NewRecorder()
	s.handleTermDelete(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if _, ok := s.terminal.find(se.ID); !ok {
		t.Fatal("deleted board association")
	}
}
func TestTerminalMappingBoundedReadinessDoesNotSendToShell(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}}
	var sends atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			pane := herdrFixturePane("idle", 2)
			delete(pane, "agent")
			herdrFixtureReply(c, map[string]any{"snapshot": map[string]any{"version": "0.9.0", "protocol": 22, "panes": []any{pane}}})
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "user@host$"}})
		default:
			sends.Add(1)
		}
	})
	h.server = s
	s.terminal.herdr = h
	se := termSession{Runtime: herdrFixtureID(t, h), Backend: "herdr"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.herdrPromptReady(ctx, se); err == nil || sends.Load() != 0 {
		t.Fatal("unknown shell accepted prompt")
	}
}

func TestTerminalMappingExactStoppedResumeAcrossRestart(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	var sends atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "workspace.create":
			if r.Params["cwd"] != s.terminal.defaultWd {
				t.Error("resume changed cwd")
			}
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input":
			text, _ := r.Params["text"].(string)
			if !strings.Contains(text, "claude --resume 01234567-abcd --model") || !strings.Contains(text, "pinned-model") {
				t.Errorf("resume lost exact identity/model: %s", text)
			}
			if strings.Contains(text, "--session-id") {
				t.Error("replayed first launch")
			}
			sends.Add(1)
			herdrFixtureReply(c, map[string]any{})
		}
	})
	h.server = s
	s.terminal.herdr = h
	id := herdrFixtureID(t, h)
	id.Pane = "stopped_pane"
	id.Occupant = "old_terminal"
	se := termSession{ID: "abcdef12", Version: 1, Backend: "herdr", Kind: "claude", ResumeID: "01234567-abcd", Started: true, LaunchPhase: "active", Runtime: id, Cwd: s.terminal.defaultWd, Model: "pinned-model"}
	if err := s.terminal.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	restart := &Server{terminal: &termCfg{regPath: s.terminal.regPath, defaultWd: s.terminal.defaultWd, herdr: h}}
	persisted, _ := restart.terminal.find(se.ID)
	resumed, did, err := restart.ensureHerdrInput(context.Background(), persisted)
	if err != nil || !did || resumed.ID != se.ID || resumed.ResumeID != se.ResumeID || resumed.Runtime.Pane != "p_1" || sends.Load() != 1 {
		t.Fatalf("resume: %+v %v %v", resumed, did, err)
	}
}
