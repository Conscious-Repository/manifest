package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeTmux records every tmux argv instead of running one; `live` answers
// list-sessions, `screen` answers capture-pane.
type fakeTmux struct {
	calls  [][]string
	live   bool
	screen string
	name   string
}

func (f *fakeTmux) run(args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{}, args...))
	switch args[0] {
	case "list-sessions":
		if f.live {
			return []byte(f.name + "\n"), nil
		}
		return nil, os.ErrNotExist
	case "capture-pane":
		if !f.live {
			return nil, os.ErrNotExist
		}
		return []byte(f.screen), nil
	}
	if hasArg(args, "new-session") {
		f.live = true // the spawn brings it up
	}
	return nil, nil
}

func hasArg(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func (f *fakeTmux) find(verb string) [][]string {
	var out [][]string
	for _, c := range f.calls {
		if c[0] == verb || hasArg(c, verb) && c[0] == "set" { // prelude-prefixed spawn
			out = append(out, c)
		}
	}
	return out
}

func fakeTmuxServer(t *testing.T) (*Server, *fakeTmux) {
	t.Helper()
	dir := t.TempDir()
	rec := &fakeTmux{}
	s := &Server{terminal: &termCfg{
		regPath: filepath.Join(dir, "terminals.json"), tmuxTmp: dir, defaultWd: dir,
		claudeProjects: filepath.Join(dir, "projects"),
	}}
	s.terminal.run = rec.run
	return s, rec
}

func postInput(t *testing.T, s *Server, id string, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/terminal/session/"+id+"/input", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.handleTermInput(w, req)
	return w
}

// A one-line message to a live session: send-keys -l <text>, then Enter;
// no spawn.
func TestInputLiveOneLine(t *testing.T) {
	s, rec := fakeTmuxServer(t)
	se := termSession{ID: "abcdef0123456789", Kind: "claude", ResumeID: "63e55a17-b5b5-464e-8a00-714a410c4422", Started: true, Name: "cc1"}
	s.terminal.upsert(se)
	rec.live, rec.name = true, tmuxName(se.ID)

	w := postInput(t, s, se.ID, map[string]string{"text": "what's the status?"})
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"relaunched":true`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	sk := rec.find("send-keys")
	if len(sk) != 2 {
		t.Fatalf("send-keys calls = %v", sk)
	}
	want := []string{"send-keys", "-t", "manifest_" + se.ID, "-l", "--", "what's the status?"}
	if strings.Join(sk[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("send-keys argv = %q, want %q", sk[0], want)
	}
	if sk[1][len(sk[1])-1] != "Enter" {
		t.Fatalf("second send-keys must be Enter: %q", sk[1])
	}
	if len(rec.find("new-session")) != 0 {
		t.Fatal("live session must not be respawned")
	}
	if row, _ := s.terminal.find(se.ID); row.LastUsed == "" {
		t.Fatal("lastUsed not touched")
	}
}

// Multi-line text rides bracketed paste: load-buffer from a file in the
// sandbox dir, paste-buffer -p, then Enter — never a raw send-keys of the
// text (the editor would submit at the first newline).
func TestInputMultiLinePastes(t *testing.T) {
	s, rec := fakeTmuxServer(t)
	se := termSession{ID: "abcdef0123456789", Kind: "claude", ResumeID: "63e55a17-b5b5-464e-8a00-714a410c4422", Started: true, Name: "cc1"}
	s.terminal.upsert(se)
	rec.live, rec.name = true, tmuxName(se.ID)
	w := postInput(t, s, se.ID, map[string]string{"text": "line one\nline two\n"})
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	lb := rec.find("load-buffer")
	pb := rec.find("paste-buffer")
	if len(lb) != 1 || len(pb) != 1 {
		t.Fatalf("load=%v paste=%v", lb, pb)
	}
	if lb[0][1] != "-b" || !strings.HasPrefix(lb[0][3], s.terminal.tmuxTmp) {
		t.Fatalf("load-buffer argv = %q", lb[0])
	}
	if !hasArg(pb[0], "-p") || !hasArg(pb[0], "manifest_"+se.ID) || !hasArg(pb[0], lb[0][2]) {
		t.Fatalf("paste-buffer argv = %q", pb[0])
	}
	sk := rec.find("send-keys")
	if len(sk) != 1 || sk[0][len(sk[0])-1] != "Enter" {
		t.Fatalf("send-keys = %v; want only Enter", sk)
	}
	if _, err := os.Stat(lb[0][3]); !os.IsNotExist(err) {
		t.Fatal("paste temp file not removed")
	}
}

// A raw key goes through send-keys -l as-is, with no Enter.
func TestInputKeyRaw(t *testing.T) {
	s, rec := fakeTmuxServer(t)
	se := termSession{ID: "abcdef0123456789", Kind: "claude", ResumeID: "63e55a17-b5b5-464e-8a00-714a410c4422", Started: true, Name: "cc1"}
	s.terminal.upsert(se)
	rec.live, rec.name = true, tmuxName(se.ID)
	w := postInput(t, s, se.ID, map[string]string{"key": "\x03"})
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	sk := rec.find("send-keys")
	if len(sk) != 1 || sk[0][len(sk[0])-1] != "\x03" || !hasArg(sk[0], "-l") {
		t.Fatalf("send-keys = %q", sk)
	}
}

// A send to a dead session relaunches it through the shared spawn
// (`claude --resume <id>` inside the detached new-session), waits for the
// prompt line, then delivers → relaunched:true and the row marked Started.
func TestInputRelaunchesDeadSession(t *testing.T) {
	s, rec := fakeTmuxServer(t)
	se := termSession{ID: "abcdef0123456789", Kind: "claude", ResumeID: "63e55a17-b5b5-464e-8a00-714a410c4422", Started: true, Name: "cc1", Cwd: "/tmp"}
	s.terminal.upsert(se)
	rec.live, rec.name, rec.screen = false, tmuxName(se.ID), "╭───╮\n│ > │\n╰───╯\n"
	termPromptWait, termPromptPoll = 2*time.Second, time.Millisecond

	w := postInput(t, s, se.ID, map[string]string{"text": "continue"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"relaunched":true`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	spawns := rec.find("new-session")
	if len(spawns) != 1 {
		t.Fatalf("spawns = %d", len(spawns))
	}
	argv := strings.Join(spawns[0], " ")
	for _, want := range []string{
		"default-terminal tmux-256color",
		"new-session -d -s manifest_" + se.ID + " -x 120 -y 32 bash -lc",
		"claude --resume " + se.ResumeID,
		"cd '/tmp'",
		"set-option -t manifest_" + se.ID + " status off",
	} {
		if !strings.Contains(argv, want) {
			t.Fatalf("spawn argv missing %q:\n%s", want, argv)
		}
	}
	// the prompt poll ran before the send, and the send came after the spawn
	var iSpawn, iCap, iSend = -1, -1, -1
	for i, c := range rec.calls {
		switch {
		case hasArg(c, "new-session") && iSpawn < 0:
			iSpawn = i
		case c[0] == "capture-pane" && iCap < 0:
			iCap = i
		case c[0] == "send-keys" && iSend < 0:
			iSend = i
		}
	}
	if !(iSpawn < iCap && iCap < iSend) {
		t.Fatalf("order spawn=%d capture=%d send=%d", iSpawn, iCap, iSend)
	}
	if row, _ := s.terminal.find(se.ID); !row.Started {
		t.Fatal("relaunched row not Started")
	}
}

// The WS attach and the detached spawn share one argv definition — same
// prelude, same inner command, same options; only the new-session flags
// differ.
func TestLaunchArgsShared(t *testing.T) {
	s, _ := fakeTmuxServer(t)
	se := termSession{ID: "abcdef0123456789", Kind: "claude", ResumeID: "63e55a17-b5b5-464e-8a00-714a410c4422", Started: true}
	att, err := s.termLaunchArgs(se, true)
	if err != nil {
		t.Fatal(err)
	}
	det, err := s.termLaunchArgs(se, false)
	if err != nil {
		t.Fatal(err)
	}
	strip := func(a []string) string {
		j := strings.Join(a, " ")
		j = strings.Replace(j, "new-session -A -s", "NS", 1)
		j = strings.Replace(j, "new-session -d -s", "NS", 1)
		j = strings.Replace(j, " -x 120 -y 32 bash", " bash", 1)
		return j
	}
	if strip(att) != strip(det) {
		t.Fatalf("attach/detached argv diverge:\n%s\n%s", strip(att), strip(det))
	}
	if !hasArg(att, "-A") || !hasArg(det, "-d") {
		t.Fatal("attach must use -A, spawn -d")
	}
}

// The screen endpoint returns the pane tail (blank tail trimmed, capped)
// and live=false with no lines once the tmux is gone.
func TestScreenTail(t *testing.T) {
	s, rec := fakeTmuxServer(t)
	se := termSession{ID: "abcdef0123456789", Kind: "claude", Name: "cc1"}
	s.terminal.upsert(se)
	rec.live, rec.name = true, tmuxName(se.ID)
	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", i))
		sb.WriteString("\n")
	}
	sb.WriteString("\n\n\n")
	rec.screen = sb.String()
	req := httptest.NewRequest("GET", "/api/terminal/session/"+se.ID+"/screen", nil)
	req.SetPathValue("id", se.ID)
	w := httptest.NewRecorder()
	s.handleTermScreen(w, req)
	var out struct {
		Live  bool     `json:"live"`
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Live || len(out.Lines) != termScreenLines || out.Lines[len(out.Lines)-1] != "line "+strings.Repeat("x", 20) {
		t.Fatalf("screen = %+v", out)
	}
	cap := rec.find("capture-pane")[0]
	if strings.Join(cap, " ") != "capture-pane -p -J -S -12 -t manifest_"+se.ID {
		t.Fatalf("capture argv = %q", cap)
	}
	rec.live = false
	w = httptest.NewRecorder()
	s.handleTermScreen(w, req)
	if !strings.Contains(w.Body.String(), `"live":false`) || !strings.Contains(w.Body.String(), `"lines":[]`) {
		t.Fatalf("dead screen = %s", w.Body.String())
	}
}
