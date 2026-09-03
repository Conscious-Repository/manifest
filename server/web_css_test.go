package server

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A CSS class defined twice in one stylesheet is invisible to every other check
// we have: the file parses, nothing errors, and the later rule silently wins.
//
// This is not hypothetical. `.ooda-split` meant "the list | inspector pane" from
// the day the portal shipped; a later commit reused the name for "one contract
// amount divided across properties" and gave it `flex-direction: column`. The
// PORTFOLIO's 340px sticky inspector was pushed underneath all 42 rows, ~1,400px
// below the fold, and the CHAT tab's thread list and engine rail stacked the same
// way. It presented as "clicking a property does nothing" — a bug report about
// behaviour, caused by a name.
//
// Two sessions edit these files. The guard is cheap; the failure is not.
func TestPortalCSSHasNoDuplicateSelectors(t *testing.T) {
	for _, sheet := range []string{
		"web/ooda/src/ooda.css",
		"web/portal/src/portal.css",
	} {
		b, err := fs.ReadFile(webFiles, sheet)
		if err != nil {
			continue // a portal that does not ship this file is not a failure
		}
		for sel, lines := range duplicateSelectors(string(b)) {
			t.Errorf("%s: %q is defined %d times (lines %v) — the last one wins, "+
				"silently. Rename the newcomer or merge the rules.",
				sheet, sel, len(lines), lines)
		}
	}
}

var (
	cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// a top-level rule head: everything before the first { on its own line
	cssRuleHead = regexp.MustCompile(`(?m)^([.#][a-zA-Z0-9_-]+)\s*\{`)
)

// duplicateSelectors returns single-class/id selectors declared more than once
// at the TOP LEVEL of a sheet, mapped to the 1-indexed lines they appear on.
//
// Deliberately narrow: only bare `.class {` or `#id {` starting a line, so
// compound selectors, descendant rules, and anything nested inside a media
// query are ignored. Those legitimately repeat — `.ooda-split` inside
// `@media (max-width: 860px)` is the intended responsive override, not a
// collision. Narrow and trusted beats broad and muted.
func duplicateSelectors(css string) map[string][]int {
	css = cssComment.ReplaceAllString(css, "")

	// blank out the body of every @-block so nested rules are not seen as
	// top-level, while keeping byte offsets stable for line counting
	body := []byte(css)
	depth, inAt := 0, false
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '@':
			if depth == 0 {
				inAt = true
			}
		case '{':
			depth++
			if inAt && depth == 1 {
				continue
			}
		case '}':
			depth--
			if depth <= 0 {
				depth = 0
				inAt = false
			}
		}
		if inAt && depth >= 1 && body[i] != '\n' {
			body[i] = ' '
		}
	}

	at := map[string][]int{}
	for _, m := range cssRuleHead.FindAllSubmatchIndex(body, -1) {
		sel := string(body[m[2]:m[3]])
		at[sel] = append(at[sel], 1+strings.Count(string(body[:m[0]]), "\n"))
	}
	dupes := map[string][]int{}
	for sel, lines := range at {
		if len(lines) > 1 {
			sort.Ints(lines)
			dupes[sel] = lines
		}
	}
	return dupes
}

// The detector itself, against the shape that actually shipped: a real
// collision must fail, and a responsive override inside @media must not.
func TestDuplicateSelectorDetector(t *testing.T) {
	const sheet = `
.ooda-split { display: flex; gap: 28px; }
.ooda-insp { width: 340px; }
@media (max-width: 860px) {
  .ooda-split { flex-direction: column; }
  .ooda-insp { width: 100%; }
}
/* .ooda-split { commented out; } */
.ooda-row.sel { background: #eee; }
.ooda-row .r { text-align: right; }
`
	if d := duplicateSelectors(sheet); len(d) != 0 {
		t.Fatalf("false positive — a @media override and a compound selector are "+
			"not collisions: %v", d)
	}

	collided := sheet + "\n.ooda-split { display: flex; flex-direction: column; }\n"
	d := duplicateSelectors(collided)
	if lines, ok := d[".ooda-split"]; !ok || len(lines) != 2 {
		t.Fatalf("the real collision was not caught: %v", d)
	}
}

// The portfolio and chat panes must keep the row-direction split they were
// written against — this is the specific rule the collision broke.
func TestOodaSplitStaysARow(t *testing.T) {
	b, err := fs.ReadFile(webFiles, "web/ooda/src/ooda.css")
	if err != nil {
		t.Fatal(err)
	}
	css := cssComment.ReplaceAllString(string(b), "")
	i := strings.Index(css, ".ooda-split {")
	if i < 0 {
		t.Fatal("the .ooda-split pane rule is gone — PORTFOLIO and CHAT depend on it")
	}
	rule := css[i : i+strings.Index(css[i:], "}")]
	if strings.Contains(rule, "column") {
		t.Fatalf(".ooda-split is a column at top level (%q) — that strands the "+
			"inspector below the list; the column belongs only in the phone "+
			"media query, where .ooda-insp is widened to match", rule)
	}
}

// The cockpit's own sheets are the larger, faster-moving half of the same
// hazard the portal test above describes, and until now nothing guarded them:
// index.html loads 26 stylesheets into ONE cascade, and the next tab to ship
// adds another. A name reused inside a single sheet loses the earlier rule
// exactly the way `.ooda-split` did.
//
// Same narrow detector, so the same things stay legal: @media overrides,
// compound selectors, and a name that two DIFFERENT sheets each define — that
// is cross-file layering, which the load order in index.html makes deliberate.
// What fails is one sheet defining one bare selector twice.
func TestCockpitCSSHasNoDuplicateSelectors(t *testing.T) {
	ents, err := fs.ReadDir(webFiles, "web/css")
	if err != nil {
		t.Fatalf("read web/css: %v", err)
	}
	sheets := 0
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
			continue
		}
		sheet := "web/css/" + e.Name()
		b, err := fs.ReadFile(webFiles, sheet)
		if err != nil {
			t.Fatalf("read %s: %v", sheet, err)
		}
		sheets++
		found := duplicateSelectors(string(b))
		for sel, lines := range found {
			if cockpitDuplicateBaseline[sheet][sel] {
				continue
			}
			t.Errorf("%s: %q is defined %d times (lines %v) — the last one wins, "+
				"silently. Rename the newcomer or merge the rules.",
				sheet, sel, len(lines), lines)
		}
		// and the baseline only ever shrinks: a fixed sheet must drop its entry,
		// or the exemption outlives the reason for it
		for sel := range cockpitDuplicateBaseline[sheet] {
			if _, still := found[sel]; !still {
				t.Errorf("%s: %q is no longer duplicated — delete it from "+
					"cockpitDuplicateBaseline so the guard covers it again.", sheet, sel)
			}
		}
	}
	if sheets == 0 {
		t.Fatal("no cockpit stylesheets were scanned — the embed layout moved")
	}
}

// cockpitDuplicateBaseline is what was already in the tree the day this guard
// was written, exempted so the check can go in without a CSS rewrite riding
// along. It is not a blessing — it is a list, and the test above makes it
// shrink-only.
//
// Two kinds are in here:
//
//   - the documented mono-label idiom (`ui-conventions.md §labels`): the
//     invariant recipe stated once at the top of the sheet, then the same
//     selector again lower down carrying only its own size/colour/padding.
//     `.rail-group-label`, `.aion-org-label`, `.reading-strip-head`,
//     `.note-bl-head`, `.o-st` and `.ta-item` are all this shape. Deliberate,
//     and the detector cannot tell it apart from the accident.
//   - genuine later-in-file overrides — `.cad-raw` (45 → 194),
//     `.signal-label` (34 → 113), `.cols-aion-people` (101 → 133). These read
//     as drift and should be merged the next time those sheets are opened;
//     they are grandfathered, not endorsed.
var cockpitDuplicateBaseline = map[string]map[string]bool{
	"web/css/05-primitives.css": {".aion-org-label": true, ".ta-item": true},
	"web/css/07-nav.css":        {".rail-group-label": true},
	"web/css/20-goals.css":      {".o-st": true},
	"web/css/40-spirits.css":    {".cad-raw": true},
	"web/css/45-feed.css":       {".signal-label": true},
	"web/css/65-reading.css":    {".reading-strip-head": true},
	"web/css/70-note.css":       {".note-bl-head": true},
	"web/css/92-aion.css":       {".cols-aion-people": true},
}
