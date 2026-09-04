package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/agentchat"
	"manifest/hermes"
	"manifest/ledger"
)

// agentChatFixture: a server with the agent-chat store rooted in a temp
// harness tree, a stub `hermes` that echoes its prompt tail and writes a usage
// report (session_id + cost), and a wired ledger. No live Hermes.
func agentChatFixture(t *testing.T, script string) (*Server, *agentchat.Store, *ledger.Store) {
	t.Helper()
	root := t.TempDir()
	stub := filepath.Join(t.TempDir(), "hermes")
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil)
	s.UseHermes(hermes.NewRunner(hermes.Config{Enabled: true, Bin: stub}), "web,memory")
	// the roster/profile lookups shell out to hosts.Hermes.Bin — point it at
	// the same stub so no test ever reaches a real `hermes` on $PATH
	var hosts HostsInfo
	hosts.Hermes.Enabled, hosts.Hermes.Bin = true, stub
	s.UseHosts(hosts)
	st := agentchat.New(filepath.Join(root, "artifacts", "chats"))
	s.UseAgentChat(st)
	led, err := ledger.New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	s.UseLedger(led)
	return s, st, led
}

// echoStub replies with the -p/-z argv it saw (so the test can assert the
// profile + prompt window) and writes a usage file naming a session.
const echoStub = `#!/bin/sh
usage=""
profile="(default)"
prompt=""
while [ $# -gt 0 ]; do
  case "$1" in
    -p) profile="$2"; shift 2;;
    -z) prompt="$2"; shift 2;;
    --usage-file) usage="$2"; shift 2;;
    *) shift;;
  esac
done
sleep 0.3
printf 'profile=%s\nREPLY to: %s\n' "$profile" "$(printf '%s' "$prompt" | tail -n 3 | head -n 1)"
[ -n "$usage" ] && printf '{"estimated_cost_usd":0.0042,"model":"stub-model","session_id":"20260904_140000_ab12cd"}' > "$usage"
exit 0
`

func agentChatJSON(t *testing.T, s *Server, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(method, path, rd))
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func waitIdle(t *testing.T, st *agentchat.Store, agent, id string) agentchat.Session {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		sess, _, _, ok := st.Get(agent, id)
		if ok && sess.Status == agentchat.StatusIdle && !st.InFlight(agent, id) {
			return sess
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("session never went idle")
	return agentchat.Session{}
}

func TestAgentChatAlfredTurnLandsReplyAndLedger(t *testing.T) {
	s, st, led := agentChatFixture(t, echoStub)

	code, r := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions", map[string]any{"text": "find me 10 gutter contractors"})
	if code != 200 || r["id"] == nil {
		t.Fatalf("create: %d %+v", code, r)
	}
	id := r["id"].(string)
	if r["status"] != "thinking" {
		t.Errorf("create status = %v, want thinking", r["status"])
	}
	// the file exists in the harness tree, flagged thinking, with the user turn
	path := filepath.Join(st.Root(), "alfred", id+".md")
	if b, err := os.ReadFile(path); err != nil {
		t.Fatalf("session file: %v", err)
	} else if !strings.Contains(string(b), "status: thinking") || !strings.Contains(string(b), "## Turn 1 — user · ") {
		t.Errorf("file mid-turn:\n%s", b)
	}
	code, r = agentChatJSON(t, s, "GET", "/api/agents/chat/alfred/sessions/"+id, nil)
	if code != 200 || r["session"].(map[string]any)["status"] != "thinking" {
		t.Errorf("get mid-turn: %d %+v", code, r)
	}

	sess := waitIdle(t, st, "alfred", id)
	if sess.Turns != 2 || sess.Title != "find me 10 gutter contractors" {
		t.Errorf("session = %+v", sess)
	}
	if sess.SpentUSD < 0.0041 || sess.SpentUSD > 0.0043 {
		t.Errorf("spent = %v", sess.SpentUSD)
	}
	if sess.HermesSession != "20260904_140000_ab12cd" {
		t.Errorf("hermes session = %q", sess.HermesSession)
	}
	_, body, _, _ := st.Get("alfred", id)
	turns := agentchat.ParseTurns(body)
	if len(turns) != 2 || turns[1].Who != "alfred" || turns[1].USD != "0.0042" {
		t.Fatalf("turns = %+v", turns)
	}
	reply := agentchat.SayBody(turns[1].Text)
	if !strings.Contains(reply, "profile=(default)") { // alfred → no -p
		t.Errorf("alfred must run under the default profile: %q", reply)
	}
	if !strings.Contains(reply, "find me 10 gutter contractors") {
		t.Errorf("prompt window missing the user message: %q", reply)
	}
	// the file is idle again and carries the say step the renderer expects
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "status: idle") || !strings.Contains(string(b), "### Step 1 — say") {
		t.Errorf("file after turn:\n%s", b)
	}

	// ledger: chat.user + chat.assistant with the Hermes session id
	entries, err := led.Day(led.Today())
	if err != nil {
		t.Fatal(err)
	}
	var user, assistant *ledger.Entry
	for i := range entries {
		switch entries[i].Kind {
		case "chat.user":
			user = &entries[i]
		case "chat.assistant":
			assistant = &entries[i]
		}
	}
	if user == nil || user.Session != id {
		t.Errorf("chat.user entry = %+v", user)
	}
	if assistant == nil || assistant.Session != id || assistant.Actor != "agent:alfred" {
		t.Fatalf("chat.assistant entry = %+v", assistant)
	}
	if assistant.Meta["sessionId"] != "20260904_140000_ab12cd" || assistant.Meta["model"] != "stub-model" {
		t.Errorf("assistant meta = %+v", assistant.Meta)
	}

	// list + roster
	code, r = agentChatJSON(t, s, "GET", "/api/agents/chat/alfred/sessions", nil)
	if code != 200 || len(r["sessions"].([]any)) != 1 {
		t.Errorf("list: %d %+v", code, r)
	}
	code, r = agentChatJSON(t, s, "GET", "/api/agents/chat/roster", nil)
	agents := r["agents"].([]any)
	if code != 200 || len(agents) < 1 || agents[0].(map[string]any)["name"] != "alfred" {
		t.Errorf("roster: %d %+v", code, r)
	}
	if agents[0].(map[string]any)["sessions"] != float64(1) {
		t.Errorf("roster alfred sessions = %v", agents[0].(map[string]any)["sessions"])
	}
}

func TestAgentChatSecondSendQueuesAndDrains(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	_, r := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions", map[string]any{"text": "first"})
	id := r["id"].(string)
	code, r := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+id+"/messages", map[string]any{"text": "second"})
	if code != 200 || r["queued"] != float64(1) {
		t.Fatalf("second send: %d %+v", code, r)
	}
	_, r = agentChatJSON(t, s, "GET", "/api/agents/chat/alfred/sessions/"+id, nil)
	if q := r["queued"].([]any); len(q) != 1 || q[0] != "second" {
		t.Errorf("queued mid-turn = %v", q)
	}
	// delete/rename-while-thinking guards
	if code, _ := agentChatJSON(t, s, "DELETE", "/api/agents/chat/alfred/sessions/"+id, nil); code != 400 {
		t.Errorf("delete while thinking = %d, want 400", code)
	}
	sess := waitIdle(t, st, "alfred", id)
	if sess.Turns != 4 {
		t.Fatalf("turns = %d, want 4 (user, alfred, user, alfred)", sess.Turns)
	}
	_, body, queued, _ := st.Get("alfred", id)
	if len(queued) != 0 {
		t.Errorf("queue not drained: %v", queued)
	}
	turns := agentchat.ParseTurns(body)
	if turns[2].Who != "user" || turns[2].Text != "second" || turns[3].Who != "alfred" {
		t.Errorf("drained turns = %+v", turns)
	}
	// the second turn's window carried both exchanges
	if !strings.Contains(agentchat.SayBody(turns[3].Text), "second") {
		t.Errorf("second reply window: %q", turns[3].Text)
	}
	// now idle: rename + delete work
	if code, _ := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+id+"/rename", map[string]any{"title": "gutters"}); code != 200 {
		t.Errorf("rename = %d", code)
	}
	if code, _ := agentChatJSON(t, s, "DELETE", "/api/agents/chat/alfred/sessions/"+id, nil); code != 200 {
		t.Errorf("delete = %d", code)
	}
	if code, _ := agentChatJSON(t, s, "GET", "/api/agents/chat/alfred/sessions/"+id, nil); code != 404 {
		t.Errorf("get after delete = %d", code)
	}
}

// A profile agent runs `hermes -p <profile> -z …`; an unknown agent is refused
// (never silently routed to the default profile).
func TestAgentChatProfileTargetsDashP(t *testing.T) {
	// the stub also answers `profile list` so the roster/resolve see "scout"
	script := `#!/bin/sh
if [ "$1" = "profile" ]; then
  printf 'Profile   Model   Gateway   Alias   Distribution\n'
  printf '◆ default   claude-x   —   —   —\n'
  printf 'scout   gpt-5   —   scout   —\n'
  exit 0
fi
` + strings.TrimPrefix(echoStub, "#!/bin/sh\n")
	s, st, _ := agentChatFixture(t, script)

	_, r := agentChatJSON(t, s, "GET", "/api/agents/chat/roster", nil)
	agents := r["agents"].([]any)
	if len(agents) != 2 || agents[1].(map[string]any)["name"] != "scout" || agents[1].(map[string]any)["profile"] != "scout" {
		t.Fatalf("roster = %+v", agents)
	}
	if agents[0].(map[string]any)["model"] != "claude-x" {
		t.Errorf("alfred model should come from the default row: %+v", agents[0])
	}

	code, r := agentChatJSON(t, s, "POST", "/api/agents/chat/scout/sessions", map[string]any{"text": "scan"})
	if code != 200 {
		t.Fatalf("create scout: %d %+v", code, r)
	}
	id := r["id"].(string)
	waitIdle(t, st, "scout", id)
	_, body, _, _ := st.Get("scout", id)
	turns := agentchat.ParseTurns(body)
	if len(turns) != 2 || turns[1].Who != "scout" || !strings.Contains(turns[1].Text, "profile=scout") {
		t.Errorf("scout turn = %+v", turns)
	}
	if _, err := os.Stat(filepath.Join(st.Root(), "scout", id+".md")); err != nil {
		t.Errorf("scout file: %v", err)
	}

	if code, _ := agentChatJSON(t, s, "POST", "/api/agents/chat/nobody/sessions", map[string]any{"text": "hi"}); code != 400 {
		t.Errorf("unknown agent create = %d, want 400", code)
	}
	if code, _ := agentChatJSON(t, s, "POST", "/api/agents/chat/Bad.Name/sessions", map[string]any{"text": "hi"}); code != 400 {
		t.Errorf("bad agent name create = %d, want 400", code)
	}
}

// Audit 2026-09-04: with the runner off, a first send is refused BEFORE the
// session file exists (no empty "new conversation" in the rail), and the
// startup repair never runs — the store syncs across devices, so a box that
// cannot own a turn must not rewrite a session another box has in flight.
func TestAgentChatRunnerOffRefusesCreateAndSkipsRecover(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chats")
	seed := agentchat.New(root)
	id, err := seed.Create("alfred", "", "in flight elsewhere", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = seed.AppendTurn("alfred", id, "user", "hello", 0)
	_ = seed.SetStatus("alfred", id, agentchat.StatusThinking)

	s := New(nil, nil, nil)
	st := agentchat.New(root)
	s.UseAgentChat(st) // runner NOT wired
	if sess, _, _, _ := st.Get("alfred", id); sess.Status != agentchat.StatusThinking || sess.Turns != 1 {
		t.Fatalf("recover must not run with the runner off: %+v", sess)
	}
	code, _ := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions", map[string]any{"text": "hi"})
	if code != 400 {
		t.Fatalf("create with text, runner off = %d, want 400", code)
	}
	if l := st.List("alfred"); len(l) != 1 {
		t.Fatalf("a refused first send must not leave a session behind: %+v", l)
	}
	// the roster still answers (never a 503) and says the runner is off
	code, r := agentChatJSON(t, s, "GET", "/api/agents/chat/roster", nil)
	if code != 200 || r["agents"].([]any)[0].(map[string]any)["enabled"] != false {
		t.Fatalf("roster: %d %+v", code, r)
	}

	// wiring the runner afterwards runs the repair once
	stub := filepath.Join(t.TempDir(), "hermes")
	if err := os.WriteFile(stub, []byte(echoStub), 0o755); err != nil {
		t.Fatal(err)
	}
	s.UseHermes(hermes.NewRunner(hermes.Config{Enabled: true, Bin: stub}), "web")
	sess, _, _, _ := st.Get("alfred", id)
	if sess.Status != agentchat.StatusIdle || sess.Turns != 2 {
		t.Fatalf("recover must run once the runner is wired: %+v", sess)
	}
}

func TestAgentChatRunnerFailureLandsSystemTurn(t *testing.T) {
	s, st, _ := agentChatFixture(t, "#!/bin/sh\necho 'boom' >&2\nexit 3\n")
	_, r := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions", map[string]any{"text": "hi"})
	id := r["id"].(string)
	waitIdle(t, st, "alfred", id)
	_, body, _, _ := st.Get("alfred", id)
	turns := agentchat.ParseTurns(body)
	if len(turns) != 2 || turns[1].Who != "system" || !strings.Contains(turns[1].Text, "boom") {
		t.Errorf("turns = %+v", turns)
	}
}
