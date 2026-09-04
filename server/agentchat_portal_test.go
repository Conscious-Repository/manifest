package server

import (
	"strings"
	"testing"
	"time"

	"manifest/chatthreads"
)

// Phase 2: the cockpit drives Kairos over the portal's chat store through the
// /api/agents/chat/<agent>/… verbs — the same shapes the Alfred routes answer
// with — and the reply returns through the existing chatSweep on read.
func TestPortalChatKairosInTheCockpit(t *testing.T) {
	srv, item := chatFixture(t) // kairos harness + AION chat store; NO Hermes-family store
	kairos := srv.findHarness("kairos").Spirits

	// roster: no Alfred store here, still 200, and kairos is a portal section
	code, r := agentChatJSON(t, srv, "GET", "/api/agents/chat/roster", nil)
	if code != 200 {
		t.Fatalf("roster: %d %+v", code, r)
	}
	var kentry map[string]any
	for _, a := range r["agents"].([]any) {
		if m := a.(map[string]any); m["name"] == "kairos" {
			kentry = m
		}
	}
	if kentry == nil || kentry["backend"] != "portal" || kentry["domain"] != "aion" || kentry["enabled"] != true {
		t.Fatalf("kairos roster entry: %+v", kentry)
	}
	if ps, _ := kentry["personas"].([]any); len(ps) == 0 {
		t.Fatalf("kairos entry must carry the persona intents for the @-typeahead: %+v", kentry)
	}

	// create + first send in one call: a REAL order lands in the kairos spool
	// carrying the [chat::] token, the persona, and the ask protocol
	code, r = agentChatJSON(t, srv, "POST", "/api/agents/chat/kairos/sessions",
		map[string]any{"text": "@kairos::brief what's the setback?", "context": []string{"aion:" + item}})
	if code != 200 || r["status"] != "thinking" {
		t.Fatalf("create: %d %+v", code, r)
	}
	id, _ := r["id"].(string)
	q := kairos.Queued()
	if len(q) != 1 {
		t.Fatalf("one order queued: %+v", q)
	}
	for _, want := range []string{"[chat:: " + id + "#", "canonize the zoning memo", "read-only ask", "PERSONA"} {
		if !strings.Contains(q[0].Request, want) {
			t.Fatalf("order missing %q:\n%s", want, q[0].Request)
		}
	}
	// the owner's message is in the portal store, attributed
	if msgs := srv.chat.Messages(id); len(msgs) != 1 || msgs[0].Kind != "ask" {
		t.Fatalf("member message: %+v", msgs)
	}

	// list: the thread shows thinking (its order is pending) and the title is
	// the first line
	code, r = agentChatJSON(t, srv, "GET", "/api/agents/chat/kairos/sessions", nil)
	sessions, _ := r["sessions"].([]any)
	if code != 200 || len(sessions) != 1 {
		t.Fatalf("list: %d %+v", code, r)
	}
	if s0 := sessions[0].(map[string]any); s0["id"] != id || s0["status"] != "thinking" || s0["agent"] != "kairos" {
		t.Fatalf("session row: %+v", s0)
	}

	// one run at a time: a second order while one is active is a 409
	code, _ = agentChatJSON(t, srv, "POST", "/api/agents/chat/kairos/sessions/"+id+"/messages", map[string]any{"text": "and the height?"})
	if code != 409 {
		t.Fatalf("second send while active must be 409, got %d", code)
	}
	code, _ = agentChatJSON(t, srv, "POST", "/api/agents/chat/kairos/sessions", map[string]any{"text": "new thread while busy"})
	if code != 409 {
		t.Fatalf("create-with-send while active must be 409, got %d", code)
	}
	if n := len(srv.chat.Threads()); n != 1 {
		t.Fatalf("a refused first send must leave no thread behind: %d threads", n)
	}

	// the runner answers; the READ sweeps it in — no new ticker
	orderID := srv.chat.Pending()[0].OrderID
	fakeChatRun(t, srv, "cr7", id, orderID, "ask", "The setback is 25 ft — see zoning.md.")
	code, r = agentChatJSON(t, srv, "GET", "/api/agents/chat/kairos/sessions/"+id, nil)
	if code != 200 {
		t.Fatalf("get: %d %+v", code, r)
	}
	body, _ := r["body"].(string)
	for _, want := range []string{"## Turn 1 — user · ", "@kairos::brief what's the setback?", "## Turn 2 — kairos · ",
		"### Step 1 — ask", "- result: completed", "### Step 2 — say", "The setback is 25 ft"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if sess := r["session"].(map[string]any); sess["status"] != "idle" || sess["turns"] != float64(2) {
		t.Fatalf("after the reply the session must be idle with 2 turns: %+v", sess)
	}

	// rename (portal rule: titles are lower-case), then delete = archive:
	// gone from the rail, still readable
	if code, _ = agentChatJSON(t, srv, "POST", "/api/agents/chat/kairos/sessions/"+id+"/rename", map[string]any{"title": "Setbacks"}); code != 200 {
		t.Fatalf("rename: %d", code)
	}
	if th, _ := portalChatThread(srv.kairosAgent(), id); th.Title != "setbacks" {
		t.Fatalf("title = %q", th.Title)
	}
	if code, _ = agentChatJSON(t, srv, "DELETE", "/api/agents/chat/kairos/sessions/"+id, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	_, r = agentChatJSON(t, srv, "GET", "/api/agents/chat/kairos/sessions", nil)
	if sessions, _ := r["sessions"].([]any); len(sessions) != 0 {
		t.Fatalf("archived thread must leave the rail: %+v", sessions)
	}
	if code, _ = agentChatJSON(t, srv, "GET", "/api/agents/chat/kairos/sessions/"+id, nil); code != 200 {
		t.Fatalf("archived thread must stay readable: %d", code)
	}

	// zeck is a reserved slug: unwired here → 503, never a Hermes-profile 400
	if code, _ = agentChatJSON(t, srv, "GET", "/api/agents/chat/zeck/sessions", nil); code != 503 {
		t.Fatalf("unwired zeck must be 503, got %d", code)
	}
	// engine + attach exist only on the portal side
	if code, _ = agentChatJSON(t, srv, "GET", "/api/agents/chat/kairos/engine", nil); code != 200 {
		t.Fatalf("engine: %d", code)
	}
	if code, _ = agentChatJSON(t, srv, "GET", "/api/agents/chat/alfred/engine", nil); code != 404 {
		t.Fatalf("engine on a Hermes agent must be 404, got %d", code)
	}
}

// The transcript body is the shared grammar: a teammate's message carries
// their name, the owner's does not; attachments ride as [file::] lines; a
// failed run is a spirit turn that says so; proposals list with their state.
func TestPortalChatBodySharedGrammar(t *testing.T) {
	srv, _ := chatFixture(t)
	ag := srv.kairosAgent()
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	_, _ = srv.chat.CreateThread("th/g", "g", "", chatthreads.Identity{ID: "ben@aion.bio", Name: "Ben"}, now)
	_, _ = srv.chat.AddMessage(chatthreads.Message{Thread: "th/g", Kind: "ask", Author: "jack@aion.bio", AuthName: "Jack", Text: "can we start?",
		Files: []chatthreads.FileRef{{Hash: strings.Repeat("a", 64), Name: "site].pdf"}}, At: now}, now)
	_, _ = srv.chat.AddMessage(chatthreads.Message{Thread: "th/g", Kind: "ask", Author: "owner", AuthName: "Benjamin", Text: "yes", At: now.Add(time.Minute)}, now)
	_, _ = srv.chat.AddMessage(chatthreads.Message{Thread: "th/g", Kind: "kairos", Author: "agent:kairos", AuthName: "Kairos", Text: "timed out",
		Ritual: "delegate", Outcome: "failed", Elapsed: "10m 0s", Run: "r1", At: now.Add(2 * time.Minute)}, now)
	_, _ = srv.chat.AddMessage(chatthreads.Message{Thread: "th/g", Kind: "kairos", Author: "agent:kairos", AuthName: "Kairos", Text: "done",
		Ritual: "delegate", Outcome: "completed", Run: "r2", At: now.Add(3 * time.Minute),
		Props: []chatthreads.Proposal{{Type: "set-field", ItemID: "aion:x", Field: "status", Value: "done", State: "pending"}}}, now)
	body := portalChatBody(ag, srv.chat.Messages("th/g"), "owner")
	for _, want := range []string{
		"## Turn 1 — user · 2026-09-04T12:00:00Z\n\nJack — can we start?\n[file:: " + strings.Repeat("a", 64) + " site).pdf]",
		"## Turn 2 — user · 2026-09-04T12:01:00Z\n\nyes\n",
		"## Turn 3 — kairos · ", "### Step 1 — delegate\n\n- result: failed · 10m 0s", "⚠ Kairos's run failed — timed out",
		"**Proposals** (decide in the AION team portal):", "- set `status` → `done` on `aion:x` — pending",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Benjamin — yes") {
		t.Fatalf("the owner's own message must not carry a name prefix:\n%s", body)
	}
}
