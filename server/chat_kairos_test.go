package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time"

	"manifest/chatthreads"
	"manifest/ledger"
	"manifest/record"
	"manifest/spirits"
	"manifest/vaultwriter"
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

// A zeck proposal targets an OODA item, so the gate must ask the OODA owner
// oracle (alias-normalized initials) and an apply must land in the OODA team
// store. The shared helper used to consult AION's pair for both agents, so
// only the admin ever passed zeck's gate and an "applied" change wrote to
// AION's items.ext.json — nothing changed in the OODA portal.
func TestOodaChatProposalGatesAndAppliesInTheOodaDomain(t *testing.T) {
	f := oodaPortalFixtureFull(t)
	srv := f.srv
	srv.UseOoda(f.live)
	oc, err := chatthreads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.UseOodaChat(oc)
	if _, err := oc.CreateThread("th/o", "roof", "", chatthreads.Identity{ID: "ben@ooda.group", Name: "Ben"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// the fixture vault owns this node as `olga-sobkiv` — the OS member's slug
	itemID := "prop/748-n-euclid#shell/windows"
	m, err := oc.AddMessage(chatthreads.Message{
		Thread: "th/o", Kind: "zeck", Author: "agent:zeck", AuthName: "Zeck", Text: "done", Run: "r1", Outcome: "completed",
		Props: []chatthreads.Proposal{{Verb: "set status", Type: "set-field", ItemID: itemID, Field: "status", Value: "done", State: "pending"}},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// not the assignee, not admin → refused by the OODA oracle
	if err := srv.OodaChatProposal("th/o", m.ID, 0, true, "bpabbassa@att.net", "Brian", "BPA", false); err == nil {
		t.Fatal("non-assignee non-admin must be refused")
	}
	// the assignee, matched through the alias map (vault slug → roster OS)
	if err := srv.OodaChatProposal("th/o", m.ID, 0, true, "me@olgasobkiv.com", "Olga", "OS", false); err != nil {
		t.Fatal(err)
	}
	if ov, ok := f.store.Ext().Overrides[itemID]; !ok || ov.Fields["status"] != "done" {
		t.Fatalf("apply must land in the OODA team store: %+v", f.store.Ext().Overrides)
	}
	if oc.Messages("th/o")[0].Props[0].State != "applied" {
		t.Fatal("proposal state not marked applied")
	}
	// the fixture wires NO AION store at all — the old code path would have
	// errored (or worse, written elsewhere); passing proves the OODA pair is
	// consulted for zeck
	if srv.threads != nil {
		t.Fatal("fixture sanity: this server must have no AION team store")
	}
}

// A zeck chat run must ledger under ZECK's identity — sweepAgent hard-coded
// Actor "agent:kairos" / Harness "kairos" for every agent it swept (audit
// B14), so the trail credited kairos with chats it never saw. Kairos's own
// values are pinned byte-identical.
func TestChatSweepLedgersEachAgentsIdentity(t *testing.T) {
	srv, _ := chatFixture(t)
	oc, err := chatthreads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.UseOodaChat(oc)
	kair := srv.findHarness("kairos")
	srv.UseHarnesses([]Harness{*kair, {Name: "zeck", Surface: "team", Spirits: spirits.NewStore(t.TempDir())}})
	led, err := ledger.New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	srv.ledgerStore = led

	_, _ = srv.chat.CreateThread("th/k", "k", "", chatthreads.Identity{ID: "ben@aion.bio", Name: "Ben"}, time.Now())
	_, _ = oc.CreateThread("th/z", "z", "", chatthreads.Identity{ID: "ben@ooda.group", Name: "Ben"}, time.Now())
	fakeAgentChatRun(t, srv, "kairos", "rk", "th/k", "o1", "ask", "the kairos answer")
	fakeAgentChatRun(t, srv, "zeck", "rz", "th/z", "o2", "ask", "the zeck answer")
	srv.chatSweep()

	entries, err := led.Day(led.Today())
	if err != nil {
		t.Fatal(err)
	}
	actorToHarness := map[string]string{}
	for _, e := range entries {
		if e.Source == "chat" {
			actorToHarness[e.Actor] = e.Harness
		}
	}
	if actorToHarness["agent:kairos"] != "kairos" {
		t.Fatalf("kairos entry must keep its identity: %+v", entries)
	}
	if actorToHarness["agent:zeck"] != "zeck" {
		t.Fatalf("a zeck run must ledger as zeck, never kairos: %+v", entries)
	}
}

// @zeck::brief in the OODA portal must reach the persona branch — the intent
// regex hard-coded @kairos:: (audit B15). A mention of someone ELSE's agent
// is prose, in both directions.
func TestChatIntentMatchesTheActiveAgent(t *testing.T) {
	srv, _ := chatFixture(t)
	oc, err := chatthreads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.UseOodaChat(oc)
	kairos, zeck := srv.kairosAgent(), srv.zeckAgent()
	for _, c := range []struct {
		ag   *chatAgent
		text string
		want string
	}{
		{zeck, "@zeck::brief what's left on the roof?", "brief"},
		{zeck, "@kairos::brief ping", ""},
		{zeck, "ask @kairos::plan then @zeck::info", "info"},    // the first mention OF zeck wins
		{kairos, "@kairos::brief what's the setback?", "brief"}, // the path that always worked
		{kairos, "@zeck::brief ping", ""},
		{kairos, "no mention at all", ""},
	} {
		if got := chatIntent(c.ag, c.text); got != c.want {
			t.Errorf("%s / %q: intent = %q, want %q", c.ag.Name, c.text, got, c.want)
		}
	}
}

// fakeChatRun drops a completed kairos run report + brief carrying a [chat::] token.
func fakeChatRun(t *testing.T, srv *Server, runID, thread, orderID, ritual, briefBody string) {
	t.Helper()
	fakeAgentChatRun(t, srv, "kairos", runID, thread, orderID, ritual, briefBody)
}

// fakeAgentChatRun is the agent-generic form — the sweep serves every wired
// agent, so its fixtures must too.
func fakeAgentChatRun(t *testing.T, srv *Server, agent, runID, thread, orderID, ritual, briefBody string) {
	t.Helper()
	root := srv.findHarness(agent).Spirits.Root()
	for _, d := range []string{"runs", "library"} {
		if err := os.MkdirAll(filepath.Join(root, "artifacts", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	req := fmt.Sprintf("work [chat:: %s#%s] [ritual:: %s]", thread, orderID, ritual)
	report := fmt.Sprintf("---\nrun: %s\nspirit: %s\nritual: %s\nrequest: %q\nstarted: 2026-08-16T05:00:00Z\nfinished: 2026-08-16T05:01:00Z\noutcome: completed\n---\nran\n", runID, agent, ritual, req)
	if err := os.WriteFile(filepath.Join(root, "artifacts", "runs", "2026-08-16-"+agent+"-"+runID+".md"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	brief := fmt.Sprintf("---\ntitle: brief\nrun: %s\ndate: 2026-08-16T05:01:00Z\n---\n%s\n", runID, briefBody)
	if err := os.WriteFile(filepath.Join(root, "artifacts", "library", runID+"-brief.md"), []byte(brief), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The 2026-08-25 "apply does nothing": zeck proposes PROPERTY changes (its
// context advertises property statuses), but the apply funneled every
// set-field into the team store's task enum — "pre_development" 400'd into an
// off-screen error node. A property set-field now applies through the same
// write the dashboard's status chip uses, admin-gated.
func TestOodaChatProposalAppliesPropertyStatus(t *testing.T) {
	f := oodaPortalFixtureFull(t)
	srv := f.srv
	srv.UseOoda(f.live)
	// the fixture's vault field is the DIRECTORY; the writer needs the same
	// capability shape production grants for property field edits
	srv.UseVault(vaultwriter.New(f.vault).WithZoneRoots("system", "extrinsic").Grant(
		vaultwriter.Capability{Name: "realestate", Zone: record.ZoneSystem,
			Pattern: "system/realestate/**", Actor: vaultwriter.ActorUserAction}))
	oc, err := chatthreads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.UseOodaChat(oc)
	if _, err := oc.CreateThread("th/p", "status", "", chatthreads.Identity{ID: "ben@ooda.group", Name: "Ben"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// the exact live shape: a BARE property slug, a property-lifecycle value
	m, err := oc.AddMessage(chatthreads.Message{
		Thread: "th/p", Kind: "zeck", Author: "agent:zeck", AuthName: "Zeck", Text: "flip it", Run: "r9", Outcome: "completed",
		Props: []chatthreads.Proposal{{Verb: "set status", Type: "set-field", ItemID: "748-n-euclid", Field: "status", Value: "pre_development", State: "pending"}},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// a non-admin member is refused with a message that names the remedy
	err = srv.OodaChatProposal("th/p", m.ID, 0, true, "me@olgasobkiv.com", "Olga", "OS", false)
	if err == nil || !strings.Contains(err.Error(), "admin") {
		t.Fatalf("non-admin must be refused clearly, got %v", err)
	}
	// the admin applies — and the PROPERTY record actually changes
	if err := srv.OodaChatProposal("th/p", m.ID, 0, true, "ben@ooda.group", "Ben", "BA", true); err != nil {
		t.Fatalf("admin apply: %v", err)
	}
	if p, ok := srv.realestate.Get("748-n-euclid"); !ok || p.Status != "pre_development" {
		t.Fatalf("property status not written: %+v", p.Status)
	}
	if oc.Messages("th/p")[0].Props[0].State != "applied" {
		t.Fatal("card not marked applied")
	}
	// a bad value refuses with the enum in the message, and stays pending
	m2, _ := oc.AddMessage(chatthreads.Message{
		Thread: "th/p", Kind: "zeck", Author: "agent:zeck", AuthName: "Zeck", Text: "x", Run: "r10", Outcome: "completed",
		Props: []chatthreads.Proposal{{Verb: "set status", Type: "set-field", ItemID: "prop/748-n-euclid", Field: "status", Value: "warp-speed", State: "pending"}},
	}, time.Now())
	err = srv.OodaChatProposal("th/p", m2.ID, 0, true, "ben@ooda.group", "Ben", "BA", true)
	if err == nil || !strings.Contains(err.Error(), "unknown status") {
		t.Fatalf("bad value must refuse with the enum, got %v", err)
	}
	// a non-status property field refuses with guidance
	m3, _ := oc.AddMessage(chatthreads.Message{
		Thread: "th/p", Kind: "zeck", Author: "agent:zeck", AuthName: "Zeck", Text: "x", Run: "r11", Outcome: "completed",
		Props: []chatthreads.Proposal{{Verb: "set entity", Type: "set-field", ItemID: "748-n-euclid", Field: "entity", Value: "x", State: "pending"}},
	}, time.Now())
	err = srv.OodaChatProposal("th/p", m3.ID, 0, true, "ben@ooda.group", "Ben", "BA", true)
	if err == nil || !strings.Contains(err.Error(), "only a property's status") {
		t.Fatalf("non-status field must refuse with guidance, got %v", err)
	}
	// replace-section for ooda refuses with the discard guidance
	m4, _ := oc.AddMessage(chatthreads.Message{
		Thread: "th/p", Kind: "zeck", Author: "agent:zeck", AuthName: "Zeck", Text: "x", Run: "r12", Outcome: "completed",
		Props: []chatthreads.Proposal{{Verb: "replace ## description", Type: "replace-section", ItemID: "748-n-euclid", Section: "description", Body: "b", State: "pending"}},
	}, time.Now())
	err = srv.OodaChatProposal("th/p", m4.ID, 0, true, "ben@ooda.group", "Ben", "BA", true)
	if err == nil || !strings.Contains(err.Error(), "aren't applicable") {
		t.Fatalf("section rewrite must refuse with guidance, got %v", err)
	}
}
