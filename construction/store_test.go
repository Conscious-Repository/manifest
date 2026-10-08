package construction

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"manifest/artifacts"
)

var fixtureProperty = SubjectRef{Kind: SubjectProperty, ID: "fixture-ooda-house"}
var fixtureOther = SubjectRef{Kind: SubjectProperty, ID: "fixture-second-house"}
var fixtureHome = SubjectRef{Kind: SubjectHome, ID: HomeSubjectID}

func openStore(t *testing.T, root string, o Options) *Store {
	t.Helper()
	if o.Now == nil {
		base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
		var mu sync.Mutex
		n := 0
		o.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); n++; return base.Add(time.Duration(n) * time.Second) }
	}
	s, err := Open(root, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func createBody(t *testing.T, requestID, title string) (*CreateRequest, string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": requestID, "title": title,
		"narrative": "Synthetic fixture: corrugated roof meets a two-wythe masonry wall. Not a real site."})
	req, hash, err := ParseCreate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return req, hash
}

func mustCreate(t *testing.T, s *Store, sub SubjectRef, requestID, title string) *State {
	t.Helper()
	req, hash := createBody(t, requestID, title)
	st, _, err := s.CreateProblem(sub, req, hash, OwnerActor(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func command(t *testing.T, problemID string, body map[string]any) *ParsedCommand {
	t.Helper()
	body["schemaVersion"] = 1
	body["problemId"] = problemID
	raw, _ := json.Marshal(body)
	pc, err := ParseCommand(raw)
	if err != nil {
		t.Fatal(err)
	}
	return pc
}

func headBytes(t *testing.T, s *Store, sub SubjectRef, id string) []byte {
	t.Helper()
	b, err := os.ReadFile(s.headPath(sub, id))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The same problem id, documents and input hash come back from a fresh Store
// on the same root (a restart).
func TestConstructionProblemDurableAcrossReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "construction")
	s := openStore(t, root, Options{})
	st := mustCreate(t, s, fixtureProperty, "create-0001", "Corrugated roof to masonry wall")
	id := st.Problem.ID
	if !ValidID(KindProblem, id) || st.Problem.SubjectRef != fixtureProperty || st.Problem.PropertyRef == nil || *st.Problem.PropertyRef != fixtureProperty {
		t.Fatalf("problem %+v", st.Problem)
	}
	if st.Problem.Steward.Agent != "alfred" || st.Problem.Steward.Explicit {
		t.Fatalf("default steward must be Alfred, not explicit: %+v", st.Problem.Steward)
	}
	if st.Problem.Climate.State != StateUnknown || st.Problem.Jurisdiction.Provenance != ProvUnknown {
		t.Fatal("context must start unknown")
	}
	st2, _, err := s.RetainInput(fixtureProperty, id, InputUpload{RequestID: "input-0001", ExpectedProblemRevision: st.Revision("problem"),
		Name: "synthetic-photo.png", Mime: "image/png", Role: "photo", Content: []byte("\x89PNG synthetic fixture bytes")}, OwnerActor())
	if err != nil {
		t.Fatal(err)
	}
	in := st2.Problem.Inputs[0]
	if in.Verification != "not-field-verified" || in.Revision != Token([]byte("\x89PNG synthetic fixture bytes")) {
		t.Fatalf("input %+v", in)
	}
	s.Close()
	s2 := openStore(t, root, Options{})
	again, err := s2.Load(fixtureProperty, id)
	if err != nil {
		t.Fatal(err)
	}
	if again.Problem.ID != id || again.Revision("problem") != st2.Revision("problem") || again.Problem.Inputs[0].Revision != in.Revision {
		t.Fatalf("reopen mismatch: %+v", again.Problem)
	}
	b, err := s2.Content(fixtureProperty, id, in.ArtifactID, in.Revision)
	if err != nil || string(b) != "\x89PNG synthetic fixture bytes" {
		t.Fatalf("content %q %v", b, err)
	}
	list, err := s2.List(fixtureProperty)
	if err != nil || len(list) != 1 || list[0].ID != id || list[0].Inputs != 1 {
		t.Fatalf("list %+v %v", list, err)
	}
}

// A replayed create (lost ACK) yields the same problem; reusing the request id
// for a different body is a conflict.
func TestConstructionReplayCreateAndCommand(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "c"), Options{})
	a := mustCreate(t, s, fixtureProperty, "create-replay", "Roof to wall")
	b := mustCreate(t, s, fixtureProperty, "create-replay", "Roof to wall")
	if a.Problem.ID != b.Problem.ID || b.Head.Generation != 1 {
		t.Fatalf("replay created a second problem or generation: %s vs %s gen %d", a.Problem.ID, b.Problem.ID, b.Head.Generation)
	}
	req, hash := createBody(t, "create-replay", "Different title")
	if _, _, err := s.CreateProblem(fixtureProperty, req, hash, OwnerActor(), nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("reused request id with new content: %v", err)
	}
	pc := command(t, a.Problem.ID, map[string]any{"requestId": "cmd-title-1", "expectedProblemRevision": a.Revision("problem"),
		"operations": []any{map[string]any{"op": "SetProblemText", "title": "Roof to wall (edited)"}}})
	st1, rc1, err := s.ExecuteCommand(fixtureProperty, pc, OwnerActor(), nil)
	if err != nil {
		t.Fatal(err)
	}
	st2, rc2, err := s.ExecuteCommand(fixtureProperty, pc, OwnerActor(), nil)
	if err != nil || rc1.ID != rc2.ID || st2.Head.Generation != st1.Head.Generation {
		t.Fatalf("identical replay must return the original receipt: %v %s %s", err, rc1.ID, rc2.ID)
	}
	if len(rc1.Operations) != 1 || string(rc1.Operations[0].Before) != `"Roof to wall"` || string(rc1.Operations[0].After) != `"Roof to wall (edited)"` {
		t.Fatalf("receipt before/after %+v", rc1.Operations)
	}
}

// A stale expected revision is refused with the current token and the head
// bytes are unchanged; so is an unknown operation or an agent attempting an
// owner-only operation.
func TestConstructionRejectedWritesLeaveHead(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "c"), Options{})
	st := mustCreate(t, s, fixtureProperty, "create-stale", "Roof to wall")
	id := st.Problem.ID
	stale := st.Revision("problem")
	if _, _, err := s.ExecuteCommand(fixtureProperty, command(t, id, map[string]any{"requestId": "cmd-a-000", "expectedProblemRevision": stale,
		"operations": []any{map[string]any{"op": "SetProblemText", "narrative": "first edit"}}}), OwnerActor(), nil); err != nil {
		t.Fatal(err)
	}
	before := headBytes(t, s, fixtureProperty, id)
	_, _, err := s.ExecuteCommand(fixtureProperty, command(t, id, map[string]any{"requestId": "cmd-b-000", "expectedProblemRevision": stale,
		"operations": []any{map[string]any{"op": "SetProblemText", "narrative": "second editor"}}}), OwnerActor(), nil)
	var e *Error
	if !errors.As(err, &e) || e.Status != 409 || e.Current["problem"] == "" || e.Current["problem"] == stale {
		t.Fatalf("stale write: %v", err)
	}
	if string(headBytes(t, s, fixtureProperty, id)) != string(before) {
		t.Fatal("a refused write changed the head")
	}
	raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": "cmd-c-000", "problemId": id, "expectedProblemRevision": stale,
		"operations": []any{map[string]any{"op": "RunShell", "cmd": "rm -rf /"}}})
	if _, err := ParseCommand(raw); StatusOf(err) != 422 || !strings.Contains(err.Error(), "unknown operation kind") {
		t.Fatalf("unknown op: %v", err)
	}
	raw, _ = json.Marshal(map[string]any{"schemaVersion": 1, "requestId": "cmd-d-000", "problemId": id, "expectedProblemRevision": stale,
		"operations": []any{map[string]any{"op": "SetProblemText", "title": "x", "approve": true}}})
	if _, err := ParseCommand(raw); StatusOf(err) != 422 {
		t.Fatalf("unknown field: %v", err)
	}
	cur, _ := s.Load(fixtureProperty, id)
	agent := AgentActor("alfred", "cap-test", "")
	_, _, err = s.ExecuteCommand(fixtureProperty, command(t, id, map[string]any{"requestId": "cmd-e-000", "expectedProblemRevision": cur.Revision("problem"),
		"operations": []any{map[string]any{"op": "SetSteward", "agent": "zeck"}}}), agent, nil)
	if StatusOf(err) != 403 {
		t.Fatalf("agent owner-only op: %v", err)
	}
	if string(headBytes(t, s, fixtureProperty, id)) != string(before) {
		t.Fatal("a forbidden write changed the head")
	}
}

// A second Store (a second process in production) on the same root cannot
// write: the flock refuses it while the first holds the writer lock.
func TestConstructionConcurrentSecondWriterRefused(t *testing.T) {
	root := filepath.Join(t.TempDir(), "c")
	a := openStore(t, root, Options{})
	st := mustCreate(t, a, fixtureProperty, "create-writer", "Roof to wall")
	b := openStore(t, root, Options{})
	if _, err := b.Load(fixtureProperty, st.Problem.ID); err != nil {
		t.Fatalf("a second reader can read: %v", err)
	}
	_, _, err := b.ExecuteCommand(fixtureProperty, command(t, st.Problem.ID, map[string]any{"requestId": "cmd-writer-2", "expectedProblemRevision": st.Revision("problem"),
		"operations": []any{map[string]any{"op": "SetProblemText", "title": "second writer"}}}), OwnerActor(), nil)
	if !errors.Is(err, ErrWriterBusy) {
		t.Fatalf("second writer: %v", err)
	}
	a.Close()
	if _, _, err := b.ExecuteCommand(fixtureProperty, command(t, st.Problem.ID, map[string]any{"requestId": "cmd-writer-3", "expectedProblemRevision": st.Revision("problem"),
		"operations": []any{map[string]any{"op": "SetProblemText", "title": "after release"}}}), OwnerActor(), nil); err != nil {
		t.Fatalf("after the first writer released: %v", err)
	}
}

// Concurrent commands in one process serialize: exactly one of two writers
// with the same expected revision wins, the other gets 409.
func TestConstructionConcurrentCAS(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "c"), Options{})
	st := mustCreate(t, s, fixtureProperty, "create-cas", "Roof to wall")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = s.ExecuteCommand(fixtureProperty, command(t, st.Problem.ID, map[string]any{"requestId": fmt.Sprintf("cmd-cas-%04d", i),
				"expectedProblemRevision": st.Revision("problem"),
				"operations":              []any{map[string]any{"op": "SetProblemText", "title": fmt.Sprintf("writer %d", i)}}}), OwnerActor(), nil)
		}(i)
	}
	wg.Wait()
	ok, conflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case StatusOf(err) == 409:
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("ok=%d conflicts=%d", ok, conflicts)
	}
}

// Crash before the head rename: the old head stays valid and a retry of the
// same request commits once. Crash after the rename (lost ACK): the commit is
// durable and a retry returns the original receipt.
func TestConstructionCrashBeforeAndAfterHeadRename(t *testing.T) {
	for _, point := range []string{"after-stage-docs", "after-stage-receipt", "before-head-rename", "after-head-rename"} {
		t.Run(point, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "c")
			s := openStore(t, root, Options{})
			st := mustCreate(t, s, fixtureProperty, "create-crash", "Roof to wall")
			id := st.Problem.ID
			before := headBytes(t, s, fixtureProperty, id)
			s.Close()
			crash := errors.New("simulated crash at " + point)
			armed := true
			s2 := openStore(t, root, Options{Failpoint: func(p string) error {
				if armed && p == point {
					return crash
				}
				return nil
			}})
			pc := command(t, id, map[string]any{"requestId": "cmd-crash-1", "expectedProblemRevision": st.Revision("problem"),
				"operations": []any{map[string]any{"op": "SetProblemText", "title": "Edited before crash"}}})
			if _, _, err := s2.ExecuteCommand(fixtureProperty, pc, OwnerActor(), nil); !errors.Is(err, crash) {
				t.Fatalf("expected crash, got %v", err)
			}
			s2.Close()
			// "restart": a fresh store with no failpoint
			s3 := openStore(t, root, Options{})
			after, err := s3.Load(fixtureProperty, id)
			if err != nil {
				t.Fatal(err)
			}
			if point == "after-head-rename" {
				if after.Problem.Title != "Edited before crash" || after.Head.Generation != 2 {
					t.Fatalf("durable commit lost: %q gen %d", after.Problem.Title, after.Head.Generation)
				}
				rc, err := s3.Receipt(fixtureProperty, id, "cmd-crash-1")
				if err != nil || rc.ResultGeneration != 2 {
					t.Fatalf("receipt after lost ACK: %+v %v", rc, err)
				}
			} else if string(headBytes(t, s3, fixtureProperty, id)) != string(before) || after.Problem.Title != "Roof to wall" {
				t.Fatal("a crash before the rename moved the head")
			}
			st2, rc, err := s3.ExecuteCommand(fixtureProperty, pc, OwnerActor(), nil)
			if err != nil || st2.Head.Generation != 2 || rc.ResultGeneration != 2 || st2.Problem.Title != "Edited before crash" {
				t.Fatalf("retry after crash: gen %d %v", st2.Head.Generation, err)
			}
		})
	}
}

// A lost upload ACK replayed retains the input once.
func TestConstructionReplayedUpload(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "c"), Options{})
	st := mustCreate(t, s, fixtureProperty, "create-upload", "Roof to wall")
	up := InputUpload{RequestID: "upload-0001", ExpectedProblemRevision: st.Revision("problem"), Name: "synthetic-drawing.pdf",
		Mime: "application/pdf", Role: "drawing", Content: []byte("%PDF-1.4 synthetic fixture drawing")}
	a, rca, err := s.RetainInput(fixtureProperty, st.Problem.ID, up, OwnerActor())
	if err != nil {
		t.Fatal(err)
	}
	b, rcb, err := s.RetainInput(fixtureProperty, st.Problem.ID, up, OwnerActor())
	if err != nil || rca.ID != rcb.ID || len(b.Problem.Inputs) != 1 || len(a.Problem.Inputs) != 1 {
		t.Fatalf("replayed upload: %v inputs=%d", err, len(b.Problem.Inputs))
	}
	up.Content = []byte("%PDF-1.4 different bytes")
	if _, _, err := s.RetainInput(fixtureProperty, st.Problem.ID, up, OwnerActor()); StatusOf(err) != 409 {
		t.Fatalf("request id reused for different bytes: %v", err)
	}
}

// Two properties may hold problems with the same title; each is invisible
// from the other's subject, and an input hash grants nothing across them —
// even when identical bytes are deduplicated in the shared private pool.
func TestConstructionBoundaryCrossProject(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "c"), Options{})
	a := mustCreate(t, s, fixtureProperty, "create-a-0001", "Corrugated roof to masonry wall")
	b := mustCreate(t, s, fixtureOther, "create-b-0001", "Corrugated roof to masonry wall")
	if a.Problem.ID == b.Problem.ID {
		t.Fatal("same title must not merge problems")
	}
	if _, err := s.Load(fixtureOther, a.Problem.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project load: %v", err)
	}
	content := []byte("synthetic shared bytes")
	sa, _, err := s.RetainInput(fixtureProperty, a.Problem.ID, InputUpload{RequestID: "upload-a-01", ExpectedProblemRevision: a.Revision("problem"),
		Name: "a.png", Mime: "image/png", Role: "photo", Content: content}, OwnerActor())
	if err != nil {
		t.Fatal(err)
	}
	in := sa.Problem.Inputs[0]
	if _, err := s.Content(fixtureOther, b.Problem.ID, in.ArtifactID, in.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("B read A's input by hash: %v", err)
	}
	if _, err := s.Content(fixtureOther, a.Problem.ID, in.ArtifactID, in.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("A's problem through B's subject: %v", err)
	}
	sb, _, err := s.RetainInput(fixtureOther, b.Problem.ID, InputUpload{RequestID: "upload-b-01", ExpectedProblemRevision: b.Revision("problem"),
		Name: "b.png", Mime: "image/png", Role: "photo", Content: content}, OwnerActor())
	if err != nil {
		t.Fatal(err)
	}
	if sb.Problem.Inputs[0].ArtifactID != in.ArtifactID {
		t.Fatal("identical bytes should deduplicate to one blob")
	}
	if got, err := s.Content(fixtureOther, b.Problem.ID, in.ArtifactID, in.Revision); err != nil || string(got) != string(content) {
		t.Fatalf("B reads its own copy: %v", err)
	}
	// each keeps its own name for the shared bytes
	if sa.Problem.Inputs[0].Name != "a.png" || sb.Problem.Inputs[0].Name != "b.png" {
		t.Fatal("metadata lives on the reference, not the blob")
	}
	// a Home problem is a different subject again
	h := mustCreate(t, s, fixtureHome, "create-h-0001", "Synthetic Home back addition")
	if h.Problem.PropertyRef != nil || h.Problem.SubjectRef != fixtureHome {
		t.Fatalf("home problem %+v", h.Problem)
	}
	if _, err := s.Load(fixtureProperty, h.Problem.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("home problem visible from a property")
	}
}

// Unknown extensions survive an edit; a newer schema opens read-only with
// exact bytes; a corrupt document is reported, never reset.
func TestConstructionSchemaExtensionsReadOnlyAndCorrupt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "c")
	s := openStore(t, root, Options{})
	st := mustCreate(t, s, fixtureProperty, "create-schema", "Roof to wall")
	id := st.Problem.ID
	// inject an extension through a commit (as a future writer would)
	st2, _, err := s.Commit(fixtureProperty, id, CommitRequest{RequestID: "ext-0001", PayloadHash: Token([]byte("ext")), Actor: OwnerActor()}, func(tx *Tx) error {
		tx.Next.Problem.Extensions = map[string]json.RawMessage{"vendor.future": json.RawMessage(`{"keep":[1,2,3]}`)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st3, _, err := s.ExecuteCommand(fixtureProperty, command(t, id, map[string]any{"requestId": "ext-edit-01", "expectedProblemRevision": st2.Revision("problem"),
		"operations": []any{map[string]any{"op": "SetProblemText", "title": "Edited with extension present"}}}), OwnerActor(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(st3.Problem.Extensions["vendor.future"]) != `{"keep":[1,2,3]}` {
		t.Fatalf("extension lost: %s", st3.Problem.Extensions["vendor.future"])
	}
	// newer schema: rewrite the problem doc bytes under a new artifact and
	// point the head at it (an offline newer writer)
	var m map[string]any
	json.Unmarshal(st3.Raw["problem"], &m)
	m["schemaVersion"] = 2
	m["futureField"] = "unknown to v1"
	newer, _ := Canonical(m)
	res, err := s.reg.Retain(putFor(newer))
	if err != nil {
		t.Fatal(err)
	}
	h, _ := s.ReadHead(fixtureProperty, id)
	h.Docs["problem"] = DocRef{Kind: DocProblem, ArtifactID: res.Artifact.ID, Revision: Token(newer), RevisionNumber: 9}
	hb, _ := json.Marshal(h)
	if err := writeAtomic(s.headPath(fixtureProperty, id), hb); err != nil {
		t.Fatal(err)
	}
	ro, err := s.Load(fixtureProperty, id)
	if err != nil || !ro.ReadOnly || string(ro.Raw["problem"]) != string(newer) {
		t.Fatalf("newer schema must open read-only with exact bytes: %v ro=%v", err, ro != nil && ro.ReadOnly)
	}
	if _, _, err := s.ExecuteCommand(fixtureProperty, command(t, id, map[string]any{"requestId": "ro-edit-001", "expectedProblemRevision": Token(newer),
		"operations": []any{map[string]any{"op": "SetProblemText", "title": "must refuse"}}}), OwnerActor(), nil); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("edit of read-only problem: %v", err)
	}
	// corrupt: flip the stored bytes of the catalog document
	cat := h.Docs["catalog"]
	blob := s.pool.BlobPath(cat.Revision)
	if err := os.WriteFile(blob, []byte(`{"tampered":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s.rawCache = map[string][]byte{}
	if _, err := s.Load(fixtureProperty, id); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("tampered document: %v", err)
	}
	if b, _ := os.ReadFile(blob); string(b) != `{"tampered":true}` {
		t.Fatal("corrupt document must not be rewritten")
	}
}

func putFor(b []byte) artifacts.Put {
	return artifacts.Put{Kind: DocProblem, Harness: "construction", Title: "problem", Content: b}
}

func mathNaN() float64 { return math.NaN() }

// Directories are 0700 and records 0600; the head and membership refuse a
// symlink planted in their place.
func TestConstructionBoundaryPermissionsAndSymlinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "c")
	s := openStore(t, root, Options{})
	st := mustCreate(t, s, fixtureProperty, "create-perm", "Roof to wall")
	id := st.Problem.ID
	for _, d := range []string{root, s.projectDir(fixtureProperty), s.problemDir(fixtureProperty, id), filepath.Join(root, "artifacts", "objects")} {
		fi, err := os.Stat(d)
		if err != nil || fi.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode %v %v", d, fi.Mode(), err)
		}
	}
	for _, f := range []string{s.headPath(fixtureProperty, id), s.membersPath(fixtureProperty, id)} {
		fi, err := os.Stat(f)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode %v %v", f, fi.Mode(), err)
		}
	}
	elsewhere := filepath.Join(t.TempDir(), "evil.json")
	os.WriteFile(elsewhere, headBytes(t, s, fixtureProperty, id), 0o600)
	os.Remove(s.headPath(fixtureProperty, id))
	if err := os.Symlink(elsewhere, s.headPath(fixtureProperty, id)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(fixtureProperty, id); err == nil {
		t.Fatal("a symlinked head must be refused")
	}
	if _, err := Open(t.TempDir()+"/relative/../x", Options{}); err == nil {
		t.Fatal("unclean root accepted")
	}
	if _, err := Open("relative/construction", Options{}); err == nil {
		t.Fatal("relative root accepted")
	}
	vault := t.TempDir()
	if _, err := Open(filepath.Join(vault, "data", "construction"), Options{Forbidden: []string{vault}}); err == nil {
		t.Fatal("store under the vault accepted")
	}
}

// The ledger is never commit authority: a failed append leaves the event
// pending, and reconciliation publishes it once, in order.
func TestConstructionLedgerPendingAndReconcile(t *testing.T) {
	var mu sync.Mutex
	var published []string
	failing := true
	pub := func(sub SubjectRef, id string, gen int64, ev []LedgerEvent) error {
		mu.Lock()
		defer mu.Unlock()
		if failing {
			return errors.New("ledger down")
		}
		for _, e := range ev {
			published = append(published, fmt.Sprintf("%d:%s", gen, e.Kind))
		}
		return nil
	}
	s := openStore(t, filepath.Join(t.TempDir(), "c"), Options{Publish: pub})
	st := mustCreate(t, s, fixtureProperty, "create-ledger", "Roof to wall")
	if _, _, err := s.ExecuteCommand(fixtureProperty, command(t, st.Problem.ID, map[string]any{"requestId": "cmd-ledger-1", "expectedProblemRevision": st.Revision("problem"),
		"operations": []any{map[string]any{"op": "SetProblemText", "title": "x"}}}), OwnerActor(), nil); err != nil {
		t.Fatalf("commit must succeed while the ledger is down: %v", err)
	}
	if s.publishedGeneration(fixtureProperty, st.Problem.ID) != 0 {
		t.Fatal("nothing should be marked published")
	}
	mu.Lock()
	failing = false
	mu.Unlock()
	if err := s.ReconcileLedger(fixtureProperty, st.Problem.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileLedger(fixtureProperty, st.Problem.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Join(published, ",") != "1:construction.problem.created,2:construction.command" {
		t.Fatalf("published %v", published)
	}
}

// Problem-level operations validate their input and record before/after.
func TestConstructionProblemOperations(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "c"), Options{})
	st := mustCreate(t, s, fixtureProperty, "create-ops", "Roof to wall")
	id := st.Problem.ID
	fact := NewID(KindClaim)
	resolved := ScopeRef{WorkID: "w-roof", TaskID: "t-roof", SourceRevision: strings.Repeat("a", 64)}
	ctx := &ApplyContext{ResolveScope: func(s ScopeRef) (ScopeRef, error) {
		if s.WorkID != "w-roof" {
			return ScopeRef{}, Invalid("unresolved work id")
		}
		return resolved, nil
	}}
	st2, rc, err := s.ExecuteCommand(fixtureProperty, command(t, id, map[string]any{"requestId": "cmd-ops-01", "expectedProblemRevision": st.Revision("problem"),
		"operations": []any{
			map[string]any{"op": "AddFact", "id": fact, "list": "existing", "text": "Two nominal 100 mm wythes (synthetic).", "provenance": "user-assumption"},
			map[string]any{"op": "SetContext", "field": "climate", "text": "", "state": "unknown", "provenance": "unknown"},
			map[string]any{"op": "SetScope", "workId": "w-roof", "taskId": "t-roof"},
			map[string]any{"op": "SetSteward", "agent": "zeck"},
		}}), OwnerActor(), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st2.Problem.Existing) != 1 || st2.Problem.ScopeRef == nil || *st2.Problem.ScopeRef != resolved || !st2.Problem.Steward.Explicit {
		t.Fatalf("ops not applied: %+v", st2.Problem)
	}
	if len(rc.Operations) != 4 {
		t.Fatalf("receipt ops %d", len(rc.Operations))
	}
	if _, _, err := s.ExecuteCommand(fixtureProperty, command(t, id, map[string]any{"requestId": "cmd-ops-02", "expectedProblemRevision": st2.Revision("problem"),
		"operations": []any{map[string]any{"op": "SetScope", "workId": "renamed-node"}}}), OwnerActor(), ctx); StatusOf(err) != 422 {
		t.Fatalf("unresolved scope must be refused, never fuzzy-matched: %v", err)
	}
	raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "requestId": "cmd-ops-03", "problemId": id, "expectedProblemRevision": st2.Revision("problem"),
		"operations": []any{map[string]any{"op": "SetLifecycle", "lifecycle": "owner-selected"}}})
	if _, err := ParseCommand(raw); StatusOf(err) != 422 {
		t.Fatalf("owner-selected must come only from a decision: %v", err)
	}
}

// Canonical encoding is stable and refuses non-finite numbers.
func TestConstructionCanonicalEncoding(t *testing.T) {
	a, err := CanonicalizeJSON([]byte(`{"b":1.50,"a":[3,2,1],"c":{"z":-0,"y":"<&>"},"d":1e2}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != `{"a":[3,2,1],"b":1.5,"c":{"y":"<&>","z":0},"d":100}` {
		t.Fatalf("canonical %s", a)
	}
	if _, err := CanonicalizeJSON([]byte(`{"x":1e999}`)); err == nil {
		t.Fatal("overflow accepted")
	}
	if _, err := Canonical(map[string]float64{"x": mathNaN()}); !errors.Is(err, ErrNonFinite) {
		t.Fatalf("NaN: %v", err)
	}
	for _, id := range []string{NewID(KindProblem), NewID(KindComponent)} {
		if !ValidID("", id) {
			t.Fatal(id)
		}
	}
	if ValidID(KindProblem, "cp-"+strings.Repeat("A", 32)) || ValidID("", "zz-"+strings.Repeat("a", 32)) || ValidID(KindProblem, "cp-../../etc") {
		t.Fatal("bad ids accepted")
	}
}
