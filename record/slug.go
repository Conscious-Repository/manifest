package record

import (
	"regexp"
	"strings"
	"unicode"
)

var slugStripRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slug is THE slug rule: lowercase, non-alphanumerics collapsed to hyphens,
// trimmed, optionally capped (cap <= 0 = uncapped; a cut never leaves a
// trailing hyphen). Empty-input fallbacks ("item", "area", …) are the
// caller's policy, not the kernel's.
func Slug(s string, cap int) string {
	s = strings.Trim(slugStripRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if cap > 0 && len(s) > cap {
		s = strings.Trim(s[:cap], "-")
	}
	return s
}

// SlugSpaces is the FILENAME rule for authored and curated notes: the note's
// lowercased title with every run of separator characters (hyphens,
// underscores, punctuation, symbols, whitespace) collapsed to one space,
// apostrophes kept, trimmed. Letters and digits of any script survive, so a
// title keeps its accents; nothing that could read as a dash does. cap is in
// runes (cap <= 0 = uncapped; a cut never leaves a trailing space).
//
// This names FILES only ("being a lizard.md"). Stable identifiers — item ids,
// feed slugs, aion-bl/<slug> — keep Slug.
func SlugSpaces(s string, cap int) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || isApostrophe(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		default:
			space = true
		}
	}
	out := b.String()
	if cap > 0 {
		if runes := []rune(out); len(runes) > cap {
			out = strings.TrimSpace(string(runes[:cap]))
		}
	}
	return out
}

func isApostrophe(r rune) bool { return r == '\'' || r == '’' || r == '‘' }
