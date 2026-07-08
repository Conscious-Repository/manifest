package vault

import (
	"regexp"
	"strings"
)

// Kind classifies a markdown file by convention.
type Kind int

const (
	KindOther Kind = iota
	KindDaily
	KindGoals
)

// dailyRe matches a daily-note filename EXACTLY: YYYY-MM-DD.md and nothing more.
// It must stay strictly anchored: the vault holds many notes whose names merely
// START with a date (e.g. "2026-01-09 meeting.md") — those are NOT daily notes.
var dailyRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.md$`)

// classify decides a file's Kind from its base name, reading frontmatter only
// when the cheap filename checks don't settle it. For daily notes it returns the
// captured date (YYYY-MM-DD); otherwise the second value is "".
func classify(base, path, goalsName string) (Kind, string) {
	if m := dailyRe.FindStringSubmatch(base); m != nil {
		return KindDaily, m[1]
	}
	if strings.EqualFold(base, goalsName) {
		return KindGoals, ""
	}
	if strings.EqualFold(frontmatterType(path), "goals") {
		return KindGoals, ""
	}
	return KindOther, ""
}
