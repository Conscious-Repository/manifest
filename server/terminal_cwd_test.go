package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTerminalCwdResolution(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	special := home + "/spaces ' $(not-a-command)"
	if err := os.Mkdir(special, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(special, home+"/link"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(home+"/missing", home+"/dangling"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ in, want, why string }{
		{"", home, ""}, {" \t", home, ""}, {"~", home, ""}, {"~/", home + "/", ""},
		{"~/link", home + "/link", ""}, {" " + special + " ", special, ""},
		{home + "/link/..", home + "/link/..", ""},
		{"relative", "", "must be absolute"}, {"$HOME", "", "must be absolute"},
		{home + "/missing", "", "does not exist"}, {file, "", "not a directory"},
		{file + "/child", "", "not a directory"}, {home + "/dangling", "", "does not exist"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := resolveTerminalCwd(tc.in, home)
			if tc.why == "" {
				if err != nil || got != tc.want {
					t.Fatalf("%q %v", got, err)
				}
				return
			}
			var invalid *invalidTerminalCwd
			if !errors.As(err, &invalid) || !strings.Contains(err.Error(), tc.why) {
				t.Fatalf("%q %v", got, err)
			}
		})
	}
	locked := home + "/locked"
	if err := os.Mkdir(locked, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0700)
	if os.Geteuid() != 0 {
		_, err := resolveTerminalCwd(locked+"/child", home)
		if terminalLaunchStatus(err) != 400 || !strings.Contains(err.Error(), "permission denied") {
			t.Fatal(err)
		}
	}
}

func TestTermCreateInvalidCwdLeavesRegistryUntouched(t *testing.T) {
	for _, kind := range []string{"codex", "claude", "shell"} {
		for _, populated := range []bool{false, true} {
			for _, lane := range []string{"local", "self", "agent", "offline", "default"} {
				t.Run(kind+"/"+lane+"/"+map[bool]string{false: "absent", true: "populated"}[populated], func(t *testing.T) {
					s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}, devices: &devCfg{selfName: "self"}}
					before := []byte("[ {\"id\":\"11223344\",\"name\":\"preserve formatting\"} ]\n")
					if populated {
						if err := os.WriteFile(s.terminal.regPath, before, 0600); err != nil {
							t.Fatal(err)
						}
					}
					var requests atomic.Int32
					if lane != "offline" {
						s.terminal.herdr = herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) { requests.Add(1); herdrFixtureReply(c, map[string]any{}) })
						s.terminal.herdr.server = s
					}
					body := map[string]string{"kind": kind, "cwd": s.terminal.defaultWd + "/missing"}
					if lane == "self" {
						body["device"] = "self"
					}
					if lane == "default" {
						s.terminal.defaultWd = body["cwd"]
						body["cwd"] = " "
					}
					body["backend"] = "herdr"
					raw, _ := json.Marshal(body)
					w := httptest.NewRecorder()
					r := httptest.NewRequest("POST", "/api/terminal/session", strings.NewReader(string(raw)))
					if lane == "agent" {
						s.handleTermAgentCreate(w, r)
					} else {
						s.handleTermCreate(w, r)
					}
					if w.Code != 400 || !strings.Contains(w.Body.String(), "cwd does not exist") || requests.Load() != 0 {
						t.Fatalf("%d %s requests=%d", w.Code, w.Body.String(), requests.Load())
					}
					after, err := os.ReadFile(s.terminal.regPath)
					if populated {
						if err != nil || string(after) != string(before) {
							t.Fatalf("registry changed: %s %v", after, err)
						}
					} else if !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("registry created: %s %v", after, err)
					}
				})
			}
		}
	}
}

func deleteCwdTestSession(s *Server, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("DELETE", "/api/terminal/session/"+id, nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.handleTermDelete(w, r)
	return w
}

func TestTerminalMappingForgetOnlyUnidentifiedIntent(t *testing.T) {
	for _, shape := range []string{"orphan", "board", "partial", "allocated", "submitted", "active", "started", "tmux", "corrupt"} {
		t.Run(shape, func(t *testing.T) {
			s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}}
			se := termSession{ID: "abcdef12", Backend: "herdr", LaunchPhase: "intent"}
			switch shape {
			case "board":
				se.BoardBrief = "/run/brief.md"
			case "partial":
				se.Runtime.Backend = "herdr"
			case "allocated", "submitted", "active":
				se.LaunchPhase = shape
			case "started":
				se.Started = true
			case "tmux":
				se.Backend = "tmux"
			}
			s.terminal.run = func(...string) ([]byte, error) { return nil, errors.New("offline") }
			if err := s.terminal.upsertChecked(se); err != nil {
				t.Fatal(err)
			}
			if shape == "corrupt" {
				if err := os.WriteFile(s.terminal.regPath, []byte("{broken"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(s.terminal.regPath)
			w := deleteCwdTestSession(s, se.ID)
			want := 502
			if shape == "orphan" || shape == "tmux" {
				want = 200
			}
			if shape == "board" {
				want = 409
			}
			if shape == "corrupt" {
				want = 500
			}
			if w.Code != want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if want == 200 {
				if _, ok := (&termCfg{regPath: s.terminal.regPath}).find(se.ID); ok {
					t.Fatal("forget not durable")
				}
			} else {
				after, _ := os.ReadFile(s.terminal.regPath)
				if string(after) != string(before) {
					t.Fatal("changed protected row")
				}
			}
		})
	}
}

func TestTerminalMappingCwdDisappearsBeforeAllocation(t *testing.T) {
	for _, mode := range []string{"fresh", "resume", "board", "rollback-fails"} {
		t.Run(mode, func(t *testing.T) {
			cwd := t.TempDir()
			s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: cwd}}
			prior := termSession{ID: "abcdef12", Backend: "herdr", Kind: "claude", Cwd: cwd, LaunchPhase: "active", Started: true, ResumeID: "01234567-abcd", Name: "keep history"}
			if mode == "resume" {
				if err := s.terminal.upsertChecked(prior); err != nil {
					t.Fatal(err)
				}
			}
			var mutations atomic.Int32
			s.terminal.herdr = herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
				if r.Method == "session.snapshot" {
					if err := os.Remove(cwd); err != nil {
						t.Error(err)
					}
					if mode == "rollback-fails" {
						if err := os.WriteFile(s.terminal.regPath, []byte("{corrupt"), 0600); err != nil {
							t.Error(err)
						}
					}
					herdrFixtureSnapshot(c, "idle", 1)
				} else {
					mutations.Add(1)
					herdrFixtureReply(c, map[string]any{})
				}
			})
			s.terminal.herdr.server = s
			var err error
			var se termSession
			brief := filepath.Join(t.TempDir(), "brief.md")
			if mode == "board" {
				se, err = s.createBoardHerdrSession("claude", cwd, "board", brief, "")
			} else {
				se, err = s.launchHerdr(context.Background(), prior)
			}
			want := 400
			if mode == "rollback-fails" {
				want = 500
			}
			if err == nil || terminalLaunchStatus(err) != want || mutations.Load() != 0 {
				t.Fatalf("%+v %v mutations %d", se, err, mutations.Load())
			}
			rows, loadErr := s.terminal.loadChecked()
			switch mode {
			case "fresh":
				if loadErr != nil || len(rows) != 0 {
					t.Fatalf("%+v %v", rows, loadErr)
				}
			case "resume":
				if loadErr != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], prior) {
					t.Fatalf("%+v %v", rows, loadErr)
				}
			case "board":
				marker, _ := os.ReadFile(filepath.Join(filepath.Dir(brief), "session"))
				if len(rows) != 1 || rows[0].BoardBrief != brief || string(marker) != se.ID {
					t.Fatal("board journal lost")
				}
			case "rollback-fails":
				if loadErr == nil || !strings.Contains(err.Error(), prior.ID) || !strings.Contains(err.Error(), "rollback failed") {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestBoardHerdrInvalidCwdBeforeJournal(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	brief := filepath.Join(t.TempDir(), "brief.md")
	_, err := s.createBoardHerdrSession("codex", s.terminal.defaultWd+"/missing", "board", brief, "")
	if terminalLaunchStatus(err) != 400 {
		t.Fatal(err)
	}
	for _, p := range []string{s.terminal.regPath, filepath.Join(filepath.Dir(brief), "session")} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("journal written %s: %v", p, err)
		}
	}
}

func TestTerminalMappingMissingResumeCwdPreservesHistory(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	sends := mappingFixtureHerdr(t, s)
	id := herdrFixtureID(t, s.terminal.herdr)
	id.Pane = "stopped_pane"
	id.Occupant = "old_terminal"
	se := termSession{ID: "abcdef12", Kind: "claude", Backend: "herdr", LaunchPhase: "active", Started: true, ResumeID: "01234567-abcd", Cwd: s.terminal.defaultWd + "/missing", Runtime: id}
	if err := s.terminal.upsertChecked(se); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.terminal.regPath)
	w := postInput(t, s, se.ID, map[string]string{"text": "do not send"})
	after, _ := os.ReadFile(s.terminal.regPath)
	if w.Code != 400 || string(before) != string(after) || sends.Load() != 0 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestTerminalMappingDeleteWaitsForAllocation(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	entered, release := make(chan struct{}), make(chan struct{})
	var allocations atomic.Int32
	s.terminal.herdr = herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 1)
		case "workspace.create":
			allocations.Add(1)
			close(entered)
			<-release // lost reply: retain unidentified intent
		default:
			t.Errorf("unexpected mutation %s", r.Method)
		}
	})
	s.terminal.herdr.server = s
	launched := make(chan error, 1)
	go func() {
		_, err := s.launchHerdr(context.Background(), termSession{ID: "abcdef12", Kind: "shell"})
		launched <- err
	}()
	<-entered
	deleted := make(chan *httptest.ResponseRecorder, 1)
	go func() { deleted <- deleteCwdTestSession(s, "abcdef12") }()
	select {
	case w := <-deleted:
		t.Errorf("delete crossed allocation: %d", w.Code)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-launched; err == nil || terminalLaunchStatus(err) != 502 {
		t.Fatal(err)
	}
	if w := <-deleted; w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if allocations.Load() != 1 {
		t.Fatal("reallocated")
	}
	if _, ok := s.terminal.find("abcdef12"); ok {
		t.Fatal("launch resurrected forgotten intent")
	}
}

func TestTerminalMappingLostAllocationReplyRetainsIntent(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	var allocations, sends atomic.Int32
	s.terminal.herdr = herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 1)
		case "workspace.create":
			allocations.Add(1) // close without a reply
		default:
			sends.Add(1)
		}
	})
	s.terminal.herdr.server = s
	se, err := s.launchHerdr(context.Background(), termSession{ID: "abcdef12", Kind: "shell"})
	if err == nil || terminalLaunchStatus(err) != 502 {
		t.Fatal(err)
	}
	restart := &Server{terminal: &termCfg{regPath: s.terminal.regPath, herdr: s.terminal.herdr}}
	row, ok := restart.terminal.find(se.ID)
	if !ok || row.LaunchPhase != "intent" || row.Runtime != (terminalIdentity{}) || row.Started {
		t.Fatalf("%+v", row)
	}
	w := postInput(t, restart, se.ID, map[string]string{"text": "never replay"})
	if w.Code != 502 || allocations.Load() != 1 || sends.Load() != 0 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := deleteCwdTestSession(restart, se.ID); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if sends.Load() != 0 {
		t.Fatal("metadata forget sent runtime mutation")
	}
}

func TestTermCreateRemoteAndLegacyDoNotStatLocalCwd(t *testing.T) {
	for _, lane := range []string{"remote", "legacy"} {
		t.Run(lane, func(t *testing.T) {
			s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}, devices: &devCfg{selfName: "self", devices: []TermDevice{{Name: "remote", Host: "example.invalid", User: "test"}}}}
			var calls atomic.Int32
			s.terminal.run = func(...string) ([]byte, error) { calls.Add(1); return nil, nil }
			body := map[string]string{"kind": "codex", "cwd": s.terminal.defaultWd + "/absent-on-local-host"}
			if lane == "remote" {
				body["device"] = "remote"
			}
			raw, _ := json.Marshal(body)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/", strings.NewReader(string(raw)))
			if lane == "remote" {
				s.handleTermCreate(w, r)
			} else {
				s.handleTermAgentCreate(w, r)
			}
			if w.Code != 200 || calls.Load() == 0 {
				t.Fatalf("%d %s calls=%d", w.Code, w.Body.String(), calls.Load())
			}
		})
	}
}

func TestTerminalMappingBoundDeletePreservesOnOutageOrMismatch(t *testing.T) {
	for _, mode := range []string{"outage", "occupant"} {
		t.Run(mode, func(t *testing.T) {
			s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}}
			mappingFixtureHerdr(t, s)
			id := herdrFixtureID(t, s.terminal.herdr)
			if mode == "outage" {
				s.terminal.herdr.Socket = filepath.Join(t.TempDir(), "absent")
			} else {
				id.Occupant = "replacement"
			}
			se := termSession{ID: "abcdef12", Backend: "herdr", LaunchPhase: "active", Started: true, Runtime: id}
			if err := s.terminal.upsertChecked(se); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(s.terminal.regPath)
			w := deleteCwdTestSession(s, se.ID)
			after, _ := os.ReadFile(s.terminal.regPath)
			if w.Code != 502 || string(after) != string(before) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
