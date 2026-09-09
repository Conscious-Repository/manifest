package server

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"manifest/spirits"
)

func ageBoardRun(t *testing.T, s *Server) (string, string) {
	t.Helper()
	h := s.findHarness("codex")
	r := h.Spirits.Runs()[0]
	task := "inbox/wire-the-fence"
	if err := boardReport(h, r.ID, task, "go", "", "running", "", time.Now().Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return task, filepath.Join(h.Spirits.Root(), "work", r.ID)
}

func TestBoardHerdrRestartAtLaunchBoundaries(t *testing.T) {
	for _, phase := range []string{"intent", "allocated", "submitted", "active"} {
		t.Run(phase, func(t *testing.T) {
			s := codingFixture(t)
			if err := s.startCodingTask(s.findHarness("codex"), "inbox/wire-the-fence", "go", "", "execute"); err != nil {
				t.Fatal(err)
			}
			task, dir := ageBoardRun(t, s)
			se := s.terminal.load()[0]
			se.LaunchPhase = phase
			if phase == "intent" {
				se.Runtime = terminalIdentity{}
			}
			if err := s.terminal.upsertChecked(se); err != nil {
				t.Fatal(err)
			}
			// Restart without the daemon: persisted phases must neither replay nor fail.
			s.UseTerminal(s.terminal.regPath, s.terminal.tmuxTmp, s.terminal.defaultWd)
			s.UseCodingRepo(se.Cwd)
			s.terminal.herdr.Socket = filepath.Join(t.TempDir(), "absent.sock")
			if got := s.delegationIndex()[task].State; got != "running" {
				t.Fatalf("%s became %s", phase, got)
			}
			if err := s.startCodingTask(s.findHarness("claude"), task, "go", "", "execute"); !errors.Is(err, spirits.ErrAlreadyActive) {
				t.Fatalf("writer unlocked: %v", err)
			}
			stored, _ := os.ReadFile(filepath.Join(dir, "session"))
			if string(stored) != se.ID || len(s.terminal.load()) != 1 {
				t.Fatal("restart lost identity or duplicated launch")
			}
			// File reconciliation remains authoritative after all events were lost.
			if err := boardWrite(filepath.Join(dir, "result.json"), []byte(`{"status":"completed","summary":"Verified and pushed."}`)); err != nil {
				t.Fatal(err)
			}
			if got := s.delegationIndex()[task].State; got != "done" {
				t.Fatalf("lost events hid durable result: %s", got)
			}
		})
	}
}

func TestBoardHerdrLabelsCannotReleaseWriter(t *testing.T) {
	for _, label := range []string{"idle", "done", "blocked", "working", "unknown"} {
		t.Run(label, func(t *testing.T) {
			s := codingFixture(t)
			s.terminal.herdr = herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
				switch r.Method {
				case "session.snapshot":
					herdrFixtureSnapshot(c, label, 1)
				case "workspace.create":
					herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane(label, 1)})
				default:
					herdrFixtureReply(c, map[string]any{})
				}
			})
			s.terminal.herdr.server = s
			if err := s.startCodingTask(s.findHarness("codex"), "inbox/wire-the-fence", "go", "", "execute"); err != nil {
				t.Fatal(err)
			}
			task, dir := ageBoardRun(t, s)
			if err := boardWrite(filepath.Join(dir, "result.json"), []byte(`{"status":"completed"}`)); err != nil {
				t.Fatal(err)
			}
			if got := s.delegationIndex()[task].State; got != "running" {
				t.Fatalf("label %s released writer: %s", label, got)
			}
			if err := s.startCodingTask(s.findHarness("claude"), task, "go", "", "execute"); !errors.Is(err, spirits.ErrAlreadyActive) {
				t.Fatalf("writer unlocked: %v", err)
			}
		})
	}
}

func TestBoardHerdrUncertainLaunchRetainsPersistedWriter(t *testing.T) {
	s := codingFixture(t)
	s.terminal.herdr.Socket = filepath.Join(t.TempDir(), "missing.sock")
	if err := s.startCodingTask(s.findHarness("codex"), "inbox/wire-the-fence", "go", "", "execute"); err == nil {
		t.Fatal("outage accepted")
	}
	task, dir := ageBoardRun(t, s)
	se := s.terminal.load()[0]
	raw, err := os.ReadFile(filepath.Join(dir, "session"))
	if err != nil || string(raw) != se.ID || se.Backend != "herdr" || se.LaunchPhase != "intent" {
		t.Fatalf("lost journal: %+v %s %v", se, raw, err)
	}
	if !strings.Contains(se.boardLaunch(), "-m '"+se.Model+"'") {
		t.Fatal("model not pinned")
	}
	if got := s.delegationIndex()[task].State; got != "running" {
		t.Fatalf("outage became %s", got)
	}
	if err := s.startCodingTask(s.findHarness("claude"), task, "go", "", "execute"); !errors.Is(err, spirits.ErrAlreadyActive) {
		t.Fatalf("writer unlocked: %v", err)
	}
}

func TestBoardHerdrFinalResultWrittenDuringStopObservation(t *testing.T) {
	for _, result := range []bool{false, true} {
		t.Run(map[bool]string{false: "death-without-result", true: "final-result"}[result], func(t *testing.T) {
			s := codingFixture(t)
			var stopped atomic.Bool
			var dir string
			s.terminal.herdr = herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
				switch r.Method {
				case "session.snapshot":
					if stopped.Load() {
						if result {
							_ = boardWrite(filepath.Join(dir, "result.json"), []byte(`{"status":"completed","summary":"Final durable write after process stop."}`))
						}
						herdrFixtureReply(c, map[string]any{"snapshot": map[string]any{"version": "0.9.0", "protocol": 22, "panes": []any{}}})
					} else {
						herdrFixtureSnapshot(c, "working", 1)
					}
				case "workspace.create":
					herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
				default:
					herdrFixtureReply(c, map[string]any{})
				}
			})
			s.terminal.herdr.server = s
			if err := s.startCodingTask(s.findHarness("codex"), "inbox/wire-the-fence", "go", "", "execute"); err != nil {
				t.Fatal(err)
			}
			task, d := ageBoardRun(t, s)
			dir = d
			stopped.Store(true)
			want := "failed"
			if result {
				want = "done"
			}
			if got := s.delegationIndex()[task].State; got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		})
	}
}
