package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// argvDumpStub records every argument (one per line) and answers briefly.
func argvDumpStub(dump string) string {
	return `#!/bin/sh
for a in "$@"; do printf '%s\n' "$a"; done > ` + dump + `
usage=""
while [ $# -gt 0 ]; do case "$1" in --usage-file) usage="$2"; shift 2;; *) shift;; esac; done
printf 'noted\n'
[ -n "$usage" ] && printf '{"estimated_cost_usd":0,"model":"stub","session_id":"s1"}' > "$usage"
exit 0
`
}

func waitFile(t *testing.T, path, want string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), want) {
			return string(b)
		}
		time.Sleep(30 * time.Millisecond)
	}
	b, _ := os.ReadFile(path)
	t.Fatalf("%s never contained %q:\n%s", path, want, b)
	return ""
}

// /goal on a native agent: the objective persists on the session, rides every
// turn's prompt while active, stays out while paused, and clears; the owner's
// per-message model, provider and effort reach Hermes as its own flags.
func TestNativeGoalRidesTurnsAndRecipientFlagsReachHermes(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "argv")
	s, st, _ := agentChatFixture(t, argvDumpStub(dump))
	home := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "provider_models_cache.json"), []byte(`{"xai-oauth":{"models":["grok-4.6"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, r := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions", map[string]any{"text": "start", "recipient": map[string]any{"agent": "alfred", "model": "grok-4.6", "provider": "xai-oauth", "effort": "high"}})
	if code != 200 {
		t.Fatalf("create: %d %+v", code, r)
	}
	id := r["id"].(string)
	argv := waitFile(t, dump, "--reasoning")
	if !strings.Contains(argv, "-m\ngrok-4.6\n--provider\nxai-oauth\n--reasoning\nhigh\n") {
		t.Fatalf("first message must carry the chosen model, provider and effort:\n%s", argv)
	}
	waitIdle(t, st, "alfred", id)
	if code, _ := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions", map[string]any{"text": "x", "recipient": map[string]any{"agent": "alfred", "model": "gpt-9", "provider": "xai-oauth"}}); code != 400 {
		t.Fatalf("an unlisted provider/model pair must be refused before any turn: %d", code)
	}
	goal := "Ship the tiles release with every test green"
	if code, r := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+id+"/goal", map[string]any{"goal": goal}); code != 200 || r["goalState"] != "active" {
		t.Fatalf("set goal: %d %+v", code, r)
	}
	if sess, _, _, _ := st.Get("alfred", id); sess.Goal != goal || sess.GoalState != "active" {
		t.Fatalf("goal not persisted: %+v", sess)
	}
	send := func(text string) string {
		_ = os.Remove(dump)
		if code, r := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+id+"/messages", map[string]any{"text": text}); code != 200 {
			t.Fatalf("send: %d %+v", code, r)
		}
		out := waitFile(t, dump, text)
		waitIdle(t, st, "alfred", id)
		return out
	}
	if out := send("next step"); !strings.Contains(out, "Standing goal the owner set for this conversation") || !strings.Contains(out, goal) {
		t.Fatalf("active goal missing from the turn:\n%s", out)
	}
	if code, _ := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+id+"/goal", map[string]any{"goal": goal, "state": "paused"}); code != 200 {
		t.Fatal("pause")
	}
	if out := send("paused step"); strings.Contains(out, goal) {
		t.Fatalf("a paused goal was sent:\n%s", out)
	}
	if code, r := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+id+"/goal", map[string]any{"goal": ""}); code != 200 || r["goal"] != "" {
		t.Fatalf("clear: %d %+v", code, r)
	}
	if sess, _, _, _ := st.Get("alfred", id); sess.Goal != "" || sess.GoalState != "" {
		t.Fatalf("goal not cleared: %+v", sess)
	}
	if code, _ := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+id+"/goal", map[string]any{"goal": strings.Repeat("x", 2001)}); code != 400 {
		t.Fatal("an oversized goal must be refused")
	}
	if code, _ := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/nope/goal", map[string]any{"goal": "g"}); code != 404 {
		t.Fatal("unknown session")
	}
}
