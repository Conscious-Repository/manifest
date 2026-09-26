package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// The FEED's verdict row on a phone, guarded by the two ways it has broken.
//
//  1. `flex: 1` on the pills divided the row EVENLY — a basis of 0 ignores the
//     label. "Confirm & apply" needs 143px and was handed 116px; with
//     `min-width: 0` letting it shrink past its content and a pill's overflow
//     visible, the words painted across both edges of their own button.
//     A verdict is never narrower than its word: keep an `auto` basis and do
//     not license shrinking below content.
//
//  2. `position: sticky; bottom: 0` kept one card's verdicts reachable down a
//     long card — but the FEED is a LIST. Every approval card pinned its own
//     row at once: a row floated in the empty space under its card while the
//     next card's row lay over that card's evidence. A card's verdicts belong
//     to the end of that card.
//
// Both shipped from this repo (2026-09-13) and were reported from a phone
// (2026-09-26). The rules live in the 860px band of 95-mobile.css.
func TestPhoneVerdictRowNeitherStarvesItsLabelsNorSticks(t *testing.T) {
	b, err := fs.ReadFile(webFiles, "web/css/95-mobile.css")
	if err != nil {
		t.Fatal(err)
	}
	css := cssComment.ReplaceAllString(string(b), "")

	for _, rule := range rulesFor(css, `.feed-actions .pill, .appr-actions .pill`) {
		if flexBasisZero.MatchString(rule) {
			t.Errorf("the phone verdict pills declare a zero flex basis (%q): the row is divided evenly and the longest label overflows its button. Use `flex: 1 1 auto`.", strings.TrimSpace(rule))
		}
	}
	for _, rule := range rulesFor(css, `#feedView .approval-card > .feed-actions > .pill`) {
		if strings.Contains(rule, "min-width: 0") {
			t.Errorf("a verdict pill is licensed to shrink below its label (%q) — that is how the words ended up outside the button.", strings.TrimSpace(rule))
		}
	}
	for _, rule := range rulesFor(css, `#feedView .approval-card > .feed-actions`) {
		if strings.Contains(rule, "position: sticky") || strings.Contains(rule, "position:sticky") {
			t.Error("the approval card's action row is sticky again: in a feed of cards every row pins at once, so rows detach from their card and cover the next one's evidence.")
		}
	}
}

// rulesFor returns the declaration block of every rule whose head is exactly
// this selector (comments already stripped).
func rulesFor(css, selector string) []string {
	var out []string
	for i := 0; ; {
		at := strings.Index(css[i:], selector)
		if at < 0 {
			return out
		}
		at += i
		i = at + len(selector)
		rest := strings.TrimLeft(css[i:], " \t\r\n")
		if !strings.HasPrefix(rest, "{") { // a longer selector that merely starts the same way
			continue
		}
		// the head must start at a line boundary: `> .feed-actions > .pill`
		// must not match inside `... > .feed-actions > .appr-save`
		if head := strings.LastIndexAny(css[:at], "{};\n"); head >= 0 && strings.TrimSpace(css[head+1:at]) != "" {
			continue
		}
		if end := strings.Index(rest, "}"); end > 0 {
			out = append(out, rest[1:end])
		}
	}
}

var flexBasisZero = regexp.MustCompile(`flex:\s*(1|1\s+1)\s*;|flex:\s*\d+\s+\d+\s+0`)
