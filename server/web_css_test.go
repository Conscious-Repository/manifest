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
