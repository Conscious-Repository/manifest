package recruiting

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/recruiting/sources"
)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// The fixpoint guarantee: parse → emit is byte-identical on canonical input,
// for every record kind.
func TestFixpointPerRecordKind(t *testing.T) {
	for _, tc := range []struct{ file, shape string }{
		{"role-mri-engineer.md", "roles/x.md"},
		{"candidate-hand-edited.md", "candidates/x.md"},
		{"seeds.md", "seeds.md"},
		{"network-people.md", "network/people.md"},
		{"network-edges.md", "network/edges.md"},
		{"outreach-log.md", "outreach/x.md"},
	} {
		raw := read(t, tc.file)
		fn := RoundTrip(tc.shape)
		if fn == nil {
			t.Fatalf("%s: no declared round-trip for shape %s", tc.file, tc.shape)
		}
		if out := fn(raw); out != raw {
			t.Errorf("%s round-trip diverged:\n%s", tc.file, firstDiff(raw, out))
		}
	}
}

// Every write-once seed must be a fixpoint of its own parser, or the very
// first thing the domain writes to the vault is already non-canonical.
func TestSeedFilesAreFixpoints(t *testing.T) {
	if len(SeedOrder) != len(SeedFiles) {
		t.Fatalf("SeedOrder has %d entries, SeedFiles has %d — the deterministic "+
			"write order must cover every seed", len(SeedOrder), len(SeedFiles))
	}
	for _, name := range SeedOrder {
		raw, ok := SeedFiles[name]
		if !ok {
			t.Fatalf("SeedOrder names %s, which SeedFiles does not define", name)
		}
		fn := RoundTrip(name)
		if fn == nil {
			t.Fatalf("%s: no declared round-trip", name)
		}
		if out := fn(raw); out != raw {
			t.Errorf("seed %s is not a fixpoint of its parser:\n%s", name, firstDiff(raw, out))
		}
	}
}

// A record a human edited in Obsidian — reordered fields, an unknown heading,
// a tab-indented child, an unknown [foo:: bar] — must survive a full
// load → mutate one field → save.
func TestHandEditSurvivesMutation(t *testing.T) {
	raw := read(t, "candidate-hand-edited.md")
	doc := ParseCandidate(raw)
	if err := doc.SetProfile("location", "Saint Louis, MO"); err != nil {
		t.Fatal(err)
	}
	out := SerializeCandidate(doc)

	if !strings.Contains(out, "[location:: Saint Louis, MO]") {
		t.Fatalf("the edit did not land:\n%s", out)
	}
	for _, want := range []string{
		"## notes",
		"a hand-added heading the app knows nothing about",
		"\t- a tab-indented child line",
		"[score:: 4] [criterion:: low-field MRI hardware] [evidence:: ev1] [foo:: bar]",
		"  > verbatim quoted evidence, preserved exactly",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("hand-edited content lost: %q\n%s", want, out)
		}
	}
	// the profile row kept its original layout — location was rewritten in
	// place, not appended to a new row
	if !strings.Contains(out, "- [title:: MRI Systems Engineer] [org:: Example Lab] [location:: Saint Louis, MO]") {
		t.Errorf("the profile row was rebuilt instead of edited in place:\n%s", out)
	}
}

// An absent corpus must parse to a valid empty document — no panic before the
// first seed lands.
func TestEmptyCorpusParses(t *testing.T) {
	if got := len(ParseSeeds("").Seeds()); got != 0 {
		t.Errorf("empty seeds.md yielded %d seeds", got)
	}
	if got := len(ParseEdges("").Edges()); got != 0 {
		t.Errorf("empty edges.md yielded %d edges", got)
	}
	if got := len(ParseNetworkPeople("").People()); got != 0 {
		t.Errorf("empty people.md yielded %d people", got)
	}
	if got := len(ParseRole("").Criteria()); got != 0 {
		t.Errorf("empty role yielded %d criteria", got)
	}
	c := ParseCandidate("").View("nobody", nil)
	if c.Stage != StageNew || c.Gate.Passed {
		t.Errorf("empty candidate: stage=%q gate=%+v", c.Stage, c.Gate)
	}
	for _, s := range []string{SerializeSeeds(ParseSeeds("")), SerializeEdges(ParseEdges("")),
		SerializeRole(ParseRole("")), SerializeCandidate(ParseCandidate(""))} {
		if s != "" {
			t.Errorf("an absent record serialized to %q, not empty", s)
		}
	}
}

// ---- store ----

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	vault := t.TempDir()
	write := func(abs string, b []byte) error {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, b, 0o644)
	}
	s := NewStore(vault, "system/aion/recruiting", write)
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	return s, vault
}

var testNow = time.Date(2026, 9, 2, 15, 4, 5, 0, time.UTC)

func TestEnsureSeedsTheFourRoles(t *testing.T) {
	s, _ := testStore(t)
	v := s.View()
	if len(v.Roles) != 4 {
		t.Fatalf("seeded %d roles, want the four D2 lanes: %+v", len(v.Roles), v.Roles)
	}
	want := map[string]bool{"MRI Engineer": true, "Mechanical Engineer": true,
		"Biomedical Engineer": true, "Scientist: Microscopy": true}
	for _, r := range v.Roles {
		if !want[r.Title] {
			t.Errorf("unexpected role %q", r.Title)
		}
		delete(want, r.Title)
	}
	if len(want) != 0 {
		t.Errorf("missing role lanes: %v", want)
	}
	// pinned is a SORT key, never a filter — all four are present, pinned first
	if !v.Roles[0].Pinned {
		t.Errorf("roles did not sort pinned-first: %+v", v.Roles)
	}
	if len(v.Seeds) != 2 {
		t.Fatalf("seeds.md should ship Benjamin and RJ and nothing else, got %+v", v.Seeds)
	}
	if len(v.Network.People) != 2 || len(v.Network.Edges) != 0 {
		t.Errorf("network seed: %d people, %d edges", len(v.Network.People), len(v.Network.Edges))
	}
}

// Ensure never overwrites: the vault is the source of truth.
func TestEnsureIsWriteOnce(t *testing.T) {
	s, _ := testStore(t)
	edited := "# AION recruiting — seeds\n\n- [id:: seed/co-mine] [class:: company] [name:: Mine]\n"
	if err := s.SaveSeeds(ParseSeeds(edited)); err != nil {
		t.Fatal(err)
	}
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	if got := s.LoadSeeds().Seeds(); len(got) != 1 || got[0].ID != "seed/co-mine" {
		t.Fatalf("Ensure clobbered an existing record: %+v", got)
	}
}

func TestQuickAddThroughManualAdapter(t *testing.T) {
	s, vault := testStore(t)
	c, err := s.AddCandidate(QuickAdd{
		Text: "https://example.test/people/dana-reyes — met at a conference",
		Name: "Dana Reyes", Role: "role/mri-engineer", Org: "Example Lab",
	}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "cand/dana-reyes" || c.Stage != StageNew || !c.PII {
		t.Fatalf("quick-add produced %+v", c)
	}
	if len(c.Evidence) != 1 || c.Evidence[0].URL != "https://example.test/people/dana-reyes" {
		t.Fatalf("the owner's line was not cited: %+v", c.Evidence)
	}
	// the contact slots exist and are EMPTY — nothing guessed an address
	if c.Profile["email"] != "" || c.Profile["phone"] != "" {
		t.Fatalf("contact details appeared without a human typing them: %+v", c.Profile)
	}
	if c.Profile["website"] != "https://example.test/people/dana-reyes" {
		t.Errorf("the link did not land on the profile: %+v", c.Profile)
	}
	raw, err := os.ReadFile(filepath.Join(vault, "system/aion/recruiting/candidates/dana-reyes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if out := SerializeCandidate(ParseCandidate(string(raw))); out != string(raw) {
		t.Errorf("a freshly written record is not a fixpoint:\n%s", firstDiff(string(raw), out))
	}
	// same name twice is a duplicate, not a second record
	if _, err := s.AddCandidate(QuickAdd{Text: "Dana Reyes", Role: "role/mri-engineer"}, testNow); err == nil {
		t.Error("a duplicate name was accepted onto the board")
	}
	if _, err := s.AddCandidate(QuickAdd{Text: "Someone", Role: "role/nope"}, testNow); err == nil {
		t.Error("an unknown role was accepted")
	}
	if _, err := s.AddCandidate(QuickAdd{Text: "  "}, testNow); err == nil {
		t.Error("an empty quick-add was accepted")
	}
}

// A known-person quick-add asserts the strongest edge in the system, and it
// lands in the ledger with its basis and its confidence.
func TestQuickAddKnownPersonWritesAnEdge(t *testing.T) {
	s, _ := testStore(t)
	if _, err := s.AddCandidate(QuickAdd{
		Text: "Marlow Finch", Role: "role/mri-engineer",
		Known: true, KnownVia: "aion-net/ben-anderson",
	}, testNow); err != nil {
		t.Fatal(err)
	}
	edges := s.LoadEdges().Edges()
	if len(edges) != 1 {
		t.Fatalf("want one edge, got %+v", edges)
	}
	e := edges[0]
	if e.Kind != string(sources.EdgeDirectKnown) || e.To != "cand/marlow-finch" ||
		e.Confidence != "0.95" || e.Inferred || e.Basis == "" {
		t.Fatalf("edge claim: %+v", e)
	}
	// and the assertion needs a node to hang off
	if _, err := s.AddCandidate(QuickAdd{Text: "Nia Ward", Known: true}, testNow); err == nil {
		t.Error("a known-person entry without an asserting node was accepted")
	}
}

func TestStageTransitionsAreClosed(t *testing.T) {
	s, _ := testStore(t)
	c, err := s.AddCandidate(QuickAdd{Text: "Robin Vale", Role: "role/mri-engineer"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetStage(c.ID, "shortlist"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "hired", "SHORTLIST", "reviewing "} {
		if _, err := s.SetStage(c.ID, bad); err == nil {
			t.Errorf("stage %q was accepted", bad)
		}
	}
	// archived is a disposition with a date, not a column you drag into
	if _, err := s.SetStage(c.ID, StageArchived); err == nil {
		t.Error("archived was reachable through the stage route")
	}
	if _, err := s.SetStage("cand/ghost", StageNew); err == nil {
		t.Error("an unknown candidate was moved")
	}
}

// Archive is retention, not deletion (D7): the file stays, the board loses it,
// and the count goes back to where it was.
func TestArchiveRetainsTheRecord(t *testing.T) {
	s, vault := testStore(t)
	c, err := s.AddCandidate(QuickAdd{Text: "Ira Solen", Role: "role/mri-engineer"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	before := roleCount(t, s, "mri-engineer")
	if before != 1 {
		t.Fatalf("open count %d after one quick-add", before)
	}
	got, err := s.Archive(c.ID, true, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stage != StageArchived || got.Archived != "2026-09-02" {
		t.Fatalf("archived candidate: %+v", got)
	}
	if n := roleCount(t, s, "mri-engineer"); n != 0 {
		t.Errorf("an archived candidate still counts against the rail: %d", n)
	}
	if _, err := os.Stat(filepath.Join(vault, "system/aion/recruiting/candidates/ira-solen.md")); err != nil {
		t.Errorf("archiving deleted the record: %v", err)
	}
	restored, err := s.Archive(c.ID, false, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Stage != StageReviewing || restored.Archived != "" {
		t.Fatalf("restored candidate: %+v", restored)
	}
}

func roleCount(t *testing.T, s *Store, slug string) int {
	t.Helper()
	for _, r := range s.View().Roles {
		if r.Slug == slug {
			return r.OpenCount
		}
	}
	t.Fatalf("no role %q", slug)
	return 0
}

func TestManualEvidenceCapture(t *testing.T) {
	s, _ := testStore(t)
	c, err := s.AddCandidate(QuickAdd{Text: "Wren Castille", Role: "role/mri-engineer"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.AddEvidence(c.ID, Evidence{
		URL: "https://example.test/paper", Kind: sources.EvidencePublication,
		Snippet: "designed a 64 mT head coil\nwith a novel former",
	}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Evidence) != 2 {
		t.Fatalf("evidence rows: %+v", got.Evidence)
	}
	ev := got.Evidence[1]
	if ev.ID != "ev2" || ev.URL != "https://example.test/paper" || ev.Collected != "2026-09-02" ||
		ev.Snippet != "designed a 64 mT head coil\nwith a novel former" {
		t.Fatalf("captured evidence: %+v", ev)
	}
	if _, err := s.AddEvidence(c.ID, Evidence{Kind: "publication"}, testNow); err == nil {
		t.Error("evidence with no url, file or quote was accepted")
	}
}

func TestSeedClassesAreClosed(t *testing.T) {
	s, _ := testStore(t)
	for _, class := range SeedClasses {
		if _, err := s.AddSeed(Seed{Class: class, Name: "Example " + class}, testNow); err != nil {
			t.Errorf("seed class %q refused: %v", class, err)
		}
	}
	for _, bad := range []string{"", "candidate", "Person"} {
		if _, err := s.AddSeed(Seed{Class: bad, Name: "x"}, testNow); err == nil {
			t.Errorf("seed class %q was accepted", bad)
		}
	}
	if _, err := s.AddSeed(Seed{Class: SeedLab, Name: ""}, testNow); err == nil {
		t.Error("a nameless seed was accepted")
	}
	got := s.LoadSeeds().Seeds()
	if len(got) != 2+len(SeedClasses) {
		t.Fatalf("seed rows: %+v", got)
	}
	last := got[len(got)-1]
	if last.Added != "2026-09-02" || last.Source != "owner" {
		t.Errorf("a seed lost its provenance: %+v", last)
	}
	// and the file is still a fixpoint after the writes
	raw := s.raw("seeds.md")
	if out := SerializeSeeds(ParseSeeds(raw)); out != raw {
		t.Errorf("seeds.md diverged after writes:\n%s", firstDiff(raw, out))
	}
}

func TestCriteriaClassesAreClosed(t *testing.T) {
	s, _ := testStore(t)
	good := []Criterion{
		{Criterion: "finite element modelling", Class: ClassMust, Weight: 3},
		{Criterion: "machining background", Class: ClassNice, Weight: 1},
		{Criterion: "requires full remote", Class: ClassDisqualifier},
	}
	r, err := s.SetRoleCriteria("mechanical-engineer", good)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Criteria) != 3 || r.Criteria[0].Weight != 3 {
		t.Fatalf("criteria: %+v", r.Criteria)
	}
	for _, bad := range [][]Criterion{
		{{Criterion: "x", Class: "required"}},
		{{Criterion: "", Class: ClassMust}},
		{{Criterion: "x", Class: ClassMust, Weight: 9}},
	} {
		if _, err := s.SetRoleCriteria("mechanical-engineer", bad); err == nil {
			t.Errorf("criteria %+v were accepted", bad)
		}
	}
	// a refused edit changed nothing
	if got := s.LoadRole("mechanical-engineer").Criteria(); len(got) != 3 {
		t.Errorf("a refused criteria edit half-wrote the section: %+v", got)
	}
	if _, err := s.SetRoleCriteria("no-such-role", good); err == nil {
		t.Error("criteria were written to an unknown role")
	}
}

// The `## posting` section is Ashby's (Phase 2); `## criteria` is Benjamin's.
// A criteria edit must not disturb the posting.
func TestCriteriaEditLeavesThePostingAlone(t *testing.T) {
	s, _ := testStore(t)
	doc := ParseRole(read(t, "role-mri-engineer.md"))
	if err := s.SaveRole("mri-engineer", doc); err != nil {
		t.Fatal(err)
	}
	before := s.LoadRole("mri-engineer").Posting()
	if _, err := s.SetRoleCriteria("mri-engineer", []Criterion{
		{Criterion: "low-field MRI hardware", Class: ClassMust, Weight: 3},
	}); err != nil {
		t.Fatal(err)
	}
	after := s.LoadRole("mri-engineer")
	if after.Posting() != before || before == "" {
		t.Fatalf("the posting changed:\n%q\n%q", before, after.Posting())
	}
	if len(after.Terms()) != 3 {
		t.Errorf("the sourcing terms were disturbed: %+v", after.Terms())
	}
}

// Nothing this package writes may reach a file it was not granted. The store
// holds only the injected writer, so the boundary is testable: a writer that
// refuses everything outside the recruiting root turns a traversal attempt
// into an error rather than a write.
func TestStoreWritesOnlyThroughTheInjectedWriter(t *testing.T) {
	vault := t.TempDir()
	var wrote []string
	s := NewStore(vault, "system/aion/recruiting", func(abs string, b []byte) error {
		rel, err := filepath.Rel(vault, abs)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasPrefix(rel, "system/aion/recruiting/") {
			return errf("write-capability violation: %q is outside aion-recruiting", rel)
		}
		wrote = append(wrote, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, b, 0o644)
	})
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, rel := range wrote {
		if !strings.HasPrefix(rel, "system/aion/recruiting/") {
			t.Errorf("wrote outside the capability: %s", rel)
		}
	}
	// a candidate id carrying a path escape resolves to nothing, rather than
	// to a file above the root
	if _, err := s.SetStage("cand/../../hiring", StageNew); err == nil {
		t.Error("a traversing candidate id resolved")
	}
	if s2 := NewStore(vault, "system/aion/recruiting", nil); s2.SaveSeeds(ParseSeeds("")) == nil {
		t.Error("a store with no injected writer wrote anyway")
	}
}

// An edge is a claim, and a claim without a basis is not one. The refusal is
// on the WRITE path, so the serializer stays total and the corpus heartbeat
// can never be broken by a hand edit.
func TestEdgesRefuseAClaimWithoutABasis(t *testing.T) {
	s, _ := testStore(t)
	edges := s.LoadEdges()
	for _, bad := range []Edge{
		{From: "a", To: "b", Kind: "direct_known", Source: "owner"},
		{From: "a", To: "b", Kind: "made_up", Basis: "x", Source: "owner"},
		{From: "a", To: "b", Kind: "direct_known", Basis: "x"},
		{From: "", To: "b", Kind: "direct_known", Basis: "x", Source: "owner"},
		{From: "a", To: "b", Kind: "direct_known", Basis: "x", Source: "owner", Confidence: "9"},
	} {
		if _, err := edges.Add(bad); err == nil {
			t.Errorf("edge %+v was accepted", bad)
		}
	}
	hand := ParseEdges(read(t, "network-edges.md") +
		"- [from:: a] [to:: b] [kind:: direct_known] [source:: owner]\n")
	if err := s.SaveEdges(hand); err == nil {
		t.Error("a basis-less edge was persisted")
	}
	if raw := read(t, "network-edges.md"); SerializeEdges(ParseEdges(raw)) != raw {
		t.Error("the edges serializer is not total")
	}
}

func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return "line " + itoa(i+1) + ":\n- " + al[i] + "\n+ " + bl[i]
		}
	}
	return "length differs: " + itoa(len(al)) + " vs " + itoa(len(bl)) + " lines"
}

// ---- the draft converter: the checkpoint between a source and the vault ----

func goodDraft() sources.CandidateDraft {
	return sources.CandidateDraft{
		SourceID: "manual", Name: "Corin Ablett",
		Evidence: []sources.Evidence{{
			SourceID: "manual", URLOrFile: "https://example.test/a",
			RetrievedAt: testNow, Kind: sources.EvidencePage, Trust: sources.TrustMedium,
		}},
	}
}

// No fact without a source: the converter refuses a draft the record could
// not later be defended from.
func TestConverterRefusesUnsourcedFacts(t *testing.T) {
	noName := goodDraft()
	noName.Name = " "
	noEvidence := goodDraft()
	noEvidence.Evidence = nil
	noSource := goodDraft()
	noSource.SourceID = ""
	evNoSource := goodDraft()
	evNoSource.Evidence[0].SourceID = ""
	evNoDate := goodDraft()
	evNoDate.Evidence[0].RetrievedAt = time.Time{}
	evNoKind := goodDraft()
	evNoKind.Evidence[0].Kind = ""
	evNoCite := goodDraft()
	evNoCite.Evidence[0].URLOrFile = ""
	edgeNoBasis := goodDraft()
	edgeNoBasis.Edges = []sources.EdgeClaim{{From: "a", Type: sources.EdgeDirectKnown, SourceID: "owner"}}
	edgeBadType := goodDraft()
	edgeBadType.Edges = []sources.EdgeClaim{{From: "a", Type: "friend", SourceID: "owner", Basis: "x"}}

	for name, d := range map[string]sources.CandidateDraft{
		"no name": noName, "no evidence": noEvidence, "no source": noSource,
		"evidence with no source": evNoSource, "evidence with no date": evNoDate,
		"evidence with no kind": evNoKind, "evidence with no citation": evNoCite,
		"edge with no basis": edgeNoBasis, "edge outside the closed set": edgeBadType,
	} {
		if err := ValidateDraft(d); err == nil {
			t.Errorf("a draft with %s was accepted", name)
		}
	}
	if err := ValidateDraft(goodDraft()); err != nil {
		t.Errorf("a well-formed draft was refused: %v", err)
	}
}

// D15, at the converter: a draft that carries an email or a phone has them
// DROPPED. A published address is evidence, and a human promotes it.
func TestConverterDropsContactDetails(t *testing.T) {
	s, _ := testStore(t)
	d := goodDraft()
	d.Role = "role/mri-engineer"
	d.Contact = map[string]string{"email": "corin@example.test", "phone": "+1 555 0100"}
	d.Evidence = append(d.Evidence, sources.Evidence{
		SourceID: "manual", URLOrFile: "https://example.test/contact",
		RetrievedAt: testNow, Kind: sources.EvidenceContactPublished,
		Snippet: "corin@example.test", Trust: sources.TrustLow,
	})
	c, err := s.AcceptDraft(d, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if c.Profile["email"] != "" || c.Profile["phone"] != "" {
		t.Fatalf("an adapter's contact fields reached the profile: %+v", c.Profile)
	}
	raw := s.raw("candidates/corin-ablett.md")
	if strings.Contains(raw, "[email:: corin@example.test]") ||
		strings.Contains(raw, "+1 555 0100") {
		t.Fatalf("a contact detail reached the record:\n%s", raw)
	}
	// but the published address survives as a CITATION the owner can act on
	if !strings.Contains(raw, "[kind:: contact_published]") ||
		!strings.Contains(raw, "corin@example.test") {
		t.Fatalf("the published address was not kept as evidence:\n%s", raw)
	}
	// and the owner may still type one by hand — that is the only path
	got, err := s.UpdateCandidate(c.ID, map[string]string{"email": "corin@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Profile["email"] != "corin@example.test" {
		t.Errorf("the owner could not type a contact address: %+v", got.Profile)
	}
}

// recruiting is private; aion is the public export contract. The import edge
// must not exist in either direction, so serving one of these records through
// the portal is something the code cannot express.
func TestRecruitingDoesNotImportAion(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatal("parsed no packages — the boundary check is not running")
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				if path := strings.Trim(imp.Path.Value, `"`); path == "manifest/aion" ||
					strings.HasPrefix(path, "manifest/aion/") {
					t.Errorf("%s imports %s — aion is the PUBLIC export contract, and "+
						"these records carry candidate PII", name, path)
				}
			}
		}
	}
}

// The domain never writes system/aion/hiring.md (D9): that file is published
// at portal.aion.bio before any sign-in gate.
func TestNothingAddressesTheHiringFile(t *testing.T) {
	s, _ := testStore(t)
	for _, name := range append(append([]string{}, SeedOrder...), Files...) {
		rel := s.Rel(name)
		if !strings.HasPrefix(rel, "system/aion/recruiting/") {
			t.Errorf("%s resolves to %s — outside the recruiting root", name, rel)
		}
		if strings.HasSuffix(rel, "/hiring.md") {
			t.Errorf("%s resolves onto the published hiring file", name)
		}
	}
	for shape := range Corpora {
		if strings.Contains(shape, "hiring") {
			t.Errorf("the corpora registry names %q", shape)
		}
	}
}
