package domainextract

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"manifest/connectorhandoff"
)

// Handoff is owner-reviewed evidence, not a machine assertion of semantic
// parity. Its exact bytes are addressed by the shared fence's evidence hash.
// Historical uncertainty is retained separately and can never authorize replay.
type Handoff struct {
	Version       int    `json:"version"`
	Duty          string `json:"duty"`
	Revision      uint64 `json:"revision"`
	Replay        *bool  `json:"replay"`
	LegacyHistory string `json:"legacyHistorySha256"`
	EngineFence   string `json:"engineFenceSha256"`
	LiveSemantic  string `json:"liveSemanticSha256"`
}

var evidenceHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ReadHandoff(dataDir string, fence connectorhandoff.RecordFence) (Handoff, error) {
	var h Handoff
	if dataDir == "" || !validRitualName(fence.Duty) || fence.Owner != "manifest" || fence.Revision == 0 || !evidenceHash.MatchString(fence.Evidence) {
		return h, fmt.Errorf("successor ownership not established")
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "domain-extraction", "handoffs", fence.Duty, fence.Evidence+".json"))
	if err != nil {
		return h, fmt.Errorf("extractor handoff evidence unavailable")
	}
	if len(b) > 8192 || fmt.Sprintf("%x", sha256.Sum256(b)) != fence.Evidence || strict(b, &h) != nil || h.Version != 1 || h.Duty != fence.Duty || h.Revision != fence.Revision || h.Replay == nil || *h.Replay || !evidenceHash.MatchString(h.LegacyHistory) || !evidenceHash.MatchString(h.EngineFence) || !evidenceHash.MatchString(h.LiveSemantic) {
		return h, fmt.Errorf("extractor handoff requires versioned history, legacy dispatch proof and owner-reviewed live semantics; replay=false")
	}
	return h, nil
}
func validRitualName(duty string) bool {
	return duty == "extractor/aion" || duty == "extractor/real-estate" || duty == "extractor/ooda-email"
}

// Readiness is read-only and deliberately never reports final decommission.
// Enabled is routing intent; fence and handoff evidence are independent gates.
func Readiness(dataDir, harness, ritual string, enabled bool) (string, string) {
	if !validRitual(ritual) {
		return "blocked", "Unsupported extraction duty."
	}
	f, err := connectorhandoff.DutyFenceSnapshot(harness, "extractor/"+ritual)
	if err != nil {
		return "blocked", "Extractor ownership history unreadable; dispatch refused."
	}
	if f.Owner == connectorhandoff.Legacy && !enabled {
		return "legacy-retiring", "Legacy owner; successor disabled. Shared engine dispatch fencing, historical reconciliation and owner-reviewed live semantic evidence remain required; replay=false."
	}
	if f.Owner != "manifest" {
		return "blocked", "Successor configuration does not establish ownership; shared extractor fence has not transferred to Manifest."
	}
	if _, err = ReadHandoff(dataDir, f); err != nil {
		return "blocked", err.Error()
	}
	if !enabled {
		return "successor-disabled", "Manifest owns the duty with reviewed live semantic evidence, but successor dispatch is disabled; no fallback or replay."
	}
	return "successor-enabled", "Manifest ownership and reviewed live semantic evidence recorded; proposals still require approval. Final decommission is a separate gate."
}

func (s *Service) acquire(ritual string) (connectorhandoff.RecordFence, func(), error) {
	// Check read-only first: a mere enablement flag must not create legacy state.
	f, err := connectorhandoff.DutyFenceSnapshot(s.harness, "extractor/"+ritual)
	if err != nil {
		return f, nil, err
	}
	if _, err = ReadHandoff(filepath.Dir(s.dir), f); err != nil {
		return f, nil, err
	}
	f, release, err := connectorhandoff.AcquireDutyFence(s.harness, "extractor/"+ritual)
	if err != nil {
		return f, nil, err
	}
	if _, err = ReadHandoff(filepath.Dir(s.dir), f); err != nil {
		release()
		return f, nil, err
	}
	return f, release, nil
}
