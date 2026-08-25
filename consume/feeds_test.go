package consume

import (
	"strings"
	"testing"
)

const handWritten = `---
categories: [feeds]
---
#feeds

Sources the CONSUME lane polls. Hand edits are preserved; unknown fields survive.

## essays
- Astral Codex Ten [id:: acx] [kind:: rss] [url:: https://astralcodexten.substack.com/feed] [mirror:: full]
- Melissa [id:: melissa] [kind:: x] [handle:: melissa] [min-chars:: 350] [note:: my favourite]

## ai
- Import AI [id:: import-ai] [kind:: rss] [url:: https://jack-clark.net/feed/]
`

// THE fixpoint test (§3). If parse→emit is not byte-identical, every poll
// churns the file and every Obsidian edit fights the app.
func TestFeedsRoundTripIsByteIdentical(t *testing.T) {
	for name, in := range map[string]string{
		"hand written":        handWritten,
		"scaffold":            feedsScaffold,
		"no trailing newline": "## a\n- X [id:: x] [kind:: rss] [url:: https://e.com/f]",
		"crlf":                "## a\r\n- X [id:: x] [kind:: rss] [url:: https://e.com/f]\r\n",
		"prose bullets": "## notes\n- just a prose bullet, no fields\n" +
			"- X [id:: x] [kind:: rss] [url:: https://e.com/f]\n",
	} {
		t.Run(name, func(t *testing.T) {
			want := strings.ReplaceAll(in, "\r\n", "\n")
			if got := ParseFeeds(in).String(); got != want {
				t.Errorf("not a fixpoint\nwant %q\ngot  %q", want, got)
			}
		})
	}
}

func TestParseFeeds(t *testing.T) {
	d := ParseFeeds(handWritten)
	subs := d.Subs()
	if len(subs) != 3 {
		t.Fatalf("want 3 subscriptions, got %d: %+v", len(subs), subs)
	}
	acx := subs[0]
	if acx.ID != "acx" || acx.Kind != KindRSS || acx.List != "essays" || !acx.Mirrors() {
		t.Errorf("acx parsed wrong: %+v", acx)
	}
	mel := subs[1]
	if mel.Kind != KindX || mel.Handle != "melissa" || mel.Min() != 350 {
		t.Errorf("melissa parsed wrong: %+v", mel)
	}
	if len(mel.Unknown) != 1 || mel.Unknown[0].Key != "note" {
		t.Errorf("unknown field not captured: %+v", mel.Unknown)
	}
	if subs[2].List != "ai" {
		t.Errorf("group not read from heading: %+v", subs[2])
	}
	// A prose bullet with no fields is not a subscription.
	if len(ParseFeeds("## x\n- just prose\n").Subs()) != 0 {
		t.Error("a fieldless bullet was read as a subscription")
	}
}

// An edit must touch ONE line. Everything else in the file — prose, other
// subscriptions, frontmatter — is the owner's and stays byte-identical.
func TestUpdateRewritesOnlyItsOwnLine(t *testing.T) {
	d := ParseFeeds(handWritten)
	sub, _ := d.Find("acx")
	sub.Title = "Astral Codex Ten (renamed)"
	sub.Mirror = MirrorExcerpt
	if !d.Update(sub) {
		t.Fatal("update failed")
	}
	before := strings.Split(handWritten, "\n")
	after := strings.Split(d.String(), "\n")
	if len(before) != len(after) {
		t.Fatalf("line count changed: %d → %d", len(before), len(after))
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
		}
	}
	if changed != 1 {
		t.Errorf("want exactly 1 changed line, got %d\n%s", changed, d.String())
	}
	got, _ := d.Find("acx")
	if got.Title != "Astral Codex Ten (renamed)" || got.Mirror != MirrorExcerpt {
		t.Errorf("edit did not take: %+v", got)
	}
}

// The reason the fixpoint matters in practice: a hand-added field must not be
// destroyed by an unrelated app edit.
func TestUnknownFieldsSurviveAnAppEdit(t *testing.T) {
	d := ParseFeeds(handWritten)
	sub, _ := d.Find("melissa")
	sub.MinChars = 500
	d.Update(sub)
	out := d.String()
	if !strings.Contains(out, "[note:: my favourite]") {
		t.Fatalf("hand-added field destroyed:\n%s", out)
	}
	if !strings.Contains(out, "[min-chars:: 500]") {
		t.Fatalf("edit did not apply:\n%s", out)
	}
	again := ParseFeeds(out)
	got, _ := again.Find("melissa")
	if len(got.Unknown) != 1 || got.Unknown[0].Value != "my favourite" {
		t.Errorf("unknown field lost on reparse: %+v", got.Unknown)
	}
}

func TestAddCreatesGroupsAndUniqueIDs(t *testing.T) {
	d := ParseFeeds(handWritten)
	d.Add(Subscription{Title: "New Thing", Kind: KindRSS, URL: "https://new.example/feed", List: "essays"})
	out := d.String()
	if strings.Count(out, "## essays") != 1 {
		t.Errorf("duplicated an existing heading:\n%s", out)
	}
	// It must land under essays, not at the end of the file.
	essaysAt := strings.Index(out, "## essays")
	aiAt := strings.Index(out, "## ai")
	newAt := strings.Index(out, "New Thing")
	if !(essaysAt < newAt && newAt < aiAt) {
		t.Errorf("new sub not inserted under its heading:\n%s", out)
	}

	d.Add(Subscription{Title: "Brand New Group", Kind: KindRSS, URL: "https://x.example/f", List: "papers"})
	if !strings.Contains(d.String(), "## papers") {
		t.Errorf("new group heading not created:\n%s", d.String())
	}

	// Colliding titles must not collide as ids, or two feeds share a cache.
	d.Add(Subscription{Title: "New Thing", Kind: KindRSS, URL: "https://other.example/feed", List: "essays"})
	seen := map[string]bool{}
	for _, s := range d.Subs() {
		if seen[s.ID] {
			t.Fatalf("duplicate id %q", s.ID)
		}
		seen[s.ID] = true
	}
	// Round-trip everything we just built.
	reparsed := ParseFeeds(d.String())
	if len(reparsed.Subs()) != len(d.Subs()) {
		t.Errorf("re-parse lost subscriptions: %d vs %d", len(reparsed.Subs()), len(d.Subs()))
	}
	if ParseFeeds(d.String()).String() != d.String() {
		t.Error("output is not itself a fixpoint")
	}
}

func TestRegroupMovesTheLine(t *testing.T) {
	d := ParseFeeds(handWritten)
	sub, _ := d.Find("import-ai")
	sub.List = "essays"
	d.Update(sub)
	out := d.String()
	if strings.Count(out, "import-ai") != 1 {
		t.Fatalf("regroup duplicated the line:\n%s", out)
	}
	got, _ := ParseFeeds(out).Find("import-ai")
	if got.List != "essays" {
		t.Errorf("regroup did not take: %+v", got)
	}
}

func TestRemove(t *testing.T) {
	d := ParseFeeds(handWritten)
	if !d.Remove("melissa") {
		t.Fatal("remove failed")
	}
	out := d.String()
	if strings.Contains(out, "melissa") {
		t.Errorf("line survived removal:\n%s", out)
	}
	if !strings.Contains(out, "acx") || !strings.Contains(out, "import-ai") {
		t.Errorf("removal took siblings with it:\n%s", out)
	}
	if d.Remove("nope") {
		t.Error("removing a missing id should report false")
	}
}

// A line typed by hand in Obsidian with no id still works, and the app stamps
// one on its next write rather than rejecting the line.
func TestHandAddedLineWithoutID(t *testing.T) {
	d := ParseFeeds("## essays\n- Some Blog [kind:: rss] [url:: https://some.example/feed]\n")
	subs := d.Subs()
	if len(subs) != 1 {
		t.Fatalf("hand-added line not read: %+v", subs)
	}
	if subs[0].ID == "" {
		t.Error("no id derived")
	}
	if subs[0].Kind != KindRSS {
		t.Errorf("kind: %q", subs[0].Kind)
	}
	// A bare handle line infers kind x.
	d2 := ParseFeeds("## x\n- Someone [handle:: someone]\n")
	if d2.Subs()[0].Kind != KindX {
		t.Errorf("handle should imply kind x: %+v", d2.Subs()[0])
	}
}

func TestEmptyDocumentGetsScaffold(t *testing.T) {
	d := ParseFeeds("")
	if !strings.Contains(d.String(), "## "+ungrouped) {
		t.Errorf("scaffold missing:\n%s", d.String())
	}
	d.Add(Subscription{Title: "First", Kind: KindRSS, URL: "https://e.com/f"})
	if len(ParseFeeds(d.String()).Subs()) != 1 {
		t.Errorf("first subscription did not survive:\n%s", d.String())
	}
}
