package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/construction"
)

// bundleFixture exports a synthetic Home problem with a template assembly
// from a temp store and removes the store.
func bundleFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "construction")
	s, err := construction.Open(root, construction.Options{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": "create-cli-0001", "title": "SYNTHETIC restore fixture", "template": construction.TemplateRoofMasonry})
	req, hash, err := construction.ParseCreate(raw)
	if err != nil {
		t.Fatal(err)
	}
	home := construction.SubjectRef{Kind: "home", ID: "home"}
	st, _, err := s.CreateProblem(home, req, hash, construction.OwnerActor(), nil)
	if err != nil {
		t.Fatal(err)
	}
	z, _, err := s.ExportBundle(home, st.Problem.ID, construction.ExportOptions{Now: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC),
		Extras: []construction.BundleExtra{{Path: "alfred/conv-1.md", Content: []byte("# synthetic native conversation copy\n")}}})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	os.RemoveAll(root)
	file := filepath.Join(t.TempDir(), "bundle.zip")
	if err := os.WriteFile(file, z, 0o600); err != nil {
		t.Fatal(err)
	}
	return file, st.Problem.ID, st.Head.Commit.Revision
}

// privateDir is a 0700 directory to restore beneath (t.TempDir's numbered
// directories follow the umask; a restore refuses a group-writable parent).
func privateDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestConstructionRestoreCLI(t *testing.T) {
	file, pid, head := bundleFixture(t)
	var out, errb bytes.Buffer
	if code := run([]string{"-bundle", file, "-verify-only"}, &out, &errb); code != 0 {
		t.Fatalf("verify-only exit %d: %s", code, errb.String())
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || v["problemId"] != pid || v["verified"] != true {
		t.Fatalf("verify output %s", out.String())
	}
	target := filepath.Join(privateDir(t), "restored")
	natives := filepath.Join(privateDir(t), "native")
	out.Reset()
	errb.Reset()
	if code := run([]string{"-bundle", file, "-target", target, "-native-out", natives}, &out, &errb); code != 0 {
		t.Fatalf("restore exit %d: %s", code, errb.String())
	}
	var r struct {
		Restored bool                       `json:"restored"`
		Report   construction.RestoreReport `json:"report"`
		Native   string                     `json:"native"`
	}
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || !r.Restored || r.Report.VerifiedHead != head || !strings.Contains(r.Native, "not resumed") {
		t.Fatalf("restore output %s", out.String())
	}
	if b, err := os.ReadFile(filepath.Join(natives, "alfred", "conv-1.md")); err != nil || !strings.Contains(string(b), "synthetic native") {
		t.Fatalf("native copy written for explicit rebinding: %v", err)
	}
	s, err := construction.Open(target, construction.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := s.Load(construction.SubjectRef{Kind: "home", ID: "home"}, pid); err != nil || st.Head.Commit.Revision != head {
		t.Fatalf("restored problem opens: %v", err)
	}
	s.Close()
	// a second restore into the now-existing target is refused
	errb.Reset()
	if code := run([]string{"-bundle", file, "-target", target}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "already exists") {
		t.Fatalf("existing target: exit %d %s", code, errb.String())
	}
	// forbidden root and usage errors
	vault := privateDir(t)
	if code := run([]string{"-bundle", file, "-target", filepath.Join(vault, "c"), "-forbid", vault}, &out, &errb); code != 1 {
		t.Fatalf("forbidden target exit %d", code)
	}
	if code := run([]string{"-target", target}, &out, &errb); code != 2 {
		t.Fatalf("missing -bundle exit %d", code)
	}
}

// -native-out follows the target rules and is checked before the restore,
// so a refused one writes nothing; the budget flag is bounded; the bundle
// must be a regular file no larger than a bundle within the budget can be,
// refused from its size before it is read.
func TestConstructionRestoreCLIRefusals(t *testing.T) {
	file, _, _ := bundleFixture(t)
	var out, errb bytes.Buffer
	exit := func(args ...string) (int, string) {
		out.Reset()
		errb.Reset()
		return run(args, &out, &errb), errb.String()
	}
	target := filepath.Join(privateDir(t), "restored")
	existing := privateDir(t)
	if code, msg := exit("-bundle", file, "-target", target, "-native-out", existing); code != 1 || !strings.Contains(msg, "already exists") {
		t.Fatalf("existing -native-out: %d %s", code, msg)
	}
	open := privateDir(t)
	os.Chmod(open, 0o777)
	if code, msg := exit("-bundle", file, "-target", target, "-native-out", filepath.Join(open, "n")); code != 1 || !strings.Contains(msg, "writable by other accounts") {
		t.Fatalf("-native-out under an unsafe parent: %d %s", code, msg)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("a refused -native-out must refuse before the restore writes anything")
	}
	// every forbidden root holds, "/" included
	if code, msg := exit("-bundle", file, "-target", target, "-forbid", "/"); code != 1 || !strings.Contains(msg, "forbidden root") {
		t.Fatalf("-forbid /: %d %s", code, msg)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("a target under a forbidden root must not be created")
	}
	if code, _ := exit("-bundle", file, "-target", target, "-native-out", target); code != 2 {
		t.Fatalf("-native-out equal to -target: %d", code)
	}
	for _, mb := range []string{"0", "-5", "4096"} {
		if code, _ := exit("-bundle", file, "-verify-only", "-max-total-mb", mb); code != 2 {
			t.Fatalf("-max-total-mb %s: %d", mb, code)
		}
	}
	if code, msg := exit("-bundle", privateDir(t), "-verify-only"); code != 1 || !strings.Contains(msg, "not a regular file") {
		t.Fatalf("a directory as the bundle: %d %s", code, msg)
	}
	// a sparse file one byte over the archive bound: refused without reading
	huge := filepath.Join(privateDir(t), "huge.zip")
	f, err := os.Create(huge)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(construction.MaxBundleArchiveBytes(construction.DefaultBundleTotalBytes) + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if code, msg := exit("-bundle", huge, "-verify-only"); code != 1 || !strings.Contains(msg, "a bundle within the 256 MiB budget is at most") {
		t.Fatalf("an oversized bundle file: %d %s", code, msg)
	}
	// an explicit larger budget is accepted for the owner's own bundle
	if code, msg := exit("-bundle", file, "-verify-only", "-max-total-mb", "1024"); code != 0 {
		t.Fatalf("a raised budget: %d %s", code, msg)
	}
}
