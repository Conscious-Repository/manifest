package domainextract

import (
	"encoding/json"
	"io"
	"io/fs"
	"sort"
	"strings"
	"unicode/utf8"

	"manifest/aion"
	"manifest/mdfm"
)

const ReducerReviewRequired = "reducerReviewRequired"
const ReducerIdentity = "manifest-offline-mechanical"
const ReducerVersion = 1
const MaxReducerFixtureBytes = 16 << 20

// ReducerBinding is repeated in every result; no result from another snapshot,
// source envelope, namespace, ritual, reducer version or partition set can mix.
type ReducerBinding struct {
	Version          int    `json:"version"`
	Reducer          string `json:"reducer"`
	Ritual           string `json:"ritual"`
	ManifestSHA256   string `json:"manifestSha256"`
	DocumentsSHA256  string `json:"documentsSha256"`
	NamespacesSHA256 string `json:"namespacesSha256"`
	PartitionCount   int    `json:"partitionCount"`
}
type ReducerRecord struct {
	Path string `json:"path"`
	Text string `json:"text"`
}
type ReducerCandidate struct {
	ID        string    `json:"id"`
	Candidate Candidate `json:"candidate"`
}
type PartitionResult struct {
	Binding    ReducerBinding     `json:"binding"`
	Index      int                `json:"index"`
	Members    []string           `json:"members"`
	Candidates []ReducerCandidate `json:"candidates"`
	Summary    string             `json:"summary"`
}

// ReducerInput is private copied evidence, never a publication or execution job.
// Digests are SHA-256 of encoding/json encodings of typed values. Partition
// digests bind the entire result, including ordered IDs, payloads and membership.
type ReducerInput struct {
	Binding         ReducerBinding    `json:"binding"`
	Manifest        ContextManifest   `json:"manifest"`
	Records         []ReducerRecord   `json:"records"`
	Documents       []Document        `json:"documents"`
	DocumentSHA256  []string          `json:"documentSha256"`
	Partitions      []PartitionResult `json:"partitions"`
	PartitionSHA256 []string          `json:"partitionSha256"`
}

// ReducerReport contains only fixed labels, counts and digests. It intentionally
// has no proposal output: even an empty set needs semantic coverage review.
type ReducerReport struct {
	Version            int      `json:"version"`
	State              string   `json:"state"`
	MechanicalState    string   `json:"mechanicalState"`
	Refusal            string   `json:"refusal"`
	Reasons            []string `json:"reasons"`
	FixtureSHA256      string   `json:"fixtureSha256,omitempty"`
	ManifestSHA256     string   `json:"manifestSha256,omitempty"`
	CandidateSetSHA256 string   `json:"candidateSetSha256,omitempty"`
	RecordCount        int      `json:"recordCount"`
	PartitionCount     int      `json:"partitionCount"`
	CandidateCount     int      `json:"candidateCount"`
}

func reducerDigest(v any) string { b, _ := json.Marshal(v); return byteHash(b) }
func reducerRefusal(reason string) ReducerReport {
	return ReducerReport{Version: ReducerVersion, State: "reducer-review-required", MechanicalState: "reducer-invalid", Refusal: ReducerReviewRequired, Reasons: []string{reason}}
}

// DecodeReducer validates strict JSON and returns redacted metadata even on
// errors. Raw decoder, filesystem and payload error strings never escape.
func DecodeReducer(raw []byte) ReducerReport {
	var in ReducerInput
	if len(raw) > MaxReducerFixtureBytes || !utf8.Valid(raw) || strict(raw, &in) != nil {
		return reducerRefusal("malformed-fixture")
	}
	return ValidateReducer(in)
}

// ReadReducerFixture reads one explicitly attested copied JSON file. No config,
// vault discovery, directory enumeration, output file or operational I/O exists.
func ReadReducerFixture(name string, copied bool) ReducerReport {
	if !copied {
		return reducerRefusal("copied-fixture-required")
	}
	f, err := openPlanFile(name, false)
	if err != nil {
		return reducerRefusal("fixture-read-unavailable")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxReducerFixtureBytes+1))
	if err != nil {
		return reducerRefusal("fixture-read-unavailable")
	}
	return DecodeReducer(raw)
}

// reducerManifest reconstructs reader metadata from exact copied bytes. Closed
// namespaces assert completeness of the supplied copy, never a live inventory.
func reducerManifest(in ReducerInput) (map[string]string, bool) {
	m := ContextManifest{Ritual: in.Binding.Ritual}
	context := map[string]string{}
	domain := "realestate"
	if m.Ritual == "aion" {
		domain = "aion"
	}
	required := map[string]string{"system/" + domain + "/backlog.md": "backlog", "system/" + domain + "/people.md": "people"}
	if domain == "aion" {
		required["system/aion/heuristics.md"] = "heuristics"
	} else {
		for _, c := range []string{"properties", "contractors", "contracts"} {
			m.Namespaces = append(m.Namespaces, NamespaceFact{Path: "system/realestate/" + c, Members: []string{}, Rule: "recursive-exact-md-closed-membership"})
		}
	}
	last := ""
	for _, r := range in.Records {
		if !fs.ValidPath(r.Path) || strings.Contains(r.Path, "\\") || r.Path <= last {
			return nil, false
		}
		last = r.Path
		category := required[r.Path]
		if category == "" {
			for n := range m.Namespaces {
				ns := &m.Namespaces[n]
				if strings.HasPrefix(r.Path, ns.Path+"/") && strings.HasSuffix(r.Path, ".md") {
					category = strings.TrimPrefix(ns.Path, "system/realestate/")
					ns.Members = append(ns.Members, r.Path)
				}
			}
		}
		if category == "" {
			return nil, false
		}
		context[r.Path] = r.Text
		fm, _ := mdfm.Split(r.Text)
		cats := mdfm.List(fm["categories"])
		sort.Strings(cats)
		m.Records = append(m.Records, ContextRecord{r.Path, byteHash([]byte(r.Text)), len(r.Text), domain, category, cats})
	}
	for p := range required {
		if _, ok := context[p]; !ok {
			return nil, false
		}
	}
	m.SHA256 = reducerDigest(m)
	return context, reducerDigest(m) == reducerDigest(in.Manifest) && m.SHA256 == in.Binding.ManifestSHA256 && reducerDigest(m.Namespaces) == in.Binding.NamespacesSHA256
}

// ValidateReducer does only mechanical checks and never emits approvals. All
// semantic outcomes remain unresolved; sorting does not select a winner.
func ValidateReducer(in ReducerInput) ReducerReport {
	b := in.Binding
	if b.Version != ReducerVersion || b.Reducer != ReducerIdentity || !validRitual(b.Ritual) || b.PartitionCount < 1 || b.PartitionCount != len(in.Partitions) || len(in.PartitionSHA256) != len(in.Partitions) {
		return reducerRefusal("inconsistent-partition-metadata")
	}
	context, ok := reducerManifest(in)
	if !ok {
		return reducerRefusal("manifest-drift")
	}
	if len(in.Documents) == 0 || len(in.DocumentSHA256) != len(in.Documents) || reducerDigest(in.Documents) != b.DocumentsSHA256 {
		return reducerRefusal("source-envelope-mismatch")
	}
	for n, d := range in.Documents {
		if b.Ritual != "ooda-email" && (!fs.ValidPath(d.Name) || strings.ContainsAny(d.Name, "\\\n\r\x00") || !strings.HasSuffix(d.Name, ".md") || strings.Contains(d.Name, "..") || strings.HasPrefix(d.Name, "system/") || strings.HasPrefix(d.Name, "extrinsic/")) {
			return reducerRefusal("invalid-source-provenance")
		}
		if in.DocumentSHA256[n] != reducerDigest(d) {
			return reducerRefusal("source-envelope-mismatch")
		}
	}
	whole := Input{Ritual: b.Ritual, Documents: in.Documents, Context: context}
	seen := map[string]bool{}
	ids := map[string]bool{}
	targets := map[string]bool{}
	reasons := map[string]bool{"global-deduplication-required": true, "participant-and-zero-output-coverage-required": true}
	allIDs := []string{}
	for n, p := range in.Partitions {
		if p.Binding != b || p.Index != n || len(p.Members) == 0 || p.Candidates == nil {
			return reducerRefusal("inconsistent-partition-metadata")
		}
		if reducerDigest(p) != in.PartitionSHA256[n] {
			return reducerRefusal("partition-digest-drift")
		}
		subset := map[string]string{}
		last := ""
		for _, name := range p.Members {
			text, exists := context[name]
			if !exists || seen[name] || name <= last {
				return reducerRefusal("invalid-partition-membership")
			}
			seen[name] = true
			subset[name] = text
			last = name
		}
		if (Input{Ritual: b.Ritual, Documents: in.Documents, Context: subset}).Validate() != nil {
			return reducerRefusal("invalid-partition-input")
		}
		response := Response{Candidates: []Candidate{}, Summary: p.Summary}
		for _, c := range p.Candidates {
			response.Candidates = append(response.Candidates, c.Candidate)
		}
		raw, err := json.Marshal(response)
		if err != nil {
			return reducerRefusal("invalid-candidate-evidence")
		}
		proposals, err := validateReplyEvidence(whole, string(raw))
		if err != nil {
			return reducerRefusal("invalid-candidate-evidence")
		}
		last = ""
		for j, c := range p.Candidates {
			if c.ID != proposals[j].ID || c.ID <= last || ids[c.ID] {
				return reducerRefusal("candidate-identity-collision-or-order")
			}
			last = c.ID
			ids[c.ID] = true
			allIDs = append(allIDs, c.ID)
			var payload aion.ProposalPayload
			key := c.Candidate.ApplyPath
			switch c.Candidate.Type {
			case "re-contract":
				reasons["money-reference-and-target-absence-review-required"] = true
			default:
				// Already strictly validated above; normalize only for conflict detection.
				_ = json.Unmarshal(c.Candidate.Payload, &payload)
				key += "|" + payload.Kind + "|" + aion.NormalizeTitle(payload.Title)
				if strings.HasSuffix(c.Candidate.Type, "resolve") {
					reasons["closure-review-required"] = true
				}
				if payload.Kind == aion.KindHeuristic {
					reasons["heuristic-new-or-reinforce-review-required"] = true
				}
			}
			if targets[key] {
				reasons["candidate-target-conflict"] = true
			}
			targets[key] = true
		}
	}
	if len(seen) != len(context) {
		return reducerRefusal("omitted-partition-membership")
	}
	if b.Ritual != "aion" {
		reasons["property-work-node-and-target-absence-review-required"] = true
	}
	sort.Strings(allIDs)
	report := reducerRefusal("")
	report.MechanicalState = "reducer-valid-mechanical-only"
	report.Reasons = []string{}
	for reason := range reasons {
		report.Reasons = append(report.Reasons, reason)
	}
	sort.Strings(report.Reasons)
	report.FixtureSHA256 = reducerDigest(in)
	report.ManifestSHA256 = b.ManifestSHA256
	report.CandidateSetSHA256 = reducerDigest(allIDs)
	report.RecordCount = len(in.Records)
	report.PartitionCount = len(in.Partitions)
	report.CandidateCount = len(allIDs)
	return report
}
