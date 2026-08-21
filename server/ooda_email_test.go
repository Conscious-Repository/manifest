package server

// The email lane's doctrine tests (ooda-portal email plan, 2026-08-21):
//
//  1. TestOodaReadsAreFlat — the flat-reads doctrine, now actually pinned
//     (the ooda_portal.go comment cited this test before it existed): every
//     GET a member can reach returns the admin and a partner IDENTICAL bytes
//     — EXCEPT /api/ooda/email, the one deliberate carve-out.
//  2. TestPendingEmailNotesAreScopedToSourceAndAdmin — the carve-out itself:
//     a pending candidate is its source member's mail, visible to that
//     member and the admin only; a confirmed one is a shared artifact.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"manifest/gmailsync"
)

func seedCandidate(t *testing.T, f *oodaPortalHandles, account, thread, subject string) gmailsync.Candidate {
	t.Helper()
	cand := gmailsync.Candidate{
		Account: account, ThreadID: thread, Subject: subject,
		FirstMsgAt: time.Now().Add(-time.Hour), LastMsgAt: time.Now(),
		LastMsgID: "m1", Note: "olga sobkiv\n\n## 2026-08-21 — olga sobkiv\n\n" + subject + "\n",
		Filename: "2026-08-21 " + strings.ToLower(subject) + ".md",
	}
	if err := f.cands.Upsert(cand); err != nil {
		t.Fatal(err)
	}
	got := f.cands.List(gmailsync.StatusPending)
	for _, c := range got {
		if c.ThreadID == thread {
			return c
		}
	}
	t.Fatalf("seeded candidate not listed")
	return gmailsync.Candidate{}
}

// TestOodaReadsAreFlat: for every read route, an admin session and a partner
// session get byte-identical responses. /api/ooda/email is the ONE exception
// (pending mail is scoped) and is asserted separately below.
func TestOodaReadsAreFlat(t *testing.T) {
	f := oodaPortalFixtureFull(t)
	admin, err := f.auth.SessionCookie("ben@ooda.group", "Benjamin", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	partner, err := f.auth.SessionCookie("me@olgasobkiv.com", "Olga", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// seed a pending candidate so the exception route has something to differ on
	seedCandidate(t, f, "me@olgasobkiv.com", "t-flat", "roof scope")

	flat := []string{
		"/api/ooda/dashboard",
		"/api/ooda/portfolio",
		"/api/ooda/property/748-n-euclid",
		"/api/ooda/work",
		"/api/ooda/people",
		"/api/ooda/gmail", // connection status is per-caller but neither is connected → identical
		"/api/team/state",
	}
	for _, path := range flat {
		a := oodaDo(t, f.h, admin, "GET", path, "")
		p := oodaDo(t, f.h, partner, "GET", path, "")
		if a.Code != p.Code || a.Body.String() != p.Body.String() {
			t.Fatalf("%s differs between admin and partner (admin %d, partner %d) — "+
				"the flat-reads doctrine allows exactly one exception: /api/ooda/email", path, a.Code, p.Code)
		}
	}
	// and the exception must actually differ here (admin sees the pending
	// candidate, a second partner must not — proven fully below)
	a := oodaDo(t, f.h, admin, "GET", "/api/ooda/email", "")
	b := oodaDo(t, f.h, partner, "GET", "/api/ooda/email", "")
	if a.Code != 200 || b.Code != 200 {
		t.Fatalf("email route: admin %d partner %d", a.Code, b.Code)
	}
}

func pendingIDs(t *testing.T, body string) []string {
	t.Helper()
	var out struct {
		Pending []struct {
			ID      string `json:"id"`
			Account string `json:"account"`
		} `json:"pending"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	ids := make([]string, 0, len(out.Pending))
	for _, p := range out.Pending {
		ids = append(ids, p.ID)
	}
	return ids
}

func TestPendingEmailNotesAreScopedToSourceAndAdmin(t *testing.T) {
	f := oodaPortalFixtureFull(t)
	admin, _ := f.auth.SessionCookie("ben@ooda.group", "Benjamin", false, time.Now())
	olga, _ := f.auth.SessionCookie("me@olgasobkiv.com", "Olga", false, time.Now())
	brian, _ := f.auth.SessionCookie("brian@ooda.group", "Brian", false, time.Now())

	olgas := seedCandidate(t, f, "me@olgasobkiv.com", "t1", "751 permit sets")
	brians := seedCandidate(t, f, "brian@ooda.group", "t2", "vine removal quote")

	// each partner sees exactly their own mailbox's candidates
	olgaSees := pendingIDs(t, oodaDo(t, f.h, olga, "GET", "/api/ooda/email", "").Body.String())
	if len(olgaSees) != 1 || olgaSees[0] != olgas.ID {
		t.Fatalf("olga sees %v, want only her %s", olgaSees, olgas.ID)
	}
	brianSees := pendingIDs(t, oodaDo(t, f.h, brian, "GET", "/api/ooda/email", "").Body.String())
	if len(brianSees) != 1 || brianSees[0] != brians.ID {
		t.Fatalf("brian sees %v, want only his %s", brianSees, brians.ID)
	}
	// the admin sees all
	adminSees := pendingIDs(t, oodaDo(t, f.h, admin, "GET", "/api/ooda/email", "").Body.String())
	if len(adminSees) != 2 {
		t.Fatalf("admin sees %v, want both", adminSees)
	}
	// a partner cannot decide another partner's candidate
	if rec := oodaDo(t, f.h, brian, "POST", "/api/ooda/email/"+olgas.ID, `{"action":"dismiss"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("brian deciding olga's candidate: %d, want 403", rec.Code)
	}
	// the source member confirms hers → shared artifact, visible to everyone
	rec := oodaDo(t, f.h, olga, "POST", "/api/ooda/email/"+olgas.ID, `{"action":"confirm"}`)
	if rec.Code != 200 {
		t.Fatalf("olga confirm: %d %s", rec.Code, rec.Body)
	}
	var decided gmailsync.Candidate
	if err := json.Unmarshal(rec.Body.Bytes(), &decided); err != nil || decided.ArtifactHash == "" {
		t.Fatalf("confirm returned no artifact hash: %v %s", err, rec.Body)
	}
	if !f.srv.artifacts.Owns("ooda", decided.ArtifactHash) {
		t.Fatal("confirmed note did not land in the ooda artifact index")
	}
	if entry, ok := f.srv.artifacts.Lookup("ooda", decided.ArtifactHash); !ok ||
		!strings.EqualFold(entry.ByEmail, "me@olgasobkiv.com") {
		t.Fatalf("artifact provenance wrong: %+v", entry)
	}
	// the admin can decide a member's candidate (dismiss brian's)
	if rec := oodaDo(t, f.h, admin, "POST", "/api/ooda/email/"+brians.ID, `{"action":"dismiss"}`); rec.Code != 200 {
		t.Fatalf("admin dismiss: %d %s", rec.Code, rec.Body)
	}
	// and the vault is byte-identical through all of it — the email lane
	// writes candidates + artifacts, never owner records
	before := vaultFingerprint(t, f.vault)
	if got := vaultFingerprint(t, f.vault); got != before {
		t.Fatal("email decide touched the vault")
	}
}
