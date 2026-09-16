package domainextract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func reducerFixture(t *testing.T, ritual string) ReducerInput {
	t.Helper()
	root := planFixture(t, ritual)
	m := manifestFor(t, root, ritual)
	docs := []Document{{Name: "private-source.md", Text: "private evidence quote"}}
	if ritual == "ooda-email" {
		docs[0].Name = "sha256:" + byteHash([]byte(docs[0].Text))
	}
	b := ReducerBinding{Version: ReducerVersion, Reducer: ReducerIdentity, Ritual: ritual, ManifestSHA256: m.SHA256, DocumentsSHA256: reducerDigest(docs), NamespacesSHA256: reducerDigest(m.Namespaces), PartitionCount: len(m.Records)}
	in := ReducerInput{Binding: b, Manifest: m, Documents: docs, DocumentSHA256: []string{reducerDigest(docs[0])}}
	for n, r := range m.Records {
		in.Records = append(in.Records, ReducerRecord{r.Path, m.context[r.Path]})
		in.Partitions = append(in.Partitions, PartitionResult{Binding: b, Index: n, Members: []string{r.Path}, Candidates: []ReducerCandidate{}, Summary: "private summary"})
	}
	sealReducer(&in)
	return in
}
func sealReducer(in *ReducerInput) {
	in.PartitionSHA256 = nil
	for _, p := range in.Partitions {
		in.PartitionSHA256 = append(in.PartitionSHA256, reducerDigest(p))
	}
}
func addReducerCandidate(t *testing.T, in *ReducerInput, partition int, title, quote string) {
	t.Helper()
	c := Candidate{Type: "aion-backlog", ApplyPath: "system/aion/backlog.md", Source: in.Documents[0].Name, Payload: json.RawMessage(`{"kind":"task","title":"` + title + `","quote":"` + quote + `","sources":["` + in.Documents[0].Name + `"]}`)}
	context, _ := reducerManifest(*in)
	raw, _ := json.Marshal(Response{Candidates: []Candidate{c}, Summary: "private"})
	ps, err := validateReplyEvidence(Input{Ritual: in.Binding.Ritual, Documents: in.Documents, Context: context}, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	in.Partitions[partition].Candidates = append(in.Partitions[partition].Candidates, ReducerCandidate{ps[0].ID, c})
	sort.Slice(in.Partitions[partition].Candidates, func(i, j int) bool {
		return in.Partitions[partition].Candidates[i].ID < in.Partitions[partition].Candidates[j].ID
	})
	sealReducer(in)
}
func TestReducerValidRedactedStable(t *testing.T) {
	for _, ritual := range []string{"aion", "real-estate", "ooda-email"} {
		t.Run(ritual, func(t *testing.T) {
			in := reducerFixture(t, ritual)
			if ritual == "aion" {
				addReducerCandidate(t, &in, 0, "private title", "private evidence quote")
			}
			raw, _ := json.Marshal(in)
			report := DecodeReducer(raw)
			if report.MechanicalState != "reducer-valid-mechanical-only" || report.Refusal != ReducerReviewRequired || report.State != "reducer-review-required" {
				t.Fatal(report)
			}
			out, _ := json.Marshal(report)
			for _, s := range []string{"private", "system/", ".md", "quote", "summary"} {
				if strings.Contains(string(out), s) {
					t.Fatalf("leaked %s: %s", s, out)
				}
			}
			for n := 0; n < 10; n++ {
				if !reflect.DeepEqual(report, DecodeReducer(raw)) {
					t.Fatal("unstable")
				}
			}
		})
	}
}
func TestReducerAdversarial(t *testing.T) {
	cases := []struct {
		name, reason string
		mutate       func(*ReducerInput)
	}{
		{"bytes", "manifest-drift", func(i *ReducerInput) { i.Records[0].Text += "changed" }},
		{"hash", "manifest-drift", func(i *ReducerInput) { i.Manifest.Records[0].SHA256 = strings.Repeat("0", 64) }},
		{"namespace", "manifest-drift", func(i *ReducerInput) { i.Manifest.Namespaces = []NamespaceFact{{Path: "unknown"}} }},
		{"unknown-record", "manifest-drift", func(i *ReducerInput) { i.Records[0].Path = "unknown.md" }},
		{"duplicate-record", "manifest-drift", func(i *ReducerInput) { i.Records[1] = i.Records[0] }},
		{"omitted-membership", "omitted-partition-membership", func(i *ReducerInput) {
			i.Partitions = i.Partitions[:2]
			i.Binding.PartitionCount = 2
			for n := range i.Partitions {
				i.Partitions[n].Binding = i.Binding
			}
			sealReducer(i)
		}},
		{"duplicate-membership", "invalid-partition-membership", func(i *ReducerInput) { i.Partitions[1].Members = i.Partitions[0].Members; sealReducer(i) }},
		{"unknown-membership", "invalid-partition-membership", func(i *ReducerInput) { i.Partitions[0].Members = []string{"unknown.md"}; sealReducer(i) }},
		{"digest", "partition-digest-drift", func(i *ReducerInput) { i.Partitions[0].Summary += "drift" }},
		{"binding", "inconsistent-partition-metadata", func(i *ReducerInput) { i.Partitions[0].Binding.Ritual = "real-estate"; sealReducer(i) }},
		{"count", "inconsistent-partition-metadata", func(i *ReducerInput) { i.Binding.PartitionCount++ }},
		{"version", "inconsistent-partition-metadata", func(i *ReducerInput) { i.Binding.Version++ }},
		{"source-envelope", "source-envelope-mismatch", func(i *ReducerInput) { i.Documents[0].Text += "drift" }},
		{"source-digest", "source-envelope-mismatch", func(i *ReducerInput) { i.DocumentSHA256[0] = "bad" }},
		{"candidate-id", "candidate-identity-collision-or-order", func(i *ReducerInput) { i.Partitions[0].Candidates[0].ID = "bad"; sealReducer(i) }},
		{"candidate-collision", "candidate-identity-collision-or-order", func(i *ReducerInput) { i.Partitions[1].Candidates = i.Partitions[0].Candidates; sealReducer(i) }},
		{"source-mismatch", "invalid-candidate-evidence", func(i *ReducerInput) { i.Partitions[0].Candidates[0].Candidate.Source = "other.md"; sealReducer(i) }},
		{"quote-mismatch", "invalid-candidate-evidence", func(i *ReducerInput) {
			i.Partitions[0].Candidates[0].Candidate.Payload = json.RawMessage(`{"kind":"task","title":"title","quote":"absent","sources":["private-source.md"]}`)
			sealReducer(i)
		}},
		{"malformed-payload", "invalid-candidate-evidence", func(i *ReducerInput) {
			i.Partitions[0].Candidates[0].Candidate.Payload = json.RawMessage(`{"kind":"task","kind":"task"}`)
			sealReducer(i)
		}},
		{"null-candidates", "inconsistent-partition-metadata", func(i *ReducerInput) { i.Partitions[1].Candidates = nil; sealReducer(i) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := reducerFixture(t, "aion")
			addReducerCandidate(t, &in, 0, "private title", "private evidence quote")
			tc.mutate(&in)
			r := ValidateReducer(in)
			if r.MechanicalState != "reducer-invalid" || !reflect.DeepEqual(r.Reasons, []string{tc.reason}) {
				t.Fatal(r)
			}
		})
	}
}
func TestReducerOrderingAndConflict(t *testing.T) {
	in := reducerFixture(t, "aion")
	addReducerCandidate(t, &in, 0, "same title", "private evidence quote")
	addReducerCandidate(t, &in, 0, "same title", "evidence quote")
	r := ValidateReducer(in)
	if r.MechanicalState != "reducer-valid-mechanical-only" || !strings.Contains(strings.Join(r.Reasons, ","), "candidate-target-conflict") || r.Refusal != ReducerReviewRequired || r.CandidateCount != 2 {
		t.Fatal(r)
	}
	in.Partitions[0].Candidates[0], in.Partitions[0].Candidates[1] = in.Partitions[0].Candidates[1], in.Partitions[0].Candidates[0]
	sealReducer(&in)
	if r := ValidateReducer(in); r.MechanicalState != "reducer-invalid" {
		t.Fatal(r)
	}
}
func TestReducerStrictJSONAndNoWrite(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"binding":{},"Binding":{}}`, `{"private":"secret"}`, `{} {}`, `{"version":1}`} {
		if r := DecodeReducer([]byte(raw)); r.MechanicalState != "reducer-invalid" {
			t.Fatal(r)
		}
	}
	in := reducerFixture(t, "aion")
	raw, _ := json.Marshal(in)
	dir := t.TempDir()
	name := filepath.Join(dir, "copied.json")
	if err := os.WriteFile(name, raw, 0400); err != nil {
		t.Fatal(err)
	}
	if r := ReadReducerFixture(name, false); r.MechanicalState != "reducer-invalid" {
		t.Fatal(r)
	}
	if r := ReadReducerFixture(name, true); r.MechanicalState != "reducer-valid-mechanical-only" {
		t.Fatal(r)
	}
	after, _ := os.ReadFile(name)
	entries, _ := os.ReadDir(dir)
	if string(after) != string(raw) || len(entries) != 1 {
		t.Fatal("fixture changed")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if r := ReadReducerFixture(filepath.Join(link, "copied.json"), true); r.MechanicalState != "reducer-invalid" {
		t.Fatal(r)
	}
}
func TestReducerOversizedWholeContext(t *testing.T) {
	root := planFixture(t, "aion")
	for _, name := range []string{"backlog", "people", "heuristics"} {
		putPlan(t, root, "system/aion/"+name+".md", strings.Repeat("x", 30000))
	}
	m := manifestFor(t, root, "aion")
	in := reducerFixture(t, "aion")
	in.Manifest = m
	in.Binding.ManifestSHA256 = m.SHA256
	for n, r := range m.Records {
		in.Records[n] = ReducerRecord{r.Path, m.context[r.Path]}
		in.Partitions[n].Binding = in.Binding
	}
	sealReducer(&in)
	if r := ValidateReducer(in); r.MechanicalState != "reducer-valid-mechanical-only" {
		t.Fatal(r)
	}
	if (Input{Ritual: "aion", Documents: in.Documents, Context: m.context}).Validate() == nil {
		t.Fatal("production limit lifted")
	}
}

func TestReducerSourceProvenance(t *testing.T) {
	for _, name := range []string{"../escape.md", "/absolute.md", "system/aion/backlog.md", "extrinsic/private.md", "source\nforge.md"} {
		in := reducerFixture(t, "aion")
		in.Documents[0].Name = name
		in.Binding.DocumentsSHA256 = reducerDigest(in.Documents)
		in.DocumentSHA256[0] = reducerDigest(in.Documents[0])
		for n := range in.Partitions {
			in.Partitions[n].Binding = in.Binding
		}
		sealReducer(&in)
		if r := ValidateReducer(in); !reflect.DeepEqual(r.Reasons, []string{"invalid-source-provenance"}) {
			t.Fatal(r)
		}
	}
	in := reducerFixture(t, "ooda-email")
	in.Documents[0].Name = "sha256:" + strings.Repeat("0", 64)
	in.Binding.DocumentsSHA256 = reducerDigest(in.Documents)
	in.DocumentSHA256[0] = reducerDigest(in.Documents[0])
	for n := range in.Partitions {
		in.Partitions[n].Binding = in.Binding
	}
	sealReducer(&in)
	if r := ValidateReducer(in); r.MechanicalState != "reducer-invalid" {
		t.Fatal(r)
	}
}

func TestReducerSemanticCasesRemainUnresolved(t *testing.T) {
	for _, tc := range []struct{ typ, path, payload, reason string }{
		{"aion-resolve", "system/aion/backlog.md", `{"kind":"task","title":"private prose","status":"done","quote":"private evidence quote","sources":["private-source.md"]}`, "closure-review-required"},
		{"aion-heuristic", "system/aion/heuristics.md", `{"kind":"heuristic","title":"principle","heuristic":{"mode":"new"},"quote":"private evidence quote","sources":["private-source.md"]}`, "heuristic-new-or-reinforce-review-required"},
	} {
		in := reducerFixture(t, "aion")
		c := Candidate{Type: tc.typ, ApplyPath: tc.path, Source: in.Documents[0].Name, Payload: json.RawMessage(tc.payload)}
		context, _ := reducerManifest(in)
		raw, _ := json.Marshal(Response{Candidates: []Candidate{c}, Summary: "private"})
		ps, err := validateReplyEvidence(Input{Ritual: "aion", Documents: in.Documents, Context: context}, string(raw))
		if err != nil {
			t.Fatal(err)
		}
		in.Partitions[0].Candidates = []ReducerCandidate{{ID: ps[0].ID, Candidate: c}}
		sealReducer(&in)
		r := ValidateReducer(in)
		if r.MechanicalState != "reducer-valid-mechanical-only" || r.Refusal != ReducerReviewRequired || !strings.Contains(strings.Join(r.Reasons, ","), tc.reason) {
			t.Fatal(r)
		}
	}
}
