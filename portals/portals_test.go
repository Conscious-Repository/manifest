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
	// The assignee (Benjamin, id 42) task appears in the "for you" block.
	if len(c.ForYou) != 1 || c.ForYou[0].Text != "closed · Close 743 N Euclid" {
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
	if len(cards) != 2 {
		t.Fatalf("want 2 benchling cards (entity + result), got %d: %+v", len(cards), cards)
	}
	// The nameless assay result identifies by schema + id.
	var sawResult bool
	for _, c := range cards {
		if c.Type != "portal-item" || c.Portal != "benchling" {
			t.Fatalf("bad card: %+v", c)
		}
		if c.Title == "qPCR res_9" {
			sawResult = true
		}
	}
	if !sawResult {
		t.Fatalf("assay result card missing: %+v", cards)
	}

	// Degraded: a failed poll keeps the last-good cache (no emptied inbox).
	fail = true
	svc.pollOne(context.Background(), mustDef("benchling"))
	if n := svc.InboxCount(); n != 2 {
		t.Fatalf("failed poll emptied the cache: InboxCount = %d, want 2", n)
	}
	row := svc.row(mustDef("benchling"))
	if row.State != StateDegraded || row.Err == "" {
		t.Fatalf("row should be degraded with a reason: %+v", row)
	}
}
