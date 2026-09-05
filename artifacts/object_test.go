package artifacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func registry(t *testing.T) (*Registry, string) {
	t.Helper()
	s, dir := store(t)
	r, err := NewRegistry(s)
	if err != nil {
		t.Fatal(err)
	}
	return r, dir
}

var t0 = time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)

// Content identity is the address: the id derives from the first bytes, the
// same registration replayed is the same object, and identical bytes cost
// one blob (the samizdat convention, through the shared pool).
func TestPutIsContentAddressedAndIdempotent(t *testing.T) {
	r, dir := registry(t)
	body := []byte("# UAE brief\n\nfindings…\n")
	p := Put{Kind: KindBrief, Title: "UAE brief", Ref: "artifacts/library/2026-08-12-uae.md", Content: body, Actor: "agent:hermes", At: t0,
		Provenance: Provenance{Source: "run", Task: "inbox/uae", Run: "r1"}}
	a, err := r.Put(p)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Created || !a.Changed || a.Artifact.Version() != 1 || a.Artifact.Head != Hash(body) || a.Revision.N != 1 || a.Revision.Parent != "" {
		t.Fatalf("first put: %+v", a)
	}
	if !ValidID(a.Artifact.ID) || a.Artifact.ID != IDFor(KindBrief, "", p.Ref, Hash(body)) {
		t.Fatalf("id derivation: %q", a.Artifact.ID)
	}
	if a.Artifact.Provenance.Task != "inbox/uae" || a.Artifact.Created != t0 {
		t.Fatalf("provenance/created: %+v", a.Artifact)
	}
	// replay: same object, nothing written
	b, err := r.Put(p)
	if err != nil {
		t.Fatal(err)
	}
	if b.Created || b.Changed || b.Artifact.ID != a.Artifact.ID || b.Artifact.Version() != 1 {
		t.Fatalf("replay must be a no-op: %+v", b)
	}
	if got := countBlobs(t, dir); got != 1 {
		t.Fatalf("want 1 blob after a replay, got %d", got)
	}
	if n := len(r.List(Filter{})); n != 1 {
		t.Fatalf("want 1 object, got %d", n)
	}
	// the same bytes at another place are a different artifact — but the
	// pool still holds them once
	c, err := r.Put(Put{Kind: KindBrief, Ref: "artifacts/library/copy.md", Content: body, At: t0})
	if err != nil {
		t.Fatal(err)
	}
	if !c.Created || c.Artifact.ID == a.Artifact.ID || c.Artifact.Head != a.Artifact.Head {
		t.Fatalf("copy elsewhere: %+v", c)
	}
	if got := countBlobs(t, dir); got != 1 {
		t.Fatalf("identical bytes must dedupe across artifacts, got %d blobs", got)
	}
	// content reads back by hash
	got, err := r.Content(a.Artifact.Head)
	if err != nil || string(got) != string(body) {
		t.Fatalf("content by hash: %q %v", got, err)
	}
	if _, err := r.Content("../../etc/passwd"); err == nil {
		t.Fatal("bad hash accepted")
	}
	if _, err := r.Put(Put{Kind: KindFile, Content: nil}); err == nil {
		t.Fatal("empty content accepted")
	}
}

// Versioned, never mutated: a revision appends and names its parent, the
// old revision is byte-identical afterwards, and its bytes still read.
func TestReviseAppendsNeverMutates(t *testing.T) {
	r, _ := registry(t)
	v1 := []byte("draft one\n")
	v2 := []byte("draft two\n")
	ref := "artifacts/library/plan.md"
	first, err := r.Put(Put{Kind: KindPlan, Title: "plan", Ref: ref, Content: v1, Actor: "owner", At: t0})
	if err != nil {
		t.Fatal(err)
	}
	before := first.Artifact.Revisions[0]

	// resolving by ref (no id): the artifact at that path takes the new bytes
	second, err := r.Put(Put{Ref: ref, Content: v2, Actor: "agent:hermes", Note: "tightened", At: t0.Add(time.Hour),
		Provenance: Provenance{Source: "run", Run: "r9", Inputs: []string{"abc"}}})
	if err != nil {
		t.Fatal(err)
	}
	a := second.Artifact
	if second.Created || !second.Changed || a.ID != first.Artifact.ID || a.Version() != 2 {
		t.Fatalf("revise: %+v", second)
	}
	if second.Revision.N != 2 || second.Revision.Parent != Hash(v1) || second.Revision.Hash != Hash(v2) || second.Revision.Note != "tightened" {
		t.Fatalf("chain: %+v", second.Revision)
	}
	if a.Head != Hash(v2) || a.Kind != KindPlan || a.Title != "plan" || a.Actor != "owner" || a.Created != t0 {
		t.Fatalf("head/identity drifted: %+v", a)
	}
	if a.Revisions[0] != before {
		t.Fatalf("revision 1 mutated:\n%+v\n%+v", before, a.Revisions[0])
	}
	if a.Provenance.Run != "r9" || strings.Join(a.Provenance.Inputs, ",") != "abc" {
		t.Fatalf("provenance merge: %+v", a.Provenance)
	}
	if b, _ := r.Content(Hash(v1)); string(b) != "draft one\n" {
		t.Fatalf("old bytes gone: %q", b)
	}
	if b, _ := r.Content(a.Head); string(b) != "draft two\n" {
		t.Fatalf("new bytes: %q", b)
	}
	// identical bytes again: no third revision
	third, err := r.Put(Put{ID: a.ID, Content: v2, At: t0.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if third.Changed || third.Artifact.Version() != 2 {
		t.Fatalf("same bytes must not version: %+v", third)
	}
	// by explicit id, the third version — Provenance never unlearns
	fourth, err := r.Put(Put{ID: a.ID, Content: []byte("draft three\n"), At: t0.Add(3 * time.Hour), Provenance: Provenance{Task: "inbox/x"}})
	if err != nil {
		t.Fatal(err)
	}
	if fourth.Artifact.Version() != 3 || fourth.Revision.Parent != Hash(v2) || fourth.Artifact.Provenance.Run != "r9" || fourth.Artifact.Provenance.Task != "inbox/x" {
		t.Fatalf("v3: %+v", fourth.Artifact)
	}
	// unknown id refuses rather than creating
	if _, err := r.Put(Put{ID: strings.Repeat("0", idLen), Content: v1}); err == nil {
		t.Fatal("unknown id accepted")
	}
	if _, err := r.Put(Put{ID: "../x", Content: v1}); err == nil {
		t.Fatal("traversing id accepted")
	}
	// the file is the truth: one object file, whole chain inside, no .tmp left
	got, ok := r.Get(a.ID)
	if !ok || got.Version() != 3 || got.Revisions[1].Parent != Hash(v1) {
		t.Fatalf("reread: %+v", got)
	}
	matches, _ := filepath.Glob(filepath.Join(r.dir, "*"))
	if len(matches) != 1 || !strings.HasSuffix(matches[0], a.ID+".json") {
		t.Fatalf("object files: %v", matches)
	}
}

func TestRegistryReadSurface(t *testing.T) {
	r, _ := registry(t)
	mk := func(kind, ref, body, task string, at time.Time) Artifact {
		t.Helper()
		res, err := r.Put(Put{Kind: kind, Harness: "hermes", Ref: ref, Content: []byte(body), At: at, Provenance: Provenance{Task: task}})
		if err != nil {
			t.Fatal(err)
		}
		return res.Artifact
	}
	old := mk(KindBrief, "artifacts/library/a.md", "a", "inbox/a", t0)
	newer := mk(KindReport, "artifacts/library/b.md", "b", "inbox/b", t0.Add(time.Hour))
	mk(KindBrief, "artifacts/library/c.md", "c", "inbox/a", t0.Add(2*time.Hour))

	all := r.List(Filter{})
	if len(all) != 3 || all[0].Ref != "artifacts/library/c.md" || all[2].ID != old.ID {
		t.Fatalf("newest first: %+v", all)
	}
	if got := r.List(Filter{Kind: KindBrief}); len(got) != 2 {
		t.Fatalf("kind filter: %d", len(got))
	}
	if got := r.List(Filter{Task: "inbox/a"}); len(got) != 2 {
		t.Fatalf("task filter: %d", len(got))
	}
	if got := r.List(Filter{Ref: "artifacts/library/b.md"}); len(got) != 1 || got[0].ID != newer.ID {
		t.Fatalf("ref filter: %+v", got)
	}
	if got := r.List(Filter{Harness: "excalibur"}); len(got) != 0 {
		t.Fatalf("harness filter: %d", len(got))
	}
	if a, ok := r.ByRef("hermes", "artifacts/library/b.md"); !ok || a.ID != newer.ID {
		t.Fatalf("ByRef: %+v %v", a, ok)
	}
	if _, ok := r.ByRef("", "artifacts/library/b.md"); ok {
		t.Fatal("ByRef must scope to the harness")
	}
	if _, ok := r.Get("nope"); ok {
		t.Fatal("bad id resolved")
	}
	// a stray file in objects/ is ignored, never a crash
	os.WriteFile(filepath.Join(r.dir, "junk.json"), []byte("{"), 0o644)
	os.WriteFile(filepath.Join(r.dir, strings.Repeat("f", idLen)+".json"), []byte(`{"id":"mismatch"}`), 0o644)
	if n := len(r.List(Filter{})); n != 3 {
		t.Fatalf("junk counted: %d", n)
	}
}

// The wire/file shape: revisions serialize with their chain, zero provenance
// stays off the wire, and a hand-read object file parses back identically.
func TestArtifactJSONRoundTrip(t *testing.T) {
	r, _ := registry(t)
	res, err := r.Put(Put{Kind: KindDocument, Ref: "artifacts/library/x.md", Content: []byte("x"), At: t0})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.Artifact)
	if strings.Contains(string(b), `"provenance"`) {
		t.Fatalf("zero provenance on the wire: %s", b)
	}
	var back Artifact
	if err := json.Unmarshal(b, &back); err != nil || back.ID != res.Artifact.ID || back.HeadRevision().Hash != res.Artifact.Head {
		t.Fatalf("round trip: %v %+v", err, back)
	}
	raw, _ := os.ReadFile(filepath.Join(r.dir, res.Artifact.ID+".json"))
	var onDisk Artifact
	if err := json.Unmarshal(raw, &onDisk); err != nil || onDisk.Version() != 1 {
		t.Fatalf("object file: %v\n%s", err, raw)
	}
}

// Task → artifact binding, derived: producers are the provenance task plus
// any task listing the artifact as an output; consumers list it as an input.
func TestLinkIndexAndTaskArtifacts(t *testing.T) {
	arts := []Artifact{
		{ID: "a111", Provenance: Provenance{Task: "inbox/research"}},
		{ID: "b222"},
	}
	bindings := []Binding{
		{Task: "inbox/research", Outputs: []string{"a111", " a111 ", "c333"}},
		{Task: "inbox/write-memo", Inputs: []string{"a111", "b222"}, Outputs: []string{"b222"}},
		{Task: "", Inputs: []string{"a111"}}, // no task — ignored
	}
	idx := LinkIndex(bindings, arts)
	if strings.Join(idx["a111"].Producers, ",") != "inbox/research" || strings.Join(idx["a111"].Consumers, ",") != "inbox/write-memo" {
		t.Fatalf("a111: %+v", idx["a111"])
	}
	if strings.Join(idx["b222"].Producers, ",") != "inbox/write-memo" || strings.Join(idx["b222"].Consumers, ",") != "inbox/write-memo" {
		t.Fatalf("b222: %+v", idx["b222"])
	}
	// bound but unknown to the registry: still linked, never dropped
	if strings.Join(idx["c333"].Producers, ",") != "inbox/research" {
		t.Fatalf("c333: %+v", idx["c333"])
	}
	out, in := TaskArtifacts("inbox/write-memo", bindings, arts)
	if strings.Join(out, ",") != "b222" || strings.Join(in, ",") != "a111,b222" {
		t.Fatalf("write-memo: out=%v in=%v", out, in)
	}
	out, in = TaskArtifacts("inbox/research", bindings, arts)
	if strings.Join(out, ",") != "a111,c333" || in != nil {
		t.Fatalf("research: out=%v in=%v", out, in)
	}
	if out, in := TaskArtifacts("", bindings, arts); out != nil || in != nil {
		t.Fatal("empty task must answer nothing")
	}
}

func TestRefAndIDGuards(t *testing.T) {
	for in, want := range map[string]string{
		"artifacts/library/a.md":   "artifacts/library/a.md",
		" artifacts//library/a.md": "artifacts/library/a.md",
		`artifacts\library\a.md`:   "artifacts/library/a.md",
		"../escape.md":             "",
		"/abs/path.md":             "",
		"":                         "",
		".":                        "",
	} {
		if got := cleanRef(in); got != want {
			t.Errorf("cleanRef(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "ABCDEF0123456789", "0123456789abcde", "0123456789abcdefg", "../../etc/passwd"} {
		if ValidID(bad) {
			t.Errorf("ValidID(%q) passed", bad)
		}
	}
	if !ValidID(IDFor("x", "", "", Hash([]byte("x")))) || !ValidHash(Hash([]byte("x"))) {
		t.Fatal("derived ids/hashes must validate")
	}
}
