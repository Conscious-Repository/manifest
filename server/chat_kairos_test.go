package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time"

	"manifest/chatthreads"
)

func chatFixture(t *testing.T) (*Server, string) {
	t.Helper()
	srv, item := kairosFixture(t)
	store, err := chatthreads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.UseChatThreads(store)
	return srv, item
}

func TestChatAskComposesRunOrder(t *testing.T) {
	srv, item := chatFixture(t)
	kairos := srv.findHarness("kairos").Spirits
	_, _ = srv.chat.CreateThread("th/x", "pig site", "", chatthreads.Identity{ID: "ben@aion.bio", Name: "Ben"}, time.Now())
	if err := srv.AionChatAsk("th/x", "@kairos::brief what's the setback?", "ask",
		[]string{"aion:" + item}, "ben@aion.bio", "Benjamin Anderson"); err != nil {
		t.Fatal(err)
	}
	q := kairos.Queued()
	if len(q) != 1 {
		t.Fatalf("one order queued: %+v", q)
	}
	req := q[0].Request
	// resolves the item id to real content (not the raw id), carries the chat
	// token + the ask protocol + the persona
	for _, want := range []string{"canonize the zoning memo", "[chat:: th/x#", "read-only ask", "PERSONA"} {
		if !strings.Contains(req, want) {
			t.Fatalf("request missing %q:\n%s", want, req)
		}
	}
	if strings.Contains(req, "CHANGES PROTOCOL") {
		t.Fatalf("ask must not carry the delegate protocol:\n%s", req)
	}
	// the member message + a pending entry are recorded
	if msgs := srv.chat.Messages("th/x"); len(msgs) != 1 || msgs[0].Kind != "ask" {
		t.Fatalf("member message: %+v", msgs)
	}
	if len(srv.chat.Pending()) != 1 {
		t.Fatalf("pending: %+v", srv.chat.Pending())
	}
}

func TestChatDelegateProtocolAndSweep(t *testing.T) {
	srv, item := chatFixture(t)
	_, _ = srv.chat.CreateThread("th/y", "plan", "", chatthreads.Identity{ID: "ben@aion.bio", Name: "Ben"}, time.Now())
	if err := srv.AionChatAsk("th/y", "tighten the plan", "delegate", []string{"aion:" + item}, "ben@aion.bio", "Ben"); err != nil {
		t.Fatal(err)
	}
	kairos := srv.findHarness("kairos").Spirits
	if !strings.Contains(kairos.Queued()[0].Request, "CHANGES PROTOCOL") {
		t.Fatal("delegate must carry the changes protocol")
	}
	// simulate the runner: a completed run carrying the SAME order id the ask
	// spooled (one order → one run → one pending) + a proposal
	orderID := srv.chat.Pending()[0].OrderID
	brief := "Tightened the plan.\n\n```manifest-proposal\n{\"type\":\"replace-section\",\"item\":\"aion:" + item +
		"\",\"section\":\"plan\",\"body\":\"1. Pull the map\\n2. Draft\"}\n```\n"
	fakeChatRun(t, srv, "cr1", "th/y", orderID, "delegate", brief)
	srv.chatSweep()
	msgs := srv.chat.Messages("th/y")
	if len(msgs) != 2 || msgs[1].Kind != "kairos" || msgs[1].Outcome != "completed" {
		t.Fatalf("kairos message: %+v", msgs)
	}
	if len(msgs[1].Props) != 1 || msgs[1].Props[0].Type != "replace-section" || msgs[1].Props[0].ItemID != item {
		t.Fatalf("proposal not parsed: %+v", msgs[1].Props)
	}
	if strings.Contains(msgs[1].Text, "```") {
		t.Fatalf("proposal block must be cleaned from the text: %q", msgs[1].Text)
	}
	// idempotent re-sweep
	srv.chatSweep()
	if len(srv.chat.Messages("th/y")) != 2 {
		t.Fatal("re-sweep duplicated the kairos message")
	}
	// pending cleared
	if len(srv.chat.Pending()) != 0 {
		t.Fatalf("pending not cleared: %+v", srv.chat.Pending())
	}
}

func TestChatProposalGateAndApply(t *testing.T) {
	srv, item := chatFixture(t)
	_, _ = srv.chat.CreateThread("th/z", "z", "", chatthreads.Identity{ID: "ben@aion.bio", Name: "Ben"}, time.Now())
	m, _ := srv.chat.AddMessage(chatthreads.Message{
		Thread: "th/z", Kind: "kairos", Author: "agent:kairos", AuthName: "Kairos", Text: "done", Run: "r1", Outcome: "completed",
		Props: []chatthreads.Proposal{{Verb: "replace ## plan", Type: "replace-section", ItemID: item, Section: "plan", Body: "1. new plan", State: "pending"}},
	}, time.Now())
	// the aion item has no owner → non-admin is refused, admin applies
	if err := srv.AionChatProposal("th/z", m.ID, 0, true, "jack@aion.bio", "Jack", "JR", false); err == nil {
		t.Fatal("non-assignee non-admin must be refused")
	}
	if err := srv.AionChatProposal("th/z", m.ID, 0, true, "ben@aion.bio", "Ben", "BA", true); err != nil {
		t.Fatal(err)
	}
	// applied through the real plan-section write path
	if rec := srv.readPlanRecord("aion:" + item); !strings.Contains(rec.Plan, "new plan") {
		t.Fatalf("proposal did not apply to the plan record: %+v", rec)
	}
	if srv.chat.Messages("th/z")[0].Props[0].State != "applied" {
		t.Fatal("proposal state not marked applied")
	}
}

// fakeChatRun drops a completed kairos run report + brief carrying a [chat::] token.
func fakeChatRun(t *testing.T, srv *Server, runID, thread, orderID, ritual, briefBody string) {
	t.Helper()
	root := srv.findHarness("kairos").Spirits.Root()
	for _, d := range []string{"runs", "library"} {
		if err := os.MkdirAll(filepath.Join(root, "artifacts", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	req := fmt.Sprintf("work [chat:: %s#%s] [ritual:: %s]", thread, orderID, ritual)
	report := fmt.Sprintf("---\nrun: %s\nspirit: kairos\nritual: %s\nrequest: %q\nstarted: 2026-08-16T05:00:00Z\nfinished: 2026-08-16T05:01:00Z\noutcome: completed\n---\nran\n", runID, ritual, req)
	if err := os.WriteFile(filepath.Join(root, "artifacts", "runs", "2026-08-16-kairos-"+runID+".md"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	brief := fmt.Sprintf("---\ntitle: brief\nrun: %s\ndate: 2026-08-16T05:01:00Z\n---\n%s\n", runID, briefBody)
	if err := os.WriteFile(filepath.Join(root, "artifacts", "library", runID+"-brief.md"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
}
