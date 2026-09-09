package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func createCodingDraft(t *testing.T, s *Server, kind string) termSession {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/terminal/sessions", strings.NewReader(`{"kind":"`+kind+`","draft":true,"name":"Reviewed handoff"}`))
	s.handleTermCreate(w, r)
	var se termSession
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &se) != nil || !se.isDraft() {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	return se
}

func TestCodingDraftReadingAndDeletingNeverTouchesRuntime(t *testing.T) {
	for _, kind := range []string{"claude", "codex"} {
		t.Run(kind, func(t *testing.T) {
			s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir(), run: func(...string) ([]byte, error) { return nil, nil }}}
			var requests atomic.Int32
			s.terminal.herdr = herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
				requests.Add(1)
				herdrFixtureReply(c, map[string]any{})
			})
			se := createCodingDraft(t, s, kind)
			// Reload from disk as a restarted server would. This is authoritative
			// never-started state, independent of daemon connectivity or caches.
			loaded, ok := (&termCfg{regPath: s.terminal.regPath}).find(se.ID)
			if !ok || !loaded.isDraft() || loaded.ResumeID != se.ResumeID {
				t.Fatal("draft identity lost on reload")
			}
			ob, err := s.observeTerm(context.Background(), loaded)
			if err != nil || ob.Process != "not-started" || ob.Connectivity != "not-started" {
				t.Fatalf("observation: %+v %v", ob, err)
			}
			for name, handler := range map[string]http.HandlerFunc{"transcript": s.handleTermTranscript, "screen": s.handleTermScreen, "list": s.handleTermSessions} {
				w := httptest.NewRecorder()
				r := httptest.NewRequest("GET", "/", nil)
				r.SetPathValue("id", se.ID)
				handler(w, r)
				if w.Code != 200 || !strings.Contains(w.Body.String(), `"process":"not-started"`) {
					t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
				}
			}
			for _, connected := range []bool{false, true} {
				h := s.terminalHub()
				h.connected = connected
				snapshot := h.snapshotLocked()
				if len(snapshot.Sessions) != 1 || snapshot.Sessions[0].Process != "not-started" {
					t.Fatalf("draft stream: %+v", snapshot)
				}
			}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/?id="+se.ID, nil)
			r.SetPathValue("id", se.ID)
			s.handleTermWS(w, r)
			if w.Code != http.StatusConflict {
				t.Fatalf("attach: %d %s", w.Code, w.Body.String())
			}
			w = httptest.NewRecorder()
			s.handleTermDelete(w, r)
			if w.Code != 200 || requests.Load() != 0 {
				t.Fatalf("delete: %d, runtime requests %d", w.Code, requests.Load())
			}
		})
	}
}

func TestCodingDraftFirstMessageLaunchesOnceAndKeepsIdentity(t *testing.T) {
	s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
	var sends atomic.Int32
	h := herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "idle", 2)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		case "pane.send_input", "agent.prompt":
			sends.Add(1)
			herdrFixtureReply(c, map[string]any{})
		case "pane.read":
			herdrFixtureReply(c, map[string]any{"read": map[string]any{"text": "❯"}})
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
	})
	h.server, s.terminal.herdr = s, h
	se := createCodingDraft(t, s, "claude")
	send := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.SetPathValue("id", se.ID)
		s.handleTermInput(w, r)
		return w
	}
	if w := send(`{"key":"\u0003"}`); w.Code != 409 || sends.Load() != 0 {
		t.Fatalf("key started draft: %d %s", w.Code, w.Body.String())
	}
	if w := send(`{"text":"Reviewed first message"}`); w.Code != 200 || sends.Load() != 2 {
		t.Fatalf("first message: %d %s, sends %d", w.Code, w.Body.String(), sends.Load())
	}
	row, _ := s.terminal.find(se.ID)
	if !row.Started || row.LaunchPhase != "active" || row.ResumeID != se.ResumeID {
		t.Fatalf("lost identity: %+v", row)
	}
	if w := send(`{"text":"Follow-up"}`); w.Code != 200 || sends.Load() != 3 {
		t.Fatalf("follow-up relaunched: %d %s, sends %d", w.Code, w.Body.String(), sends.Load())
	}
}

func TestCodingDraftRejectsAmbiguousRuntimeState(t *testing.T) {
	base := termSession{ID: "abcdef12", Kind: "codex", Backend: "herdr", LaunchPhase: "draft"}
	for _, mutate := range []func(*termSession){
		func(s *termSession) { s.Started = true },
		func(s *termSession) { s.Runtime.Pane = "allocated" },
		func(s *termSession) { s.LaunchPhase = "intent" },
		func(s *termSession) { s.BoardBrief = "/work/brief.md" },
		func(s *termSession) { s.Resume = true },
		func(s *termSession) { s.Device = "remote" },
	} {
		se := base
		mutate(&se)
		if se.isDraft() {
			t.Fatalf("unsafe draft: %+v", se)
		}
		s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}}
		if _, _, err := s.ensureHerdrInputLocked(context.Background(), se); err == nil {
			t.Fatalf("ambiguous state allowed launch: %+v", se)
		}
	}
}

func TestCodingDraftCreationRejectsOtherLaunchModes(t *testing.T) {
	for _, body := range []string{
		`{"kind":"shell","draft":true}`,
		`{"kind":"claude","draft":true,"resumePicker":true}`,
		`{"kind":"codex","draft":true,"device":"remote"}`,
		`{"kind":"codex","draft":true,"keep":true}`,
		`{"kind":"codex","draft":true,"cwd":"relative/path"}`,
	} {
		s := &Server{terminal: &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json"), defaultWd: t.TempDir()}}
		w := httptest.NewRecorder()
		s.handleTermCreate(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if w.Code != 400 || len(s.terminal.load()) != 0 {
			t.Fatalf("invalid draft persisted: %s: %d %s", body, w.Code, w.Body.String())
		}
	}
}
