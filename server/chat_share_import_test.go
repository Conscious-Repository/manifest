package server

import (
	"encoding/json"
	"manifest/agentchat"
	"manifest/artifacts"
	"manifest/chatthreads"
	"strings"
	"testing"
	"time"
)

func TestSharingImportPreservesHistoryAuthorsAndFiles(t *testing.T) {
	created := "2026-09-10T12:00:00Z"
	source := agentchat.Session{Agent: "kairos-private", ID: "20260910-120000-abcd", Title: "Shared research", Created: created, Status: agentchat.StatusIdle}
	source.Origin = &agentchat.Origin{Context: "Retained origin context"}
	long := strings.Repeat("Full retained message. ", 400)
	hash := artifacts.Hash([]byte("file"))
	body := "## Turn 1 — user · " + created + "\n\n" + long + "\n[file:: " + hash + " reviewed.md]\n\n## Turn 2 — kairos-private · 2026-09-10T12:01:00Z\n\n### Step 1 — search\n\nFULL_TOOL_TRACE\n\n### Step 2 — say\n\nResearch reply\n"
	views := []codingContinuationView{{ID: "native-one", Agent: "codex", Turns: []termTurn{{ID: "reply-one", Who: "assistant", TS: "2026-09-10T12:02:00Z", Blocks: []termBlock{{T: "step", Cast: "exec", Result: "NATIVE_TRACE"}, {T: "say", Text: "Code reply"}}}}}}
	r := chatShareReview{FutureMessages: true, OwnerEmail: "owner@aion.bio", OwnerName: "Owner", TargetAgent: "kairos", Audience: "AION team", Session: source, Body: body, SourceRevision: agentchat.ShareRevision(source, body), Continuations: views, Timeline: conversationTimeline(source, body, views), Files: []chatShareFile{{Hash: hash, Name: "reviewed.md", Size: 4, References: []string{"turn:1"}}}}
	encoded, _ := json.Marshal(r)
	r.Revision = artifacts.Hash(encoded)
	thread, messages, err := buildChatShareImport(r, "imported-thread")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || thread.SharedSource.ID != source.ID || thread.ImportRevision != r.Revision {
		t.Fatal(thread, messages)
	}
	if messages[0].Source.TimestampKnown || messages[0].Text != source.Origin.Context {
		t.Fatal("invented origin time or dropped context")
	}
	if !strings.Contains(messages[1].Text, strings.TrimSpace(long)) || strings.Contains(messages[1].Text, "[file::") || len(messages[1].Files) != 1 {
		t.Fatal("lost message or attachment")
	}
	if messages[2].Kind != "agent" || messages[2].Author != "agent:kairos-private" || !strings.Contains(string(messages[2].Source.Record), "FULL_TOOL_TRACE") {
		t.Fatal("lost agent identity or original trace", messages[2])
	}
	if messages[3].Kind != "agent" || messages[3].Author != "agent:codex" || messages[3].Text != "Code reply" || !strings.Contains(string(messages[3].Source.Record), "NATIVE_TRACE") {
		t.Fatal("lost native author or original blocks", messages[3])
	}
	_, again, err := buildChatShareImport(r, thread.ID)
	a, _ := json.Marshal(messages)
	b, _ := json.Marshal(again)
	if err != nil || string(a) != string(b) {
		t.Fatal("import changes on retry")
	}
	store, err := chatthreads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportSharedThread(thread, messages, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportSharedThread(thread, again, time.Now()); err != nil {
		t.Fatal(err)
	}
	got := store.Messages(thread.ID)
	if len(got) != 4 || got[1].Text != messages[1].Text {
		t.Fatal("store truncated or duplicated import")
	}
	ag := &chatAgent{Name: "kairos", Display: "Kairos", Domain: "aion"}
	projected := portalChatBody(ag, got, "owner@aion.bio")
	if !strings.Contains(projected, "— codex ·") || strings.Contains(projected, "user · 2026-09-10T12:02:00Z") {
		t.Fatal("native assistant mislabeled as user", projected)
	}
	r.OwnerEmail = "changed@aion.bio"
	if _, _, err := buildChatShareImport(r, thread.ID); err == nil {
		t.Fatal("changed attribution accepted")
	}
}

func TestStoredReviewDigestSurvivesSchemaAdditions(t *testing.T) {
	// Deliberately non-alphabetic field order with a nested revision field.
	unsigned := `{"extra":{"revision":"nested","futureField":1234567890123456789},"revision":"","futureMessages":true}`
	digest := artifacts.Hash([]byte(unsigned))
	signed := strings.Replace(unsigned, `"revision":""`, `"revision":"`+digest+`"`, 1)
	if !validStoredShareReview([]byte(signed)) {
		t.Fatal("unknown field or original order invalidated digest")
	}
	if validStoredShareReview([]byte(strings.Replace(signed, "futureMessages\":true", "futureMessages\":false", 1))) {
		t.Fatal("changed review accepted")
	}
	if validStoredShareReview([]byte(strings.Replace(signed, `"futureMessages":true`, `"futureMessages":true,"futureMessages":false`, 1))) {
		t.Fatal("ambiguous duplicate key accepted")
	}
}
