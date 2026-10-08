package construction

// Restore (§9, P9). A recovery bundle is restored only into a NEW directory:
// an explicit absolute path that does not exist yet, directly beneath an
// existing real directory that no other account can rename or replace, and
// outside every forbidden root (CheckNewDir).
//
// Before anything is written the whole bundle is read and checked in memory
// within fixed budgets. A header pass runs before any entry is decompressed
// and refuses too many entries, an oversized entry, a decompressed total
// over the restore budget, and an entry or the archive as a whole expanding
// beyond the ratio budgets. Then every entry is checked: clean relative path
// (no absolute, traversal, backslash, NUL), not a symlink or special file, no
// duplicates, every byte matching its manifest SHA-256, no unlisted file,
// schema/format known, and the store paths matching the manifest's subject
// and problem.
//
// The store is then rebuilt in a private staging directory beside the
// target. The artifacts are re-retained (the same deterministic ids and
// revisions must come back; anything else is a refused collision), the store
// files are written, and the problem is reopened from the restored bytes and
// verified. Only then is the target name claimed exclusively and the
// verified stage renamed onto it, and the problem verified once more from
// there. Any failure removes the stage, so a refused restore leaves no
// target. Native conversation copies are returned to the caller; restore
// never starts or resumes an agent.
//
// The store writes use path names, so these destination rules refuse unsafe
// targets rather than anchor every write to a directory descriptor. They
// hold against other OS accounts, not against a process running as the
// owner, which can change the owner's files anyway.

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"manifest/artifacts"
)

// Restore budgets. A bundle is held in memory while it is checked, so these
// bound what a hostile archive can make a restore allocate. The defaults
// suit one local problem (an input is at most MaxInputBytes). An owner
// restoring their own larger bundle raises the total explicitly
// (RestoreOptions.MaxTotalBytes, construction-restore -max-total-mb), never
// beyond HardMaxBundleTotalBytes. ExportBundle refuses a bundle these checks
// would refuse, and stores entries uncompressed rather than exceed a ratio,
// so they never make a backup unrestorable.
const (
	MaxBundleEntries        = 50000
	MaxBundleEntryBytes     = 32 << 20
	DefaultBundleTotalBytes = 256 << 20
	HardMaxBundleTotalBytes = 2 << 30
	MaxBundleRatio          = 500      // one entry over 1 MiB: decompressed ÷ compressed
	MaxBundleAggregateRatio = 100      // the archive: decompressed total ÷ archive bytes
	BundleRatioFloor        = 16 << 20 // a decompressed total this small skips the aggregate ratio
)

// RestoreOptions configure a restore.
type RestoreOptions struct {
	Forbidden []string
	// MaxTotalBytes raises the decompressed budget for an owner's own larger
	// bundle: 0 means DefaultBundleTotalBytes, at most HardMaxBundleTotalBytes.
	MaxTotalBytes int64
}

func (o RestoreOptions) totalBudget() (int64, error) {
	if o.MaxTotalBytes == 0 {
		return DefaultBundleTotalBytes, nil
	}
	return o.MaxTotalBytes, checkBudget(o.MaxTotalBytes)
}

func checkBudget(maxTotal int64) error {
	if maxTotal <= 0 || maxTotal > HardMaxBundleTotalBytes {
		return Invalid(fmt.Sprintf("the restore budget must be 1–%d bytes", int64(HardMaxBundleTotalBytes)))
	}
	return nil
}

// MaxBundleArchiveBytes bounds the archive itself: the decompressed budget
// plus room for headers and entries stored uncompressed.
func MaxBundleArchiveBytes(maxTotal int64) int64 { return maxTotal + maxTotal/8 }

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

// ReadBundle validates a bundle completely in memory, within the default
// budget, and returns its manifest and files. Nothing is written.
func ReadBundle(r io.ReaderAt, size int64) (*BundleManifest, map[string][]byte, error) {
	return ReadBundleWithin(r, size, DefaultBundleTotalBytes)
}

// ReadBundleWithin is ReadBundle with an explicit decompressed budget.
func ReadBundleWithin(r io.ReaderAt, size, maxTotal int64) (*BundleManifest, map[string][]byte, error) {
	if err := checkBudget(maxTotal); err != nil {
		return nil, nil, err
	}
	if size <= 0 || size > MaxBundleArchiveBytes(maxTotal) {
		return nil, nil, restoreErr("the archive is %d bytes; a bundle within the %d-byte budget is at most %d", size, maxTotal, MaxBundleArchiveBytes(maxTotal))
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, nil, restoreErr("not a zip archive: %v", err)
	}
	if err := checkArchive(zr.File, size, maxTotal); err != nil {
		return nil, nil, err
	}
	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, nil, restoreErr("entry %q: %v", f.Name, err)
		}
		// the header sizes were budgeted above; archive/zip also refuses
		// content longer than its header declares
		b, err := io.ReadAll(io.LimitReader(rc, int64(f.UncompressedSize64)+1))
		rc.Close()
		if err != nil {
			return nil, nil, restoreErr("entry %q: %v", f.Name, err)
		}
		if uint64(len(b)) != f.UncompressedSize64 {
			return nil, nil, restoreErr("entry %q size does not match its header", f.Name)
		}
		files[f.Name] = b
	}
	mb, ok := files["manifest.json"]
	if !ok {
		return nil, nil, restoreErr("no manifest.json")
	}
	var man BundleManifest
	if err := decodeOne(mb, &man); err != nil {
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

// checkArchive is the header pass: it refuses a bundle before any entry is
// decompressed. Sizes come from the headers; reading then holds each entry
// to its declared size.
func checkArchive(files []*zip.File, size, maxTotal int64) error {
	if len(files) > MaxBundleEntries {
		return restoreErr("too many entries (%d; at most %d)", len(files), MaxBundleEntries)
	}
	seen := make(map[string]bool, len(files))
	var total uint64
	for _, f := range files {
		if bad := checkBundlePath(f.Name); bad != "" {
			return restoreErr("entry %q: %s", f.Name, bad)
		}
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 || mode&(os.ModeDevice|os.ModeNamedPipe|os.ModeSocket|os.ModeCharDevice) != 0 || f.FileInfo().IsDir() {
			return restoreErr("entry %q is not a regular file", f.Name)
		}
		if seen[f.Name] {
			return restoreErr("duplicate entry %q", f.Name)
		}
		seen[f.Name] = true
		if f.UncompressedSize64 > MaxBundleEntryBytes {
			return restoreErr("entry %q exceeds %d bytes", f.Name, MaxBundleEntryBytes)
		}
		if total += f.UncompressedSize64; total > uint64(maxTotal) {
			return restoreErr("bundle expands beyond %d bytes, the restore budget (for your own larger bundle, raise it explicitly: construction-restore -max-total-mb)", maxTotal)
		}
	}
	if p := ratioProblem(files, size); p != "" {
		return restoreErr("%s", p)
	}
	return nil
}

// ratioProblem reports an entry, or the archive as a whole, expanding
// beyond the ratio budgets ("" when within them). The aggregate check covers
// what the per-entry one cannot: many entries, each small or each just
// under its ratio, that together decompress to far more than the archive.
func ratioProblem(files []*zip.File, size int64) string {
	var total uint64
	for _, f := range files {
		if f.UncompressedSize64 > 1<<20 && (f.CompressedSize64 == 0 || f.UncompressedSize64/f.CompressedSize64 > MaxBundleRatio) {
			return fmt.Sprintf("entry %q expands more than %d×", f.Name, MaxBundleRatio)
		}
		total += f.UncompressedSize64
	}
	if total > BundleRatioFloor && total > MaxBundleAggregateRatio*uint64(max(size, 0)) {
		return fmt.Sprintf("the archive decompresses to %d bytes, more than %d× its %d bytes", total, MaxBundleAggregateRatio, size)
	}
	return ""
}

// CheckNewDir reports whether dir may be created by a restore, and returns
// its parent. dir must be absolute and clean and must not exist in any form
// (a dangling symlink included). Its parent must be an existing real
// directory whose whole chain is safe from other accounts (safeDirChain),
// and dir must lie outside every forbidden root. The caller then creates it
// exclusively, mode 0700.
func CheckNewDir(dir string, forbidden []string) (string, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || filepath.Dir(dir) == dir {
		return "", Invalid("the target must be an absolute clean path to a new directory")
	}
	if _, err := os.Lstat(dir); err == nil {
		return "", Conflict("the target already exists; a restore creates a new directory and never writes into an existing one", nil)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(dir)
	if err := safeDirChain(parent); err != nil {
		return "", err
	}
	// the chain holds no symlink, so dir is its own resolved path
	if f, err := forbiddenRootOf(dir, forbidden); err != nil {
		return "", Forbidden("forbidden root " + f + " cannot be resolved (" + err.Error() + "); refusing")
	} else if f != "" {
		return "", Forbidden("the target lies under a forbidden root")
	}
	return parent, nil
}

// safeDirChain refuses a parent that another account could rename or
// replace during a restore. Every directory from dir up to "/" must be a
// real directory (no symlink anywhere in the path), owned by root, by this
// user or by the owner of "/" (which is root as seen through a user-namespace
// mapping), and not writable by group or others unless the sticky bit is set
// (as on /tmp, where other accounts cannot rename entries they do not own).
func safeDirChain(dir string) error {
	trusted := map[uint32]bool{0: true, uint32(os.Geteuid()): true}
	if fi, err := os.Lstat("/"); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			trusted[st.Uid] = true
		}
	}
	for d := dir; ; d = filepath.Dir(d) {
		fi, err := os.Lstat(d)
		if err != nil {
			return Invalid("the target's parent must exist: " + err.Error())
		}
		if p := unsafeDir(d, fi, trusted); p != "" {
			return Invalid("unsafe restore location: " + p)
		}
		if filepath.Dir(d) == d {
			return nil
		}
	}
}

// unsafeDir says why one directory of the chain is unsafe ("" when safe).
func unsafeDir(path string, fi os.FileInfo, trusted map[uint32]bool) string {
	if fi.Mode()&os.ModeSymlink != 0 {
		return path + " is a symlink"
	}
	if !fi.IsDir() {
		return path + " is not a directory"
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return path + " has no owner information"
	}
	if !trusted[st.Uid] {
		return fmt.Sprintf("%s is owned by another account (uid %d)", path, st.Uid)
	}
	if fi.Mode().Perm()&0o022 != 0 && fi.Mode()&os.ModeSticky == 0 {
		return path + " is writable by other accounts without the sticky bit"
	}
	return ""
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

// Restore validates the bundle and restores it into a new directory root.
func Restore(r io.ReaderAt, size int64, root string, o RestoreOptions) (*RestoreReport, error) {
	maxTotal, err := o.totalBudget()
	if err != nil {
		return nil, err
	}
	parent, err := CheckNewDir(root, o.Forbidden)
	if err != nil {
		return nil, err
	}
	man, files, err := ReadBundleWithin(r, size, maxTotal)
	if err != nil {
		return nil, err
	}
	sub, pid := man.Subject, man.ProblemID
	prefix := "store/projects/" + ProjectKey(sub) + "/"
	var head Head
	if err := decodeOne(files[prefix+"problems/"+pid+"/head.json"], &head); err != nil || head.ProblemID != pid || head.Subject != sub || head.Kind != DocHead {
		return nil, restoreErr("head does not belong to the manifest's problem")
	}
	if head.Commit.Revision != man.HeadCommit || head.Generation != man.Generation {
		return nil, restoreErr("head does not match the manifest")
	}
	if err := precheckBundle(head, files, prefix+"problems/"+pid+"/members.jsonl"); err != nil {
		return nil, err
	}
	// rebuild and verify the store in a private stage beside the target
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(root)+".restore-")
	if err != nil {
		return nil, err
	}
	placed := false
	defer func() {
		if !placed {
			os.RemoveAll(stage)
		}
	}()
	rep, err := restoreInto(stage, man, files, o.Forbidden)
	if err != nil {
		return nil, err
	}
	// claim the target name exclusively (re-checking the chain first), then
	// rename the verified stage onto the claimed directory. rename(2)
	// replaces a directory only while it is empty; os.Rename refuses any
	// existing directory, so the system call is used directly.
	if _, err := CheckNewDir(root, o.Forbidden); err != nil {
		return nil, err
	}
	if restoreBeforeClaim != nil {
		restoreBeforeClaim(root)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, Conflict("the target appeared during the restore; nothing was placed there", nil)
		}
		return nil, err
	}
	if err := syscall.Rename(stage, root); err != nil {
		os.Remove(root)
		return nil, fmt.Errorf("place the restored store: %w", err)
	}
	placed = true
	s, err := Open(root, Options{Forbidden: o.Forbidden})
	if err == nil {
		var verified string
		_, _, verified, err = verifyRestored(s, sub, pid)
		s.Close()
		if err == nil && verified != rep.VerifiedHead {
			err = fmt.Errorf("head %s, expected %s", verified, rep.VerifiedHead)
		}
	}
	if err != nil {
		os.RemoveAll(root) // created by this restore; never leave half of one
		return nil, fmt.Errorf("the restored problem does not verify at its target: %w", err)
	}
	return rep, nil
}

// restoreBeforeClaim lets a test act between the last check and the
// exclusive claim (a target appearing in that window). Nil in production.
var restoreBeforeClaim func(root string)

// restoreInto rebuilds the store from a checked bundle in dir and verifies
// it from the written bytes.
func restoreInto(dir string, man *BundleManifest, files map[string][]byte, forbidden []string) (*RestoreReport, error) {
	sub, pid := man.Subject, man.ProblemID
	prefix := "store/projects/" + ProjectKey(sub) + "/"
	s, err := Open(dir, Options{Forbidden: forbidden})
	if err != nil {
		return nil, err
	}
	defer s.Close()
	if err := s.acquireWriter(); err != nil {
		return nil, err
	}
	rep := &RestoreReport{Subject: sub, ProblemID: pid, Generation: man.Generation, Native: map[string][]byte{}, NativeFiles: []string{},
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
	if rep.Documents, rep.Receipts, rep.VerifiedHead, err = verifyRestored(s, sub, pid); err != nil {
		return nil, err
	}
	for p, b := range files {
		if strings.HasPrefix(p, "native/") {
			rep.Native[p] = b
			rep.NativeFiles = append(rep.NativeFiles, p)
		}
	}
	return rep, nil
}

// verifyRestored loads the problem from the store's bytes and checks its
// documents, receipt history and membership.
func verifyRestored(s *Store, sub SubjectRef, pid string) (docs, receipts int, head string, err error) {
	st, err := s.Load(sub, pid)
	if err != nil {
		return 0, 0, "", fmt.Errorf("restored problem does not verify: %w", err)
	}
	hist, err := s.History(sub, pid, 0)
	if err != nil {
		return 0, 0, "", fmt.Errorf("restored history does not verify: %w", err)
	}
	for k := range st.Head.Docs {
		if !s.IsMember(sub, pid, st.Head.Docs[k].ArtifactID, st.Head.Docs[k].Revision) {
			return 0, 0, "", fmt.Errorf("restored document %s is not a member", k)
		}
	}
	return len(st.Head.Docs), len(hist), st.Head.Commit.Revision, nil
}
