package aion

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// Synthetic fixtures only. The held bodies are invented canaries — never a
// real held note — and every distinctive word in them is what the leak guard
// hunts for across the whole rendered pack.
const (
	tOpenA     = "2026-03-02 hevolution memo.md"
	tOpenB     = "2026-05-14 investor sync with maria.md"
	tInternalA = "2026-06-20 lab ops review.md"
	tInternalB = "2026-07-01 candidate interview omar.md"
	tHeldA     = "2026-08-09 quokka equity sync.md"
	tHeldB     = "2026-08-30 - 2026-09-02 zebra offer thread.md"
	tUnmapped  = "2026-09-20 wallaby sync.md"
	tNotAion   = "2026-09-01 unrelated ooda call.md"
)

func fixtureRaws() []RawNote {
	note := func(cats, people, body string) []byte {
		return []byte("---\ncategories:\n" + cats + "---\n" + people + "\n\n" + body + "\n")
	}
	return []RawNote{
		{Name: tOpenA, Body: note("  - aion\n  - research\n", "[[maria lopez]]",
			"# Hevolution memo\n\nUltrasound screening needs several transducers because conversion efficiency depends on the crystal.\n\nSecond paragraph.")},
		{Name: tOpenB, Body: note("  - aion\n  - fundraising\n  - sync\n", "[[maria lopez]] [[acme ventures]]",
			"Maria wants the deck by Friday. Round target unchanged.")},
		{Name: tInternalA, Body: note("  - aion\n  - sync\n", "[[omar haddad]]",
			"| a | b |\n|---|---|\n| 1 | 2 |\n\nLab ops: the incubator rig moves to bay two next week.")},
		{Name: tInternalB, Body: note("  - aion\n  - interview\n", "[[omar haddad]]",
			"Omar walked through his imaging pipeline work.")},
		{Name: tHeldA, Body: note("  - aion\n  - sync\n", "[[quokka person]]",
			"QUOKKACANARY equity split discussion: the vestibule percentages and the hypothecary schedule were agreed.")},
		{Name: tHeldB, Body: note("  - aion\n  - sync\n", "[[zebra person]]",
			"ZEBRACANARY offer letter: base of the stipendiary and the signing bonus.")},
		{Name: tUnmapped, Body: note("  - aion\n  - sync\n", "[[wallaby person]]",
			"WALLABYCANARY untiered conversation.")},
		{Name: tNotAion, Body: note("  - ooda\n  - sync\n", "[[someone]]", "Not an AION note at all.")},
	}
}

func fixtureTierMap() TierMap {
	return TierMap{
		tOpenA: {Tier: TierOpen, Reason: "research"}, tOpenB: {Tier: TierOpen, Reason: "fundraising"},
		tInternalA: {Tier: TierInternal, Reason: "ops"}, tInternalB: {Tier: TierInternal, Reason: "hiring"},
		tHeldA: {Tier: TierHeld, Reason: "compensation/equity"}, tHeldB: {Tier: TierHeld, Reason: "compensation/offer"},
	}
}

func fixturePeople(key string) (string, bool) {
	switch key {
	case "maria lopez":
		return "Maria Lopez", true
	case "omar haddad":
		return "Omar Haddad", true
	case "quokka person", "zebra person", "wallaby person":
		return strings.Title(key), true
	}
	return "", false
}

func fixtureInput(t *testing.T) TranscriptPackInput {
	t.Helper()
	corpus := BuildTranscriptCorpus(fixtureTierMap(), fixtureRaws(), fixturePeople)
	return TranscriptPackInput{
		Revision: "c0ffee0011223344",
		At:       time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC),
		Corpus:   corpus,
		Email:    ProjectEmailDays(fixtureEmailThreads()),
	}
}

func TestBuildTranscriptCorpusAppliesTheTierGate(t *testing.T) {
	c := BuildTranscriptCorpus(fixtureTierMap(), fixtureRaws(), fixturePeople)
	var names []string
	for _, n := range c.Notes {
		names = append(names, n.Name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join([]string{tOpenA, tOpenB, tInternalA, tInternalB}, ",") {
		t.Fatalf("eligible notes = %v", names)
	}
	if c.Held != 2 || c.Unmapped != 1 {
		t.Fatalf("held %d unmapped %d, want 2 and 1", c.Held, c.Unmapped)
	}
	if len(c.UnmappedNames) != 1 || c.UnmappedNames[0] != tUnmapped {
		t.Fatalf("unmapped names = %v", c.UnmappedNames)
	}
	for _, n := range c.Notes {
		if n.Name == tOpenB {
			if n.Date != "2026-05-14" || n.Title != "investor sync with maria" {
				t.Fatalf("date/title = %q/%q", n.Date, n.Title)
			}
			if strings.Join(n.People, ",") != "Maria Lopez" { // acme ventures is not a person
				t.Fatalf("people = %v", n.People)
			}
			if len(n.Hash) != 64 {
				t.Fatalf("hash = %q", n.Hash)
			}
		}
	}
	first, last := c.Span()
	if first != "2026-03-02" || last != "2026-07-01" {
		t.Fatalf("span = %s → %s", first, last)
	}
}

// Same input → identical bytes, and the stamp file is last in write order.
func TestTranscriptPackIsDeterministic(t *testing.T) {
	a := RenderTranscriptPack(fixtureInput(t))
	b := RenderTranscriptPack(fixtureInput(t))
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("renders differ in length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Path != b[i].Path || a[i].Body != b[i].Body {
			t.Fatalf("%s differs across two renders", a[i].Path)
		}
	}
	if a[len(a)-1].Path != TranscriptStampFile {
		t.Fatalf("last file = %s, want %s", a[len(a)-1].Path, TranscriptStampFile)
	}
	paths := map[string]bool{}
	for _, f := range a {
		paths[f.Path] = true
	}
	for _, want := range []string{"transcripts/INDEX.md", "transcripts/" + tOpenA, "transcripts/" + tInternalB,
		"digests/fundraising.md", "digests/hiring.md", "digests/research.md", "digests/strategy.md",
		"digests/email-2026-09-10.md", "digests/README.md"} {
		if !paths[want] {
			t.Errorf("%s missing from the pack", want)
		}
	}
	// verbatim: the transcript file IS the note's bytes
	for _, f := range a {
		if f.Path == "transcripts/"+tOpenA && f.Body != string(fixtureRaws()[0].Body) {
			t.Fatal("transcript body is not the vault's own text")
		}
	}
}

var tokenRe = regexp.MustCompile(`[a-z0-9]{6,}`)

func tokensOf(s string) map[string]bool {
	out := map[string]bool{}
	for _, tok := range tokenRe.FindAllString(strings.ToLower(s), -1) {
		out[tok] = true
	}
	return out
}

// THE held-leak guard. Not a held filename, not a held title, not a canary,
// and not any word that exists ONLY because a held note exists (the pack
// rendered with the held notes deleted from disk is the baseline — anything
// the real pack says that the baseline does not is a leak by inference).
func TestTranscriptPackNeverLeaksHeldMaterial(t *testing.T) {
	in := fixtureInput(t)
	pack := RenderTranscriptPack(in)

	var withoutHeld []RawNote
	for _, r := range fixtureRaws() {
		if r.Name != tHeldA && r.Name != tHeldB && r.Name != tUnmapped {
			withoutHeld = append(withoutHeld, r)
		}
	}
	base := in
	base.Corpus = BuildTranscriptCorpus(fixtureTierMap(), withoutHeld, fixturePeople)
	baseline := RenderTranscriptPack(base)
	allowed := map[string]bool{}
	for _, f := range baseline {
		for tok := range tokensOf(f.Path + "\n" + f.Body) {
			allowed[tok] = true
		}
	}
	for _, tok := range []string{"unmapped", "awaiting"} { // the coverage line's own words
		allowed[tok] = true
	}

	forbidden := map[string]bool{}
	for _, r := range fixtureRaws() {
		if r.Name == tHeldA || r.Name == tHeldB || r.Name == tUnmapped {
			for tok := range tokensOf(r.Name + "\n" + string(r.Body)) {
				if !allowed[tok] {
					forbidden[tok] = true
				}
			}
		}
	}
	for _, must := range []string{"quokkacanary", "zebracanary", "wallabycanary", "hypothecary", "stipendiary"} {
		if !forbidden[must] {
			t.Fatalf("guard is not armed: %q should be a forbidden token", must)
		}
	}
	for _, f := range pack {
		if strings.HasPrefix(f.Path, "transcripts/") && f.Path != "transcripts/INDEX.md" {
			if f.Path == "transcripts/"+tHeldA || f.Path == "transcripts/"+tHeldB || f.Path == "transcripts/"+tUnmapped {
				t.Fatalf("%s was written", f.Path)
			}
		}
		text := strings.ToLower(f.Path + "\n" + f.Body)
		for _, name := range []string{tHeldA, tHeldB, tUnmapped, "quokka equity", "zebra offer", "wallaby"} {
			if strings.Contains(text, strings.ToLower(name)) {
				t.Fatalf("%s references held/unmapped note %q", f.Path, name)
			}
		}
		for tok := range tokensOf(text) {
			if forbidden[tok] {
				t.Fatalf("%s carries token %q that exists only in held material", f.Path, tok)
			}
		}
	}
	// and the counts ARE there — exclusion is stated, not hidden
	for _, f := range pack {
		if f.Path == "transcripts/INDEX.md" || strings.HasPrefix(f.Path, "digests/") && !strings.HasPrefix(f.Path, "digests/email-") {
			if !strings.Contains(f.Body, "held notes excluded: 2") {
				t.Fatalf("%s does not state the held count", f.Path)
			}
			if !strings.Contains(f.Body, "unmapped notes excluded: 1") {
				t.Fatalf("%s does not state the unmapped count", f.Path)
			}
		}
	}
}

func TestTranscriptIndexRows(t *testing.T) {
	pack := RenderTranscriptPack(fixtureInput(t))
	var index string
	for _, f := range pack {
		if f.Path == "transcripts/INDEX.md" {
			index = f.Body
		}
	}
	if !strings.Contains(index, "revision: c0ffee0011223344") {
		t.Fatal("INDEX carries no revision stamp")
	}
	if !strings.Contains(index, "Coverage: 4 notes (2 open · 2 internal) · span 2026-03-02 → 2026-07-01 · held notes excluded: 2 · unmapped notes excluded: 1") {
		t.Fatalf("coverage line wrong:\n%s", index)
	}
	for _, row := range []string{
		"| 2026-07-01 | candidate interview omar | internal | Omar Haddad | `" + tInternalB + "` |",
		"| 2026-05-14 | investor sync with maria | open | Maria Lopez | `" + tOpenB + "` |",
	} {
		if !strings.Contains(index, row) {
			t.Fatalf("row missing:\n%s\n---\n%s", row, index)
		}
	}
	// newest first
	if strings.Index(index, tInternalB) > strings.Index(index, tOpenA) {
		t.Fatal("INDEX is not newest-first")
	}
}

// Digest coverage: each topic counts only its eligible members, states the
// span of what it counted, and always states the whole held count.
func TestDigestCoverageCounts(t *testing.T) {
	pack := RenderTranscriptPack(fixtureInput(t))
	body := map[string]string{}
	for _, f := range pack {
		body[f.Path] = f.Body
	}
	cases := map[string]struct {
		count int
		span  string
		has   []string
	}{
		"digests/fundraising.md": {1, "2026-05-14 → 2026-05-14", []string{"investor sync with maria", "Maria wants the deck by Friday. Round target unchanged."}},
		"digests/hiring.md":      {1, "2026-07-01 → 2026-07-01", []string{"candidate interview omar", "Omar walked through his imaging pipeline work."}},
		"digests/research.md":    {1, "2026-03-02 → 2026-03-02", []string{"hevolution memo", "Ultrasound screening needs several transducers"}},
		"digests/strategy.md":    {0, "no dated notes", []string{"none in the eligible corpus"}},
	}
	for path, want := range cases {
		b, ok := body[path]
		if !ok {
			t.Fatalf("%s missing", path)
		}
		line := "Coverage: " + itoa(want.count) + " notes counted · span " + want.span + " · held notes excluded: 2 · unmapped notes excluded: 1"
		if !strings.Contains(b, line) {
			t.Fatalf("%s coverage wrong — want %q in:\n%s", path, line, b)
		}
		for _, h := range want.has {
			if !strings.Contains(b, h) {
				t.Fatalf("%s lacks %q:\n%s", path, h, b)
			}
		}
	}
	readme := body["digests/README.md"]
	if !strings.Contains(readme, "Coverage: 4 notes counted · span 2026-03-02 → 2026-07-01 · held notes excluded: 2") ||
		!strings.Contains(readme, "Tier map: 2 open · 2 internal · 2 held") {
		t.Fatalf("README coverage wrong:\n%s", readme)
	}
	// the lab-ops internal note has a table first; the lead skips it
	if lead := LeadParagraph(fixtureRaws()[2].Body, 240); lead != "Lab ops: the incubator rig moves to bay two next week." {
		t.Fatalf("lead = %q", lead)
	}
}

// The portal sees the OPEN subset only, addressed by sha256 of the verbatim
// bytes with the note's date and title beside it.
func TestPortalArtifactsAreOpenOnly(t *testing.T) {
	c := BuildTranscriptCorpus(fixtureTierMap(), fixtureRaws(), fixturePeople)
	arts := c.PortalArtifacts()
	if len(arts) != 2 {
		t.Fatalf("artifacts = %+v", arts)
	}
	for _, a := range arts {
		if a.Tier != TierOpen || a.Name != tOpenA && a.Name != tOpenB {
			t.Fatalf("non-open artifact %+v", a)
		}
		if a.Date == "" || a.Title == "" || len(a.Hash) != 64 || a.Size == 0 || a.Mime != "text/markdown" {
			t.Fatalf("incomplete artifact %+v", a)
		}
	}
}

// ---- email digest -----------------------------------------------------------

func fixtureEmailThreads() []EmailThread {
	return []EmailThread{
		{Name: "2026-09-10 lab supplies quote 1a04e57870972dba", ThreadID: "1a04e57870972dba", Date: "2026-09-10",
			Subject: "lab supplies quote", Senders: []string{"Maria Lopez"}, Mapped: true, Tier: TierInternal,
			Actions: []EmailAction{{Kind: "task", Title: "Approve the transducer order"}, {Kind: "decision", Title: "Buy the Olympus V303 first"}},
			Text:    "BODYCANARY-ONE the quote for the transducers is attached, thanks"},
		{Name: "2026-09-09 - 2026-09-10 intro to the robotics group", ThreadID: "abc", Date: "2026-09-09", EndDate: "2026-09-10",
			Subject: "intro to the robotics group", Senders: []string{"Omar Haddad", "Maria Lopez"}, Mapped: false,
			Text: "BODYCANARY-TWO would love to connect you two"},
		{Name: "2026-09-10 v job offer", ThreadID: "def", Date: "2026-09-10",
			Subject: "v job offer", Senders: []string{"V Person"}, Mapped: true, Tier: TierHeld,
			Text: "OFFERCANARY base salary and equity"},
		{Name: "2026-09-10 visa question", ThreadID: "ghi", Date: "2026-09-10",
			Subject: "visa question", Senders: []string{"Omar Haddad"}, Mapped: false,
			Text: "IMMIGRATIONCANARY my h1b transfer timing"},
		{Name: "2026-09-10 catching up", ThreadID: "jkl", Date: "2026-09-10",
			Subject: "catching up", Senders: []string{"Old Friend"}, Mapped: false,
			Text: "FAMILYCANARY my wife and kids say hi, also the divorce is final"},
		{Name: "2026-09-11 family office intro", ThreadID: "mno", Date: "2026-09-11",
			Subject: "family office intro", Senders: []string{"Maria Lopez"}, Mapped: false,
			Text: "BODYCANARY-THREE a family office that backs longevity companies"},
	}
}

func TestEmailRoutingHoldsSensitiveThreads(t *testing.T) {
	days := ProjectEmailDays(fixtureEmailThreads())
	if len(days) != 2 || days[0].Date != "2026-09-10" || days[1].Date != "2026-09-11" {
		t.Fatalf("days = %+v", days)
	}
	d := days[0]
	if len(d.Threads) != 2 || len(d.Held) != 3 {
		t.Fatalf("day 1: %d shown, %d held (want 2 and 3)", len(d.Threads), len(d.Held))
	}
	reasons := map[string]string{}
	for _, h := range d.Held {
		reasons[h.Thread.Subject] = h.Reason
	}
	if reasons["v job offer"] != "tier: held" || reasons["visa question"] != "pattern: immigration" || reasons["catching up"] != "pattern: family" {
		t.Fatalf("reasons = %v", reasons)
	}
	// "family office" is investor vocabulary, not a relative
	if len(days[1].Threads) != 1 || len(days[1].Held) != 0 {
		t.Fatalf("family office thread was held: %+v", days[1])
	}
	for _, s := range []string{"we agreed on a 15% pay raise", "attached the offer letter", "the NDA draft", "her surgery went well",
		"performance improvement plan", "negotiating the term sheet", "stock option grant"} {
		if _, ok := SensitiveReason(s); !ok {
			t.Errorf("%q should be sensitive", s)
		}
	}
	for _, s := range []string{"pip install manifest", "the MRI coil arrived", "clinical imaging protocol", "legal entity name on the invoice"} {
		if r, ok := SensitiveReason(s); ok {
			t.Errorf("%q wrongly held as %s", s, r)
		}
	}
}

// Kairos's digest carries subjects, senders, decisions and action items, the
// held COUNT — and never a body, never a held subject or name.
func TestEmailDigestNeverCarriesBodiesOrHeldItems(t *testing.T) {
	days := ProjectEmailDays(fixtureEmailThreads())
	at := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	digest := RenderEmailDigest(days[0], "c0ffee0011223344", at)
	for _, want := range []string{
		"Coverage: 2 threads · 3 items held for owner review",
		"- lab supplies quote\n", "- intro to the robotics group\n",
		"- Maria Lopez (2)\n", "- Omar Haddad (1)\n",
		"**intro to the robotics group** (2026-09-09 → 2026-09-10) · with Omar Haddad, Maria Lopez",
		"- Buy the Olympus V303 first — from: lab supplies quote",
		"- [task] Approve the transducer order — from: lab supplies quote",
	} {
		if !strings.Contains(digest, want) {
			t.Fatalf("digest lacks %q:\n%s", want, digest)
		}
	}
	for _, leak := range []string{"CANARY", "job offer", "visa", "catching up", "V Person", "Old Friend", "salary", "wife", "h1b", "1a04e57870972dba"} {
		if strings.Contains(digest, leak) {
			t.Fatalf("digest leaks %q:\n%s", leak, digest)
		}
	}
	hold := RenderEmailHold(days[0], "c0ffee0011223344", at)
	for _, want := range []string{"Held: 3", "v job offer", "tier: held", "visa question", "pattern: immigration", "catching up", "pattern: family"} {
		if !strings.Contains(hold, want) {
			t.Fatalf("hold file lacks %q:\n%s", want, hold)
		}
	}
	if strings.Contains(hold, "CANARY") {
		t.Fatal("hold file carries a body")
	}
	if RenderEmailDigest(days[0], "c0ffee0011223344", at) != digest {
		t.Fatal("email digest is not deterministic")
	}
}

func TestEmailHelpers(t *testing.T) {
	if got := EmailSubjectFromName("2026-08-29 - 2026-09-07 building new mris in st. louis 1a04e57870972dba", "1a04e57870972dba"); got != "building new mris in st. louis" {
		t.Fatalf("subject = %q", got)
	}
	if got := EmailSubjectFromName("2026-07-24 ooda group investor update - july", "zzz"); got != "ooda group investor update - july" {
		t.Fatalf("subject = %q", got)
	}
	a, ok := ParseExtractorAction("aion: decision — Use a CRO for the pig work")
	if !ok || a.Kind != "decision" || a.Title != "Use a CRO for the pig work" {
		t.Fatalf("action = %+v %v", a, ok)
	}
	if _, ok := ParseExtractorAction("re: contract — x"); ok {
		t.Fatal("non-aion action accepted")
	}
}
