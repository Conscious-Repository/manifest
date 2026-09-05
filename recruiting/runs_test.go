package recruiting

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"manifest/recruiting/sources"
)

// The source-run substrate (plan §4.9, D14/D15), proven from the outside:
// a run is a cache in dataDir, never a record; a dry run touches nothing;
// accept moves exactly ONE draft into exactly ONE record through the
// converter gate; the cache is private and sweeps itself after RunTTL
// unless the owner pins it.

// fakeAdapter is a scripted adapter: it hands back whatever drafts the test
// gave it. It holds no writer, no store and no os path — the same shape the
// contract demands of a real adapter, so the run store is exercised exactly
// the way a Phase 3b adapter would exercise it.
type fakeAdapter struct {
	id     string
	drafts []sources.CandidateDraft
	err    error
	seen   []sources.Scope
}

func (f *fakeAdapter) ID() string         { return f.id }
func (f *fakeAdapter) Kind() sources.Kind { return sources.KindScholarly }
func (f *fakeAdapter) Scope() []sources.ScopeField {
	return []sources.ScopeField{{Key: "query", Label: "query", Required: true}}
}
func (f *fakeAdapter) Search(_ context.Context, s sources.Scope) ([]sources.CandidateDraft, error) {
	f.seen = append(f.seen, s)
	if f.err != nil {
		return nil, f.err
	}
	return append([]sources.CandidateDraft(nil), f.drafts...), nil
}
func (f *fakeAdapter) Enrich(_ context.Context, d sources.CandidateDraft) (sources.CandidateDraft, error) {
	return d, nil
}
func (f *fakeAdapter) GraphEdges(_ context.Context, d sources.CandidateDraft) ([]sources.EdgeClaim, error) {
	return d.Edges, nil
}

// citedDraft is a draft that passes the converter gate: a name, a source,
// and one evidence row with a url, a kind and a retrieval date.
func citedDraft(name, ext string) sources.CandidateDraft {
	return sources.CandidateDraft{
		SourceID:   "fake",
		ExternalID: ext,
		Name:       name,
		Org:        "Example Lab",
		Title:      "research engineer",
		Links:      []string{"https://example.test/people/" + strings.ToLower(strings.ReplaceAll(name, " ", "-"))},
		Evidence: []sources.Evidence{{
			SourceID:    "fake",
			URLOrFile:   "https://example.test/paper/" + ext,
			RetrievedAt: testNow,
			Snippet:     "built a 64 mT coil for " + name,
			Kind:        sources.EvidencePublication,
			Trust:       sources.TrustHigh,
		}},
	}
}

// testRunStore wires a record store over a temp vault and a run cache in a
// SEPARATE temp dir, the way main wires dataDir beside the vault.
func testRunStore(t *testing.T, a *fakeAdapter) (*RunStore, *Store, string) {
	t.Helper()
	store, vault := testStore(t)
	rs, err := NewRunStore(filepath.Join(t.TempDir(), "recruiting", "runs"), store)
	if err != nil {
		t.Fatal(err)
	}
	if a != nil {
		rs.Register(a)
	}
	rs.Register(sources.Manual{Owner: "benjamin"})
	return rs, store, vault
}

// snapshot reads every regular file under dir into path → bytes, so two
// snapshots compare byte-for-byte rather than by "looks the same".
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertIdentical(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("%s: %d files before, %d after", what, len(before), len(after))
	}
	for p, b := range before {
		a, ok := after[p]
		if !ok {
			t.Fatalf("%s: %s vanished", what, p)
		}
		if a != b {
			t.Fatalf("%s: %s changed:\n%s", what, p, firstDiff(b, a))
		}
	}
}

// assertOnlyChanged is assertIdentical with an allowance: exactly the named
// files may appear or change, everything else must be byte-identical. It
// exists because a pass now leaves ONE mark in the vault — the tombstone that
// makes the decision outlive its run — and the invariant worth guarding is
// still "and nothing else", not "nothing at all".
func assertOnlyChanged(t *testing.T, what string, before, after map[string]string, allowed ...string) {
	t.Helper()
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	allow := func(p string) bool {
		for a := range ok {
			if p == a || strings.HasSuffix(p, "/"+a) {
				return true
			}
		}
		return false
	}
	for _, p := range added(before, after) {
		if !allow(p) {
			t.Fatalf("%s: %s changed and was not allowed to", what, p)
		}
	}
	for p, b := range before {
		if allow(p) {
			continue
		}
		a, okAfter := after[p]
		if !okAfter {
			t.Fatalf("%s: %s vanished", what, p)
		}
		if a != b {
			t.Fatalf("%s: %s changed:\n%s", what, p, firstDiff(b, a))
		}
	}
}

// added returns the paths present after but not before, or changed.
func added(before, after map[string]string) []string {
	var out []string
	for p, a := range after {
		if b, ok := before[p]; !ok || b != a {
			out = append(out, p)
		}
	}
	return out
}

func mustRun(t *testing.T, rs *RunStore, req RunRequest) Run {
	t.Helper()
	run, err := rs.Execute(context.Background(), req, testNow)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// ---- D14: the cache lives outside the vault ----

func TestRunStoreRefusesARootInsideTheVault(t *testing.T) {
	store, vault := testStore(t)
	for _, root := range []string{
		vault,
		filepath.Join(vault, "runs"),
		filepath.Join(vault, "system", "aion", "recruiting", "runs"),
	} {
		if _, err := NewRunStore(root, store); err == nil {
			t.Errorf("a run cache at %s (inside the vault) was accepted", root)
		}
	}
	if _, err := NewRunStore(filepath.Join(t.TempDir(), "runs"), store); err != nil {
		t.Errorf("a run cache beside the vault was refused: %v", err)
	}
	if _, err := NewRunStore(t.TempDir(), nil); err == nil {
		t.Error("a run store without a record store was accepted — accept would have nowhere to write")
	}
}

// ---- dry run ----

// A dry run produces a queue and nothing else: every vault byte is what it
// was, the run is triaged at once (there is nothing to decide), and accept
// refuses it afterwards — the checkbox meant what it said.
func TestDryRunLeavesTheVaultByteIdentical(t *testing.T) {
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{citedDraft("Dana Reyes", "A1"), citedDraft("Kim Osei", "A2")}}
	rs, _, vault := testRunStore(t, fake)
	before := snapshot(t, vault)

	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "low-field mri", DryRun: true})
	assertIdentical(t, "after a dry run", before, snapshot(t, vault))

	if !run.Scope.DryRun {
		t.Fatal("the run forgot it was dry")
	}
	if run.Counts.Fetched != 2 || run.Counts.New != 2 || len(run.Drafts) != 2 {
		t.Fatalf("counts %+v drafts %d", run.Counts, len(run.Drafts))
	}
	// ⚠ a preview holds a real queue: it is NOT triaged while drafts pend,
	// because those drafts can be accepted (owner decision 2026-09-04)
	if !run.TriagedAt.IsZero() {
		t.Fatalf("a run with pending drafts started its expiry clock: triaged=%v", run.TriagedAt)
	}

	// and accepting from a preview WORKS — the checkbox never protected
	// anything Execute wasn't already doing (it writes no record either way),
	// while accept stays a deliberate one-record gesture
	if _, _, err := rs.Accept(run.ID, "d1", testNow); err != nil {
		t.Fatalf("accept from a preview run: %v", err)
	}
	got, err := rs.Get(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Drafts[0].Status != DraftAccepted || got.Counts.Accepted != 1 {
		t.Fatalf("accept did not move the queue: %+v", got.Drafts[0])
	}
}

// ---- accept / reject ----

// Accept promotes ONE draft: exactly one new file appears under candidates/,
// it is that draft's record, and the draft leaves `new` exactly once.
func TestAcceptWritesExactlyOneCandidateRecord(t *testing.T) {
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{
		citedDraft("Dana Reyes", "A1"), citedDraft("Kim Osei", "A2"), citedDraft("Lee Park", "A3"),
	}}
	rs, store, vault := testRunStore(t, fake)
	before := snapshot(t, vault)
	slugsBefore := len(store.CandidateSlugs())

	run := mustRun(t, rs, RunRequest{Source: "fake", Role: "role/mri-engineer", Query: "coil"})
	if run.TriagedAt.IsZero() == false {
		t.Fatalf("a run with new drafts is not triaged: %+v", run.RunState)
	}
	assertIdentical(t, "after a live run (before any accept)", before, snapshot(t, vault))

	got, c, err := rs.Accept(run.ID, "d2", testNow)
	if err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, vault)
	changed := added(before, after)
	if len(changed) != 1 || !strings.HasPrefix(changed[0], "system/aion/recruiting/candidates/") {
		t.Fatalf("accept changed %v, want exactly one candidate file", changed)
	}
	if n := len(store.CandidateSlugs()); n != slugsBefore+1 {
		t.Fatalf("candidates went from %d to %d", slugsBefore, n)
	}
	if c.Name != "Kim Osei" || c.Role != "role/mri-engineer" || c.SourceRef != "fake:A2" {
		t.Fatalf("accepted the wrong draft or lost its provenance: %+v", c)
	}
	rec := after[changed[0]]
	for _, want := range []string{"source_ref: fake:A2", "https://example.test/paper/A2", "built a 64 mT coil for Kim Osei", "pii: true"} {
		if !strings.Contains(rec, want) {
			t.Errorf("the record lacks %q:\n%s", want, rec)
		}
	}

	d := got.Drafts[1]
	if d.Status != DraftAccepted || d.CandidateID != c.ID || d.DecidedAt.IsZero() {
		t.Fatalf("draft after accept: %+v", d)
	}
	if got.Counts.Accepted != 1 || got.Counts.New != 3 || got.Drafts[0].Status != DraftNew || got.Drafts[2].Status != DraftNew {
		t.Fatalf("accept touched more than one draft: %+v %+v", got.Counts, got.Drafts)
	}
	if !got.TriagedAt.IsZero() {
		t.Fatal("two drafts are still new, yet the run is triaged")
	}

	// a draft leaves `new` once: the second accept and a reject both refuse
	if _, _, err := rs.Accept(run.ID, "d2", testNow); err == nil {
		t.Error("accepting an accepted draft succeeded")
	}
	if _, err := rs.Reject(run.ID, "d2", "", testNow); err == nil {
		t.Error("rejecting an accepted draft succeeded")
	}
	assertIdentical(t, "after the refused re-accept", after, snapshot(t, vault))
}

func TestRejectWritesNoCandidateRecord(t *testing.T) {
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{citedDraft("Dana Reyes", "A1")}}
	rs, store, vault := testRunStore(t, fake)
	before := snapshot(t, vault)

	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	got, err := rs.Reject(run.ID, "d1", "not this role", testNow)
	if err != nil {
		t.Fatal(err)
	}
	// A pass writes ONE thing to the vault and it is not a record: the
	// tombstone that stops the next sweep re-asking (passed.md — an id, a
	// name key, a reason and a date). Every other file is untouched, and no
	// candidate exists.
	assertOnlyChanged(t, "after reject", before, snapshot(t, vault), "passed.md")
	if len(store.CandidateSlugs()) != 0 {
		t.Fatal("reject produced a candidate")
	}
	// the key is the EXTERNAL id when the source gave one — the same rule the
	// duplicate check uses, so suppression and dedupe cannot drift
	if p, ok := store.PassedSet()[PassedKey("fake", "A1", "Dana Reyes")]; !ok || p.Reason != "not this role" {
		t.Fatalf("the pass left no tombstone: %+v", store.PassedSet())
	}
	if got.Drafts[0].Status != DraftRejected || got.Counts.Rejected != 1 || got.Drafts[0].DecidedAt.IsZero() {
		t.Fatalf("draft after reject: %+v", got.Drafts[0])
	}
	// nothing left to decide → the D14 clock starts
	if got.TriagedAt.IsZero() || !got.ExpiresAt.Equal(testNow.Add(RunTTL)) {
		t.Fatalf("rejecting the last new draft did not triage the run: %+v", got.RunState)
	}
	if _, err := rs.Reject(run.ID, "d1", "", testNow); err == nil {
		t.Error("rejecting a rejected draft succeeded")
	}
	if _, _, err := rs.Accept(run.ID, "d1", testNow); err == nil {
		t.Error("accepting a rejected draft succeeded")
	}
	if _, err := rs.Reject(run.ID, "d9", "", testNow); err == nil {
		t.Error("rejecting a draft that does not exist succeeded")
	}
}

// ---- the converter gate ----

// No fact without a source: a draft with no evidence, an evidence row that
// cannot be pointed at (no url/file, no retrieval date), or an edge with no
// source or basis is refused by accept — and the vault stays byte-identical.
func TestAcceptRefusesUncitedDrafts(t *testing.T) {
	noEvidence := citedDraft("Dana Reyes", "B1")
	noEvidence.Evidence = nil

	emptyURL := citedDraft("Dana Reyes", "B2")
	emptyURL.Evidence[0].URLOrFile = ""

	noDate := citedDraft("Dana Reyes", "B3")
	noDate.Evidence[0].RetrievedAt = time.Time{}

	noKind := citedDraft("Dana Reyes", "B4")
	noKind.Evidence[0].Kind = ""

	edgeNoSource := citedDraft("Dana Reyes", "B5")
	edgeNoSource.Edges = []sources.EdgeClaim{{From: "aion-net/ben-anderson", Type: sources.EdgeCoauthor, Basis: "paper X", Confidence: 0.5}}

	edgeNoBasis := citedDraft("Dana Reyes", "B6")
	edgeNoBasis.Edges = []sources.EdgeClaim{{From: "aion-net/ben-anderson", Type: sources.EdgeCoauthor, SourceID: "fake", Confidence: 0.5}}

	edgeBadType := citedDraft("Dana Reyes", "B7")
	edgeBadType.Edges = []sources.EdgeClaim{{From: "aion-net/ben-anderson", Type: "best_friends", SourceID: "fake", Basis: "x", Confidence: 0.5}}

	for _, tc := range []struct {
		name  string
		draft sources.CandidateDraft
	}{
		{"zero evidence", noEvidence},
		{"evidence with empty url/file", emptyURL},
		{"evidence with zero retrieval date", noDate},
		{"evidence with no kind", noKind},
		{"edge with empty source", edgeNoSource},
		{"edge with empty basis", edgeNoBasis},
		{"edge outside the closed set", edgeBadType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateDraft(tc.draft); err == nil {
				t.Fatalf("ValidateDraft passed a draft with %s", tc.name)
			}
			fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{tc.draft}}
			rs, store, vault := testRunStore(t, fake)
			before := snapshot(t, vault)
			// the queue may hold it — a run is a cache of what the source said —
			// but the converter refuses to make it a record
			run := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
			if _, _, err := rs.Accept(run.ID, "d1", testNow); err == nil {
				t.Fatalf("accept wrote a record from a draft with %s", tc.name)
			}
			assertIdentical(t, "after the refused accept", before, snapshot(t, vault))
			if len(store.CandidateSlugs()) != 0 {
				t.Fatal("a candidate appeared anyway")
			}
			got, _ := rs.Get(run.ID)
			if got.Drafts[0].Status != DraftNew || got.Counts.Accepted != 0 {
				t.Fatalf("a refused accept still moved the draft: %+v", got.Drafts[0])
			}
		})
	}
}

// An owner note IS citable by the owner's own words — the one evidence kind
// with no url — so the manual source's bare-name entry still converts.
func TestOwnerNoteIsTheOnlyUncitedEvidenceThatPasses(t *testing.T) {
	note := citedDraft("Dana Reyes", "")
	note.Evidence[0].URLOrFile = ""
	note.Evidence[0].Kind = sources.EvidenceOwnerNote
	note.Evidence[0].Snippet = "met at ISMRM"
	if err := ValidateDraft(note); err != nil {
		t.Fatalf("an owner note with the owner's words was refused: %v", err)
	}
	note.Evidence[0].Snippet = "  "
	if err := ValidateDraft(note); err == nil {
		t.Fatal("an owner note with no words passed")
	}
}

// ---- D15: no contact detail from a machine ----

// An adapter that sets an email or phone has it DROPPED by the converter:
// the record's contact slots are written empty, a mailto:/tel: link never
// lands, and the address string is nowhere in the file.
func TestAdapterSetContactIsDropped(t *testing.T) {
	d := citedDraft("Dana Reyes", "C1")
	d.Contact = map[string]string{"email": "dana@example.test", "phone": "+1 555 0100"}
	d.Links = append(d.Links, "mailto:dana@example.test", "tel:+15550100", "SMS:+15550100")
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{d}}
	rs, store, vault := testRunStore(t, fake)

	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	// the queue keeps what the source said (a run is a cache, kept verbatim)…
	if got, _ := rs.Get(run.ID); got.Drafts[0].Draft.Contact["email"] != "dana@example.test" {
		t.Fatalf("the queue rewrote the adapter's draft: %+v", got.Drafts[0].Draft)
	}
	// …and the converter drops it on the way to the record
	_, c, err := rs.Accept(run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if c.Profile["email"] != "" || c.Profile["phone"] != "" {
		t.Fatalf("contact detail reached the profile: %+v", c.Profile)
	}
	if c.Profile["website"] != "https://example.test/people/dana-reyes" {
		t.Fatalf("the real link was lost with the mailto: %+v", c.Profile)
	}
	rec := snapshot(t, vault)["system/aion/recruiting/candidates/"+CandidateSlug(c.ID)+".md"]
	if rec == "" {
		t.Fatalf("no record for %s under %v", c.ID, store.CandidateSlugs())
	}
	for _, leak := range []string{"dana@example.test", "555 0100", "5550100", "mailto:", "tel:", "sms:"} {
		if strings.Contains(strings.ToLower(rec), leak) {
			t.Errorf("the record carries %q:\n%s", leak, rec)
		}
	}
	// the sanitiser is a pure function of the draft; nothing else moves
	clean := SanitizeDraft(d)
	if clean.Contact != nil || len(clean.Links) != 1 || clean.Name != d.Name || len(clean.Evidence) != 1 {
		t.Fatalf("SanitizeDraft: %+v", clean)
	}
}

// ---- the cache on disk ----

// A run is a search for named people: its directories are 0700 and its
// files 0600, it holds the raw response verbatim, and it never lands in the
// vault.
func TestRunCacheIsPrivateAndOutsideTheVault(t *testing.T) {
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{citedDraft("Dana Reyes", "A1")}}
	rs, _, vault := testRunStore(t, fake)
	before := snapshot(t, vault)
	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "coil"})
	if _, err := rs.Reject(run.ID, "d1", "", testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Pin(run.ID, true); err != nil {
		t.Fatal(err)
	}
	// the run cache itself never touches the vault; the pass's tombstone does,
	// and only that
	assertOnlyChanged(t, "the vault after a run, a reject and a pin", before, snapshot(t, vault), "passed.md")

	root := rs.Root()
	if rel, err := filepath.Rel(vault, root); err == nil && !strings.HasPrefix(rel, "..") {
		t.Fatalf("the run root %s is inside the vault %s", root, vault)
	}
	for _, dir := range []string{root, filepath.Join(root, run.ID)} {
		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !fi.IsDir() || fi.Mode().Perm() != 0o700 {
			t.Errorf("%s is %v, want a 0700 directory", dir, fi.Mode())
		}
	}
	files := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s is %v, want 0600", p, fi.Mode())
		}
		if !strings.HasSuffix(p, ".json") {
			t.Errorf("unexpected non-json file in the cache: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"run.json": true, "drafts.json": true, "response.json": true}
	for name := range want {
		if _, err := os.Stat(filepath.Join(root, run.ID, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if files != len(want) {
		t.Errorf("%d files in the cache, want %d (no leftover .tmp)", files, len(want))
	}

	// the raw response is what the adapter said, before dedupe or sanitising
	var raw []sources.CandidateDraft
	if err := readJSON(filepath.Join(root, run.ID, "response.json"), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1 || raw[0].Name != "Dana Reyes" || raw[0].SourceID != "fake" {
		t.Fatalf("response.json: %+v", raw)
	}
}

// ---- D14: the sweep ----

// A triaged, unpinned run goes after RunTTL; a pinned one stays; an
// untriaged one has no expiry and is never swept.
func TestTriagedUnpinnedRunExpiresPinnedDoesNot(t *testing.T) {
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{citedDraft("Dana Reyes", "A1")}}
	rs, _, _ := testRunStore(t, fake)

	open := mustRun(t, rs, RunRequest{Source: "fake", Query: "still open"})
	triaged := mustRun(t, rs, RunRequest{Source: "fake", Query: "decided"})
	pinned := mustRun(t, rs, RunRequest{Source: "fake", Query: "kept"})
	for _, id := range []string{triaged.ID, pinned.ID} {
		if _, err := rs.Reject(id, "d1", "", testNow); err != nil {
			t.Fatal(err)
		}
	}
	if run, err := rs.Pin(pinned.ID, true); err != nil || !run.Pinned {
		t.Fatalf("pin: %v %+v", err, run.RunState)
	}
	ids := func(runs []Run) map[string]bool {
		out := map[string]bool{}
		for _, r := range runs {
			out[r.ID] = true
		}
		return out
	}

	// a day short of the TTL: everything is still there
	have := ids(rs.Runs(testNow.Add(RunTTL - 24*time.Hour)))
	if !have[open.ID] || !have[triaged.ID] || !have[pinned.ID] {
		t.Fatalf("a run was swept before its expiry: %v", have)
	}
	// past the TTL: the triaged, unpinned run goes — and only that one
	gone := rs.Sweep(testNow.Add(RunTTL + time.Second))
	if len(gone) != 1 || gone[0] != triaged.ID {
		t.Fatalf("swept %v, want only %s", gone, triaged.ID)
	}
	if _, err := os.Stat(filepath.Join(rs.Root(), triaged.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the swept run's directory is still there: %v", err)
	}
	have = ids(rs.Runs(testNow.Add(RunTTL + time.Second)))
	if have[triaged.ID] {
		t.Error("the swept run is still listed")
	}
	if !have[open.ID] {
		t.Error("an untriaged run was swept — it has no expiry")
	}
	if !have[pinned.ID] {
		t.Error("a pinned run was swept")
	}
	// a year on, the open run is still there and the pinned one too
	have = ids(rs.Runs(testNow.Add(365 * 24 * time.Hour)))
	if !have[open.ID] || !have[pinned.ID] {
		t.Errorf("after a year: %v", have)
	}
	// unpin → the next listing takes it
	if _, err := rs.Pin(pinned.ID, false); err != nil {
		t.Fatal(err)
	}
	if have = ids(rs.Runs(testNow.Add(RunTTL + time.Second))); have[pinned.ID] {
		t.Error("an unpinned, expired run survived the listing")
	}
	if _, err := rs.Get(pinned.ID); err == nil {
		t.Error("a swept run still loads")
	}
}

// ---- scope + dedupe ----

func TestExecuteScopeRules(t *testing.T) {
	var many []sources.CandidateDraft
	for i := 0; i < MaxRunMax+50; i++ {
		many = append(many, citedDraft("Person "+string(rune('A'+i%26))+string(rune('a'+i/26)), "P"+string(rune('0'+i%10))+string(rune('a'+i/10))))
	}
	fake := &fakeAdapter{id: "fake", drafts: many}
	rs, _, _ := testRunStore(t, fake)

	if _, err := rs.Execute(context.Background(), RunRequest{Source: "nope", Query: "x"}, testNow); err == nil {
		t.Error("an unknown source ran")
	}
	if _, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "   "}, testNow); err == nil {
		t.Error("a run with no query ran")
	}
	if _, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Role: "role/astronaut", Query: "x"}, testNow); err == nil {
		t.Error("a run against an unknown role ran")
	}
	if len(fake.seen) != 0 {
		t.Fatalf("the adapter was called for a refused scope: %+v", fake.seen)
	}

	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "x"})
	if run.Scope.Max != DefaultRunMax || run.Counts.Fetched != DefaultRunMax || len(run.Drafts) != DefaultRunMax {
		t.Errorf("no max → %d, got scope %d fetched %d", DefaultRunMax, run.Scope.Max, run.Counts.Fetched)
	}
	run = mustRun(t, rs, RunRequest{Source: "fake", Query: "x", Max: 10_000})
	if run.Scope.Max != MaxRunMax || run.Counts.Fetched != MaxRunMax {
		t.Errorf("max 10000 → %d, got scope %d fetched %d", MaxRunMax, run.Scope.Max, run.Counts.Fetched)
	}
	run = mustRun(t, rs, RunRequest{Source: " fake ", Query: " x ", Role: "role/mri-engineer", Max: 3, Fields: map[string]string{" org ": " WashU "}})
	if run.Scope.Query != "x" || run.Scope.Role != "role/mri-engineer" || run.Scope.Fields["org"] != "WashU" || run.Counts.Fetched != 3 {
		t.Errorf("scope was not trimmed and capped: %+v", run.Scope)
	}
	if run.Drafts[0].Draft.Role != "role/mri-engineer" || run.Drafts[0].Draft.SourceID != "fake" {
		t.Errorf("the draft did not inherit the scope's role / the adapter's id: %+v", run.Drafts[0].Draft)
	}
	if run.Source != "fake" || !strings.Contains(run.ID, "-fake-") || !strings.HasPrefix(run.ID, "20260902-150405-") {
		t.Errorf("run id / source: %s %s", run.ID, run.Source)
	}

	// an adapter error is the run's error — and nothing is cached for it
	n := len(rs.Runs(testNow))
	fake.err = errors.New("upstream 503")
	if _, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "x"}, testNow); err == nil || !strings.Contains(err.Error(), "upstream 503") {
		t.Errorf("adapter error was not surfaced: %v", err)
	}
	if len(rs.Runs(testNow)) != n {
		t.Error("a failed run left a cache directory")
	}
}

// Dedupe: a draft that matches a vault record by external id, or by name
// (+ org), arrives as `duplicate` naming the record, and accept refuses it.
// An accepted draft's external id lands on the record, so the SAME person
// under a different spelling dedupes next time by id rather than by name.
func TestRunDedupesAgainstTheVault(t *testing.T) {
	fake := &fakeAdapter{id: "fake"}
	rs, store, _ := testRunStore(t, fake)
	if _, err := store.AddCandidate(QuickAdd{Text: "Dana Reyes", Org: "Example Lab", Role: "role/mri-engineer"}, testNow); err != nil {
		t.Fatal(err)
	}

	sameName := citedDraft("dana  REYES", "N1")
	sameName.Org = "Example Lab"
	otherOrg := citedDraft("Dana Reyes", "N2")
	otherOrg.Org = "Somewhere Else"
	fresh := citedDraft("Kim Osei", "K1")
	fake.drafts = []sources.CandidateDraft{sameName, otherOrg, fresh}

	run := mustRun(t, rs, RunRequest{Source: "fake", Query: "x"})
	if run.Counts.Duplicate != 1 || run.Counts.New != 2 {
		t.Fatalf("counts: %+v", run.Counts)
	}
	dup := run.Drafts[0]
	if dup.Status != DraftDuplicate || dup.CandidateID == "" || dup.Reason != "same name and org" || dup.Draft.Dedupe.CandidateID != dup.CandidateID {
		t.Fatalf("same name+org: %+v", dup)
	}
	if run.Drafts[1].Status != DraftNew {
		t.Fatalf("a same-name draft at a different org should stay new for the owner to decide: %+v", run.Drafts[1])
	}
	if _, _, err := rs.Accept(run.ID, "d1", testNow); err == nil {
		t.Error("accepting a duplicate succeeded")
	}
	// the store refuses the same-name accept regardless of org — a reason in
	// the queue beats a failing accept, but the gate still holds
	if _, _, err := rs.Accept(run.ID, "d2", testNow); err == nil {
		t.Error("a second Dana Reyes reached the board")
	}
	// accept Kim → next run dedupes Kim by external id even under a new name
	if _, _, err := rs.Accept(run.ID, "d3", testNow); err != nil {
		t.Fatal(err)
	}
	renamed := citedDraft("K. Osei", "K1")
	fake.drafts = []sources.CandidateDraft{renamed}
	run = mustRun(t, rs, RunRequest{Source: "fake", Query: "x"})
	if d := run.Drafts[0]; d.Status != DraftDuplicate || d.Reason != "external id fake:K1" {
		t.Fatalf("external-id dedupe: %+v", d)
	}
}

// The manual adapter through the run store: one draft, the owner's words as
// its citation, `known` only with an explicit asserting node.
func TestManualSourceRunsThroughTheQueue(t *testing.T) {
	rs, store, _ := testRunStore(t, nil)
	infos := rs.Sources()
	if len(infos) != 1 || infos[0].ID != "manual" || infos[0].Kind != sources.KindManual || len(infos[0].Fields) == 0 {
		t.Fatalf("sources: %+v", infos)
	}
	run := mustRun(t, rs, RunRequest{Source: "manual", Role: "role/mri-engineer", Query: "https://example.test/people/dana-reyes", Max: 50})
	if run.Counts.Fetched != 1 || run.Drafts[0].Draft.Evidence[0].SourceID != "manual" {
		t.Fatalf("manual run: %+v", run)
	}
	if _, err := rs.Execute(context.Background(), RunRequest{Source: "manual", Query: "Dana", Fields: map[string]string{"known": "true"}}, testNow); err == nil {
		t.Error("a known-person entry without an asserting node ran")
	}
	_, c, err := rs.Accept(run.ID, "d1", testNow)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "dana reyes" || len(store.CandidateSlugs()) != 1 {
		t.Fatalf("accepted manual draft: %+v", c)
	}
}

func TestRunsListNewestFirst(t *testing.T) {
	fake := &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{citedDraft("Dana Reyes", "A1")}}
	rs, _, _ := testRunStore(t, fake)
	if got := rs.Runs(testNow); len(got) != 0 {
		t.Fatalf("an empty cache listed %d runs", len(got))
	}
	var order []string
	for i := 0; i < 3; i++ {
		run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "x"}, testNow.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		order = append(order, run.ID)
	}
	got := rs.Runs(testNow)
	if len(got) != 3 || got[0].ID != order[2] || got[2].ID != order[0] {
		t.Fatalf("listing order: %v want reverse of %v", []string{got[0].ID, got[1].ID, got[2].ID}, order)
	}
	// a stray directory that is not a run is ignored, not a crash
	if err := os.MkdirAll(filepath.Join(rs.Root(), "not-a-run"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := rs.Runs(testNow); len(got) != 3 {
		t.Errorf("a stray dir changed the listing: %d", len(got))
	}
	if _, err := rs.Get("../escape"); err == nil {
		t.Error("a path-shaped run id loaded")
	}
}

// ---- the adapter boundary, checked from THIS side ----

// recruiting/sources must stay writeless: it does not import recruiting
// (one direction only), no non-test file calls an os write path, and no
// adapter carries a func / pointer / interface field it could write through.
// The sources package has its own copy of this guard; this one runs from
// the package that would be reached if it ever failed.
func TestSourcesPackageStaysWriteless(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, "sources", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatal("parsed no packages under recruiting/sources — the boundary check is not running")
	}
	banned := map[string]bool{"WriteFile": true, "Create": true, "OpenFile": true,
		"MkdirAll": true, "Mkdir": true, "Rename": true, "Remove": true, "RemoveAll": true,
		"Chmod": true, "Symlink": true, "Truncate": true}
	files := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			files++
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if path == "manifest/recruiting" || strings.HasPrefix(path, "manifest/recruiting/") {
					t.Errorf("%s imports %s — an adapter must not reach the record store", name, path)
				}
				if path == "manifest/vaultwriter" || path == "manifest/aion" || strings.HasPrefix(path, "manifest/aion/") {
					t.Errorf("%s imports %s — an adapter holds no writer and no public contract", name, path)
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "os" && banned[sel.Sel.Name] {
					t.Errorf("%s calls os.%s — an adapter holds no write path", name, sel.Sel.Name)
				}
				return true
			})
		}
	}
	if files == 0 {
		t.Fatal("no source files scanned")
	}
	for _, a := range []sources.Adapter{sources.Manual{}} {
		ty := reflect.TypeOf(a)
		for ty.Kind() == reflect.Ptr {
			ty = ty.Elem()
		}
		for i := 0; i < ty.NumField(); i++ {
			f := ty.Field(i)
			switch f.Type.Kind() {
			case reflect.Func, reflect.Ptr, reflect.Interface, reflect.Chan:
				t.Errorf("%s.%s is a %s — an adapter that can hold a writer can write", ty.Name(), f.Name, f.Type.Kind())
			}
		}
	}
	// and the run cache is the ONE direct write path recruiting holds: it is
	// rooted in runs.go, and nothing else in the package opens a file to write
	own, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range own {
		for name, file := range pkg.Files {
			if filepath.Base(name) == "runs.go" {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "os" && banned[sel.Sel.Name] {
					t.Errorf("%s calls os.%s — only runs.go may write, and only under the run root", name, sel.Sel.Name)
				}
				return true
			})
		}
	}
}
