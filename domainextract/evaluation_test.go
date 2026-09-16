package domainextract

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func evaluationFixture(t *testing.T, candidates bool) EvaluationFixture {
	t.Helper()
	s := reducerFixture(t, "aion")
	run := EvaluationRun{Raw: "---\nrun: private-run\nspirit: extractor\nritual: aion\noutcome: completed\nstarted: 2026-09-16T00:00:00Z\nfinished: 2026-09-16T00:01:00Z\n---\nprivate run body", SnapshotSHA256: reducerDigest(s.Binding), ProposalIDs: []string{}, Proposals: []EvaluationProposal{}, ZeroOutput: !candidates}
	run.SHA256 = byteHash([]byte(run.Raw))
	if candidates {
		addReducerCandidate(t, &s, 0, "private title", "private evidence quote")
		c := s.Partitions[0].Candidates[0].Candidate
		ctx, _ := reducerManifest(s)
		raw, _ := json.Marshal(Response{Candidates: []Candidate{c}, Summary: "private"})
		ps, err := validateReplyEvidence(Input{Ritual: "aion", Documents: s.Documents, Context: ctx}, string(raw))
		if err != nil {
			t.Fatal(err)
		}
		p := ps[0]
		p.ExtractionSnapshot = ""
		p.Status = "rejected"
		p.Created = "2026-09-16T00:00:00Z"
		h := sha1.Sum([]byte(strings.ToLower(p.Action + "|" + p.Body)))
		p.ID = hex.EncodeToString(h[:])[:12]
		run.ProposalIDs = []string{p.ID}
		run.Proposals = []EvaluationProposal{{Proposal: p, SHA256: reducerDigest(p), Candidate: c, Decision: "rejected"}}
	}
	in := EvaluationFixture{Version: 1, Successor: s, Legacy: []EvaluationRun{run}, Attestation: EvaluationAttestation{ExternallyPinned: true, CompleteHistory: true, CompleteParticipants: true, CompleteContext: true, ImmutableSources: true, SuccessorZeroOutput: !candidates, OwnerReview: "reviewed"}}
	in.Attestation.EvidenceSHA256 = EvaluationEvidenceDigest(in)
	return in
}
func TestEvaluationEqualityStillRefuses(t *testing.T) {
	for _, nonzero := range []bool{false, true} {
		in := evaluationFixture(t, nonzero)
		raw, _ := json.Marshal(in)
		r := DecodeEvaluation(raw)
		if r.EvidenceState != "consistent-copied-claims-only" || r.Comparison != "comparison-unrun" || r.State != "evaluationIncomplete" || r.Refusal != "semanticReviewRequired" {
			t.Fatal(r)
		}
		if nonzero && (r.LegacyCount != 1 || r.SuccessorCount != 1) {
			t.Fatal(r)
		}
		out, _ := json.Marshal(r)
		for _, s := range []string{"private", "system/", ".md", "rejected", "2026"} {
			if strings.Contains(string(out), s) {
				t.Fatalf("leaked %s", s)
			}
		}
	}
}
func TestEvaluationAdversarial(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*EvaluationFixture)
	}{
		{"source-drift", func(i *EvaluationFixture) { i.Successor.Documents[0].Text += "changed" }},
		{"context-drift", func(i *EvaluationFixture) { i.Successor.Records[0].Text += "changed" }},
		{"membership-gap", func(i *EvaluationFixture) { i.Legacy[0].Proposals = nil }},
		{"membership-substitution", func(i *EvaluationFixture) { i.Legacy[0].ProposalIDs[0] = "other" }},
		{"duplicate-run", func(i *EvaluationFixture) { i.Legacy = append(i.Legacy, i.Legacy[0]) }},
		{"duplicate-proposal", func(i *EvaluationFixture) {
			r := &i.Legacy[0]
			r.Proposals = append(r.Proposals, r.Proposals[0])
			r.ProposalIDs = append(r.ProposalIDs, r.ProposalIDs[0])
		}},
		{"run-drift", func(i *EvaluationFixture) { i.Legacy[0].Raw += "changed" }},
		{"proposal-drift", func(i *EvaluationFixture) { i.Legacy[0].Proposals[0].Proposal.Body += "changed" }},
		{"snapshot-drift", func(i *EvaluationFixture) { i.Legacy[0].SnapshotSHA256 = strings.Repeat("0", 64) }},
		{"decision-drift", func(i *EvaluationFixture) { i.Legacy[0].Proposals[0].Decision = "approved" }},
		{"payload-substitution", func(i *EvaluationFixture) { i.Legacy[0].Proposals[0].Candidate.Payload = json.RawMessage(`{}`) }},
		{"source-substitution", func(i *EvaluationFixture) { i.Legacy[0].Proposals[0].Candidate.Source = "other.md" }},
		{"candidate-collision", func(i *EvaluationFixture) {
			i.Successor.Partitions[1].Candidates = i.Successor.Partitions[0].Candidates
			sealReducer(&i.Successor)
		}},
		{"false-zero", func(i *EvaluationFixture) { i.Attestation.SuccessorZeroOutput = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := evaluationFixture(t, true)
			tc.mutate(&in)
			in.Attestation.EvidenceSHA256 = EvaluationEvidenceDigest(in)
			if r := ValidateEvaluation(in); r.EvidenceState != "invalid" {
				t.Fatal(r)
			}
		})
	}
	in := evaluationFixture(t, true)
	in.Attestation.EvidenceSHA256 = strings.Repeat("0", 64)
	if r := ValidateEvaluation(in); r.EvidenceState != "invalid" {
		t.Fatal(r)
	}
}
func TestEvaluationMissingAttestations(t *testing.T) {
	in := evaluationFixture(t, false)
	in.Legacy[0].ZeroOutput = false
	in.Attestation = EvaluationAttestation{OwnerReview: "pending"}
	in.Attestation.EvidenceSHA256 = EvaluationEvidenceDigest(in)
	r := ValidateEvaluation(in)
	reasons := strings.Join(r.Reasons, ",")
	for _, want := range []string{"zero-output-attestation-required", "owner-review-required", "participant-coverage-required", "historical-membership-review-required", "external-trust-required", "snapshot-attestation-required"} {
		if !strings.Contains(reasons, want) {
			t.Fatal(r)
		}
	}
}
func TestEvaluationReaderNoWrite(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "copy.json")
	raw, _ := json.Marshal(evaluationFixture(t, true))
	if err := os.WriteFile(name, raw, 0400); err != nil {
		t.Fatal(err)
	}
	if r := ReadEvaluationFixture(name, true); r.EvidenceState != "consistent-copied-claims-only" {
		t.Fatal(r)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(name, link); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.Symlink(dir, parent); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{link, filepath.Join(parent, "copy.json"), dir, "relative.json", dir + "/../copy.json"} {
		if r := ReadEvaluationFixture(p, true); r.EvidenceState != "invalid" {
			t.Fatal(r)
		}
	}
	if r := ReadEvaluationFixture(name, false); r.EvidenceState != "invalid" {
		t.Fatal(r)
	}
	after, _ := os.ReadFile(name)
	entries, _ := os.ReadDir(dir)
	if string(after) != string(raw) || len(entries) != 2 {
		t.Fatal("wrote fixture")
	}
	for _, raw := range []string{`null`, `{}`, `{"version":1,"Version":1}`, `{"private":"secret"}`, `{} {}`} {
		if r := DecodeEvaluation([]byte(raw)); r.EvidenceState != "invalid" {
			t.Fatal(r)
		}
	}
}

func TestEvaluationDifferentCandidatesStillUnresolved(t *testing.T) {
	in := evaluationFixture(t, true)
	in.Successor.Partitions[0].Candidates = []ReducerCandidate{}
	addReducerCandidate(t, &in.Successor, 0, "a different wording", "private evidence quote")
	in.Attestation.EvidenceSHA256 = EvaluationEvidenceDigest(in)
	r := ValidateEvaluation(in)
	if r.EvidenceState != "consistent-copied-claims-only" || r.Comparison != "comparison-unrun" || r.Refusal != "semanticReviewRequired" {
		t.Fatal(r)
	}
}
func TestEvaluationLegacyNormalizedCollision(t *testing.T) {
	in := evaluationFixture(t, true)
	e := in.Legacy[0].Proposals[0]
	e.Proposal.Action = "different native action"
	h := sha1.Sum([]byte(strings.ToLower(e.Proposal.Action + "|" + e.Proposal.Body)))
	e.Proposal.ID = hex.EncodeToString(h[:])[:12]
	e.SHA256 = reducerDigest(e.Proposal)
	in.Legacy[0].Proposals = append(in.Legacy[0].Proposals, e)
	in.Legacy[0].ProposalIDs = append(in.Legacy[0].ProposalIDs, e.Proposal.ID)
	in.Attestation.EvidenceSHA256 = EvaluationEvidenceDigest(in)
	r := ValidateEvaluation(in)
	if r.EvidenceState != "invalid" || r.Reasons[0] != "legacy-candidate-collision" {
		t.Fatal(r)
	}
}
