package construction

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"testing"
)

func unzip(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	var names []string
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = data
		names = append(names, f.Name)
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("entries must be in path order: %v", names)
	}
	return out
}

// The detail package carries one revision's assembly, report, section SVG
// and vector PDF, GLB, schedule, evidence summary, README and a manifest
// whose hashes match every file; all derivatives share the geometry hash;
// the private narrative and inputs are omitted; the ZIP is deterministic.
func TestConstructionDetailExportPackage(t *testing.T) {
	s, st, id := templateProblem(t)
	secret := "PRIVATE NARRATIVE: the owner's notes about the neighbour"
	st = mustExec(t, s, st, "", map[string]any{"op": "SetProblemText", "narrative": secret})
	a := st.Assemblies[id]
	tok := st.Revision("assembly:" + id)
	z1, man, err := DetailPackage(st, a, tok, st.Validation[id], PackageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	z2, _, _ := DetailPackage(st, a, tok, st.Validation[id], PackageOptions{})
	if !bytes.Equal(z1, z2) {
		t.Fatal("the package must be byte-identical for the same revision")
	}
	files := unzip(t, z1)
	for _, want := range []string{"assembly.json", "validation.json", "section.svg", "section.pdf", "section.json", "model.glb", "bom.csv", "evidence-summary.json", "README.md", "manifest.json"} {
		if len(files[want]) == 0 {
			t.Fatalf("missing %s", want)
		}
	}
	var mf PackageManifest
	if err := json.Unmarshal(files["manifest.json"], &mf); err != nil {
		t.Fatal(err)
	}
	if len(mf.Files) != len(files)-1 {
		t.Fatalf("manifest lists %d of %d files", len(mf.Files), len(files)-1)
	}
	for _, f := range mf.Files {
		if Token(files[f.Path]) != f.SHA256 || len(files[f.Path]) != f.Bytes {
			t.Fatalf("manifest hash mismatch for %s", f.Path)
		}
	}
	if man.AssemblyRevision != tok || man.GeometryHash != a.ModelHash || len(mf.Omitted) < 4 || mf.Notice != NonApprovalNotice {
		t.Fatalf("manifest identity %+v", mf)
	}
	// every derivative names the same geometry hash
	sum, err := ParseGLB(files["model.glb"])
	if err != nil || sum.Extras["geometryHash"] != a.ModelHash || sum.Extras["assemblyRevision"] != tok {
		t.Fatalf("glb %+v %v", sum, err)
	}
	if !strings.Contains(string(files["section.svg"]), a.ModelHash) || !strings.Contains(string(files["section.json"]), a.ModelHash) {
		t.Fatal("the section derivatives must name the same geometry hash")
	}
	if err := CheckSVG(files["section.svg"]); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(files["README.md"]), NonApprovalNotice) || !strings.Contains(string(files["README.md"]), "print at 100%") {
		t.Fatal("README carries the notice and the scale instruction")
	}
	var canon Assembly
	if err := DecodeStrict(files["assembly.json"], DocAssembly, &canon); err != nil || canon.ID != id {
		t.Fatalf("assembly.json must be the canonical revision: %v", err)
	}
	if Token(files["assembly.json"]) != tok {
		t.Fatal("assembly.json bytes are exactly the stored revision")
	}
	for name, b := range files {
		if strings.Contains(string(b), secret) || strings.Contains(string(b), "Fixture Way") {
			t.Fatalf("%s leaks private material", name)
		}
	}
	if !strings.Contains(string(files["bom.csv"]), "generic — unspecified") {
		t.Fatal("the schedule says when no product is specified")
	}
	// paper/scale that cannot hold the default window is refused
	if _, _, err := DetailPackage(st, a, tok, st.Validation[id], PackageOptions{Drawing: DrawingOptions{Paper: "A4", Scale: 1, Window: [4]float64{-300, -500, 1500, 500}}}); err == nil {
		t.Fatal("a window that does not fit must be refused")
	}
}
