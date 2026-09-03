package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"manifest/aion"
	"manifest/record"
	"manifest/recruiting"
	"manifest/vaultwriter"
)

// The leak suite (plan §6). Candidate PII must never reach the public AION
// portal pack or the Kairos context pack — not because the render happens to
// omit it today, but because there is no path by which it could arrive.
//
// R2 is the reason this is a suite rather than a single assertion:
// system/aion/hiring.md is parsed by aion/hiring.go, shipped by the
// projection, written verbatim into server/web/portal/content/hiring.md, and
// served open-read at portal.aion.bio BEFORE any sign-in gate. A recruiting
// record that ever reached that file would be public immediately.

// recruitingCanaries are strings that exist ONLY in the private recruiting
// records — a candidate name, an address, an evidence quote, an edge basis.
// Any one of them appearing in a rendered contract file or a pack file is a
// leak, and the test says which surface leaked which fact.
var recruitingCanaries = map[string]string{
	"candidate name":  "CANARY-candidate-thessaly-vane",
	"candidate email": "CANARY-candidate-email@example.test",
	"evidence quote":  "CANARY-evidence-verbatim-quote",
	"edge basis":      "CANARY-edge-basis-same-lab",
	"seed name":       "CANARY-seed-target-lab",
	"profile org":     "CANARY-candidate-employer",
}

// leakVault builds a vault holding BOTH the seven aion corpora and a
// recruiting tree stuffed with canaries, wired the way production wires them:
// one vaultwriter, two capabilities, two stores that do not know about each
// other.
func leakVault(t *testing.T) (*Server, string) {
	t.Helper()
	vault := t.TempDir()
	vw := vaultwriter.New(vault).WithAudit(t.TempDir()).Grant(
		vaultwriter.Capability{Name: "aion", Zone: record.ZoneSystem,
			Pattern: "system/aion/**", Actor: vaultwriter.ActorUserAction},
		vaultwriter.Capability{Name: "aion-recruiting", Zone: record.ZoneSystem,
			Pattern: "system/aion/recruiting/**", Actor: vaultwriter.ActorUserAction},
	)
	aionStore := aion.NewStore(vault, "system/aion", vw.BindAbs("aion"))
	for name, body := range aion.SeedFiles {
		if err := vw.WriteCap("aion", "system/aion/"+name, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	recStore := recruiting.NewStore(vault, "system/aion/recruiting", vw.BindAbs("aion-recruiting"))
	if err := recStore.Ensure(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	c, err := recStore.AddCandidate(recruiting.QuickAdd{
		Text: recruitingCanaries["candidate name"], Role: "role/mri-engineer",
		Org:   recruitingCanaries["profile org"],
		Known: true, KnownVia: "aion-net/ben-anderson",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recStore.UpdateCandidate(c.ID, map[string]string{
		"email": recruitingCanaries["candidate email"],
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := recStore.AddEvidence(c.ID, recruiting.Evidence{
		URL: "https://example.test/a", Kind: "publication",
		Snippet: recruitingCanaries["evidence quote"],
	}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := recStore.AddSeed(recruiting.Seed{
		Class: "lab", Name: recruitingCanaries["seed name"],
	}, now); err != nil {
		t.Fatal(err)
	}
	edges := recStore.LoadEdges()
	if _, err := edges.Add(recruiting.Edge{
		From: "aion-net/ben-anderson", To: c.ID, Kind: "same_lab",
		Basis: recruitingCanaries["edge basis"], Confidence: "0.45",
		Inferred: true, Source: "public_profile",
	}); err != nil {
		t.Fatal(err)
	}
	if err := recStore.SaveEdges(edges); err != nil {
		t.Fatal(err)
	}
	// the canaries really are on disk, or the whole suite is vacuous
	found := map[string]bool{}
	_ = filepath.Walk(filepath.Join(vault, "system", "aion", "recruiting"),
		func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return nil
			}
			b, _ := os.ReadFile(p)
			for name, canary := range recruitingCanaries {
				if strings.Contains(string(b), canary) {
					found[name] = true
				}
			}
			return nil
		})
	for name := range recruitingCanaries {
		if !found[name] {
			t.Fatalf("the %s canary is not in the vault — this suite would pass "+
				"against a leak", name)
		}
	}
	return &Server{aion: aionStore, recruiting: recStore}, vault
}

// The rendered contract — the nine files AionLive serves the team portal —
// and the kairos pack rendered from them must contain no recruiting fact.
func TestRecruitingNeverEntersPortalContract(t *testing.T) {
	s, _ := leakVault(t)

	rendered, err := aion.RenderContract(s.aionExportInput("2026-09-02T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered) != len(aion.ContractPaths()) {
		t.Fatalf("rendered %d files, want %d", len(rendered), len(aion.ContractPaths()))
	}
	for p, b := range rendered {
		for name, canary := range recruitingCanaries {
			if strings.Contains(string(b), canary) {
				t.Errorf("the portal contract file %s leaked the %s", p, name)
			}
		}
	}

	// the kairos pack is rendered FROM the contract, so it inherits the
	// guarantee — assert it rather than assuming it
	snap := &aionPackSnapshot{Revision: "test", At: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
		Files: map[string][]byte{}}
	for contractPath, b := range rendered {
		snap.Files[contractURLPath(contractPath)] = b
	}
	pack := aionPackRender(snap)
	if len(pack) == 0 {
		t.Fatal("the pack rendered nothing — this assertion would be vacuous")
	}
	for name, body := range pack {
		for canaryName, canary := range recruitingCanaries {
			if strings.Contains(body, canary) {
				t.Errorf("the kairos pack file %s leaked the %s", name, canaryName)
			}
		}
	}
}

// The widening tripwire. The export contract is nine files; a tenth is how a
// private surface would arrive at the portal without anyone deciding to send
// it there.
func TestAionContractPathsAreExactlyNine(t *testing.T) {
	paths := aion.ContractPaths()
	if len(paths) != 9 {
		t.Fatalf("the contract is %d files, not nine: %v", len(paths), paths)
	}
	want := map[string]bool{
		"server/web/portal/content/hiring.md":     true,
		"server/web/portal/content/references.md": true,
		"server/web/portal/data/finances.json":    true,
		"server/web/portal/data/vto.json":         true,
		"server/web/portal/data/goals.json":       true,
		"server/web/portal/data/backlog.json":     true,
		"server/web/portal/data/heuristics.json":  true,
		"server/web/portal/data/people.json":      true,
		"server/web/portal/data/meta.json":        true,
	}
	for _, p := range paths {
		if !want[p] {
			t.Errorf("the contract gained %s", p)
		}
		delete(want, p)
		if strings.Contains(p, "recruit") || strings.Contains(p, "candidate") {
			t.Errorf("a recruiting surface entered the contract: %s", p)
		}
	}
	for p := range want {
		t.Errorf("the contract lost %s", p)
	}
}

// The portal listener builds its own mux (server/portal.go) and gains nothing
// from the cockpit's. Every recruiting path must 404 there — the privacy
// boundary is the mux, not the path string.
func TestRecruitingRoutesAbsentFromPortalMux(t *testing.T) {
	s, _ := leakVault(t)
	s.UseAion(s.aion, "", "", "", t.TempDir())
	h, err := PortalHandler(PortalOptions{Live: s.aionLive})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"/api/aion/recruiting",
		"/api/aion/recruiting/seeds",
		"/api/aion/recruiting/roles/mri-engineer",
		"/api/aion/recruiting/candidate",
		"/api/aion/recruiting/candidate/stage/cand/x",
		"/api/aion/recruiting/candidate/evidence/cand/x",
		"/api/aion/recruiting/network",
		"/content/recruiting.md",
		"/data/recruiting.json",
		"/data/candidates.json",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, p, strings.NewReader("{}")))
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s answered %d on the portal listener, want 404\n%s",
					method, p, w.Code, w.Body.String())
			}
		}
	}
}

// The compile-time canary: the recruiting service is handed a store and
// NOTHING else. If this ever fails, someone gave the private domain a handle
// on the public projection, and every other guard here becomes advisory.
func TestRecruitingStoreRootIsNotAnExportInput(t *testing.T) {
	ty := reflect.TypeOf(recruiting.Store{})
	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		name := f.Type.String()
		if strings.Contains(name, "aion") || strings.Contains(name, "AionLive") {
			t.Errorf("recruiting.Store.%s is %s — the private store must not hold "+
				"the public projection", f.Name, name)
		}
	}
	// and the export input is assembled from the aion store alone
	in := reflect.TypeOf(aion.ExportInput{})
	for i := 0; i < in.NumField(); i++ {
		f := in.Field(i)
		if strings.Contains(strings.ToLower(f.Name), "recruit") ||
			strings.Contains(strings.ToLower(f.Name), "candidate") ||
			strings.Contains(f.Type.String(), "recruiting.") {
			t.Errorf("aion.ExportInput gained %s %s — the contract takes no "+
				"recruiting input, ever", f.Name, f.Type)
		}
	}
}

// §4.3 Option A: the `aion` capability's system/aion/** pattern nominally
// includes system/aion/recruiting/**, and that overlap is ACCEPTED rather
// than declared away. What makes it safe is not the declaration — it is that
// aion.Store.Path joins a BARE FILENAME onto its root and is only ever called
// with a member of aion.Files. These two assertions are the practical
// boundary, standing in for the Capability.Except field that Option B would
// have added.
func TestAionFilesCannotAddressRecruiting(t *testing.T) {
	// (i) no addressable aion corpus name contains a path separator, so no
	// aion.Store call can descend into a subdirectory at all
	names := append([]string{}, aion.Files...)
	for name := range aion.Corpora {
		names = append(names, name)
	}
	for name := range aion.SeedFiles {
		names = append(names, name)
	}
	for _, name := range names {
		if strings.ContainsAny(name, `/\`) {
			t.Errorf("the aion corpus name %q contains a path separator — the "+
				"bare-filename property is what keeps system/aion/recruiting/ out "+
				"of reach", name)
		}
		if name != path.Clean(name) || strings.HasPrefix(name, ".") {
			t.Errorf("the aion corpus name %q is not a plain filename", name)
		}
	}

	// (ii) every filename aion.Store CAN address resolves outside recruiting/
	vault := t.TempDir()
	store := aion.NewStore(vault, "system/aion", nil)
	recRoot := filepath.Join(vault, "system", "aion", "recruiting") + string(filepath.Separator)
	for _, name := range names {
		abs := store.Path(name)
		if strings.HasPrefix(abs, recRoot) {
			t.Errorf("aion.Store.Path(%q) = %q, inside the private recruiting root", name, abs)
		}
		if filepath.Dir(abs) != filepath.Join(vault, "system", "aion") {
			t.Errorf("aion.Store.Path(%q) escaped its own root: %q", name, abs)
		}
	}

	// and the recruiting store's own paths all land INSIDE it
	rec := recruiting.NewStore(vault, "system/aion/recruiting", nil)
	for _, name := range append(append([]string{}, recruiting.SeedOrder...), recruiting.Files...) {
		if !strings.HasPrefix(rec.Path(name), recRoot) {
			t.Errorf("recruiting.Store.Path(%q) = %q, outside its own root", name, rec.Path(name))
		}
	}
	// D9: the domain never addresses the published hiring file
	if strings.HasSuffix(rec.Rel("hiring.md"), "system/aion/hiring.md") {
		t.Error("a recruiting write could reach the published hiring file")
	}
}
