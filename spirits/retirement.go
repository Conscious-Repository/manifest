package spirits

import (
	"fmt"
	"path/filepath"
)

// RetirementReason is the approved Phase 2 manual-launch policy, scoped to
// exact harness/spirit/ritual identities. Markdown remains schedule truth;
// reversing this policy requires a reviewed code change, not runtime state.
func RetirementReason(harness, spirit, ritual string) string {
	switch harness + "/" + spirit + "/" + ritual {
	case "excalibur/concierge/briefing", "excalibur/ea-coordinator/waiting-on", "excalibur/sage/skill-cast":
		return fmt.Sprintf("%s/%s is retired/paused; history preserved, no replacement. See Agents: #/agents/ritual/%s/%s", spirit, ritual, spirit, ritual)
	}
	return ""
}

// WithHarnessName binds the configured identity even in a renamed worktree.
func (s *Store) WithHarnessName(name string) *Store {
	s.harnessName = name
	return s
}

func (s *Store) RetirementReason(spirit, ritual string) string {
	name := s.harnessName
	if name == "" {
		name = filepath.Base(filepath.Clean(s.root))
	}
	return RetirementReason(name, spirit, ritual)
}
