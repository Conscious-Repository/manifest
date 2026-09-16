package domainextract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func planFixture(t *testing.T, ritual string) string {
	t.Helper()
	root := t.TempDir()
	domain := "realestate"
	if ritual == "aion" {
		domain = "aion"
	}
	for _, name := range []string{"backlog", "people", "heuristics"} {
		if name == "heuristics" && ritual != "aion" {
			continue
		}
		putPlan(t, root, "system/"+domain+"/"+name+".md", "private prose <>&\n")
	}
	if ritual != "aion" {
		for _, name := range []string{"properties", "contractors", "contracts"} {
			if err := os.MkdirAll(filepath.Join(root, "system/realestate", name), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}
func putPlan(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func manifestFor(t *testing.T, root, ritual string) ContextManifest {
	t.Helper()
	m, e := ReadContextManifest(root, ritual)
	if e != nil {
		t.Fatal(e)
	}
	return m
}

func TestContextPlanCompleteDeterministicAndRedacted(t *testing.T) {
	for _, ritual := range []string{"aion", "real-estate", "ooda-email"} {
		t.Run(ritual, func(t *testing.T) {
			root := planFixture(t, ritual)
			if ritual != "aion" {
				putPlan(t, root, "system/realestate/properties/z/private-name.md", "---\ncategories: [property]\n---\nprivate prose")
				putPlan(t, root, "system/realestate/properties/a.md", "private prose")
			}
			m := manifestFor(t, root, ritual)
			other := manifestFor(t, root, ritual)
			if !reflect.DeepEqual(m, other) {
				t.Fatal("nondeterministic")
			}
			for n := 1; n < len(m.Records); n++ {
				if m.Records[n-1].Path >= m.Records[n].Path {
					t.Fatal("not sorted")
				}
			}
			if ritual != "aion" && (len(m.Namespaces) != 3 || len(m.Namespaces[0].Members) != 2 || len(m.Namespaces[1].Members) != 0) {
				t.Fatal("incomplete namespace")
			}
			p, e := PlanContext(m, nil, 56000)
			if e != nil || !p.ContextOnly || p.Refusal != ContextPartitionUnavailable {
				t.Fatal(p, e)
			}
			b, _ := json.Marshal(RedactedContextReport(m, p))
			for _, s := range []string{"private prose", "private-name", "system/", "categories", "property\""} {
				if strings.Contains(string(b), s) {
					t.Fatalf("redaction leaked %s", s)
				}
			}
			// Repeated planning neither changes nor adds a fixture file.
			if !reflect.DeepEqual(m, manifestFor(t, root, ritual)) {
				t.Fatal("fixture changed")
			}
		})
	}
}
func TestContextPlanExactBudgetAndCompleteSinglePartition(t *testing.T) {
	root := planFixture(t, "aion")
	m := manifestFor(t, root, "aion")
	docs := []Document{{Name: "source.md", Text: "quote \" <>&\n"}}
	raw, _ := json.Marshal(Input{Ritual: "aion", Documents: docs, Context: m.context})
	for _, delta := range []int{-1, 0, 1} {
		p, e := PlanContext(m, docs, len(raw)+delta)
		if e != nil {
			t.Fatal(e)
		}
		if p.SerializedBytes != len(raw) {
			t.Fatal("wrong serialized byte count")
		}
		if delta < 0 {
			if p.InputState != "context-too-large" || p.Refusal != ContextPartitionUnavailable || len(p.Partitions) != 0 || len(p.MissingMergeSemantics) < 6 {
				t.Fatal(p)
			}
		} else {
			if p.PartitionState != "partition-planned" || len(p.Partitions) != 1 || len(p.Partitions[0]) != len(m.Records) || p.SemanticState != "semantic-review-required" {
				t.Fatal(p)
			}
		}
	}
	for _, budget := range []int{-1, 0, 56001} {
		if _, e := PlanContext(m, docs, budget); e == nil {
			t.Fatal("invalid budget accepted")
		}
	}
}
func TestContextManifestDriftAndDuplicate(t *testing.T) {
	root := planFixture(t, "aion")
	for _, mutate := range []func(*ContextManifest){
		func(m *ContextManifest) { m.Records = append(m.Records, m.Records[0]) },
		func(m *ContextManifest) { m.Records[0].Bytes++ },
		func(m *ContextManifest) { m.Records[0].SHA256 = strings.Repeat("0", 64) },
		func(m *ContextManifest) { m.Records[0].Path = "../escape" },
		func(m *ContextManifest) { delete(m.context, m.Records[0].Path) },
	} {
		m := manifestFor(t, root, "aion")
		mutate(&m)
		if _, e := PlanContext(m, nil, 56000); e == nil {
			t.Fatal("drift accepted")
		}
	}
	before := manifestFor(t, root, "aion")
	putPlan(t, root, "system/aion/backlog.md", "changed bytes")
	after := manifestFor(t, root, "aion")
	if before.SHA256 == after.SHA256 {
		t.Fatal("byte drift missed")
	}
	if e := os.Remove(filepath.Join(root, "system/aion/people.md")); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadContextManifest(root, "aion"); e == nil {
		t.Fatal("missing path accepted")
	}
}
func TestContextManifestRefusesSymlinksAndEscape(t *testing.T) {
	for _, name := range []string{"system/aion/people.md", "system/aion", "system"} {
		t.Run(name, func(t *testing.T) {
			root := planFixture(t, "aion")
			target := filepath.Join(root, name)
			if e := os.RemoveAll(target); e != nil {
				t.Fatal(e)
			}
			if e := os.Symlink(t.TempDir(), target); e != nil {
				t.Fatal(e)
			}
			if _, e := ReadContextManifest(root, "aion"); e == nil {
				t.Fatal("symlink accepted")
			}
		})
	}
	root := planFixture(t, "aion")
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(root, alias); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{alias, root + "/../escape", "relative"} {
		if _, e := ReadContextManifest(p, "aion"); e == nil {
			t.Fatal("unsafe root accepted")
		}
	}
	re := planFixture(t, "real-estate")
	if e := os.Symlink(root, filepath.Join(re, "system/realestate/properties/ignored.txt")); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadContextManifest(re, "real-estate"); e == nil {
		t.Fatal("ignored symlink accepted")
	}
}
func TestContextPlanCrossReferencesAndNamespaceAmbiguity(t *testing.T) {
	root := planFixture(t, "ooda-email")
	for _, name := range []string{"properties/site.md", "properties/nested/SITE.md", "contractors/builder.md", "contracts/existing.md"} {
		putPlan(t, root, "system/realestate/"+name, "---\ncategories: [property, contractor]\n---\nsite builder work-node existing contract\n"+strings.Repeat("x", 20000))
	}
	m := manifestFor(t, root, "ooda-email")
	p, e := PlanContext(m, nil, 56000)
	if e != nil || len(m.Records) != 6 || p.InputState != "context-too-large" || p.Refusal != ContextPartitionUnavailable || len(p.Partitions) != 0 {
		t.Fatal(p, e)
	}
	if !strings.Contains(strings.Join(p.Dependencies, " "), "category-casefold-slug-ambiguity") {
		t.Fatal("missing global reference dependency")
	}
	// Same slug in another namespace member remains evidence, never deduplicated.
	if len(m.Namespaces[0].Members) != 2 {
		t.Fatal("ambiguous slug lost")
	}
	putPlan(t, root, "system/realestate/contracts/new.md", "new")
	if m.SHA256 == manifestFor(t, root, "ooda-email").SHA256 {
		t.Fatal("namespace drift missed")
	}
	if e := os.RemoveAll(filepath.Join(root, "system/realestate/contracts")); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadContextManifest(root, "ooda-email"); e == nil {
		t.Fatal("missing namespace accepted")
	}
}
func TestPlanningBoundaryAndDurableReport(t *testing.T) {
	root := planFixture(t, "aion")
	out := filepath.Join(t.TempDir(), "report.json")
	excluded := filepath.Join(t.TempDir(), "vault-does-not-exist")
	if e := PlanningPaths(root, excluded, out); e != nil {
		t.Fatal(e)
	}
	for _, paths := range [][3]string{{root, root, out}, {root, excluded, root + "/report"}, {root, excluded, excluded + "/report"}, {"", excluded, out}, {root, filepath.Dir(root), out}} {
		if PlanningPaths(paths[0], paths[1], paths[2]) == nil {
			t.Fatal("boundary accepted")
		}
	}
	m := manifestFor(t, root, "aion")
	p, _ := PlanContext(m, nil, 56000)
	report := RedactedContextReport(m, p)
	if e := WriteContextReport(out, report); e != nil {
		t.Fatal(e)
	}
	info, _ := os.Stat(out)
	if info.Mode().Perm() != 0600 {
		t.Fatal("public report")
	}
	before, _ := os.ReadFile(out)
	if e := WriteContextReport(out, report); e == nil {
		t.Fatal("overwrite accepted")
	}
	after, _ := os.ReadFile(out)
	if string(before) != string(after) {
		t.Fatal("report mutated")
	}
	if _, e := os.Stat(excluded); !os.IsNotExist(e) {
		t.Fatal("excluded vault touched")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(filepath.Dir(out), alias); e != nil {
		t.Fatal(e)
	}
	if e := WriteContextReport(filepath.Join(alias, "new.json"), report); e == nil {
		t.Fatal("symlink output accepted")
	}
}
