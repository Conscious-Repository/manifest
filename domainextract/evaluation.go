package domainextract

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"manifest/aion"
	"manifest/approvals"
	"manifest/mdfm"
)

// EvaluationFixture is private, owner-supplied evidence, never an execution job.
// Version 1 deliberately refuses legacy adaptation: native history has no
// complete output membership or immutable source/context binding contract.
type EvaluationFixture struct {
	Version     int                   `json:"version"`
	Successor   ReducerInput          `json:"successor"`
	Legacy      []EvaluationRun       `json:"legacy"`
	Attestation EvaluationAttestation `json:"attestation"`
}

type EvaluationRun struct {
	// Raw is the exact copied native Markdown report, not a discovered path.
	Raw            string               `json:"raw"`
	SHA256         string               `json:"sha256"`
	SnapshotSHA256 string               `json:"snapshotSha256"`
	ProposalIDs    []string             `json:"proposalIds"`
	Proposals      []EvaluationProposal `json:"proposals"`
	ZeroOutput     bool                 `json:"zeroOutput"`
}

type EvaluationProposal struct {
	Proposal approvals.Proposal `json:"proposal"`
	SHA256   string             `json:"sha256"`
	// Candidate is an owner-supplied transcription, validated against the payload
	// fence, never trusted as a reviewed legacy semantic adapter.
	Candidate Candidate `json:"candidate"`
	Decision  string    `json:"decision"`
}

// These are claims supplied by the owner, not authenticated signatures. The
// owner must pin EvidenceSHA256 independently outside this mutable fixture.
type EvaluationAttestation struct {
	EvidenceSHA256       string `json:"evidenceSha256"`
	ExternallyPinned     bool   `json:"externallyPinned"`
	CompleteHistory      bool   `json:"completeHistory"`
	CompleteParticipants bool   `json:"completeParticipants"`
	CompleteContext      bool   `json:"completeContext"`
	ImmutableSources     bool   `json:"immutableSources"`
	SuccessorZeroOutput  bool   `json:"successorZeroOutput"`
	OwnerReview          string `json:"ownerReview"` // pending or reviewed
}

type EvaluationReport struct {
	Version        int      `json:"version"`
	State          string   `json:"state"`
	Refusal        string   `json:"refusal"`
	EvidenceState  string   `json:"evidenceState"`
	Comparison     string   `json:"comparison"`
	Reasons        []string `json:"reasons"`
	EvidenceSHA256 string   `json:"evidenceSha256,omitempty"`
	RunCount       int      `json:"runCount"`
	LegacyCount    int      `json:"legacyCount"`
	SuccessorCount int      `json:"successorCount"`
}

// EvaluationEvidenceDigest excludes attestations to avoid self-referential
// hashing; the externally retained receipt must bind this digest AND claims.
func EvaluationEvidenceDigest(in EvaluationFixture) string {
	return reducerDigest(struct {
		Version   int             `json:"version"`
		Successor ReducerInput    `json:"successor"`
		Legacy    []EvaluationRun `json:"legacy"`
	}{in.Version, in.Successor, in.Legacy})
}
func evaluationRefusal(reason string) EvaluationReport {
	return EvaluationReport{Version: 1, State: "evaluationIncomplete", Refusal: "semanticReviewRequired", EvidenceState: "invalid", Comparison: "comparison-unrun", Reasons: []string{reason}}
}
func DecodeEvaluation(raw []byte) EvaluationReport {
	var in EvaluationFixture
	if len(raw) > MaxReducerFixtureBytes || !utf8.Valid(raw) || strict(raw, &in) != nil {
		return evaluationRefusal("malformed-fixture")
	}
	return ValidateEvaluation(in)
}
func ReadEvaluationFixture(name string, copied bool) EvaluationReport {
	if !copied {
		return evaluationRefusal("copied-fixture-required")
	}
	f, err := openPlanFile(name, false)
	if err != nil {
		return evaluationRefusal("fixture-read-unavailable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxReducerFixtureBytes+1))
	if err != nil {
		return evaluationRefusal("fixture-read-unavailable")
	}
	return DecodeEvaluation(raw)
}

// ValidateEvaluation does not infer native run membership from counts, writes,
// prose, dates or proposal IDs. It validates explicit copied claims and refuses
// semantic comparison even when all claims and payload checks are consistent.
func ValidateEvaluation(in EvaluationFixture) EvaluationReport {
	if in.Version != 1 || len(in.Legacy) == 0 {
		return evaluationRefusal("missing-history-or-version")
	}
	sr := ValidateReducer(in.Successor)
	if sr.MechanicalState != "reducer-valid-mechanical-only" {
		return evaluationRefusal("invalid-successor-evidence")
	}
	if EvaluationEvidenceDigest(in) != in.Attestation.EvidenceSHA256 {
		return evaluationRefusal("external-digest-mismatch")
	}
	context, _ := reducerManifest(in.Successor)
	input := Input{Ritual: in.Successor.Binding.Ritual, Documents: in.Successor.Documents, Context: context}
	seenRuns := map[string]bool{}
	seenProposals := map[string]bool{}
	seenCandidates := map[string]bool{}
	count := 0
	missingZero := false
	for _, run := range in.Legacy {
		if run.Raw == "" || byteHash([]byte(run.Raw)) != run.SHA256 || run.SnapshotSHA256 != reducerDigest(in.Successor.Binding) {
			return evaluationRefusal("legacy-snapshot-or-digest-mismatch")
		}
		fm, _ := mdfm.Split(run.Raw)
		if fm["run"] == "" || seenRuns[fm["run"]] || fm["spirit"] != "extractor" || fm["ritual"] != input.Ritual || fm["outcome"] != "completed" || fm["started"] == "" || fm["finished"] == "" {
			return evaluationRefusal("unsupported-or-duplicate-legacy-run")
		}
		start, startErr := time.Parse(time.RFC3339, fm["started"])
		finish, finishErr := time.Parse(time.RFC3339, fm["finished"])
		if startErr != nil || finishErr != nil || finish.Before(start) {
			return evaluationRefusal("invalid-legacy-run-time")
		}
		seenRuns[fm["run"]] = true
		if run.ProposalIDs == nil || run.Proposals == nil || len(run.ProposalIDs) != len(run.Proposals) {
			return evaluationRefusal("legacy-membership-gap")
		}
		if len(run.Proposals) == 0 && !run.ZeroOutput {
			missingZero = true
		}
		if len(run.Proposals) > 0 && run.ZeroOutput {
			return evaluationRefusal("inconsistent-zero-output")
		}
		for n, e := range run.Proposals {
			p := e.Proposal
			// Only an exact, conservative subset of the native schema is supported.
			// Trimmed or edited historical bodies whose IDs no longer verify refuse.
			h := sha1.Sum([]byte(strings.ToLower(p.Action + "|" + p.Body)))
			if p.ID != hex.EncodeToString(h[:])[:12] || seenProposals[p.ID] || run.ProposalIDs[n] != p.ID || reducerDigest(p) != e.SHA256 {
				return evaluationRefusal("legacy-membership-or-proposal-drift")
			}
			seenProposals[p.ID] = true
			if p.Agent != "extractor" || p.Ritual != input.Ritual || p.Proposed != "" || p.ExtractionSnapshot != "" || p.Action == "" || p.Created == "" || p.Type != e.Candidate.Type || p.ApplyPath != e.Candidate.ApplyPath || p.Status != e.Decision || (e.Decision != "pending" && e.Decision != "approved" && e.Decision != "rejected") {
				return evaluationRefusal("unsupported-legacy-proposal")
			}
			if _, err := time.Parse(time.RFC3339, p.Created); err != nil {
				return evaluationRefusal("invalid-legacy-proposal-time")
			}
			fence := "aion"
			if strings.HasPrefix(p.Type, "re-") {
				fence = "re"
			}
			if p.Type == "re-contract" {
				fence = "re-contract"
			}
			block, ok := mdfm.ExtractFencedBlock(p.Body, fence)
			// Refuse ambiguous/multiple fences and noncanonical payload transcriptions.
			if !ok || strings.Count(p.Body, "````") != 2 {
				return evaluationRefusal("legacy-adapter-unavailable")
			}
			var a, b any = &aion.ProposalPayload{}, &aion.ProposalPayload{}
			if p.Type == "re-contract" {
				a, b = &approvals.ReContractPayload{}, &approvals.ReContractPayload{}
			}
			if strict([]byte(block), a) != nil || strict(e.Candidate.Payload, b) != nil || reducerDigest(a) != reducerDigest(b) {
				return evaluationRefusal("legacy-payload-mismatch")
			}
			// Validate the original block strictly too; decoding to any alone would lose
			// duplicate keys and silently normalize invalid historical payloads.
			c := e.Candidate
			c.Payload = json.RawMessage(block)
			raw, err := json.Marshal(Response{Candidates: []Candidate{c}, Summary: "copied-evidence"})
			if err != nil {
				return evaluationRefusal("invalid-legacy-candidate")
			}
			ps, err := validateReplyEvidence(input, string(raw))
			if err != nil {
				return evaluationRefusal("invalid-legacy-candidate")
			}
			if seenCandidates[ps[0].ID] {
				return evaluationRefusal("legacy-candidate-collision")
			}
			seenCandidates[ps[0].ID] = true
			count++
		}
	}
	r := evaluationRefusal("legacy-adapter-review-required")
	r.EvidenceState = "consistent-copied-claims-only"
	r.EvidenceSHA256 = in.Attestation.EvidenceSHA256
	r.RunCount = len(in.Legacy)
	r.LegacyCount = count
	r.SuccessorCount = sr.CandidateCount
	a := in.Attestation
	if !a.ExternallyPinned {
		r.Reasons = append(r.Reasons, "external-trust-required")
	}
	if !a.CompleteHistory {
		r.Reasons = append(r.Reasons, "historical-membership-review-required")
	}
	if !a.CompleteParticipants {
		r.Reasons = append(r.Reasons, "participant-coverage-required")
	}
	if !a.CompleteContext || !a.ImmutableSources {
		r.Reasons = append(r.Reasons, "snapshot-attestation-required")
	}
	if missingZero || (sr.CandidateCount == 0 && !a.SuccessorZeroOutput) {
		r.Reasons = append(r.Reasons, "zero-output-attestation-required")
	}
	if sr.CandidateCount > 0 && a.SuccessorZeroOutput {
		return evaluationRefusal("inconsistent-zero-output")
	}
	if a.OwnerReview != "reviewed" && a.OwnerReview != "pending" {
		return evaluationRefusal("owner-review-status-required")
	}
	if a.OwnerReview != "reviewed" {
		r.Reasons = append(r.Reasons, "owner-review-required")
	}
	r.Reasons = append(r.Reasons, sr.Reasons...)
	r.Reasons = append(r.Reasons, "cross-system-identity-and-target-comparison-unavailable", "exact-equality-is-not-semantic-proof")
	return r
}
