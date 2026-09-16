package spirits

import (
	"fmt"
	"manifest/excaliburretire"
	"path/filepath"
)

// RetirementReason is the approved retirement/paused manual-launch policy, scoped to
// exact harness/spirit/ritual identities. Markdown remains schedule truth;
// reversing this policy requires a reviewed code change, not runtime state.
func RetirementReason(harness, spirit, ritual string) string {
	if harness == "excalibur" && spirit == "extractor" && pausedExtractor(ritual) {
		return "Extractor capability paused/unavailable; legacy history and queues preserved. No successor parity or migration claimed; no legacy revival or replay."
	}
	switch harness + "/" + spirit + "/" + ritual {
	case "excalibur/warden/audit", "excalibur/concierge/briefing", "excalibur/ea-coordinator/waiting-on", "excalibur/sage/skill-cast", "excalibur/extractor/re-intake":
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
	if name == "excalibur" && s.engineUnavailable() {
		return "Excalibur engine retired/unavailable; history is read-only. Extractor capabilities remain paused, not migrated."
	}
	return RetirementReason(name, spirit, ritual)
}

func pausedExtractor(ritual string) bool {
	return ritual == "aion" || ritual == "ooda-email" || ritual == "real-estate"
}

func (s *Store) EngineRetired() bool {
	name := s.harnessName
	if name == "" {
		name = filepath.Base(filepath.Clean(s.root))
	}
	return name == "excalibur" && excaliburretire.Retired(s.migrationDataDir)
}

func (s *Store) engineUnavailable() bool {
	name := s.harnessName
	if name == "" {
		name = filepath.Base(filepath.Clean(s.root))
	}
	return name == "excalibur" && excaliburretire.Unavailable(s.migrationDataDir)
}
