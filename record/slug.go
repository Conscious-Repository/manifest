package record

import (
	"regexp"
	"strings"
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
