package approvals

import "testing"

// The apply-path allow-lists are a BYTE-CONTRACT with the excalibur engine's
// audit predicates (audit.AppendXQueuePathAllowed / audit.UpdateVaultSkillPathAllowed
// / audit.ApplyPathAllowed). The two live in separate modules, so the guarantee is
// this shared vector table: it is duplicated verbatim in the engine's
// audit/bytecontract_test.go, and both assert the SAME expected verdicts. If you
// change a predicate on one side, this table changes, and the sibling test must be
// updated to match — a diverging allow-list breaks one of the two suites.
//
// Keep this table identical to engine/internal/audit/bytecontract_test.go.

type pathVec struct {
	path   string
	xqueue bool // AppendXQueuePathAllowed
	skill  bool // UpdateVaultSkillPathAllowed
	apply  bool // ApplyPathAllowed (tuning surfaces)
}

var contractVectors = []pathVec{
	// append-x-queue: exactly the vault-root x-posts file
	{"x posts.md", true, false, false},
	{"x posts.MD", false, false, false},
	{"notes/x posts.md", false, false, false},
	{"x posts.txt", false, false, false},
	{"xposts.md", false, false, false},
	{"../x posts.md", false, false, false},
	// update-vault-skill: exactly SKILL.md or references/<name>.md under skills/x-content
	{"skills/x-content/SKILL.md", false, true, false},
	{"skills/x-content/references/voice-benjamin.md", false, true, false},
	{"skills/x-content/references/patterns-andercot.md", false, true, false},
	{"skills/x-content/references/sub/nested.md", false, false, false},
	{"skills/x-content/evil.md", false, false, false},
	{"skills/x-content/SKILL.txt", false, false, false},
	{"skills/other/SKILL.md", false, false, false},
	{"skills/x-content/../evil.md", false, false, false},
	{"skills/x-content/references/../SKILL.md", false, false, false},
	{"skills/x-content/references/", false, false, false},
	{"skills\\x-content\\SKILL.md", false, false, false},
	{"/abs/skills/x-content/SKILL.md", false, false, false},
	// tuning surfaces (existing ApplyPathAllowed) — none are xqueue/skill
	{"chargebook.md", false, false, true},
	{"spirits/scribe/cornerstone.md", false, false, true},
	{"spirits/critic/rituals/audit-drafts.md", false, false, true},
	{"spirits/scribe/rituals/tune.md", false, false, true},
	{"spirits/scribe/memories/long-term.md", false, false, false},
	// cross-checks: no path satisfies more than one predicate
	{"", false, false, false},
	{"skills/x-content", false, false, false},
}

func TestByteContract_ManifestPredicates(t *testing.T) {
	for _, v := range contractVectors {
		if got := AppendXQueuePathAllowed(v.path); got != v.xqueue {
			t.Errorf("AppendXQueuePathAllowed(%q) = %v, want %v", v.path, got, v.xqueue)
		}
		if got := UpdateVaultSkillPathAllowed(v.path); got != v.skill {
			t.Errorf("UpdateVaultSkillPathAllowed(%q) = %v, want %v", v.path, got, v.skill)
		}
		if got := ApplyPathAllowed(v.path); got != v.apply {
			t.Errorf("ApplyPathAllowed(%q) = %v, want %v", v.path, got, v.apply)
		}
	}
}
