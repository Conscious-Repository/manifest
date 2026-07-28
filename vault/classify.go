package vault

import (
	"strings"

	"manifest/record"
)

// Kind classifies a markdown file by convention.
type Kind int

const (
	KindOther Kind = iota
	KindDaily
	KindGoals
)

// dailyRe is the kernel's exact daily-note filename grammar (YYYY-MM-DD.md
// and nothing more — dated-PREFIX notes are not daily notes).
var dailyRe = record.DailyNoteRe

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
	if strings.EqualFold(record.FrontmatterScalar(path, "type"), "goals") {
		return KindGoals, ""
	}
	return KindOther, ""
}
