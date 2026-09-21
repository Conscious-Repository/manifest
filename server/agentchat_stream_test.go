package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"manifest/agentchat"
)

func TestAgentChatStreamFileEvents(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	id, err := st.Create("alfred", "", "stream", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.AppendTurn("alfred", id, "user", "hello", 0); err != nil {
		t.Fatal(err)
	}
	if err = st.SetStatus("alfred", id, agentchat.StatusThinking); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/agents/chat/alfred/sessions/"+id+"/stream?after=-1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers: %v %v", resp.StatusCode, resp.Header)
	}
	scan := bufio.NewScanner(resp.Body)
	seq := 0
	read := func(want string) map[string]any {
		t.Helper()
		lines := []string{}
		for scan.Scan() {
			if scan.Text() == "" {
				break
			}
			lines = append(lines, scan.Text())
		}
		if len(lines) != 3 || lines[1] != "event: "+want {
			t.Fatalf("want %s, got %v (%v)", want, lines, scan.Err())
		}
		var ev struct {
			Seq  int            `json:"seq"`
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[2], "data: ")), &ev); err != nil {
			t.Fatal(err)
		}
		seq++
		if ev.Seq != seq || ev.Type != want {
			t.Fatalf("bad envelope: %+v", ev)
		}
		return ev.Data
	}
	read("turn.started")
	_, err = st.AppendTurn("alfred", id, "alfred", "### Step 1 — thinking\n\n- tokens: 42\n\nRecorded reasoning\n\n### Step 2 — search\n\n- result: found\n\n### Step 3 — say\n\nHello", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SetStatus("alfred", id, agentchat.StatusIdle); err != nil {
		t.Fatal(err)
	}
	if d := read("thinking.tokens"); d["tokens"] != float64(42) {
		t.Fatal(d)
	}
	if d := read("thinking.delta"); d["text"] != "Recorded reasoning" {
		t.Fatal(d)
	}
	if d := read("tool.started"); d["cast"] != "search" || d["rationale"] != "found" {
		t.Fatal(d)
	}
	read("tool.completed")
	if d := read("assistant.delta"); d["text"] != "Hello" {
		t.Fatal(d)
	}
	read("turn.completed")
	_, err = st.AppendTurn("alfred", id, "system", "failed", 0)
	if err != nil {
		t.Fatal(err)
	}
	read("turn.started")
	read("error")
	read("session.error")
	read("turn.completed")
}

func TestAgentChatStreamMissingSession(t *testing.T) {
	s, _, _ := agentChatFixture(t, echoStub)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/agents/chat/alfred/sessions/invalid/stream", nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestAgentChatReasoningUsageRecorded(t *testing.T) {
	script := strings.Replace(echoStub, `"estimated_cost_usd":0.0042`, `"reasoning_tokens":73,"estimated_cost_usd":0.0042`, 1)
	s, st, _ := agentChatFixture(t, script)
	id, err := st.Create("alfred", "", "reasoning", "")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := st.Accept("alfred", id, "reasoning-test", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, claimed, err := st.Claim("alfred", id)
	if err != nil || !claimed {
		t.Fatalf("claim %v %v", claimed, err)
	}
	if err = s.runAgentChatTurn("alfred", id, accepted.Delivery.ID); err != nil {
		t.Fatal(err)
	}
	_, body, _, _ := st.Get("alfred", id)
	if !strings.Contains(body, "- tokens: 73") || !strings.Contains(body, "### Step 2 — say") {
		t.Fatal(body)
	}
}

func TestAgentChatTailReconnectAndQueuedTurns(t *testing.T) {
	s := agentchat.Session{Turns: 2, Status: agentchat.StatusThinking}
	old := "## Turn 1 — user · 2026-09-21T00:00:00Z\n\nold\n\n## Turn 2 — alfred · 2026-09-21T00:00:01Z\n\n### Step 1 — say\n\nold reply"
	tail := agentChatTail{lastTurn: s.Turns}
	var events []string
	send := func(kind string, _ map[string]any) { events = append(events, kind) }
	tail.advance(s, old, send)
	tail.advance(s, old, send)
	if strings.Join(events, ",") != "turn.started" {
		t.Fatal(events)
	}
	// Two fast replies between ticks are both delivered, even when the
	// intermediate idle flag was never observed.
	body := old + "\n\n## Turn 3 — user · 2026-09-21T00:00:02Z\n\none\n\n## Turn 4 — alfred · 2026-09-21T00:00:03Z\n\n### Step 1 — say\n\none\n\n## Turn 5 — user · 2026-09-21T00:00:04Z\n\ntwo\n\n## Turn 6 — alfred · 2026-09-21T00:00:05Z\n\n### Step 1 — say\n\ntwo"
	s.Status = agentchat.StatusIdle
	s.Turns = 6
	tail.advance(s, body, send)
	count := len(events)
	tail.advance(s, body, send)
	if len(events) != count || strings.Count(strings.Join(events, ","), "turn.completed") != 2 {
		t.Fatal(events)
	}
}
