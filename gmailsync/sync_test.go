package gmailsync

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type fakeFetch struct {
	ids     []string
	threads map[string]struct {
		subject string
		msgs    []Msg
	}
	listedAfter time.Time
}

func (f *fakeFetch) ThreadIDsSince(_ context.Context, after time.Time, _ int) ([]string, error) {
	f.listedAfter = after
	return f.ids, nil
}

func (f *fakeFetch) ThreadFull(_ context.Context, id string) (string, []Msg, error) {
	t := f.threads[id]
	return t.subject, t.msgs, nil
}

func testLoop(t *testing.T, fetch *fakeFetch, roster fakeRoster) (*Loop, *Candidates) {
	t.Helper()
	tok, err := NewTokens(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := tok.Put("partner@gmail.com", &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	cand, err := NewCandidates(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Loop{
		Tokens:      tok,
		Candidates:  cand,
		OAuthConfig: func() (*oauth2.Config, error) { return &oauth2.Config{}, nil },
		Roster:      func() Resolver { return roster },
		NewFetcher:  func(oauth2.TokenSource) Fetcher { return fetch },
	}, cand
}

func msgAt(id, from, to string, at time.Time, body string) Msg {
	return Msg{ID: id, From: from, To: to, Internal: at, Body: body}
}

func TestSyncPassFiltersAndUpserts(t *testing.T) {
	now := time.Now()
	fetch := &fakeFetch{
		ids: []string{"deal", "personal", "cal"},
		threads: map[string]struct {
			subject string
			msgs    []Msg
		}{
			// roster participant (brian) → qualifies
			"deal": {"751 roof scope", []Msg{
				msgAt("m1", "Brian <brian@ooda.group>", "partner@gmail.com", now.Add(-2*time.Hour), "scope attached"),
			}},
			// nobody on the roster → personal, never surfaces
			"personal": {"dinner", []Msg{
				msgAt("m2", "friend@example.com", "partner@gmail.com", now.Add(-1*time.Hour), "pizza?"),
			}},
			// pure calendar machinery → skipped
			"cal": {"Invitation: walkthrough", []Msg{
				{ID: "m3", From: "brian@ooda.group", Subject: "Invitation: walkthrough", HasCalendar: true, Internal: now},
			}},
		},
	}
	roster := fakeRoster{"brian@ooda.group": "brian anderson"}
	loop, cands := testLoop(t, fetch, roster)
	loop.Pass(context.Background())

	pending := cands.List(StatusPending)
	if len(pending) != 1 || pending[0].ThreadID != "deal" {
		t.Fatalf("pending = %+v, want just the deal thread", pending)
	}
	if pending[0].Filename == "" || pending[0].Note == "" {
		t.Fatalf("candidate not rendered: %+v", pending[0])
	}
	// watermark advanced to the newest message seen (the calendar one)
	if wm := cands.Watermark("partner@gmail.com"); wm.Before(now.Add(-time.Minute)) {
		t.Fatalf("watermark not advanced: %v", wm)
	}
	// second pass with a grown pending thread re-renders IN PLACE (same id)
	th := fetch.threads["deal"]
	th.msgs = append(th.msgs, msgAt("m4", "partner@gmail.com", "brian@ooda.group", now, "approved"))
	fetch.threads["deal"] = th
	loop.Pass(context.Background())
	pending = cands.List(StatusPending)
	if len(pending) != 1 || pending[0].LastMsgID != "m4" {
		t.Fatalf("pending after growth = %+v", pending)
	}
}

func TestDismissedThreadStaysMuted(t *testing.T) {
	now := time.Now()
	fetch := &fakeFetch{
		ids: []string{"deal"},
		threads: map[string]struct {
			subject string
			msgs    []Msg
		}{
			"deal": {"scope", []Msg{msgAt("m1", "brian@ooda.group", "partner@gmail.com", now, "hi")}},
		},
	}
	loop, cands := testLoop(t, fetch, fakeRoster{"brian@ooda.group": "brian anderson"})
	loop.Pass(context.Background())
	p := cands.List(StatusPending)
	if len(p) != 1 {
		t.Fatalf("want 1 pending, got %d", len(p))
	}
	if _, err := cands.Decide(p[0].ID, "partner@gmail.com", false, "", false); err != nil {
		t.Fatal(err)
	}
	// the thread grows — but stays muted
	th := fetch.threads["deal"]
	th.msgs = append(th.msgs, msgAt("m2", "brian@ooda.group", "partner@gmail.com", now.Add(time.Hour), "more"))
	fetch.threads["deal"] = th
	loop.Pass(context.Background())
	if got := len(cands.List(StatusPending)); got != 0 {
		t.Fatalf("dismissed thread resurfaced: %d pending", got)
	}
}

func TestConfirmedGrowthSpawnsFreshCandidate(t *testing.T) {
	now := time.Now()
	fetch := &fakeFetch{
		ids: []string{"deal"},
		threads: map[string]struct {
			subject string
			msgs    []Msg
		}{
			"deal": {"scope", []Msg{msgAt("m1", "brian@ooda.group", "partner@gmail.com", now.Add(-time.Hour), "first")}},
		},
	}
	loop, cands := testLoop(t, fetch, fakeRoster{"brian@ooda.group": "brian anderson"})
	loop.Pass(context.Background())
	first := cands.List(StatusPending)[0]
	if _, err := cands.Decide(first.ID, "partner@gmail.com", true, "sha256:abc", false); err != nil {
		t.Fatal(err)
	}
	th := fetch.threads["deal"]
	th.msgs = append(th.msgs, msgAt("m2", "brian@ooda.group", "partner@gmail.com", now, "second"))
	fetch.threads["deal"] = th
	loop.Pass(context.Background())
	pending := cands.List(StatusPending)
	if len(pending) != 1 {
		t.Fatalf("want 1 growth candidate, got %d", len(pending))
	}
	g := pending[0]
	if g.ID == first.ID || g.Seq != 2 {
		t.Fatalf("growth candidate not fresh: %+v", g)
	}
	if !strings.Contains(g.Note, "second") || strings.Contains(g.Note, "first") {
		t.Fatalf("growth note must carry ONLY fresh messages:\n%s", g.Note)
	}
}
