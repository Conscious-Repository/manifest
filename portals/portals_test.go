package portals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// fixedNow gives the service a deterministic clock (no wall time in assertions).
func chicago(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("no tzdata")
	}
	return loc
}

func svcFor(t *testing.T, now time.Time, ts *httptest.Server, which string) *Service {
	t.Helper()
	svc := New(t.TempDir(), chicago(t))
	svc.nowFn = func() time.Time { return now }
	svc.hc = ts.Client()
	if which == "clickup" {
		svc.cuBase = ts.URL
	} else {
		svc.bnBase = ts.URL
	}
	return svc
}

// ---- credential store: 0600, env override, masking ----

func TestCredStore(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	def := mustDef("clickup")

	if st.HasCreds("clickup", def) {
		t.Fatal("sealed store should not report creds")
	}
	if err := st.SetCreds("clickup", def, map[string]string{"token": "pk_secret1234"}); err != nil {
		t.Fatal(err)
	}
	if !st.HasCreds("clickup", def) {
		t.Fatal("should have creds after SetCreds")
	}
	if got := st.Masked("clickup", def); got != "····1234" {
		t.Fatalf("mask = %q, want ····1234", got)
	}
	// File must be 0600 and contain the raw key (it's the secret store) but never
	// leak elsewhere — here we only assert the mode.
	fi, err := os.Stat(filepath.Join(dir, "portals", "clickup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("cred file mode = %o, want 600", fi.Mode().Perm())
	}
	// Env override wins.
	t.Setenv("MANIFEST_PORTAL_CLICKUP_TOKEN", "pk_envoverride9999")
	if got := st.Creds("clickup", def)["token"]; got != "pk_envoverride9999" {
		t.Fatalf("env override not applied: %q", got)
	}
	// Clear returns to sealed.
	if err := st.Clear("clickup"); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("MANIFEST_PORTAL_CLICKUP_TOKEN")
	if st.HasCreds("clickup", def) {
		t.Fatal("Clear should seal the portal")
	}
}

// ---- ClickUp poller → deterministic daily digest ----

func clickupFixture(t *testing.T, now time.Time) *httptest.Server {
	t.Helper()
	ms := func(off time.Duration) string {
		return strconv.FormatInt(now.Add(off).UnixMilli(), 10)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"id": 42}})
		case "/team":
			json.NewEncoder(w).Encode(map[string]any{"teams": []map[string]any{{"id": "T1"}}})
		case "/team/T1/task":
			json.NewEncoder(w).Encode(map[string]any{
				"last_page": true,
				"tasks": []map[string]any{
					{"id": "a1", "name": "Close 743 N Euclid", "status": map[string]any{"status": "done"},
						"date_created": ms(-3 * time.Hour), "date_updated": ms(-1 * time.Hour), "date_closed": ms(-1 * time.Hour),
						"url": "https://app.clickup.com/t/a1", "list": map[string]any{"name": "Bayard"},
						"assignees": []map[string]any{{"id": 42}}},
					{"id": "a2", "name": "Walk audit", "status": map[string]any{"status": "open"},
						"date_created": ms(-2 * time.Hour), "date_updated": ms(-2 * time.Hour),
						"url": "https://app.clickup.com/t/a2", "list": map[string]any{"name": "Bayard"},
						"assignees": []map[string]any{{"id": 7}}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestClickUpDigest(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 7, 23, 15, 0, 0, 0, loc)
	ts := clickupFixture(t, now)
	defer ts.Close()
	svc := svcFor(t, now, ts, "clickup")
	if err := svc.store.SetCreds("clickup", mustDef("clickup"), map[string]string{"token": "pk_x"}); err != nil {
		t.Fatal(err)
	}

	svc.pollOne(context.Background(), mustDef("clickup"))

	cards := svc.Cards()
	if len(cards) != 1 {
		t.Fatalf("want exactly one digest card, got %d", len(cards))
	}
	c := cards[0]
	if c.Type != "portal-digest" || c.Portal != "clickup" {
		t.Fatalf("bad card: %+v", c)
	}
	if !c.Pinned {
		t.Fatal("today's digest should be pinned")
	}
	// The assignee (Benjamin, id 42) task appears in the "for you" block, and a
	// closed task reads "completed" (deterministic change text, not a bare verb).
	if len(c.ForYou) != 1 || c.ForYou[0].Text != "completed · Close 743 N Euclid" {
		t.Fatalf("forYou = %+v", c.ForYou)
	}
	// One Bayard group with both lines.
	if len(c.Groups) != 1 || c.Groups[0].List != "Bayard" || len(c.Groups[0].Lines) != 2 {
		t.Fatalf("groups = %+v", c.Groups)
	}

	// Idempotent: a re-poll produces the same single card with the same id.
	id := c.ID
	svc.pollOne(context.Background(), mustDef("clickup"))
	again := svc.Cards()
	if len(again) != 1 || again[0].ID != id {
		t.Fatalf("re-poll not idempotent: %+v", again)
	}

	// A quiet day (dismissed) produces no card, and it survives a reload.
	svc.Dismiss(id)
	if n := svc.InboxCount(); n != 0 {
		t.Fatalf("after dismiss InboxCount = %d, want 0", n)
	}
	reloaded := New(svc.dataDir(), loc)
	reloaded.nowFn = func() time.Time { return now }
	if !reloaded.store.HasCreds("clickup", mustDef("clickup")) {
		t.Fatal("reloaded store lost creds")
	}
	if n := reloaded.InboxCount(); n != 0 {
		t.Fatalf("dismiss did not survive reload: InboxCount = %d", n)
	}
}

// ---- Benchling poller → itemized cards; new+edited; degraded keeps cache ----

func TestBenchlingItems(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 7, 23, 15, 0, 0, 0, loc)
	mod := now.Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	var fail bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		switch r.URL.Path {
		case "/custom-entities":
			json.NewEncoder(w).Encode(map[string]any{"customEntities": []map[string]any{
				{"id": "ent_1", "name": "Plasmid pBEN-1", "createdAt": mod, "modifiedAt": mod,
					"webURL": "https://x.benchling.com/ent_1", "creator": map[string]any{"name": "Ben"},
					"schema": map[string]any{"name": "Plasmid"}},
			}})
		case "/assay-results":
			json.NewEncoder(w).Encode(map[string]any{"assayResults": []map[string]any{
				{"id": "res_9", "createdAt": mod, "modifiedAt": mod,
					"schema": map[string]any{"name": "qPCR"}},
			}})
		default:
			json.NewEncoder(w).Encode(map[string]any{}) // other resources empty
		}
	}))
	defer ts.Close()

	svc := svcFor(t, now, ts, "benchling")
	if err := svc.store.SetCreds("benchling", mustDef("benchling"),
		map[string]string{"tenant": "x", "apiKey": "sk_secret"}); err != nil {
		t.Fatal(err)
	}
	svc.pollOne(context.Background(), mustDef("benchling"))

	cards := svc.Cards()
	if len(cards) != 1 {
		t.Fatalf("want one benchling day digest (entity + result), got %d: %+v", len(cards), cards)
	}
	c := cards[0]
	if c.Type != "portal-digest" || c.Portal != "benchling" || !c.Pinned {
		t.Fatalf("bad card: %+v", c)
	}
	// Both objects are lines; the nameless assay result identifies by schema + id.
	var lines []DigestLine
	for _, g := range c.Groups {
		lines = append(lines, g.Lines...)
	}
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %+v", c.Groups)
	}
	var sawResult bool
	for _, ln := range lines {
		if ln.Text == "qPCR res_9" {
			sawResult = true
		}
		if ln.Count != 1 || ln.Change != "new" {
			t.Fatalf("line should be one new save: %+v", ln)
		}
	}
	if !sawResult {
		t.Fatalf("assay result line missing: %+v", lines)
	}
	if c.Detail != "2 changes · 2 items · Ben" {
		t.Fatalf("summary = %q", c.Detail)
	}

	// Degraded: a failed poll keeps the last-good cache (no emptied inbox).
	fail = true
	svc.pollOne(context.Background(), mustDef("benchling"))
	if n := svc.InboxCount(); n != 1 {
		t.Fatalf("failed poll emptied the cache: InboxCount = %d, want 1", n)
	}
	row := svc.row(mustDef("benchling"))
	if row.State != StateDegraded || row.Err == "" {
		t.Fatalf("row should be degraded with a reason: %+v", row)
	}
}

// TestBenchlingPartialFailure pins the tolerance fix found live: one endpoint
// 400ing (Benchling's inconsistent sort enum on /requests) must NOT discard the
// changes the other resources returned — the portal stays open with their data.
func TestBenchlingPartialFailure(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 7, 23, 15, 0, 0, 0, loc)
	mod := now.Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/requests": // always 400 — even the sortless fallback
			http.Error(w, "bad sort", http.StatusBadRequest)
		case "/entries":
			json.NewEncoder(w).Encode(map[string]any{"entries": []map[string]any{
				{"id": "ent_e", "name": "Thymus culture", "createdAt": mod, "modifiedAt": mod,
					"webURL": "https://aion.benchling.com/x", "creator": map[string]any{"name": "Ellie"}},
			}})
		default:
			json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	svc := svcFor(t, now, ts, "benchling")
	if err := svc.store.SetCreds("benchling", mustDef("benchling"),
		map[string]string{"tenant": "aion", "apiKey": "sk_x"}); err != nil {
		t.Fatal(err)
	}
	svc.pollOne(context.Background(), mustDef("benchling"))

	if row := svc.row(mustDef("benchling")); row.State != StateOpen {
		t.Fatalf("one bad endpoint must not degrade the portal: state = %s (%s)", row.State, row.Err)
	}
	cards := svc.Cards()
	if len(cards) != 1 || len(cards[0].Groups) != 1 || len(cards[0].Groups[0].Lines) != 1 || cards[0].Groups[0].Lines[0].Text != "Thymus culture" {
		t.Fatalf("the working resource's change was discarded: %+v", cards)
	}
	// A notebook entry (no schema) gets the kind label as its noun, and the
	// new/edited signal rides on the line's Change.
	if ln := cards[0].Groups[0].Lines[0]; ln.Detail != "notebook entry" || ln.Change != "new" {
		t.Fatalf("entry line detail=%q change=%q, want \"notebook entry\"/\"new\"", ln.Detail, ln.Change)
	}
}

// TestClickUpChangeDiff pins the snapshot-diff "what changed" text: a status move
// reads "Backlog → In Progress", an assignee add reads "assigned …" — deterministic,
// no LLM.
func TestClickUpChangeDiff(t *testing.T) {
	loc := chicago(t)
	now := time.Date(2026, 7, 23, 15, 0, 0, 0, loc)
	// prior snapshot: task was in Backlog, unassigned.
	prior := map[string]string{"t9": cuSnap{Status: "Backlog"}.encode()}
	c := newClickUp("pk_x", "http://unused", nil)
	task := cuTask{
		ID: "t9", Name: "Kill curve", URL: "u",
		DateUpdated: strconv.FormatInt(now.UnixMilli(), 10),
		DateCreated: strconv.FormatInt(now.Add(-72*time.Hour).UnixMilli(), 10),
	}
	task.Status.Status = "In Progress"
	old, had := decodeSnap(prior["t9"])
	ev := c.classify(task, 0, now, old, had)
	if ev.Change != "Backlog → In Progress" {
		t.Fatalf("status move change = %q, want \"Backlog → In Progress\"", ev.Change)
	}
	// Same status, a new assignee → "assigned Ellie".
	prior2 := cuSnap{Status: "In Progress"}.encode()
	task.Assignees = append(task.Assignees, struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}{ID: 5, Username: "Ellie"})
	old2, had2 := decodeSnap(prior2)
	if ev := c.classify(task, 0, now, old2, had2); ev.Change != "assigned Ellie" {
		t.Fatalf("assignee-add change = %q, want \"assigned Ellie\"", ev.Change)
	}
}

// ---- Benchling day digest: saves collapse per object, lines carry the count,
// who and kind, group by project; a dismissed day resurfaces only with what
// landed after the dismissal (2026-09-20) ----

func TestBenchlingDigestCollapsesSaves(t *testing.T) {
	loc := chicago(t)
	day := "2026-09-20"
	at := func(h, m int) time.Time { return time.Date(2026, 9, 20, h, m, 0, 0, loc) }
	ev := func(kind, ext, title, detail, change, actor, proj string, ts time.Time) Event {
		return Event{ID: "benchling:" + kind + ":" + ext + ":" + ts.UTC().Format("20060102150405"), Portal: "benchling", Kind: kind,
			Title: title, Detail: detail, Change: change, Actor: actor, At: ts,
			URL: "https://aion.benchling.com/aion/f/lib_n6yfjIcu9i-" + proj + "/" + ext + "-x/edit"}
	}
	var events []Event
	for i := 0; i < 18; i++ { // one notebook entry saved eighteen times
		events = append(events, ev("entry", "etr_1", "W-008 Results table", "notebook entry", "edited", "Ellie", "w-008-magnetoacoustics", at(9, i)))
	}
	events = append(events,
		ev("entity", "bfi_1", "GPLD7M - 2", "3_endpoint RNA seq", "new", "Aion", "aion-registry", at(10, 0)),
		ev("entity", "bfi_1", "GPLD7M - 2", "3_endpoint RNA seq", "edited", "Ellie", "aion-registry", at(11, 0)),
		ev("entry", "etr_2", "Thymus culture", "notebook entry", "edited", "Ellie", "w-008-magnetoacoustics", at(12, 0)),
		ev("entry", "etr_3", "Yesterday", "notebook entry", "edited", "Ellie", "w-008-magnetoacoustics", time.Date(2026, 9, 19, 12, 0, 0, 0, loc)),
	)
	id, groups, summary, latest := buildBenchlingDigest(events, day, loc, time.Time{})
	if id != "benchling-digest:2026-09-20" || !latest.Equal(at(12, 0)) {
		t.Fatalf("id/latest = %s %v", id, latest)
	}
	if summary != "21 changes · 3 items · Ellie, Aion" {
		t.Fatalf("summary = %q", summary)
	}
	if len(groups) != 2 || groups[0].List != "w 008 magnetoacoustics" || groups[1].List != "aion registry" {
		t.Fatalf("groups = %+v", groups)
	}
	// busiest project first; within it the latest change first
	w := groups[0].Lines
	if len(w) != 2 || w[0].Text != "Thymus culture" || w[1].Text != "W-008 Results table" {
		t.Fatalf("w-008 lines = %+v", w)
	}
	if w[1].Count != 18 || w[1].Who != "Ellie" || w[1].Detail != "notebook entry" || w[1].Change != "edited" {
		t.Fatalf("collapsed line = %+v", w[1])
	}
	// a new-then-edited entity reads as new, and names both people in order
	g := groups[1].Lines[0]
	if g.Count != 2 || g.Change != "new" || g.Who != "Aion, Ellie" {
		t.Fatalf("entity line = %+v", g)
	}
	// dismissed at 11:30: only the 12:00 change comes back
	_, groups, summary, _ = buildBenchlingDigest(events, day, loc, at(11, 30))
	if len(groups) != 1 || len(groups[0].Lines) != 1 || groups[0].Lines[0].Text != "Thymus culture" || summary != "1 change · 1 item · Ellie" {
		t.Fatalf("after dismissal = %+v %q", groups, summary)
	}
	// dismissed after the last change: nothing to show
	if _, groups, _, _ = buildBenchlingDigest(events, day, loc, at(12, 0)); groups != nil {
		t.Fatalf("expected no groups after a late dismissal, got %+v", groups)
	}
	// no folder in the URL → the "—" group
	if p := benchProject("https://aion.benchling.com/"); p != "" {
		t.Fatalf("project = %q", p)
	}
}
