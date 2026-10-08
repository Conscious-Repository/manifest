package construction

// Restore (§9, P9). A recovery bundle is restored only into an EMPTY,
// explicit, absolute root outside every forbidden root. Before anything is
// written, every entry is checked: clean relative path (no absolute,
// traversal, backslash, NUL), not a symlink or special file, no duplicates,
// per-entry/total/count/expansion budgets, every byte matching its manifest
// SHA-256, no unlisted file, schema/format known, and the store paths
// matching the manifest's subject and problem. Then the artifacts are
// re-retained (the same deterministic ids and revisions must come back —
// anything else is a refused collision), the store files are written, and
// the problem is reopened from the restored bytes and verified. Native
// conversation copies are returned to the caller; restore never starts or
// resumes an agent.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"manifest/artifacts"
)

// Restore budgets.
const (
	MaxBundleEntries    = 100000
	MaxBundleEntryBytes = 64 << 20
	MaxBundleTotalBytes = 2 << 30
	MaxBundleRatio      = 500
)

// RestoreOptions configure a restore.
type RestoreOptions struct {
	Forbidden []string
}

// RestoreReport says what was restored and what was not.
type RestoreReport struct {
	Subject      SubjectRef        `json:"subject"`
	ProblemID    string            `json:"problemId"`
	Generation   int64             `json:"generation"`
	Artifacts    int               `json:"artifacts"`
	Documents    int               `json:"documents"`
	Receipts     int               `json:"receipts"`
	Native       map[string][]byte `json:"-"`
	NativeFiles  []string          `json:"nativeFiles"`
	Categories   []BundleCategory  `json:"categories"`
	Missing      []string          `json:"missing"`
	Complete     bool              `json:"complete"`
	VerifiedHead string            `json:"verifiedHead"`
}

func restoreErr(format string, a ...any) error {
	return &Error{Status: 422, Kind: "invalid", Message: "bundle refused: " + fmt.Sprintf(format, a...)}
}

// ReadBundle validates a bundle completely in memory and returns its
// manifest and files. Nothing is written.
func ReadBundle(r io.ReaderAt, size int64) (*BundleManifest, map[string][]byte, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, nil, restoreErr("not a zip archive: %v", err)
	}
	if len(zr.File) > MaxBundleEntries {
		return nil, nil, restoreErr("too many entries (%d)", len(zr.File))
	}
	files := map[string][]byte{}
	var total int64
	for _, f := range zr.File {
		if bad := checkBundlePath(f.Name); bad != "" {
			return nil, nil, restoreErr("entry %q: %s", f.Name, bad)
		}
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 || mode&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket|os.ModeCharDevice) != 0 || f.FileInfo().IsDir() {
			return nil, nil, restoreErr("entry %q is not a regular file", f.Name)
		}
		if _, dup := files[f.Name]; dup {
			return nil, nil, restoreErr("duplicate entry %q", f.Name)
		}
		if f.UncompressedSize64 > MaxBundleEntryBytes {
			return nil, nil, restoreErr("entry %q exceeds %d bytes", f.Name, MaxBundleEntryBytes)
		}
		if f.UncompressedSize64 > 1<<20 && f.CompressedSize64 > 0 && f.UncompressedSize64/f.CompressedSize64 > MaxBundleRatio {
			return nil, nil, restoreErr("entry %q expands more than %d×", f.Name, MaxBundleRatio)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, restoreErr("entry %q: %v", f.Name, err)
		}
		b, err := io.ReadAll(io.LimitReader(rc, MaxBundleEntryBytes+1))
		rc.Close()
		if err != nil {
			return nil, nil, restoreErr("entry %q: %v", f.Name, err)
		}
		if int64(len(b)) > MaxBundleEntryBytes || uint64(len(b)) != f.UncompressedSize64 {
			return nil, nil, restoreErr("entry %q size does not match its header", f.Name)
		}
		total += int64(len(b))
		if total > MaxBundleTotalBytes {
			return nil, nil, restoreErr("bundle expands beyond %d bytes", int64(MaxBundleTotalBytes))
		}
		files[f.Name] = b
	}
	mb, ok := files["manifest.json"]
	if !ok {
		return nil, nil, restoreErr("no manifest.json")
	}
	var man BundleManifest
	d := json.NewDecoder(bytes.NewReader(mb))
	d.DisallowUnknownFields()
	if err := d.Decode(&man); err != nil {
		return nil, nil, restoreErr("manifest: %v", err)
	}
	if man.Format != BundleFormat {
		return nil, nil, restoreErr("unknown bundle format %q", man.Format)
	}
	if man.SchemaVersion > SchemaVersion {
		return nil, nil, restoreErr("bundle schema %d is newer than this build (%d)", man.SchemaVersion, SchemaVersion)
	}
	if err := ValidSubject(man.Subject); err != nil || !ValidID(KindProblem, man.ProblemID) {
		return nil, nil, restoreErr("manifest names no valid subject/problem")
	}
	listed := map[string]bool{"manifest.json": true}
	for _, mf := range man.Files {
		b, ok := files[mf.Path]
		if !ok {
			return nil, nil, restoreErr("listed file %q is missing", mf.Path)
		}
		if len(b) != mf.Bytes || Token(b) != mf.SHA256 {
			return nil, nil, restoreErr("file %q does not match its manifest hash", mf.Path)
		}
		listed[mf.Path] = true
	}
	for p := range files {
		if !listed[p] {
			return nil, nil, restoreErr("unlisted file %q", p)
		}
	}
	prefix := "store/projects/" + ProjectKey(man.Subject) + "/"
	for p := range files {
		switch {
		case p == "manifest.json" || p == "README.md":
		case strings.HasPrefix(p, "artifacts/objects/"):
			if !artifacts.ValidID(strings.TrimSuffix(strings.TrimPrefix(p, "artifacts/objects/"), ".json")) || !strings.HasSuffix(p, ".json") {
				return nil, nil, restoreErr("bad artifact object path %q", p)
			}
		case strings.HasPrefix(p, "artifacts/blobs/"):
			if h := strings.TrimPrefix(p, "artifacts/blobs/"); !ValidToken(h) || Token(files[p]) != h {
				return nil, nil, restoreErr("blob %q is not named by its hash", p)
			}
		case strings.HasPrefix(p, "native/"):
		case strings.HasPrefix(p, prefix):
			rest := strings.TrimPrefix(p, prefix)
			okPath := rest == "subject.json" || rest == "problems/"+man.ProblemID+"/head.json" || rest == "problems/"+man.ProblemID+"/members.jsonl" ||
				rest == "problems/"+man.ProblemID+"/ledger.json" || (strings.HasPrefix(rest, "creates/") && strings.HasSuffix(rest, ".json") && ValidRequestID(strings.TrimSuffix(strings.TrimPrefix(rest, "creates/"), ".json")))
			if !okPath {
				return nil, nil, restoreErr("unexpected store path %q", p)
			}
		default:
			return nil, nil, restoreErr("unexpected path %q", p)
		}
	}
	for _, need := range []string{prefix + "subject.json", prefix + "problems/" + man.ProblemID + "/head.json", prefix + "problems/" + man.ProblemID + "/members.jsonl"} {
		if _, ok := files[need]; !ok {
			return nil, nil, restoreErr("missing %s", need)
		}
	}
	return &man, files, nil
}

// emptyTarget checks the restore root: absolute, clean, not a symlink,
// outside forbidden roots, and empty (or absent).
func emptyTarget(root string, forbidden []string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return Invalid("restore target must be an absolute clean path")
	}
	if fi, err := os.Lstat(root); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return Invalid("restore target must be a real directory")
		}
		ents, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		if len(ents) > 0 {
			return Conflict("restore target is not empty; restore never merges into a populated root", nil)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(root))
	if err != nil {
		return Invalid("restore target's parent must exist: " + err.Error())
	}
	resolved := filepath.Join(parent, filepath.Base(root))
	for _, f := range forbidden {
		if f == "" {
			continue
		}
		rf, err := filepath.EvalSymlinks(f)
		if err != nil {
			rf = filepath.Clean(f)
		}
		if resolved == rf || strings.HasPrefix(resolved, rf+string(os.PathSeparator)) {
			return Forbidden("restore target lies under a forbidden root")
		}
	}
	return nil
}

// precheckBundle verifies, in memory, that every head document decodes
// from its bundled bytes, the receipt chain is complete, and every member
// named by the ACL has its object and bytes in the bundle.
func precheckBundle(head Head, files map[string][]byte, membersPath string) error {
	blob := func(rev string) ([]byte, bool) {
		b, ok := files["artifacts/blobs/"+rev]
		return b, ok
	}
	probe := newState(head)
	for key, ref := range head.Docs {
		b, ok := blob(ref.Revision)
		if !ok || Token(b) != ref.Revision {
			return restoreErr("document %s bytes are missing from the bundle", key)
		}
		if docKindFor(key) != ref.Kind {
			return restoreErr("document %s has kind %s", key, ref.Kind)
		}
		if err := probe.decode(key, ref.Kind, b); err != nil && !errors.Is(err, ErrNewerSchema) {
			return restoreErr("document %s does not decode: %v", key, err)
		}
	}
	tok, steps := head.Commit.Revision, 0
	for tok != "" {
		if steps++; steps > 1000000 {
			return restoreErr("receipt chain does not end")
		}
		b, ok := blob(tok)
		if !ok {
			return restoreErr("receipt %s is missing from the bundle", tok[:12])
		}
		var rc Receipt
		if err := DecodeStrict(b, DocReceipt, &rc); err != nil {
			return restoreErr("receipt %s does not decode: %v", tok[:12], err)
		}
		tok = rc.ParentCommit
	}
	for _, line := range strings.Split(strings.TrimSpace(string(files[membersPath])), "\n") {
		if line == "" {
			continue
		}
		var m member
		if err := json.Unmarshal([]byte(line), &m); err != nil || !ValidToken(m.Revision) {
			return restoreErr("membership line is malformed")
		}
		if _, ok := blob(m.Revision); !ok {
			return restoreErr("member %s@%s bytes are missing from the bundle", m.ArtifactID, m.Revision[:12])
		}
		if _, ok := files["artifacts/objects/"+m.ArtifactID+".json"]; !ok {
			return restoreErr("member %s object is missing from the bundle", m.ArtifactID)
		}
	}
	return nil
}

// Restore validates the bundle and restores it into an empty root.
func Restore(r io.ReaderAt, size int64, root string, o RestoreOptions) (*RestoreReport, error) {
	if err := emptyTarget(root, o.Forbidden); err != nil {
		return nil, err
	}
	man, files, err := ReadBundle(r, size)
	if err != nil {
		return nil, err
	}
	sub, pid := man.Subject, man.ProblemID
	prefix := "store/projects/" + ProjectKey(sub) + "/"
	var head Head
	hd := json.NewDecoder(bytes.NewReader(files[prefix+"problems/"+pid+"/head.json"]))
	hd.DisallowUnknownFields()
	if err := hd.Decode(&head); err != nil || head.ProblemID != pid || head.Subject != sub || head.Kind != DocHead {
		return nil, restoreErr("head does not belong to the manifest's problem")
	}
	if head.Commit.Revision != man.HeadCommit || head.Generation != man.Generation {
		return nil, restoreErr("head does not match the manifest")
	}
	if err := precheckBundle(head, files, prefix+"problems/"+pid+"/members.jsonl"); err != nil {
		return nil, err
	}
	s, err := Open(root, Options{Forbidden: o.Forbidden})
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		s.Close()
		if !ok {
			os.RemoveAll(root) // verified empty before; never leave half a restore
		}
	}()
	if err := s.acquireWriter(); err != nil {
		return nil, err
	}
	rep := &RestoreReport{Subject: sub, ProblemID: pid, Generation: head.Generation, Native: map[string][]byte{}, NativeFiles: []string{},
		Categories: man.Categories, Missing: man.Missing, Complete: man.Complete}
	for p, b := range files {
		if !strings.HasPrefix(p, "artifacts/objects/") {
			continue
		}
		var a artifacts.Artifact
		if err := json.Unmarshal(b, &a); err != nil {
			return nil, restoreErr("artifact object %s: %v", p, err)
		}
		id := strings.TrimSuffix(strings.TrimPrefix(p, "artifacts/objects/"), ".json")
		if a.ID != id || len(a.Revisions) == 0 {
			return nil, restoreErr("artifact object %s names another id", p)
		}
		for _, rv := range a.Revisions {
			blob, ok := files["artifacts/blobs/"+rv.Hash]
			if !ok {
				continue // a revision of a shared object that this problem does not own
			}
			res, err := s.reg.Retain(artifacts.Put{Kind: a.Kind, Harness: a.Harness, Ref: a.Ref, Title: a.Title, Content: blob, Actor: a.Actor, At: a.Created, Provenance: a.Provenance})
			if err != nil {
				return nil, restoreErr("re-retain %s: %v", id, err)
			}
			if res.Artifact.ID != id || res.Revision.Hash != rv.Hash {
				return nil, restoreErr("artifact %s would restore as %s@%s (collision refused)", id, res.Artifact.ID, res.Revision.Hash[:12])
			}
			rep.Artifacts++
		}
	}
	write := func(rel string) error {
		b, ok := files[prefix+rel]
		if !ok {
			return nil
		}
		return writeAtomic(filepath.Join(s.projectDir(sub), filepath.FromSlash(rel)), b)
	}
	for p := range files {
		if strings.HasPrefix(p, prefix+"creates/") {
			if err := write(strings.TrimPrefix(p, prefix)); err != nil {
				return nil, err
			}
		}
	}
	for _, rel := range []string{"subject.json", "problems/" + pid + "/members.jsonl", "problems/" + pid + "/ledger.json", "problems/" + pid + "/head.json"} {
		if err := write(rel); err != nil {
			return nil, err
		}
	}
	// reopen from the restored bytes and verify the whole closure
	st, err := s.Load(sub, pid)
	if err != nil {
		return nil, fmt.Errorf("restored problem does not verify: %w", err)
	}
	rep.Documents = len(st.Head.Docs)
	hist, err := s.History(sub, pid, 0)
	if err != nil {
		return nil, fmt.Errorf("restored history does not verify: %w", err)
	}
	rep.Receipts = len(hist)
	for k := range st.Head.Docs {
		if !s.IsMember(sub, pid, st.Head.Docs[k].ArtifactID, st.Head.Docs[k].Revision) {
			return nil, fmt.Errorf("restored document %s is not a member", k)
		}
	}
	rep.VerifiedHead = st.Head.Commit.Revision
	ok = true
	for p, b := range files {
		if strings.HasPrefix(p, "native/") {
			rep.Native[p] = b
			rep.NativeFiles = append(rep.NativeFiles, p)
		}
	}
	return rep, nil
}
