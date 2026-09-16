package approvals

// ExtractionFeasibility is a versioned assessment of the implemented APIs, not
// a live storage probe or a grant. A complete snapshot cannot lift these holds.
// Keep receipt history immutable: older terminal receipts may omit this field.
type ExtractionFeasibility struct {
	Version  int                 `json:"version"`
	State    string              `json:"state"`
	Replay   bool                `json:"replay"`
	Blockers []ExtractionBlocker `json:"blockers"`
}

type ExtractionBlocker struct {
	Code     string `json:"code"`
	Boundary string `json:"boundary"`
	Required string `json:"required"`
}

const ExtractionCommitUnavailable = "commitUnavailable"

// CheckExtractionCommitBoundary is deterministic and performs no I/O. It
// describes this binary's boundary; it does not accept caller assertions that
// locks or hashes make the existing APIs transactional. Executable witnesses
// for the assessment are documented in docs/extractor-transactions.md.
func CheckExtractionCommitBoundary() ExtractionFeasibility {
	return ExtractionFeasibility{Version: 1, State: ExtractionCommitUnavailable, Blockers: []ExtractionBlocker{
		{"external-writers-uncoordinated", "vaultwriter.editMu and manifest-sync cycle mutex are process-local; external editors bypass both", "an enforced owner-controlled write service or filesystem coordination boundary covering editors, sync, app writers and readers throughout validation/publication"},
		{"dependency-manifest-incomplete", "V1 snapshots contain positive file hashes only; input reads and namespace enumeration are independent", "a review-bound consistent manifest including expected absence, namespace membership, external store identities/generations and selected content hashes"},
		{"write-set-not-prepared", "AION/RE applies read then write; contract apply selects paths and mutates multiple records incrementally", "render and capability-check every before/after image before any mutation under the coordination boundary"},
		{"stores-not-enlisted", "realestate FileStore publishes blob then files.json; vaultindex SQL transactions and artifact pool/registry locks are independent", "pin external store identities and revisions; enlist authoritative writes or derive indexes from one durable committed generation"},
		{"audit-not-transactional", "vaultwriter audits after mutation, reports failures out of band and does not fsync the audit record", "durable transaction-linked audit evidence with before/after hashes and explicit post-write uncertainty"},
		{"settlement-not-transactional", "Confirm applies before writing approved and removing pending; approval fences do not cover vault or proposal editors", "persist exact approval settlement intent and atomically reconcile it with write and audit evidence under one decision protocol"},
		{"recovery-refusal-only", "startup recovery quarantines unsupported/interrupted records; all-after hashes cannot establish audit or settlement", "a crash-tested durable commit protocol; any unproven landed write remains uncertain, with no blind rollback or replay"},
	}}
}
