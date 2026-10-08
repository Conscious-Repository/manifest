package construction

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// richProblem: a researched problem with a product, an applied substitution,
// an accepted decision, a saved view, a detail package export and inputs —
// the full closure a recovery bundle must carry.
func richProblem(t *testing.T) (*Store, *State, string) {
	t.Helper()
	s, st, asm := researchedProblem(t)
	ops, err := CatalogFixtureOps(filepath.Join("testdata", "roof-wall"), st.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	st = mustExec(t, s, st, "", ops["board-120"])
	ins := st.Assemblies[asm].firstOf(TypeInsulation).ID
	st = mustExec(t, s, st, asm, map[string]any{"op": "SetProduct", "componentId": ins, "productId": ops["board-120"]["id"], "applyDimensions": true})
	st = mustExec(t, s, st, "", map[string]any{"op": "SaveView", "viewId": NewID(KindView), "expectedRevision": "", "view": viewBody(asm)})
	dec := NewID(KindDecision)
	st = mustExec(t, s, st, "", map[string]any{"op": "ProposeDecision", "decisionId": dec, "assemblyId": asm, "title": "Use the base", "proposal": "Working detail."})
	st = mustExec(t, s, st, "", map[string]any{"op": "ApproveDecision", "decisionId": dec, "expectedDecisionRevision": st.Revision("decision:" + dec)})
	st, _, err = s.RetainInput(fixtureProperty, st.Problem.ID, InputUpload{RequestID: "in-photo-rich", ExpectedProblemRevision: st.Revision("problem"),
		Name: "synthetic.png", Mime: "image/png", Role: "photo", Content: []byte("\x89PNG\r\n\x1a\nSYNTHETIC")}, OwnerActor())
	if err != nil {
		t.Fatal(err)
	}
	a := st.Assemblies[asm]
	tok := st.Revision("assembly:" + asm)
	z, _, err := DetailPackage(st, a, tok, st.Validation[asm], PackageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := Canonical(map[string]any{"export": "package", "rev": tok})
	_, _, err = s.Commit(fixtureProperty, st.Problem.ID, CommitRequest{RequestID: "export-pkg-rich", PayloadHash: Token(payload), Actor: OwnerActor()}, func(tx *Tx) error {
		id, rev, err := tx.Retain("construction-derived", "package.zip", z)
		if err != nil {
			return err
		}
		tx.Next.Derived.Artifacts = append(tx.Next.Derived.Artifacts, DerivedRecord{ArtifactID: id, Revision: rev, Format: "package", Name: "package.zip", AssemblyID: asm,
			AssemblyRevision: tok, GeometryHash: a.ModelHash, Generator: PackageGenerator, Parameters: map[string]string{}, InputHashes: []string{tok}, Size: int64(len(z)),
			CreatedAt: tx.Now.Format(time.RFC3339Nano), RequestID: "export-pkg-rich"})
		tx.ViewOnly()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.Load(fixtureProperty, st.Problem.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s, st, asm
}

// The private bundle restores into an empty root with the original root
// removed: identical ids, revisions, history, evidence, catalog, decisions,
// runs, views, inputs and derived files; the rebuilt model and detail
// package are byte-identical; the restored store accepts new commits.
func TestConstructionRecoveryBundleRoundTrip(t *testing.T) {
	s, st, asm := richProblem(t)
	pid := st.Problem.ID
	fixed := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	z1, man, err := s.ExportBundle(fixtureProperty, pid, ExportOptions{Now: fixed, Missing: []string{"native conversations live in the agent chat store (not supplied here)"}})
	if err != nil {
		t.Fatal(err)
	}
	z2, _, _ := s.ExportBundle(fixtureProperty, pid, ExportOptions{Now: fixed, Missing: []string{"native conversations live in the agent chat store (not supplied here)"}})
	if !bytes.Equal(z1, z2) {
		t.Fatal("the bundle is deterministic for the same state")
	}
	members, _ := s.members(fixtureProperty, pid)
	blobs := 0
	for _, f := range man.Files {
		if f.Kind == "artifact-blob" {
			blobs++
		}
	}
	if blobs != len(members) || man.Complete || man.HeadCommit != st.Head.Commit.Revision || man.Notice != NonApprovalNotice {
		t.Fatalf("manifest: %d blobs for %d members, complete=%v", blobs, len(members), man.Complete)
	}
	// keep what we compare against, then remove the original root entirely
	orig := map[string][]byte{}
	for k, b := range st.Raw {
		orig[k] = b
	}
	hist0, _ := s.History(fixtureProperty, pid, 0)
	origPackage := st.Derived.Artifacts[len(st.Derived.Artifacts)-1]
	origPkgBytes, _ := s.Content(fixtureProperty, pid, origPackage.ArtifactID, origPackage.Revision)
	inputRef := st.Problem.Inputs[0]
	wantPkg, _, err := DetailPackage(st, st.Assemblies[asm], st.Revision("assembly:"+asm), st.Validation[asm], PackageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	root := s.Root()
	s.Close()
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(privateDir(t), "restored")
	rep, err := Restore(bytes.NewReader(z1), int64(len(z1)), target, RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Artifacts != len(members) || rep.Receipts != len(hist0) || rep.VerifiedHead != st.Head.Commit.Revision || rep.ProblemID != pid {
		t.Fatalf("restore report %+v", rep)
	}
	r := openStore(t, target, Options{})
	got, err := r.Load(fixtureProperty, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Raw) != len(orig) {
		t.Fatalf("documents %d vs %d", len(got.Raw), len(orig))
	}
	for k, b := range orig {
		if !bytes.Equal(got.Raw[k], b) || got.Revision(k) != st.Revision(k) {
			t.Fatalf("document %s differs after restore", k)
		}
	}
	hist1, _ := r.History(fixtureProperty, pid, 0)
	if len(hist1) != len(hist0) || hist1[len(hist1)-1].ID != hist0[len(hist0)-1].ID || hist1[0].ID != hist0[0].ID {
		t.Fatal("the full history is preserved")
	}
	if b, err := r.Content(fixtureProperty, pid, inputRef.ArtifactID, inputRef.Revision); err != nil || Token(b) != inputRef.Revision {
		t.Fatalf("inputs restore byte-exact: %v", err)
	}
	if b, err := r.Content(fixtureProperty, pid, origPackage.ArtifactID, origPackage.Revision); err != nil || !bytes.Equal(b, origPkgBytes) {
		t.Fatal("derived exports restore byte-exact")
	}
	for _, run := range got.Runs {
		for _, sg := range run.Stages {
			for _, at := range sg.Attempts {
				if at.Result != nil {
					if _, err := r.Content(fixtureProperty, pid, at.Result.ID, at.Result.Revision); err != nil {
						t.Fatalf("stage result %s/%s restores: %v", sg.Name, at.ID, err)
					}
				}
			}
		}
	}
	// every older assembly revision resolves; the model rebuilds identically
	ahist, err := r.AssemblyHistory(fixtureProperty, pid, asm, 0)
	if err != nil || len(ahist) < 2 {
		t.Fatalf("assembly history %d %v", len(ahist), err)
	}
	if _, _, err := r.AssemblyAt(fixtureProperty, pid, asm, ahist[len(ahist)-1].Revision); err != nil {
		t.Fatal(err)
	}
	ir, err := Compile(got.Assemblies[asm], got.Catalog)
	if err != nil || ir.Hash != got.Assemblies[asm].ModelHash {
		t.Fatalf("the semantic model rebuilds to the same hash without any provider: %v", err)
	}
	tok := got.Revision("assembly:" + asm)
	pkg, _, err := DetailPackage(got, got.Assemblies[asm], tok, got.Validation[asm], PackageOptions{})
	if err != nil || !bytes.Equal(pkg, wantPkg) {
		t.Fatalf("the detail package regenerates byte-identically: %v", err)
	}
	if got.Problem.SelectedAssembly == nil || len(got.Decisions) != 1 || len(got.Views) != 1 || len(got.Catalog.Products) != 1 || len(got.Evidence.Evidence) == 0 {
		t.Fatal("decisions, views, catalog and evidence are restored")
	}
	// the restored store is live: a new commit lands
	got2 := mustExecOn(t, r, got, asm, map[string]any{"op": "SetPitch", "value": 12, "unit": "deg"})
	if got2.Head.Generation != got.Head.Generation+1 {
		t.Fatal("the restored store accepts new commits")
	}
}

func mustExecOn(t *testing.T, s *Store, st *State, asm string, ops ...map[string]any) *State {
	t.Helper()
	next, _, err := exec(t, s, st, asm, OwnerActor(), ops...)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// rezip writes files (in order) with an optional header tweak.
func rezip(t *testing.T, files map[string][]byte, order []string, tweak func(name string, h *zip.FileHeader)) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range order {
		h := &zip.FileHeader{Name: p, Method: zip.Deflate}
		if tweak != nil {
			tweak(p, h)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(files[p])
	}
	zw.Close()
	return buf.Bytes()
}

func TestConstructionRestoreRefusals(t *testing.T) {
	s, st, _ := templateProblem(t)
	z, man, err := s.ExportBundle(fixtureProperty, st.Problem.ID, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, files, err := ReadBundle(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for p := range files {
		order = append(order, p)
	}
	sort.Strings(order)
	clone := func() map[string][]byte {
		out := map[string][]byte{}
		for k, v := range files {
			out[k] = append([]byte{}, v...)
		}
		return out
	}
	refuse := func(name string, bundle []byte, want string) {
		t.Helper()
		target := filepath.Join(privateDir(t), "r")
		_, err := Restore(bytes.NewReader(bundle), int64(len(bundle)), target, RestoreOptions{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: expected refusal containing %q, got %v", name, want, err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("%s: a refused bundle writes nothing", name)
		}
	}
	// tampered blob bytes
	f := clone()
	for p := range f {
		if strings.HasPrefix(p, "artifacts/blobs/") {
			f[p] = append(f[p], ' ')
			break
		}
	}
	refuse("tampered", rezip(t, f, order, nil), "does not match its manifest hash")
	// traversal / absolute / backslash names
	for _, bad := range []string{"../evil.json", "/etc/passwd", "store/../../x", `artifacts\blobs\x`} {
		f := clone()
		f[bad] = []byte("x")
		refuse("path "+bad, rezip(t, f, append(append([]string{}, order...), bad), nil), "entry")
	}
	// a symlink entry
	f = clone()
	f["native/link"] = []byte("/etc/passwd")
	refuse("symlink", rezip(t, f, append(append([]string{}, order...), "native/link"), func(name string, h *zip.FileHeader) {
		if name == "native/link" {
			h.SetMode(os.ModeSymlink | 0o777)
		}
	}), "not a regular file")
	// duplicate entries
	refuse("duplicate", rezip(t, files, append(append([]string{}, order...), order[1]), nil), "duplicate entry")
	// an unlisted file
	f = clone()
	f["native/unlisted.md"] = []byte("x")
	refuse("unlisted", rezip(t, f, append(append([]string{}, order...), "native/unlisted.md"), nil), "unlisted file")
	// a decompression bomb
	f = clone()
	f["native/bomb.bin"] = make([]byte, 8<<20)
	refuse("bomb", rezip(t, f, append(append([]string{}, order...), "native/bomb.bin"), nil), "expands more than")
	// a newer schema
	m2 := *man
	m2.SchemaVersion = SchemaVersion + 1
	f = clone()
	f["manifest.json"], _ = json.Marshal(m2)
	refuse("newer schema", rezip(t, f, order, nil), "newer than this build")
	// a store path for another subject
	f = clone()
	foreign := "store/projects/" + ProjectKey(fixtureOther) + "/subject.json"
	f[foreign] = []byte("{}")
	m3 := *man
	m3.Files = append(append([]BundleFile{}, man.Files...), BundleFile{Path: foreign, Bytes: 2, SHA256: Token([]byte("{}")), Kind: "store"})
	f["manifest.json"], _ = json.Marshal(m3)
	refuse("foreign store path", rezip(t, f, append(append([]string{}, order...), foreign), nil), "unexpected path")
	// consistent files, but a head document's bytes are absent: refused before writing
	f = clone()
	var dropped string
	for k, ref := range st.Head.Docs {
		if k == "problem" {
			dropped = "artifacts/blobs/" + ref.Revision
		}
	}
	delete(f, dropped)
	m4 := *man
	m4.Files = nil
	for _, bf := range man.Files {
		if bf.Path != dropped {
			m4.Files = append(m4.Files, bf)
		}
	}
	f["manifest.json"], _ = json.Marshal(m4)
	var kept []string
	for _, p := range order {
		if p != dropped {
			kept = append(kept, p)
		}
	}
	refuse("missing document bytes", rezip(t, f, kept, nil), "bytes are missing from the bundle")
	// targets: populated, forbidden, relative (CheckNewDir has its own test)
	pop := t.TempDir()
	os.WriteFile(filepath.Join(pop, "keep.txt"), []byte("x"), 0o600)
	if _, err := Restore(bytes.NewReader(z), int64(len(z)), pop, RestoreOptions{}); StatusOf(err) != 409 {
		t.Fatalf("a populated target is refused: %v", err)
	}
	vault := privateDir(t)
	if _, err := Restore(bytes.NewReader(z), int64(len(z)), filepath.Join(vault, "c"), RestoreOptions{Forbidden: []string{vault}}); StatusOf(err) != 403 {
		t.Fatalf("a target under a forbidden root is refused: %v", err)
	}
	if _, err := Restore(bytes.NewReader(z), int64(len(z)), "relative/dir", RestoreOptions{}); StatusOf(err) != 422 {
		t.Fatalf("a relative target is refused: %v", err)
	}
}
