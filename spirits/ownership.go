package spirits

import (
	"errors"
	"os"
	"path/filepath"

	"manifest/connectorhandoff"
	"manifest/domainextract"
	"manifest/personalemail"
)

// Ownership describes routing intent and the legacy file independently. Neither
// an enablement flag nor a disabled legacy schedule proves a completed handoff.
func (s *Store) projectOwnership(r *RitualRow) {
	r.Harness = s.harnessName
	if r.Harness == "" {
		r.Harness = filepath.Base(filepath.Clean(s.root))
	}
	r.ConfiguredOwner = s.dutyOwners[r.Spirit+"/"+r.Ritual]
	r.EngineRetired = s.EngineRetired()
	r.LegacyActionable = r.Valid && !r.Retired && r.ConfiguredOwner == ""
	if s.emailDuty(r.Spirit, r.Ritual) {
		s.projectEmailOwnership(r)
		return
	}
	if source := s.connectorSource(r.Spirit, r.Ritual); source != "" && s.migrationDataDir != "" {
		s.projectTranscriptOwnership(r, source)
		return
	}
	if r.Harness == "excalibur" && r.Spirit == "extractor" && (r.Ritual == "aion" || r.Ritual == "real-estate" || r.Ritual == "ooda-email") {
		r.CapabilityPaused = true
		r.LegacyActionable = false
		r.MigrationState, r.MigrationDetail = domainextract.Readiness(s.migrationDataDir, s.root, r.Ritual, r.ConfiguredOwner != "")
		r.LegacyActionable = r.LegacyActionable && s.connectorDispatchGuard(r.Spirit, r.Ritual) == nil
		if f, err := connectorhandoff.DutyFenceSnapshot(s.root, r.Spirit+"/"+r.Ritual); err == nil {
			r.ConfiguredOwner = f.Owner
			r.FenceProtected = f.Revision > 0 && (f.Owner == "blocked" || f.Owner == "manifest")
			r.SuccessorEnabled = f.Owner == "manifest" && r.MigrationState == "successor-enabled"
			r.CapabilityPaused = !r.SuccessorEnabled
			if r.SuccessorEnabled {
				r.Retired = false
				r.RetirementReason = ""
				// Board enablement follows the validated successor, not the legacy schedule.
				r.Enabled = true
				r.PausedReason = ""
				r.Provider, r.Model, r.Toolset, r.MaxSteps = "lab-sparks", "deepseek-v4.1-flash", "none / no_mcp", "1"
			}
			if f.Owner == "blocked" && r.FenceProtected && !r.LegacyEnabled {
				r.MigrationState = "paused"
				r.MigrationDetail = "Capability paused/unavailable; legacy state and queues preserved without replay. No successor migration or parity claimed. Semantic and application safety gaps remain."
			} else if f.Owner == "excalibur" {
				r.MigrationState = "pause-pending"
				r.MigrationDetail = "Manifest legacy dispatch is unavailable by policy. Explicit legacy schedule pause and blocked ownership fence still required before engine retirement. History and queues preserved; no migration claimed."
			}
		}
		return
	}
	if r.Retired {
		r.MigrationState, r.MigrationDetail = "retired", r.RetirementReason
		if r.LegacyEnabled {
			r.MigrationState = "retirement-conflict"
			r.MigrationDetail += "; legacy file still enabled: engine schedule must be reconciled"
		}
		return
	}
	if r.ConfiguredOwner != "" {
		r.MigrationState = "handoff-unverified"
		r.MigrationDetail = "Configured for " + r.ConfiguredOwner + "; legacy manual dispatch blocked. State transfer and successor execution require evidence."
		if r.LegacyEnabled {
			r.MigrationState = "ownership-conflict"
			r.MigrationDetail += " Legacy file remains enabled; possible dual dispatch."
		}
		return
	}
	r.ConfiguredOwner = r.Harness
	if r.Harness != "excalibur" {
		return
	}
	r.MigrationState = "legacy-retiring"
	switch r.Spirit + "/" + r.Ritual {
	case "ea-coordinator/email-sync":
		r.MigrationDetail = "Legacy owner; personal email successor and per-thread state transfer are not implemented."
	case "ea-coordinator/granola-sync", "ea-coordinator/pocket-sync":
		r.MigrationDetail = "Legacy owner; Manifest transcript successor is not enabled. Pause, state/approval reconciliation and handoff evidence remain required."
	case "extractor/aion", "extractor/real-estate", "extractor/ooda-email":
		r.MigrationDetail = "Legacy owner; replacement is not enabled. Verified execution and extraction quality comparison remain required; existing model and authority retained."
	default:
		r.MigrationDetail = "Legacy owner; no successor ownership established."
	}
}

// Email has a dedicated activation receipt; connector-handoff/email.json is
// not written by its worker and cannot establish successor ownership.
func (s *Store) projectEmailOwnership(r *RitualRow) {
	r.ConfiguredOwner = connectorhandoff.Legacy
	r.LegacyActionable = false
	r.MigrationState = "blocked"
	r.MigrationDetail = "Email ownership uncertain: matching dispatch fence and worker activation required."
	f, enabled, err := personalemail.OwnershipSnapshot(s.migrationDataDir, s.root)
	r.FenceProtected = f.Revision > 0 && f.Owner != connectorhandoff.Legacy
	if err != nil {
		r.MigrationDetail += " " + err.Error() + "."
		return
	}
	// A surviving incompatible migration label is an explicit contradiction.
	record, err := connectorhandoff.Read(s.migrationDataDir, "email")
	if err != nil && !errors.Is(err, os.ErrNotExist) || err == nil && record.Owner != "manifest" {
		r.MigrationDetail += " Connector handoff label contradicts activation or is unreadable."
		return
	}
	r.ConfiguredOwner = "manifest"
	r.SuccessorEnabled = enabled
	r.MigrationState = "fence-protected"
	if enabled {
		r.MigrationState = "successor-enabled"
	}
	r.MigrationDetail = "Hash-bound personal-email activation matches dispatch fence; legacy dispatch fenced."
	if !r.LegacyEnabled {
		r.MigrationDetail += " Legacy schedule disabled."
	} else {
		r.MigrationDetail += " Legacy schedule still enabled; fence protects dispatch."
	}
	r.MigrationDetail += " Worker enablement is configuration, not liveness, semantic parity or final decommission."
}
