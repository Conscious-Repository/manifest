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
	target := filepath.Join(t.TempDir(), "restored")
	natives := filepath.Join(t.TempDir(), "native")
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
	// a second restore into the now-populated target is refused
	errb.Reset()
	if code := run([]string{"-bundle", file, "-target", target}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "not empty") {
		t.Fatalf("populated target: exit %d %s", code, errb.String())
	}
	// forbidden root and usage errors
	vault := t.TempDir()
	if code := run([]string{"-bundle", file, "-target", filepath.Join(vault, "c"), "-forbid", vault}, &out, &errb); code != 1 {
		t.Fatalf("forbidden target exit %d", code)
	}
	if code := run([]string{"-target", target}, &out, &errb); code != 2 {
		t.Fatalf("missing -bundle exit %d", code)
	}
}
