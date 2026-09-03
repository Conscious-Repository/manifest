package recruiting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every test here runs against testdata/ashby-jobboard.json (a scrubbed
// capture of the real AION board: same field set, same value vocabulary,
// synthetic ids and abbreviated text) served over httptest. No test makes a
// network call; the real URL is only ever compared as a string.

func ashbyFixture(t *testing.T) []byte {
	t.Helper()
	return []byte(read(t, "ashby-jobboard.json"))
}

// ashbyServer serves the given body/status as the public board and returns a
// client bound to it.
func ashbyServer(t *testing.T, status int, body []byte) *AshbyPublic {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("public mirror used %s, not GET", r.Method)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("public mirror sent an Authorization header — Phase 2 has no key")
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return NewAshbyPublic(srv.URL+"/posting-api/job-board/AION%20Biosciences?includeCompensation=true", srv.Client())
}

// sectionBytes returns the verbatim text of one `## heading` section — the
// heading line through the line before the next heading (or EOF).
func sectionBytes(content, heading string) string {
	marker := "## " + heading
	at := strings.Index(content, marker+"\n")
	if at < 0 {
		return ""
	}
	rest := content[at+len(marker)+1:]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next+1]
	}
	return marker + "\n" + rest
}

// The default client points at the real board — asserted as a string, never
// dialled — and the fixture parses into the four AION lanes with the values
// a role record carries.
func TestAshbyPublicParsesFixture(t *testing.T) {
	if NewAshbyPublic("", nil).url != AshbyPublicBoardURL {
		t.Fatalf("default url is not the public board")
	}
	if !strings.HasPrefix(AshbyPublicBoardURL, "https://api.ashbyhq.com/posting-api/job-board/") {
		t.Fatalf("not the posting API: %s", AshbyPublicBoardURL)
	}
	posts, err := ParseAshbyBoard(ashbyFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 4 {
		t.Fatalf("postings=%d, want 4", len(posts))
	}
	want := map[string]bool{"Mechanical Engineer": true, "Biomedical Engineer": true,
		"MRI Engineer": true, "Scientist: Microscopy": true}
	for _, p := range posts {
		if !want[p.Title] {
			t.Errorf("unexpected posting %q", p.Title)
		}
		delete(want, p.Title)
		if p.ID == "" || !strings.HasPrefix(p.ID, "00000000-0000-4000-8000-") {
			t.Errorf("%s: id %q is not the fixture's synthetic id", p.Title, p.ID)
		}
		if p.Location != "St. Louis, Missouri" {
			t.Errorf("%s: location %q", p.Title, p.Location)
		}
		if p.Employment != "full-time on-site" {
			t.Errorf("%s: employment %q", p.Title, p.Employment)
		}
		if !p.Listed || p.Remote {
			t.Errorf("%s: listed=%v remote=%v", p.Title, p.Listed, p.Remote)
		}
		if !strings.Contains(p.Description, "The role") || !strings.Contains(p.JobURL, p.ID) {
			t.Errorf("%s: description/jobUrl not carried: %q %q", p.Title, p.Description[:20], p.JobURL)
		}
	}
	if len(want) != 0 {
		t.Fatalf("postings missing: %v", want)
	}
}

// Fetch over httptest: the fixture maps to the four seeded roles by title,
// each record picks up exactly the Ashby-owned keys, and a second sync on
// the same day writes nothing.
func TestAshbySyncMapsFixtureOntoFourRoles(t *testing.T) {
	s, vault := testStore(t)
	client := ashbyServer(t, http.StatusOK, ashbyFixture(t))
	posts, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.SyncAshbyPostings(posts, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.Postings != 4 || len(res.Updated) != 4 || len(res.Created) != 0 || len(res.Unlisted) != 0 {
		t.Fatalf("result: %+v", res)
	}
	for _, slug := range []string{"mri-engineer", "mechanical-engineer", "biomedical-engineer", "scientist-microscopy"} {
		raw, err := os.ReadFile(filepath.Join(vault, "system/aion/recruiting/roles", slug+".md"))
		if err != nil {
			t.Fatal(err)
		}
		d := ParseRole(string(raw))
		if d.Get("source") != SourceAshbyPublic || d.Get("synced") != "2026-09-02" {
			t.Errorf("%s: source=%q synced=%q", slug, d.Get("source"), d.Get("synced"))
		}
		if d.Get("ashby_posting_id") == "" || d.Get("ashby_job_id") != "" || d.Get("ashby_project_id") != "" {
			t.Errorf("%s: posting id %q; job/project ids must stay blank (%q/%q)", slug,
				d.Get("ashby_posting_id"), d.Get("ashby_job_id"), d.Get("ashby_project_id"))
		}
		if d.Get("location") != "St. Louis, Missouri" || d.Get("employment") != "full-time on-site" {
			t.Errorf("%s: location=%q employment=%q", slug, d.Get("location"), d.Get("employment"))
		}
		if d.Get("handoff_mode") != "" || d.Get("status") != "open" {
			t.Errorf("%s: a non-owned key moved: handoff=%q status=%q", slug, d.Get("handoff_mode"), d.Get("status"))
		}
		if !strings.Contains(d.Posting(), "The role") || !strings.Contains(d.Posting(), "St. Louis, MO (on-site).") {
			t.Errorf("%s: posting body not mirrored:\n%s", slug, d.Posting())
		}
		if out := SerializeRole(d); out != string(raw) {
			t.Errorf("%s: a synced record is not a fixpoint:\n%s", slug, firstDiff(string(raw), out))
		}
	}
	if len(s.RoleSlugs()) != 4 {
		t.Fatalf("roles after sync: %v", s.RoleSlugs())
	}

	again, err := s.SyncAshbyPostings(posts, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Unchanged) != 4 || len(again.Updated) != 0 {
		t.Fatalf("second sync was not a no-op: %+v", again)
	}
}

// The invariant the phase exists to keep: `## criteria` is byte-identical
// across a sync, and so is everything else the mirror does not own — the
// sourcing terms, a hand-added section, reordered frontmatter, a hand-edited
// posting id that now matches.
func TestAshbySyncPreservesCriteriaByteForByte(t *testing.T) {
	s, vault := testStore(t)
	dir := filepath.Join(vault, "system/aion/recruiting/roles")

	// hand-edit the MRI role the way Obsidian would: an unknown frontmatter
	// key, a note section after the posting, a stale posting body
	edited := `---
id: role/mri-engineer
owner_note: keep
title: MRI Engineer
status: open
location: Saint Louis, MO
employment: full-time on-site
ashby_job_id:
ashby_posting_id:
ashby_project_id:
handoff_mode:
pinned: true
source: owner
synced:
---

## criteria
- [criterion:: low-field MRI hardware] [class:: must] [weight:: 3] [note:: hand-added]
- [weight:: 3] [class:: must] [criterion:: pulse sequence or coil design]
<!-- a comment Benjamin left -->
- [criterion:: requires full remote] [class:: disqualifier]

## sourcing
- [term:: low-field MRI] [term:: coil design]

## posting
an old posting body that must go

## notes
- keep me verbatim
`
	if err := os.WriteFile(filepath.Join(dir, "mri-engineer.md"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, slug := range s.RoleSlugs() {
		b, _ := os.ReadFile(filepath.Join(dir, slug+".md"))
		before[slug] = string(b)
	}

	posts, err := ParseAshbyBoard(ashbyFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncAshbyPostings(posts, testNow); err != nil {
		t.Fatal(err)
	}

	for slug, was := range before {
		b, _ := os.ReadFile(filepath.Join(dir, slug+".md"))
		now := string(b)
		for _, heading := range []string{"criteria", "sourcing", "notes"} {
			if sectionBytes(was, heading) != sectionBytes(now, heading) {
				t.Errorf("%s: `## %s` changed across sync:\n--- before\n%s--- after\n%s",
					slug, heading, sectionBytes(was, heading), sectionBytes(now, heading))
			}
		}
		// every frontmatter line the mirror does not own is untouched, in place
		wasFM, nowFM := ParseRole(was).FM, ParseRole(now).FM
		if len(wasFM) != len(nowFM) {
			t.Fatalf("%s: frontmatter grew from %d to %d lines", slug, len(wasFM), len(nowFM))
		}
		for i := range wasFM {
			key, _, _ := strings.Cut(wasFM[i], ":")
			if inSetFold(ashbyPostingKeys, strings.TrimSpace(key)) {
				continue
			}
			if wasFM[i] != nowFM[i] {
				t.Errorf("%s: non-owned frontmatter line %d moved: %q → %q", slug, i, wasFM[i], nowFM[i])
			}
		}
	}

	mri := ParseRole(before["mri-engineer"])
	after := s.LoadRole("mri-engineer")
	if after.Get("owner_note") != "keep" || after.Get("source") != SourceAshbyPublic {
		t.Fatalf("mri: owner_note=%q source=%q", after.Get("owner_note"), after.Get("source"))
	}
	if strings.Contains(after.Posting(), "an old posting body") || !strings.Contains(after.Posting(), "MRI Engineer") {
		t.Fatalf("mri: posting not replaced:\n%s", after.Posting())
	}
	if got, want := len(after.Criteria()), len(mri.Criteria()); got != want {
		t.Fatalf("mri: criteria count %d → %d", want, got)
	}
	if !strings.HasSuffix(SerializeRole(after), "## notes\n- keep me verbatim\n") {
		t.Fatalf("mri: the trailing notes section did not survive:\n%s", SerializeRole(after))
	}
}

// A posting the vault has no lane for becomes a new role with the owner's
// empty criteria; a lane the board dropped is reported and left alone; a
// posting id match wins over a title match.
func TestAshbySyncCreatesAndReports(t *testing.T) {
	s, _ := testStore(t)
	posts, err := ParseAshbyBoard(ashbyFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncAshbyPostings(posts, testNow); err != nil {
		t.Fatal(err)
	}

	// the board renames the MRI posting and adds a lane
	renamed := posts[2]
	renamed.Title = "Low-Field MRI Engineer"
	next := []AshbyPosting{renamed, {ID: "00000000-0000-4000-8000-0000000000ff",
		Title: "Firmware Engineer", Location: "St. Louis, Missouri", Employment: "full-time on-site",
		Listed: true, Description: "## Details\nfirmware."}}
	res, err := s.SyncAshbyPostings(next, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Updated, ",") != "mri-engineer" {
		t.Fatalf("posting-id match did not win: %+v", res)
	}
	if strings.Join(res.Created, ",") != "firmware-engineer" {
		t.Fatalf("created: %+v", res)
	}
	if strings.Join(res.Unlisted, ",") != "biomedical-engineer,mechanical-engineer,scientist-microscopy" {
		t.Fatalf("unlisted: %+v", res)
	}
	mri := s.LoadRole("mri-engineer")
	if mri.Get("title") != "Low-Field MRI Engineer" || mri.Get("id") != "role/mri-engineer" {
		t.Fatalf("mri: title=%q id=%q", mri.Get("title"), mri.Get("id"))
	}
	if len(mri.Criteria()) != 5 {
		t.Fatalf("mri criteria moved: %d", len(mri.Criteria()))
	}
	fw := s.LoadRole("firmware-engineer")
	if fw.Get("id") != "role/firmware-engineer" || fw.Get("pinned") != "false" || fw.Get("status") != "open" {
		t.Fatalf("new role frontmatter: %v", fw.FM)
	}
	if n := len(fw.Criteria()); n != 2 {
		t.Fatalf("new role criteria: want the owner seed (2), got %d", n)
	}
	// a heading inside mirrored text cannot swallow the record's sections
	if fw.Posting() != "\\## Details\nfirmware." || section(fw.Sections, "Details") != nil {
		t.Fatalf("posting heading not escaped: %q", fw.Posting())
	}
	// the dropped lanes are untouched
	if s.LoadRole("mechanical-engineer").Get("status") != "open" {
		t.Fatalf("an unlisted lane was changed")
	}
}

// Malformed and empty responses are refused by name, and none of them
// touches the vault. An honest `jobs: []` is zero postings, zero writes.
func TestAshbyPublicHandlesBadResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"http 500", http.StatusInternalServerError, `{"jobs":[]}`, "HTTP 500"},
		{"http 404", http.StatusNotFound, ``, "HTTP 404"},
		{"empty body", http.StatusOK, ``, "empty response"},
		{"html", http.StatusOK, `<html>maintenance</html>`, "not JSON"},
		{"no jobs field", http.StatusOK, `{"apiVersion":1}`, "no jobs field"},
		{"jobs not a list", http.StatusOK, `{"jobs":{"id":"x"}}`, "not JSON"},
		{"job without id", http.StatusOK, `{"jobs":[{"title":"Ghost"}]}`, "no id or title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := ashbyServer(t, tc.status, []byte(tc.body))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			posts, err := client.Fetch(ctx)
			if err == nil {
				t.Fatalf("no error; posts=%+v", posts)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%q, want it to name %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "maintenance") {
				t.Fatalf("error echoed the body: %q", err)
			}
		})
	}

	// an empty board: nothing matched, nothing written
	writes := 0
	vault := t.TempDir()
	s := NewStore(vault, "system/aion/recruiting", func(abs string, b []byte) error {
		writes++
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, b, 0o644)
	})
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	seeded := writes
	posts, err := ashbyServer(t, http.StatusOK, []byte(`{"apiVersion":1,"jobs":[]}`)).Fetch(context.Background())
	if err != nil || len(posts) != 0 {
		t.Fatalf("empty board: posts=%v err=%v", posts, err)
	}
	res, err := s.SyncAshbyPostings(posts, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.Postings != 0 || len(res.Unlisted) != 4 || len(res.Updated)+len(res.Created) != 0 {
		t.Fatalf("empty board result: %+v", res)
	}
	if writes != seeded {
		t.Fatalf("an empty board wrote %d records", writes-seeded)
	}
	if s.LoadRole("mri-engineer").Get("source") != "owner" {
		t.Fatalf("an empty board changed a role")
	}
}

// A write refused by the boundary surfaces as the sync's error, not a
// half-mirrored board that reports success.
func TestAshbySyncSurfacesWriteRefusal(t *testing.T) {
	s, _ := testStore(t)
	posts, err := ParseAshbyBoard(ashbyFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	s.write = func(string, []byte) error { return errf("refused by capability") }
	if _, err := s.SyncAshbyPostings(posts, testNow); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("err=%v", err)
	}
}
