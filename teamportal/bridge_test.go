package teamportal

import (
	"testing"
	"time"

	"manifest/portals"
	"manifest/threads"
)

// cardActions indexes a Cards() result by "<action>|<actor>" so a test can
// ask exactly which writes surfaced.
func cardActions(cards []portals.Card) map[string]bool {
	out := map[string]bool{}
	for _, c := range cards {
		out[c.Change+"|"+c.Actor] = true
	}
	return out
}

// The owner's own writes must never nag his own feed — under EITHER identity
// he writes with. Through the portal he is his admin email; through the
// cockpit's todo panel the threads store writes him as the bare "owner" token
// into the SAME activity.log when the two stores share a team dir (the OODA
// layout: realEstate.teamDir == ooda.teamDir). Everyone else — a member, an
// agent, a proposal — still files a card.
func TestBridgeSuppressesOwnerUnderBothIdentities(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// the cockpit's thread store, rooted in the same dir → same activity.log
	ts, err := threads.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	admin := Identity{Email: "ben@ooda.group", Name: "Benjamin"}

	// 1. the owner through the portal (his email)
	if _, err := st.AddComment(admin, "team/roof", "owner via portal", now); err != nil {
		t.Fatal(err)
	}
	// 2. the owner's email in a different case (Google may hand it back mixed)
	if _, err := st.AddComment(Identity{Email: "Ben@OODA.group", Name: "Benjamin"}, "team/roof", "owner mixed case", now.Add(1*time.Second)); err != nil {
		t.Fatal(err)
	}
	// 3. the owner through the cockpit todo panel (the "owner" token) — the
	// exact line shape the live bug reproduced
	owner := threads.Identity{ID: OwnerActor, Name: "Benjamin"}
	if _, err := ts.Add(owner, "re:aion-bl/5-hedge-electricians", threads.ActComment, "can you get their contact information?", nil, nil,
		map[string]any{"agent": "agent:alfred", "mode": "ask"}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Add(owner, "re:aion-bl/5-hedge-electricians", "assign", "assigned to agent:alfred (do)", nil, nil,
		map[string]any{"assignee": "agent:alfred"}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	// 4. another member's comment — must notify
	if _, err := st.AddComment(Identity{Email: "brian@ooda.group", Name: "Brian"}, "team/roof", "member comment", now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	// 5. an agent's comment on the thread store — must notify
	if _, err := ts.Add(threads.Identity{ID: "agent:hermes", Name: "Alfred"}, "re:aion-bl/5-hedge-electricians", threads.ActComment,
		"found five", nil, nil, nil, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	// 6. an agent's comment through the portal store — must notify
	if _, err := st.AddComment(Identity{Email: "agent:codex", Name: "Codex"}, "team/roof", "agent via portal", now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	// 7. a member's proposal — files a card on a bridge without the approvable lane
	if _, err := st.Propose(Identity{Email: "brian@ooda.group", Name: "Brian"}, "sam@aion.bio", "SM",
		"task", "a preview proposal", "", "", now.Add(7*time.Second)); err != nil {
		t.Fatal(err)
	}

	b := NewBridgeNamed(st, t.TempDir(), "ben@ooda.group", "ooda-portal", "https://portal.ooda.group/#work")
	got := cardActions(b.Cards(now.Add(time.Minute)))

	for _, self := range []string{
		"comment|ben@ooda.group", "comment|Ben@OODA.group",
		"comment|owner", "assign|owner",
	} {
		if got[self] {
			t.Errorf("the owner's own write surfaced as a notice: %s", self)
		}
	}
	for _, other := range []string{
		"comment|brian@ooda.group", "comment|agent:hermes", "comment|agent:codex", "propose|brian@ooda.group",
	} {
		if !got[other] {
			t.Errorf("a write that must notify was dropped: %s (got %v)", other, got)
		}
	}
	if n := len(got); n != 4 {
		t.Fatalf("expected exactly 4 cards, got %d: %v", n, got)
	}

	// the approvable lane still hides the proposal notice, and nothing else
	got = cardActions(b.SuppressProposeNotices().Cards(now.Add(time.Minute)))
	if got["propose|brian@ooda.group"] || len(got) != 3 {
		t.Fatalf("SuppressProposeNotices regressed: %v", got)
	}
}

// A bridge with no admin configured still recognises the cockpit token, and
// never treats an empty actor as the owner.
func TestBridgeOwnerTokenWithoutAdmin(t *testing.T) {
	b := &Bridge{}
	if !b.isOwner(OwnerActor) {
		t.Fatal("the cockpit token must read as the owner even with no admin email")
	}
	if b.isOwner("") || b.isOwner("ben@ooda.group") || b.isOwner("agent:hermes") {
		t.Fatal("no admin configured: only the token is the owner")
	}
	b.admin = "ben@ooda.group"
	if !b.isOwner("BEN@ooda.group") || b.isOwner("brian@ooda.group") || b.isOwner("ben@aion.bio") {
		t.Fatal("admin match must be case-insensitive and exact")
	}
}
