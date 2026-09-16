package domainextract

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
	"manifest/mdfm"
)

const ContextPartitionUnavailable = "contextPartitionUnavailable"

// ContextRecord is private planning evidence. Paths and categories must not be
// included in the redacted diagnostic; even a filename can identify a person.
type ContextRecord struct {
	Path       string   `json:"path"`
	SHA256     string   `json:"sha256"`
	Bytes      int      `json:"bytes"`
	Domain     string   `json:"domain"`
	Category   string   `json:"category"`
	Categories []string `json:"categories"`
}

type NamespaceFact struct {
	Path    string   `json:"path"`
	Members []string `json:"members"`
	// Closed applies only to the copied snapshot and the reader's selection rule.
	// An unlisted Markdown path is absent; this is not a live CAS generation.
	Rule string `json:"rule"`
}

type ContextManifest struct {
	Ritual     string          `json:"ritual"`
	Records    []ContextRecord `json:"records"`
	Namespaces []NamespaceFact `json:"namespaces"`
	SHA256     string          `json:"sha256"`
	context    map[string]string
}

func byteHash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// openPlanFile refuses symlinks in every component, including ancestors of the
// fixture root. Descriptor-relative opens avoid check/open symlink races.
func openPlanFile(name string, directory bool) (*os.File, error) {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return nil, errors.New("invalidPlanningPath")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("planningReadUnavailable")
	}
	parts := strings.Split(strings.TrimPrefix(name, "/"), "/")
	for n, part := range parts {
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		if n < len(parts)-1 || directory {
			flags |= unix.O_DIRECTORY
		}
		next, e := unix.Openat(fd, part, flags, 0)
		unix.Close(fd)
		if e != nil {
			return nil, errors.New("planningReadUnavailable")
		}
		fd = next
	}
	f := os.NewFile(uintptr(fd), name)
	info, err := f.Stat()
	if err != nil || (!directory && !info.Mode().IsRegular()) {
		f.Close()
		return nil, errors.New("planningReadUnavailable")
	}
	return f, nil
}

// ReadContextManifest enumerates the complete ReadInput context without its
// early budget stop. It only reads explicitly supplied copied snapshot bytes.
// Missing required files/directories and any symlink refuse, never mean empty.
func ReadContextManifest(fixture, ritual string) (ContextManifest, error) {
	return readContextManifest(fixture, ritual, 0)
}

func readContextManifest(fixture, ritual string, maxRecordBytes int64) (ContextManifest, error) {
	m := ContextManifest{Ritual: ritual, context: map[string]string{}}
	if !validRitual(ritual) {
		return m, errors.New("unsupportedRitual")
	}
	root, err := openPlanFile(fixture, true)
	if err != nil {
		return m, err
	}
	root.Close()
	domain := "realestate"
	if ritual == "aion" {
		domain = "aion"
	}
	add := func(name, category string) error {
		if !fs.ValidPath(name) || strings.Contains(name, "\\") {
			return errors.New("invalidContextPath")
		}
		if _, ok := m.context[name]; ok {
			return errors.New("duplicateContextPath")
		}
		f, e := openPlanFile(filepath.Join(fixture, name), false)
		if e != nil {
			return e
		}
		var reader io.Reader = f
		if maxRecordBytes > 0 {
			reader = io.LimitReader(f, maxRecordBytes+1)
		}
		b, e := io.ReadAll(reader)
		f.Close()
		if e != nil {
			return errors.New("planningReadUnavailable")
		}
		if maxRecordBytes > 0 && int64(len(b)) > maxRecordBytes {
			return errors.New("contextRecordTooLarge")
		}
		fm, _ := mdfm.Split(string(b))
		cats := mdfm.List(fm["categories"])
		sort.Strings(cats)
		m.context[name] = string(b)
		m.Records = append(m.Records, ContextRecord{name, byteHash(b), len(b), domain, category, cats})
		return nil
	}
	for _, category := range []string{"backlog", "people", "heuristics"} {
		if category == "heuristics" && ritual != "aion" {
			continue
		}
		if err := add("system/"+domain+"/"+category+".md", category); err != nil {
			return ContextManifest{}, err
		}
	}
	if ritual != "aion" {
		for _, category := range []string{"properties", "contractors", "contracts"} {
			ns := NamespaceFact{Path: "system/realestate/" + category, Members: []string{}, Rule: "recursive-exact-md-closed-membership"}
			var walk func(string) error
			walk = func(dir string) error {
				f, e := openPlanFile(filepath.Join(fixture, dir), true)
				if e != nil {
					return e
				}
				entries, e := f.ReadDir(-1)
				f.Close()
				if e != nil {
					return errors.New("planningReadUnavailable")
				}
				sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
				for _, entry := range entries {
					name := dir + "/" + entry.Name()
					if entry.Type()&os.ModeSymlink != 0 {
						return errors.New("contextSymlink")
					}
					if entry.IsDir() {
						if e := walk(name); e != nil {
							return e
						}
						continue
					}
					if !strings.HasSuffix(name, ".md") {
						continue
					}
					if e := add(name, category); e != nil {
						return e
					}
					ns.Members = append(ns.Members, name)
				}
				return nil
			}
			if err := walk(ns.Path); err != nil {
				return ContextManifest{}, err
			}
			sort.Strings(ns.Members)
			m.Namespaces = append(m.Namespaces, ns)
		}
	}
	sort.Slice(m.Records, func(i, j int) bool { return m.Records[i].Path < m.Records[j].Path })
	b, _ := json.Marshal(m)
	m.SHA256 = byteHash(b)
	return m, nil
}

type ContextPlan struct {
	InputState      string `json:"inputState"`
	PartitionState  string `json:"partitionState"`
	SemanticState   string `json:"semanticState"`
	Refusal         string `json:"refusal,omitempty"`
	Budget          int    `json:"budget"`
	SerializedBytes int    `json:"serializedBytes"`
	ContextOnly     bool   `json:"contextOnly"`
	ManifestSHA256  string `json:"manifestSha256"`
	// Whole-domain dependency groups deliberately overapproximate references;
	// text matching cannot prove absence of semantic deduplication or closure.
	Dependencies          []string   `json:"dependencies"`
	MissingMergeSemantics []string   `json:"missingMergeSemantics"`
	Partitions            [][]string `json:"partitions"`
}

// PlanContext never splits records or proposes a reducer. A single complete
// input may fit; multiple model turns remain unavailable under today's contract.
func PlanContext(m ContextManifest, documents []Document, budget int) (ContextPlan, error) {
	p := ContextPlan{Budget: budget, SemanticState: "semantic-review-required", ContextOnly: len(documents) == 0, ManifestSHA256: m.SHA256, Partitions: [][]string{}, Dependencies: []string{"all-records:deduplication", "all-records:people-and-source-provenance"}}
	if budget <= 0 || budget > 56000 {
		return p, errors.New("invalidContextBudget")
	}
	// Verify exported evidence and private bytes together; caller edits, duplicate
	// paths, and deserialized manifests lacking original bytes cannot plan.
	seen := map[string]bool{}
	for _, r := range m.Records {
		b, ok := m.context[r.Path]
		if !ok || seen[r.Path] || r.Bytes != len(b) || r.SHA256 != byteHash([]byte(b)) {
			return p, errors.New("contextManifestDrift")
		}
		seen[r.Path] = true
	}
	copy := m
	copy.SHA256 = ""
	raw, _ := json.Marshal(copy)
	if len(seen) != len(m.context) || m.SHA256 != byteHash(raw) || !validRitual(m.Ritual) {
		return p, errors.New("contextManifestDrift")
	}
	if m.Ritual != "ooda-email" {
		p.Dependencies = append(p.Dependencies, "all-records:explicit-closure-and-exact-title")
	}
	if m.Ritual == "aion" {
		p.Dependencies = append(p.Dependencies, "all-records:heuristic-new-or-reinforce")
	} else {
		p.Dependencies = append(p.Dependencies, "all-records:category-casefold-slug-ambiguity-and-work-node-identity", "all-records:existing-contract-and-target-absence")
	}
	i := Input{Ritual: m.Ritual, Documents: documents, Context: m.context}
	raw, _ = json.Marshal(i)
	p.SerializedBytes = len(raw)
	p.InputState = "context-within-budget"
	if len(raw) > budget {
		p.InputState = "context-too-large"
	}
	if len(raw) <= budget && len(documents) > 0 && i.Validate() == nil {
		p.PartitionState = "partition-planned"
		names := []string{}
		for _, r := range m.Records {
			names = append(names, r.Path)
		}
		p.Partitions = append(p.Partitions, names)
		return p, nil
	}
	p.PartitionState = "partition-unavailable"
	p.Refusal = ContextPartitionUnavailable
	if len(documents) == 0 {
		p.MissingMergeSemantics = append(p.MissingMergeSemantics, "exact-source-envelope-required")
	} else if len(raw) <= budget {
		return p, errors.New("invalidExtractionSources")
	}
	if len(raw) > budget {
		p.MissingMergeSemantics = append(p.MissingMergeSemantics,
			"global-candidate-identity-dedup-and-conflict-resolution",
			"complete-participant-and-zero-output-coverage",
			"global-closure-and-heuristic-reconciliation",
			"closed-category-namespace-reference-and-target-absence-validation",
			"source-artifact-identity-and-exact-evidence-binding",
			"partition-membership-reducer-version-and-whole-snapshot-binding",
			"deterministic-final-proposal-ids-and-uncertain-no-replay-recovery")
	}
	return p, nil
}

// RedactedContextReport omits private paths, frontmatter categories and content.
func RedactedContextReport(m ContextManifest, p ContextPlan) any {
	type record struct {
		PathSHA256 string `json:"pathSha256"`
		SHA256     string `json:"sha256"`
		Bytes      int    `json:"bytes"`
	}
	records := []record{}
	total := 0
	for _, r := range m.Records {
		records = append(records, record{byteHash([]byte(r.Path)), r.SHA256, r.Bytes})
		total += r.Bytes
	}
	// Partition membership contains private filenames; only expose counts.
	counts := []int{}
	for _, partition := range p.Partitions {
		counts = append(counts, len(partition))
	}
	p.Partitions = nil
	return struct {
		Version               int         `json:"version"`
		Plan                  ContextPlan `json:"plan"`
		Records               any         `json:"records"`
		RecordCount           int         `json:"recordCount"`
		RawBytes              int         `json:"rawBytes"`
		NamespaceCount        int         `json:"namespaceCount"`
		PartitionRecordCounts []int       `json:"partitionRecordCounts"`
	}{1, p, records, len(records), total, len(m.Namespaces), counts}
}

// PlanningPaths validates explicit isolation boundaries without discovering or
// opening a vault/config. The operator supplies the real vault boundary and a
// copied fixture; symlinked fixture/output ancestors are independently refused.
func PlanningPaths(fixture, excludedVault, output string) error {
	within := func(a, b string) bool { return a == b || strings.HasPrefix(a, b+string(os.PathSeparator)) }
	for _, p := range []string{fixture, excludedVault, output} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" {
			return errors.New("invalidPlanningPath")
		}
	}
	if within(fixture, excludedVault) || within(excludedVault, fixture) || within(output, excludedVault) || within(output, fixture) {
		return errors.New("planningBoundaryOverlap")
	}
	return nil
}

// WriteContextReport creates a new private report, never overwrites a file.
func WriteContextReport(output string, report any) error {
	parent, err := openPlanFile(path.Dir(output), true)
	if err != nil {
		return err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), path.Base(output), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return errors.New("reportCreateUnavailable")
	}
	f := os.NewFile(uintptr(fd), output)
	err = json.NewEncoder(f).Encode(report)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return errors.New("reportWriteUnavailable")
	}
	if ce != nil {
		return errors.New("reportWriteUnavailable")
	}
	return parent.Sync()
}
