package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// The fetched half of the typing cascade, through the real handler: rungs
// 3–5 (ask the page) and rung 4 (ask GitHub) run when — and ONLY when — the
// pure rungs left the question open.

// stubWeb answers the page probe. It records every URL it was asked about,
// which is how these tests prove a probe did NOT happen.
type stubWeb struct {
	sources.Manual
	types sources.PageTypes
	err   error
	asked []string
}

func (s *stubWeb) ID() string { return "web" }
func (s *stubWeb) ProbeTypes(_ context.Context, url string) (sources.PageTypes, error) {
	s.asked = append(s.asked, url)
	if s.err != nil {
		return sources.PageTypes{}, s.err
	}
	return s.types, nil
}

// stubGitHub answers the account probe.
type stubGitHub struct {
	sources.Manual
	kind  string
	err   error
	asked []string
}

func (s *stubGitHub) ID() string { return "github" }
func (s *stubGitHub) AccountType(_ context.Context, login string) (string, error) {
	s.asked = append(s.asked, login)
	if s.err != nil {
		return "", s.err
	}
	return s.kind, nil
}

func testCascadeServer(t *testing.T, adapters ...sources.Adapter) http.Handler {
	t.Helper()
	s, _, _, _ := testRecruitingServer(t)
	rs, err := recruiting.NewRunStore(filepath.Join(t.TempDir(), "recruiting", "runs"), s.recruiting)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range adapters {
		rs.Register(a)
	}
	s.UseRecruitingRuns(rs)
	return s.Handler()
}

func previewResolution(t *testing.T, mux http.Handler, paste string) recruiting.Resolution {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"text": paste})
	w := sourcesDo(t, mux, http.MethodPost, "/api/aion/recruiting/intake/preview", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("preview %q → %d: %s", paste, w.Code, w.Body.String())
	}
	var out struct {
		Resolution recruiting.Resolution `json:"resolution"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Resolution
}

// A lab page is the case this rung exists for: one host, one path, no
// identifier — and the page's own JSON-LD says what it is.
func TestPreviewAsksThePageWhatItIs(t *testing.T) {
	web := &stubWeb{types: sources.PageTypes{
		JSONLD: []string{"WebPage", "ResearchOrganization"}, OGType: "website"}}
	mux := testCascadeServer(t, web)

	got := previewResolution(t, mux, "https://yablonskiylab.wustl.edu/")
	if got.Class != recruiting.SeedLab {
		t.Fatalf("the page said ResearchOrganization: %+v", got)
	}
	if got.Rung != recruiting.RungPage || got.Asked != "ResearchOrganization" {
		t.Fatalf("the answer must name the rung and what it read: %+v", got)
	}
	if len(web.asked) != 1 || web.asked[0] != "https://yablonskiylab.wustl.edu/" {
		t.Fatalf("probed: %v", web.asked)
	}
}

// ⚠ THE COST RULE. A resolution that already knows what it is makes no
// request at all — a DOI does not need a lab crawler's opinion, and every
// probe is a round trip the owner waits through.
func TestPreviewDoesNotProbeWhatItAlreadyKnows(t *testing.T) {
	web := &stubWeb{types: sources.PageTypes{JSONLD: []string{"Person"}}}
	gh := &stubGitHub{kind: "Organization"}
	mux := testCascadeServer(t, web, gh)

	for _, paste := range []string{
		"10.1038/s41586-020-2649-2",
		"0000-0002-5263-5070",
		"https://github.com/numpy/numpy",
		"https://openalex.org/W3005144120",
	} {
		got := previewResolution(t, mux, paste)
		if got.Rung != recruiting.RungIdentifier && got.Rung != recruiting.RungHost {
			t.Errorf("%q was answered by %q", paste, got.Rung)
		}
	}
	if len(web.asked) != 0 || len(gh.asked) != 0 {
		t.Fatalf("a certain resolution probed anyway: web=%v github=%v", web.asked, gh.asked)
	}
}

// D12 has no loophole here: LinkedIn resolves LinkOnly, and LinkOnly means
// nothing fetches it — including the probe.
func TestPreviewNeverProbesALinkOnlyResolution(t *testing.T) {
	web := &stubWeb{types: sources.PageTypes{JSONLD: []string{"Person"}}}
	mux := testCascadeServer(t, web)
	for _, paste := range []string{
		"https://www.linkedin.com/company/acme-bio",
		"https://x.com/karpathy",
		"https://scholar.google.com/citations?user=abc",
	} {
		if got := previewResolution(t, mux, paste); !got.LinkOnly {
			t.Fatalf("%q should be link-only: %+v", paste, got)
		}
	}
	if len(web.asked) != 0 {
		t.Fatalf("a link-only resolution was fetched: %v", web.asked)
	}
}

// Rung 4: github.com/<login> is the ambiguity a URL cannot settle.
func TestPreviewAsksGitHubWhatAnAccountIs(t *testing.T) {
	gh := &stubGitHub{kind: "Organization"}
	web := &stubWeb{types: sources.PageTypes{JSONLD: []string{"Person"}}}
	mux := testCascadeServer(t, gh, web)

	got := previewResolution(t, mux, "https://github.com/numpy")
	if got.Class != recruiting.SeedCompany || got.Dest != recruiting.DestSeed {
		t.Fatalf("GitHub said Organization: %+v", got)
	}
	if got.Rung != recruiting.RungAccount {
		t.Fatalf("rung: %+v", got)
	}
	if len(gh.asked) != 1 || gh.asked[0] != "numpy" {
		t.Fatalf("asked: %v", gh.asked)
	}
	// the account host answers for its own accounts — the web probe has no
	// business in it
	if len(web.asked) != 0 {
		t.Fatalf("the page was probed for a GitHub account: %v", web.asked)
	}
}

// ⚠ A PROBE THAT FAILS IS SILENT. It is an addition to an answer we already
// have: a site that 404s, blocks robots or ships broken JSON-LD leaves the
// owner exactly the scaffold they would have had, and never an error about a
// request they did not ask for.
func TestAFailedProbeLeavesTheScaffoldStanding(t *testing.T) {
	web := &stubWeb{err: errors.New("robots.txt disallows /")}
	gh := &stubGitHub{err: errors.New("404")}
	mux := testCascadeServer(t, web, gh)

	site := previewResolution(t, mux, "https://lab.example.edu/")
	if site.Rung != recruiting.RungHost || len(site.Suggest) == 0 {
		t.Fatalf("the host rung's answer must stand: %+v", site)
	}
	acct := previewResolution(t, mux, "https://github.com/numpy")
	if acct.Class != recruiting.SeedPerson || acct.Rung != recruiting.RungHost {
		t.Fatalf("the guess stands when GitHub will not answer: %+v", acct)
	}
}

// With no adapters wired at all — a cockpit without the source layer — the
// pure rungs still answer, because they never needed anything.
func TestTheCascadeDegradesToItsPureRungs(t *testing.T) {
	mux := testCascadeServer(t)
	if got := previewResolution(t, mux, "10.1038/s41586-020-2649-2"); got.Class != recruiting.SeedWork {
		t.Fatalf("a DOI needs nothing wired: %+v", got)
	}
	if got := previewResolution(t, mux, "https://lab.example.edu/"); got.Rung != recruiting.RungHost {
		t.Fatalf("the host rung still answers: %+v", got)
	}
}
