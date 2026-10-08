package construction

// Private recovery bundle (§9.2, P9). One problem's complete retained
// closure as a deterministic ZIP: the head, the membership ACL, the ledger
// projection state and creation intents (the store's own files), every
// member artifact object with its exact bytes (every document revision,
// receipt, input, source snapshot, stage result, context packet and derived
// export), plus caller-supplied extras (native conversations the problem
// points at). The manifest lists path, bytes and SHA-256 of every file,
// tool and schema versions, and completeness per category; what the domain
// cannot include (native conversations not supplied, linked property/task
// records) is listed as missing, never claimed. This is a private backup,
// distinct from the redacted detail package.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"manifest/artifacts"
)

// BundleFormat names the recovery bundle layout.
const BundleFormat = "construction-recovery-bundle/1"

// BundleFile is one manifest entry.
type BundleFile struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind"` // store | artifact-object | artifact-blob | native | readme
}

// BundleCategory says how complete one category of the closure is.
type BundleCategory struct {
	Name     string `json:"name"`
	State    string `json:"state"` // complete | partial | missing | not-applicable
	Count    int    `json:"count"`
	Note     string `json:"note,omitempty"`
	Excluded int    `json:"excluded,omitempty"`
}

// BundleManifest describes a recovery bundle.
type BundleManifest struct {
	Format        string           `json:"format"`
	SchemaVersion int              `json:"schemaVersion"`
	Compiler      string           `json:"compiler"`
	Convention    string           `json:"coordinateConvention"`
	RuleSet       string           `json:"ruleSet"`
	Subject       SubjectRef       `json:"subject"`
	ProblemID     string           `json:"problemId"`
	Generation    int64            `json:"generation"`
	HeadCommit    string           `json:"headCommit"`
	CreatedAt     string           `json:"createdAt"`
	Notice        string           `json:"notice"`
	Files         []BundleFile     `json:"files"`
	Categories    []BundleCategory `json:"categories"`
	Missing       []string         `json:"missing"`
	Complete      bool             `json:"complete"`
}

// BundleExtra is a file supplied by the caller (e.g. a native
// conversation), stored under native/.
type BundleExtra struct {
	Path    string // relative, under native/
	Content []byte
}

// ExportOptions select extras and declare what the caller could not include.
type ExportOptions struct {
	Extras []BundleExtra
	// Missing lists dependencies the caller knows of but could not include.
	Missing []string
	Now     time.Time
}

func storeRel(sub SubjectRef, id, name string) string {
	return "store/projects/" + ProjectKey(sub) + "/problems/" + id + "/" + name
}

// ExportBundle writes the recovery bundle of one problem.
func (s *Store) ExportBundle(sub SubjectRef, id string, o ExportOptions) ([]byte, *BundleManifest, error) {
	h, err := s.ReadHead(sub, id)
	if err != nil && !errors.Is(err, ErrNewerSchema) {
		return nil, nil, err
	}
	if _, err := s.Load(sub, id); err != nil {
		return nil, nil, err // the closure must verify before it is exported
	}
	now := o.Now
	if now.IsZero() {
		now = s.now()
	}
	files := map[string][]byte{}
	kinds := map[string]string{}
	put := func(path, kind string, b []byte) {
		files[path], kinds[path] = b, kind
	}
	subjRaw, err := readNoFollow(filepath.Join(s.projectDir(sub), "subject.json"))
	if err != nil {
		return nil, nil, corrupt("subject index unreadable: %v", err)
	}
	put("store/projects/"+ProjectKey(sub)+"/subject.json", "store", subjRaw)
	for _, name := range []string{"head.json", "members.jsonl", "ledger.json"} {
		b, err := readNoFollow(filepath.Join(s.problemDir(sub, id), name))
		if errors.Is(err, os.ErrNotExist) && name == "ledger.json" {
			continue
		}
		if err != nil {
			return nil, nil, corrupt("%s unreadable: %v", name, err)
		}
		put(storeRel(sub, id, name), "store", b)
	}
	// creation intents that name this problem
	if ents, err := os.ReadDir(filepath.Join(s.projectDir(sub), "creates")); err == nil {
		for _, e := range ents {
			b, err := readNoFollow(filepath.Join(s.projectDir(sub), "creates", e.Name()))
			if err != nil {
				continue
			}
			var ci createIntent
			if json.Unmarshal(b, &ci) == nil && ci.ProblemID == id && ValidRequestID(ci.RequestID) && e.Name() == ci.RequestID+".json" {
				put("store/projects/"+ProjectKey(sub)+"/creates/"+e.Name(), "store", b)
			}
		}
	}
	members, err := s.members(sub, id)
	if err != nil {
		return nil, nil, err
	}
	keys := make([]string, 0, len(members))
	for k := range members {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	objects := 0
	for _, k := range keys {
		aid, rev, _ := strings.Cut(k, "@")
		a, ok := s.reg.Get(aid)
		if !ok {
			return nil, nil, corrupt("member artifact %s missing", aid)
		}
		if _, ok := a.Revision(rev); !ok {
			return nil, nil, corrupt("member revision %s missing from %s", rev[:12], aid)
		}
		b, err := s.reg.Content(rev)
		if err != nil || artifacts.Hash(b) != rev {
			return nil, nil, corrupt("member bytes %s unreadable or altered", rev[:12])
		}
		if _, have := files["artifacts/objects/"+aid+".json"]; !have {
			ob, err := json.Marshal(a)
			if err != nil {
				return nil, nil, err
			}
			put("artifacts/objects/"+aid+".json", "artifact-object", ob)
			objects++
		}
		put("artifacts/blobs/"+rev, "artifact-blob", b)
	}
	natives := 0
	for _, x := range o.Extras {
		p := "native/" + strings.TrimPrefix(filepath.ToSlash(x.Path), "/")
		if bad := checkBundlePath(p); bad != "" {
			return nil, nil, Invalid("extra " + x.Path + ": " + bad)
		}
		put(p, "native", x.Content)
		natives++
	}
	st, _ := s.Load(sub, id)
	// what a restore will accept: refuse here rather than write a backup
	// that cannot be restored
	var total int64
	for p, b := range files {
		if int64(len(b)) > MaxBundleEntryBytes {
			return nil, nil, &Error{Status: 413, Kind: "too-large", Message: fmt.Sprintf("%s is %d bytes; a restore accepts at most %d per entry", p, len(b), MaxBundleEntryBytes)}
		}
		total += int64(len(b))
	}
	const allowance = 1 << 20 // README and manifest
	if len(files)+2 > MaxBundleEntries || total+allowance > HardMaxBundleTotalBytes {
		return nil, nil, &Error{Status: 413, Kind: "too-large", Message: fmt.Sprintf("the recovery bundle would hold %d entries and %d bytes; a restore accepts at most %d entries and %d bytes",
			len(files)+2, total, MaxBundleEntries, int64(HardMaxBundleTotalBytes))}
	}
	budget := ""
	if total+allowance > DefaultBundleTotalBytes {
		budget = fmt.Sprintf("This bundle decompresses to about %d MiB, more than the default restore budget (%d MiB). Restore it with -max-total-mb %d.\n\n",
			total>>20, DefaultBundleTotalBytes>>20, (total+allowance)>>20+1)
	}
	readme := fmt.Sprintf("# Construction recovery bundle (private)\n\n%s\n\nProblem %s (%s:%s), generation %d.\n\nThis is a PRIVATE backup of one construction problem's complete retained closure. "+
		"It is not a sharing package (see the detail package for that).\n\nRestore only into a NEW directory (it must not exist yet; its parent must) outside the vault:\n\n    construction-restore -bundle <this.zip> -target <new dir>\n\n%s"+
		"The restore verifies every path, size and SHA-256 before writing, recreates the exact artifact ids and revisions, and reopens the problem read from the restored bytes. "+
		"Native conversations listed under native/ are reference copies; a restore never starts or resumes an agent.\n", NonApprovalNotice, id, sub.Kind, sub.ID, h.Generation, budget)
	put("README.md", "readme", []byte(readme))
	man := &BundleManifest{Format: BundleFormat, SchemaVersion: SchemaVersion, Compiler: CompilerVersion, Convention: CoordinateConvention, RuleSet: RuleSet,
		Subject: sub, ProblemID: id, Generation: h.Generation, HeadCommit: h.Commit.Revision, CreatedAt: now.UTC().Format(time.RFC3339),
		Notice: NonApprovalNotice, Missing: append([]string{}, o.Missing...)}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		man.Files = append(man.Files, BundleFile{Path: p, Bytes: len(files[p]), SHA256: Token(files[p]), Kind: kinds[p]})
	}
	cats := []BundleCategory{
		{Name: "documents-and-history", State: "complete", Count: len(keys), Note: "every document revision, receipt, input, source snapshot, stage result, packet and derived file the problem retained"},
		{Name: "artifact-objects", State: "complete", Count: objects},
	}
	convs := 0
	if st != nil && st.Problem != nil {
		convs = len(st.Problem.Conversations)
	}
	switch {
	case convs == 0:
		cats = append(cats, BundleCategory{Name: "native-conversations", State: "not-applicable"})
	case natives >= convs:
		cats = append(cats, BundleCategory{Name: "native-conversations", State: "complete", Count: natives, Note: "reference copies; never resumed by restore"})
	case natives == 0:
		cats = append(cats, BundleCategory{Name: "native-conversations", State: "missing", Count: 0, Excluded: convs, Note: "native chats live outside the construction store and were not supplied"})
	default:
		cats = append(cats, BundleCategory{Name: "native-conversations", State: "partial", Count: natives, Excluded: convs - natives})
	}
	cats = append(cats, BundleCategory{Name: "linked-source-records", State: "not-applicable", Note: "property/Home/task records are authoritative in their own stores and are never restored from here"})
	man.Categories = cats
	man.Complete = len(man.Missing) == 0
	for _, c := range cats {
		if c.State == "missing" || c.State == "partial" {
			man.Complete = false
		}
	}
	mb, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	order := append([]string{"manifest.json"}, paths...)
	content := func(p string) []byte {
		if p == "manifest.json" {
			return mb
		}
		return files[p]
	}
	out, err := zipBundle(order, content, zip.Deflate)
	if err != nil {
		return nil, nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil || ratioProblem(zr.File, int64(len(out))) != "" {
		// content compressible beyond a restore's ratio budgets is stored
		// uncompressed instead, so the backup stays restorable
		if out, err = zipBundle(order, content, zip.Store); err != nil {
			return nil, nil, err
		}
		if zr, err = zip.NewReader(bytes.NewReader(out), int64(len(out))); err != nil {
			return nil, nil, err
		}
	}
	// the restore's own header checks, at the budget this bundle needs
	need := max(int64(DefaultBundleTotalBytes), total+int64(len(mb))+int64(len(files["README.md"])))
	if err := checkArchive(zr.File, int64(len(out)), need); err != nil {
		return nil, nil, fmt.Errorf("the recovery bundle would not pass a restore's checks: %w", err)
	}
	if int64(len(out)) > MaxBundleArchiveBytes(need) {
		return nil, nil, &Error{Status: 413, Kind: "too-large", Message: fmt.Sprintf("the recovery bundle is %d bytes, more than a restore accepts", len(out))}
	}
	return out, man, nil
}

// zipBundle writes the entries in order with one compression method and a
// fixed timestamp, so the same closure gives the same bytes.
func zipBundle(order []string, content func(string) []byte, method uint16) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	stamp := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range order {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: p, Method: method, Modified: stamp})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(content(p)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// checkBundlePath refuses anything that is not a clean relative path.
func checkBundlePath(p string) string {
	switch {
	case p == "" || len(p) > 512:
		return "empty or too long"
	case strings.ContainsAny(p, "\\\x00:"):
		return "backslash, NUL or colon"
	case strings.HasPrefix(p, "/"):
		return "absolute"
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "empty, '.' or '..' segment"
		}
	}
	if filepath.ToSlash(filepath.Clean(p)) != p {
		return "not clean"
	}
	return ""
}
