package construction

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// privateDir is a 0700 directory to restore beneath: a restore refuses a
// parent that other accounts could write, and t.TempDir's numbered
// directories follow the umask.
func privateDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

// rawEntry is a zip entry whose header sizes are written exactly as given,
// whatever the (unread) data is: the shape of a hostile archive.
type rawEntry struct {
	name   string
	method uint16
	declUn uint64
	data   []byte
}

func rawZip(t *testing.T, entries []rawEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.CreateRaw(&zip.FileHeader{Name: e.name, Method: e.method, CompressedSize64: uint64(len(e.data)),
			UncompressedSize64: e.declUn, CRC32: crc32.ChecksumIEEE(e.data)})
		if err != nil {
			t.Fatal(err)
		}
		w.Write(e.data)
	}
	zw.Close()
	return buf.Bytes()
}

// noRead is an archive that must be refused from its size alone.
type noRead struct{ t *testing.T }

func (n noRead) ReadAt([]byte, int64) (int, error) {
	n.t.Error("the archive was read although its size alone refuses it")
	return 0, io.EOF
}

// The header pass bounds what a hostile archive can make a restore allocate,
// before any entry is decompressed: the archive size, the entry count, each
// entry's size, the decompressed total, and the per-entry and aggregate
// expansion ratios. The entries below carry garbage instead of deflate data,
// so a refusal naming a budget (not a flate error) proves nothing was
// decompressed. An explicit, larger budget never lifts a ratio.
func TestConstructionRestoreBudgets(t *testing.T) {
	garbage := []byte("not deflate data at all")
	refused := func(name string, z []byte, maxTotal int64, want string) {
		t.Helper()
		if _, _, err := ReadBundleWithin(bytes.NewReader(z), int64(len(z)), maxTotal); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: want a refusal containing %q, got %v", name, want, err)
		}
		target := filepath.Join(privateDir(t), "r")
		if _, err := Restore(bytes.NewReader(z), int64(len(z)), target, RestoreOptions{MaxTotalBytes: maxTotal}); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s (restore): want %q, got %v", name, want, err)
		}
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			t.Fatalf("%s: a refused bundle created the target", name)
		}
	}
	// many entries of exactly 1 MiB (each under the per-entry ratio's 1 MiB
	// floor) that together decompress to 40 MiB from a few KB: the
	// memory-bomb shape only the aggregate ratio catches
	var bomb []rawEntry
	for i := 0; i < 40; i++ {
		bomb = append(bomb, rawEntry{fmt.Sprintf("native/part-%02d.bin", i), zip.Deflate, 1 << 20, garbage})
	}
	refused("aggregate ratio", rawZip(t, bomb), DefaultBundleTotalBytes, fmt.Sprintf("more than %d×", MaxBundleAggregateRatio))
	refused("aggregate ratio, larger budget", rawZip(t, bomb), 1<<30, fmt.Sprintf("more than %d×", MaxBundleAggregateRatio))
	// a declared total over the default budget, refused while summing headers
	var big []rawEntry
	for i := 0; i < 9; i++ {
		big = append(big, rawEntry{fmt.Sprintf("native/big-%d.bin", i), zip.Deflate, 31 << 20, garbage})
	}
	refused("decompressed total", rawZip(t, big), DefaultBundleTotalBytes, "the restore budget")
	// raising the budget lets the total through but not the per-entry ratio
	refused("per-entry ratio, larger budget", rawZip(t, big), 512<<20, fmt.Sprintf("expands more than %d×", MaxBundleRatio))
	refused("entry size", rawZip(t, []rawEntry{{"native/huge.bin", zip.Deflate, MaxBundleEntryBytes + 1, garbage}}), DefaultBundleTotalBytes, "exceeds")
	// sizes are not trusted: content longer than its header is refused
	refused("header understates the content", rawZip(t, []rawEntry{{"native/short.md", zip.Store, 4, []byte("sixteen bytes!!!")}}), DefaultBundleTotalBytes, "native/short.md")
	// the entry count
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i <= MaxBundleEntries; i++ {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("native/n%d", i), Method: zip.Store})
		w.Write([]byte{'x'})
	}
	zw.Close()
	refused("entry count", buf.Bytes(), DefaultBundleTotalBytes, "too many entries")
	// the archive size alone, before anything is read
	if _, _, err := ReadBundleWithin(noRead{t}, MaxBundleArchiveBytes(DefaultBundleTotalBytes)+1, DefaultBundleTotalBytes); err == nil || !strings.Contains(err.Error(), "the archive is") {
		t.Fatalf("an oversized archive: %v", err)
	}
	// the budget itself is bounded
	for _, b := range []int64{-1, HardMaxBundleTotalBytes + 1} {
		if _, err := Restore(bytes.NewReader(nil), 0, filepath.Join(privateDir(t), "r"), RestoreOptions{MaxTotalBytes: b}); StatusOf(err) != 422 {
			t.Fatalf("budget %d: %v", b, err)
		}
	}
}

// Restore creates a NEW directory beneath a real parent that other accounts
// cannot rename or replace, rebuilds the store in a private stage beside it,
// and renames the verified stage into place. Existing targets (empty
// directories and dangling symlinks included), symlinked parents or
// ancestors, missing parents and parents writable by other accounts without
// the sticky bit are refused before anything is written; a sticky
// world-writable parent (like /tmp) is accepted. A failure after staging
// began leaves neither a target nor a stage behind.
func TestConstructionRestoreTargetRules(t *testing.T) {
	s, st, _ := templateProblem(t)
	z, man, err := s.ExportBundle(fixtureProperty, st.Problem.ID, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	restore := func(target string) error {
		_, err := Restore(bytes.NewReader(z), int64(len(z)), target, RestoreOptions{})
		return err
	}
	entries := func(dir string) []string {
		ents, _ := os.ReadDir(dir)
		var out []string
		for _, e := range ents {
			out = append(out, e.Name())
		}
		return out
	}
	// an existing empty directory is not a new one, and stays untouched
	empty := filepath.Join(privateDir(t), "empty")
	os.Mkdir(empty, 0o700)
	if err := restore(empty); StatusOf(err) != 409 || !strings.Contains(err.Error(), "already exists") || len(entries(empty)) != 0 {
		t.Fatalf("existing empty target is refused up front: %v %v", err, entries(empty))
	}
	// a dangling symlink at the target is refused, never followed
	p := privateDir(t)
	elsewhere := filepath.Join(privateDir(t), "elsewhere")
	os.Symlink(elsewhere, filepath.Join(p, "link"))
	if err := restore(filepath.Join(p, "link")); StatusOf(err) != 409 || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("dangling symlink target: %v", err)
	}
	if _, err := os.Lstat(elsewhere); !os.IsNotExist(err) {
		t.Fatal("the symlink was followed")
	}
	// a symlinked parent, and a symlinked ancestor
	realDir := privateDir(t)
	os.Mkdir(filepath.Join(realDir, "sub"), 0o700)
	links := privateDir(t)
	os.Symlink(realDir, filepath.Join(links, "parent"))
	for _, target := range []string{filepath.Join(links, "parent", "r"), filepath.Join(links, "parent", "sub", "r")} {
		if err := restore(target); StatusOf(err) != 422 || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("%s: %v", target, err)
		}
	}
	if got := entries(realDir); len(got) != 1 || len(entries(filepath.Join(realDir, "sub"))) != 0 {
		t.Fatalf("nothing is written through a symlinked parent: %v", got)
	}
	// a missing parent
	if err := restore(filepath.Join(privateDir(t), "no", "r")); StatusOf(err) != 422 {
		t.Fatalf("missing parent: %v", err)
	}
	// parents other accounts could write: world and group, without sticky
	for _, mode := range []os.FileMode{0o777, 0o770} {
		open := privateDir(t)
		os.Chmod(open, mode)
		if err := restore(filepath.Join(open, "r")); StatusOf(err) != 422 || !strings.Contains(err.Error(), "writable by other accounts") {
			t.Fatalf("parent %o: %v", mode, err)
		}
		if len(entries(open)) != 0 {
			t.Fatalf("parent %o: something was written: %v", mode, entries(open))
		}
	}
	// a sticky world-writable parent (as /tmp) is accepted
	sticky := privateDir(t)
	if err := os.Chmod(sticky, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := restore(filepath.Join(sticky, "r")); err != nil {
		t.Fatalf("sticky parent: %v", err)
	}
	if got := entries(sticky); len(got) != 1 || got[0] != "r" {
		t.Fatalf("only the target remains, no stage: %v", got)
	}
	if fi, _ := os.Lstat(filepath.Join(sticky, "r")); fi.Mode().Perm() != 0o700 {
		t.Fatalf("the target is private: %v", fi.Mode())
	}
	// a target that appears after the last check is refused by the exclusive
	// claim itself, and nothing is written into it
	raced := privateDir(t)
	restoreBeforeClaim = func(root string) {
		os.Mkdir(root, 0o700)
		os.WriteFile(filepath.Join(root, "theirs.txt"), []byte("x"), 0o600)
	}
	err = restore(filepath.Join(raced, "r"))
	restoreBeforeClaim = nil
	if StatusOf(err) != 409 || !strings.Contains(err.Error(), "appeared during the restore") {
		t.Fatalf("a target appearing mid-restore: %v", err)
	}
	if got := entries(raced); len(got) != 1 || got[0] != "r" || len(entries(filepath.Join(raced, "r"))) != 1 {
		t.Fatalf("the raced target is untouched and no stage remains: %v / %v", got, entries(filepath.Join(raced, "r")))
	}
	// a failure after staging began (an artifact object whose metadata no
	// longer derives its id: a refused collision, found while re-retaining)
	_, files, err := ReadBundle(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	var obj string
	for p := range files {
		if strings.HasPrefix(p, "artifacts/objects/") {
			obj = p
			break
		}
	}
	var o map[string]any
	json.Unmarshal(files[obj], &o)
	o["kind"] = "tampered-kind"
	files[obj], _ = json.Marshal(o)
	m := *man
	m.Files = nil
	for _, bf := range man.Files {
		if bf.Path == obj {
			bf.Bytes, bf.SHA256 = len(files[obj]), Token(files[obj])
		}
		m.Files = append(m.Files, bf)
	}
	files["manifest.json"], _ = json.Marshal(m)
	var order []string
	for p := range files {
		order = append(order, p)
	}
	sort.Strings(order)
	tampered := rezip(t, files, order, nil)
	parent := privateDir(t)
	_, err = Restore(bytes.NewReader(tampered), int64(len(tampered)), filepath.Join(parent, "r"), RestoreOptions{})
	if err == nil || !(strings.Contains(err.Error(), "collision refused") || strings.Contains(err.Error(), "re-retain")) {
		t.Fatalf("a refused re-retain while staging: %v", err)
	}
	if got := entries(parent); len(got) != 0 {
		t.Fatalf("a failed restore left %v behind", got)
	}
}

// fakeDir is a directory FileInfo with chosen owner and mode.
type fakeDir struct {
	mode os.FileMode
	sys  any
}

func (f fakeDir) Name() string       { return "d" }
func (f fakeDir) Size() int64        { return 0 }
func (f fakeDir) Mode() os.FileMode  { return f.mode }
func (f fakeDir) ModTime() time.Time { return time.Time{} }
func (f fakeDir) IsDir() bool        { return f.mode.IsDir() }
func (f fakeDir) Sys() any           { return f.sys }

// Ownership cannot be changed in a test, so the per-directory rule is checked
// on its own: another account's directory, a symlink, a non-directory,
// unknown ownership and other-writable modes are unsafe; this user's, root's
// and sticky world-writable directories are safe.
func TestConstructionRestoreUnsafeDir(t *testing.T) {
	trusted := map[uint32]bool{0: true, 1001: true}
	owned := func(uid uint32) *syscall.Stat_t { return &syscall.Stat_t{Uid: uid} }
	for _, c := range []struct {
		fi   fs.FileInfo
		want string
	}{
		{fakeDir{fs.ModeDir | 0o755, owned(1001)}, ""},
		{fakeDir{fs.ModeDir | 0o755, owned(0)}, ""},
		{fakeDir{fs.ModeDir | fs.ModeSticky | 0o777, owned(0)}, ""},
		{fakeDir{fs.ModeDir | 0o755, owned(4242)}, "owned by another account (uid 4242)"},
		{fakeDir{fs.ModeDir | 0o775, owned(1001)}, "writable by other accounts"},
		{fakeDir{fs.ModeDir | 0o757, owned(0)}, "writable by other accounts"},
		{fakeDir{fs.ModeSymlink | 0o777, owned(1001)}, "is a symlink"},
		{fakeDir{0o644, owned(1001)}, "not a directory"},
		{fakeDir{fs.ModeDir | 0o755, nil}, "no owner information"},
	} {
		got := unsafeDir("/x", c.fi, trusted)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Fatalf("%v uid %v: got %q, want %q", c.fi.Mode(), c.fi.Sys(), got, c.want)
		}
	}
}

// Content a deflated bundle would carry past a restore's ratio budgets (a
// 6 MiB owner document of one repeated character deflates about 1000×) is
// stored uncompressed instead, so the backup restores; the export refuses
// what no restore accepts.
func TestConstructionExportStaysRestorable(t *testing.T) {
	s, st, _ := templateProblem(t)
	text := bytes.Repeat([]byte("a"), 6<<20)
	st, _, err := s.RetainInput(fixtureProperty, st.Problem.ID, InputUpload{RequestID: "in-repeat-001", ExpectedProblemRevision: st.Revision("problem"),
		Name: "repetitive.txt", Mime: "text/plain; charset=utf-8", Role: "document", Content: text}, OwnerActor())
	if err != nil {
		t.Fatal(err)
	}
	z, _, err := s.ExportBundle(fixtureProperty, st.Problem.ID, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Method != zip.Store {
			t.Fatalf("%s is compressed; the fallback stores every entry", f.Name)
		}
	}
	rep, err := Restore(bytes.NewReader(z), int64(len(z)), filepath.Join(privateDir(t), "r"), RestoreOptions{})
	if err != nil || rep.VerifiedHead != st.Head.Commit.Revision {
		t.Fatalf("the stored bundle restores: %v", err)
	}
	// an entry over the per-entry limit is refused at export
	_, _, err = s.ExportBundle(fixtureProperty, st.Problem.ID, ExportOptions{Extras: []BundleExtra{{Path: "alfred/huge.md", Content: make([]byte, MaxBundleEntryBytes+1)}}})
	if StatusOf(err) != 413 {
		t.Fatalf("an over-limit entry is refused at export: %v", err)
	}
}

// Every forbidden root holds however it is written: "/" (which contains
// every path), trailing or doubled separators, "." and ".." segments, a
// path relative to the working directory, a symlink to the root, and a root
// that does not exist yet beneath a symlinked ancestor. Exact and nested
// targets are refused, by the restore check and by the store-root check
// that Restore also passes through. Valid targets keep working: a sibling
// whose name merely starts with the root's, the root's parent, and a
// "..x"-named entry beside it.
func TestConstructionForbiddenRootContainment(t *testing.T) {
	base := privateDir(t)
	vault := filepath.Join(base, "vault")
	if err := os.MkdirAll(filepath.Join(vault, "deep", "er"), 0o700); err != nil {
		t.Fatal(err)
	}
	links := privateDir(t)
	os.Symlink(vault, filepath.Join(links, "to-vault"))
	os.Symlink(base, filepath.Join(links, "to-base"))
	refused := func(name, target string, forbidden ...string) {
		t.Helper()
		if _, err := CheckNewDir(target, forbidden); StatusOf(err) != 403 {
			t.Errorf("%s: %s with forbidden %q must be refused, got %v", name, target, forbidden, err)
		}
	}
	allowed := func(name, target string, forbidden ...string) {
		t.Helper()
		if _, err := CheckNewDir(target, forbidden); err != nil {
			t.Errorf("%s: %s with forbidden %q must pass, got %v", name, target, forbidden, err)
		}
	}
	refused("root /", filepath.Join(base, "r"), "/")
	refused("root / after others", filepath.Join(base, "r"), "", filepath.Join(links, "elsewhere"), "/")
	refused("nested", filepath.Join(vault, "c"), vault)
	refused("deeply nested", filepath.Join(vault, "deep", "er", "c"), vault)
	refused("exact (root not created yet)", filepath.Join(base, "future"), filepath.Join(base, "future"))
	refused("trailing separator", filepath.Join(vault, "c"), vault+"/")
	refused("doubled separators", filepath.Join(vault, "c"), "/"+vault+"//")
	refused("dot segments", filepath.Join(vault, "c"), vault+"/./deep/../")
	refused("..x-named entry inside", filepath.Join(vault, "..x"), vault)
	refused("symlink to the root", filepath.Join(vault, "c"), filepath.Join(links, "to-vault"))
	refused("missing root below a symlinked ancestor", filepath.Join(base, "later"), filepath.Join(links, "to-base", "later"))
	allowed("sibling sharing a prefix", filepath.Join(base, "vault2"), vault)
	allowed("the root's parent", filepath.Join(base, "x"), vault)
	allowed("..x-named entry beside the root", filepath.Join(base, "..vault"), vault)
	allowed("no forbidden roots", filepath.Join(base, "x"), "")
	// relative roots are taken from the working directory
	t.Chdir(base)
	refused("relative", filepath.Join(vault, "c"), "vault")
	refused("relative with dot segments", filepath.Join(vault, "c"), "./vault/deep/../")
	refused("relative dot", filepath.Join(vault, "c"), ".")
	allowed("relative sibling", filepath.Join(base, "vault2"), "vault")
	// the store-root check (which Restore applies to its stage and target)
	for _, f := range []string{"/", "vault/", "."} {
		if _, err := Open(filepath.Join(vault, "store-"+strings.Trim(f, "/.")), Options{Forbidden: []string{f}}); err == nil || !strings.Contains(err.Error(), "forbidden root") {
			t.Errorf("store root under forbidden %q: %v", f, err)
		}
	}
	s, err := Open(filepath.Join(base, "store-ok"), Options{Forbidden: []string{"vault", "/" + vault + "/"}})
	if err != nil {
		t.Fatalf("a store root outside every forbidden root opens: %v", err)
	}
	s.Close()
	// end to end: forbidding "/" refuses any restore, writing nothing
	src, st, _ := templateProblem(t)
	z, _, err := src.ExportBundle(fixtureProperty, st.Problem.ID, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	parent := privateDir(t)
	if _, err := Restore(bytes.NewReader(z), int64(len(z)), filepath.Join(parent, "r"), RestoreOptions{Forbidden: []string{"/"}}); StatusOf(err) != 403 {
		t.Fatalf("a restore with / forbidden: %v", err)
	}
	if ents, _ := os.ReadDir(parent); len(ents) != 0 {
		t.Fatalf("a refused restore wrote %d entries", len(ents))
	}
}
