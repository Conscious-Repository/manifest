package construction

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"manifest/artifacts"
)

// Head is the only mutable file of a problem: a pointer to the complete,
// immutable revision closure of the problem at one generation, plus the
// request-id index that makes every mutation replay-safe.
type Head struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Kind          string                  `json:"kind"`
	ProblemID     string                  `json:"problemId"`
	Subject       SubjectRef              `json:"subject"`
	Generation    int64                   `json:"generation"`
	Docs          map[string]DocRef       `json:"docs"`
	Commit        DocRef                  `json:"commit"`
	Receipts      map[string]ReceiptIndex `json:"receipts"`
	UpdatedAt     string                  `json:"updatedAt"`
}

// DocRef names one immutable document revision in the private registry.
type DocRef struct {
	Kind           string `json:"kind"`
	ArtifactID     string `json:"artifactId"`
	Revision       string `json:"revision"`
	RevisionNumber int    `json:"revisionNumber"`
}

// ReceiptIndex maps a request id to the one logical commit it produced.
type ReceiptIndex struct {
	PayloadHash string `json:"payloadHash"`
	Generation  int64  `json:"generation"`
	Receipt     string `json:"receipt"`
	ReceiptID   string `json:"receiptId"`
}

// State is a problem's decoded closure at one generation.
type State struct {
	Head       Head
	ReadOnly   bool
	Problem    *Problem
	Assemblies map[string]*Assembly
	Validation map[string]*ValidationReport
	Decisions  map[string]*Decision
	Runs       map[string]*ResearchRun
	Views      map[string]*View
	Catalog    *Catalog
	Evidence   *EvidenceBundle
	Derived    *DerivedBundle
	// Raw holds the exact stored bytes of every document (newer-schema ones
	// included) so a read-only problem can still be exported byte-for-byte.
	Raw map[string][]byte
}

// Revision is the stored token of a document key ("" if absent).
func (st *State) Revision(key string) string { return st.Head.Docs[key].Revision }

// Options configure a Store.
type Options struct {
	// Forbidden roots (vault, public/share trees): the store refuses to live
	// under any of them, after symlink resolution.
	Forbidden []string
	Now       func() time.Time
	// Failpoint is a test hook called at named commit stages; a non-nil
	// error aborts there, simulating a crash at that point.
	Failpoint func(point string) error
	// Publish receives a commit's activity projection after the head is
	// durable. A failure leaves the events pending; ReconcileLedger retries.
	Publish func(subject SubjectRef, problemID string, generation int64, events []LedgerEvent) error
}

// Store is the construction domain's durable record store.
type Store struct {
	root    string
	pool    *artifacts.Store
	reg     *artifacts.Registry
	now     func() time.Time
	fail    func(string) error
	publish func(SubjectRef, string, int64, []LedgerEvent) error

	mu     sync.Mutex // serializes commits in this process
	lockMu sync.Mutex
	lockF  *os.File // the single-writer flock, held once acquired

	rawMu    sync.Mutex
	rawCache map[string][]byte // immutable document bytes by revision token
}

// Open opens (creating if needed) the store at root. root must be absolute,
// must not be a symlink, and must resolve outside every forbidden root.
func Open(root string, o Options) (*Store, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, fmt.Errorf("construction: store root must be an absolute clean path")
	}
	if fi, err := os.Lstat(root); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			return nil, fmt.Errorf("construction: store root %s must be a real directory", root)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	// the construction root is construction-owned: hold it to owner-only
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if f, err := forbiddenRootOf(resolved, o.Forbidden); err != nil {
		return nil, fmt.Errorf("construction: forbidden root %s cannot be resolved: %w", f, err)
	} else if f != "" {
		return nil, fmt.Errorf("construction: store root %s lies under forbidden root %s", root, f)
	}
	pool, err := artifacts.NewWithOptions(filepath.Join(root, "artifacts"), artifacts.Options{Private: true})
	if err != nil {
		return nil, err
	}
	reg, err := artifacts.NewRegistry(pool)
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, pool: pool, reg: reg, now: o.Now, fail: o.Failpoint, publish: o.Publish, rawCache: map[string][]byte{}}
	if s.now == nil {
		s.now = time.Now
	}
	return s, nil
}

// Root is the store's directory.
func (s *Store) Root() string { return s.root }

// Registry exposes the private artifact registry to in-package callers
// (exports/restore) and tests.
func (s *Store) Registry() *artifacts.Registry { return s.reg }

// Close releases the single-writer lock.
func (s *Store) Close() error {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	if s.lockF != nil {
		err := s.lockF.Close()
		s.lockF = nil
		return err
	}
	return nil
}

// acquireWriter takes the process-exclusive writer lock (flock) on first
// mutation and holds it for the life of the Store. A second writer — another
// process, or a second Store on the same root — is refused.
func (s *Store) acquireWriter() error {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	if s.lockF != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(s.root, ".writer.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrWriterBusy
		}
		return err
	}
	s.lockF = f
	return nil
}

// ---- paths -------------------------------------------------------------------------

// ProjectKey is the path key of a subject: SHA-256(kind NUL id). It is only
// a path; records keep the exact subject reference.
func ProjectKey(sub SubjectRef) string {
	sum := sha256.Sum256([]byte(sub.Kind + "\x00" + sub.ID))
	return hex.EncodeToString(sum[:])
}

func (s *Store) projectDir(sub SubjectRef) string {
	return filepath.Join(s.root, "projects", ProjectKey(sub))
}

func (s *Store) problemDir(sub SubjectRef, id string) string {
	return filepath.Join(s.projectDir(sub), "problems", id)
}

func (s *Store) headPath(sub SubjectRef, id string) string {
	return filepath.Join(s.problemDir(sub, id), "head.json")
}

// readNoFollow reads a file, refusing a symlink at the final component.
func readNoFollow(p string) ([]byte, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("construction: %s is not a regular file", p)
	}
	var b bytes.Buffer
	if _, err := b.ReadFrom(f); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// writeAtomic writes b to p via a synced temp file, rename, directory sync.
func writeAtomic(p string, b []byte) error {
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(f.Name(), p); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// ---- reading ------------------------------------------------------------------------

// ReadHead reads a problem's head (ErrNotFound when absent).
func (s *Store) ReadHead(sub SubjectRef, id string) (Head, error) {
	if err := ValidSubject(sub); err != nil {
		return Head{}, err
	}
	if !ValidID(KindProblem, id) {
		return Head{}, NotFound("no such construction problem")
	}
	for _, d := range []string{s.projectDir(sub), s.problemDir(sub, id)} {
		if fi, err := os.Lstat(d); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return Head{}, corrupt("%s is a symlink", d)
		}
	}
	raw, err := readNoFollow(s.headPath(sub, id))
	if errors.Is(err, os.ErrNotExist) {
		return Head{}, NotFound("no such construction problem")
	}
	if err != nil {
		return Head{}, err
	}
	var h Head
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&h); err != nil {
		return Head{}, corrupt("head %s: %v", id, err)
	}
	if h.Kind != DocHead || h.ProblemID != id || h.Subject != sub {
		return Head{}, corrupt("head %s does not belong to this subject", id)
	}
	if h.SchemaVersion > SchemaVersion {
		return h, ErrNewerSchema
	}
	return h, nil
}

// docBytes reads an immutable document by its token, verifying the hash.
func (s *Store) docBytes(ref DocRef) ([]byte, error) {
	if !ValidToken(ref.Revision) {
		return nil, corrupt("bad revision token")
	}
	s.rawMu.Lock()
	if b, ok := s.rawCache[ref.Revision]; ok {
		s.rawMu.Unlock()
		return b, nil
	}
	s.rawMu.Unlock()
	b, err := s.reg.Content(ref.Revision)
	if err != nil {
		return nil, corrupt("document %s missing: %v", ref.Revision[:12], err)
	}
	if Token(b) != ref.Revision {
		return nil, corrupt("document %s bytes do not match their revision", ref.Revision[:12])
	}
	s.rawMu.Lock()
	if len(s.rawCache) > 4096 {
		s.rawCache = map[string][]byte{}
	}
	s.rawCache[ref.Revision] = b
	s.rawMu.Unlock()
	return b, nil
}

// Load reads and verifies a problem's complete closure.
func (s *Store) Load(sub SubjectRef, id string) (*State, error) {
	h, err := s.ReadHead(sub, id)
	readOnly := errors.Is(err, ErrNewerSchema)
	if err != nil && !readOnly {
		return nil, err
	}
	st := newState(h)
	st.ReadOnly = readOnly
	keys := make([]string, 0, len(h.Docs))
	for k := range h.Docs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ref := h.Docs[key]
		raw, err := s.docBytes(ref)
		if err != nil {
			return nil, err
		}
		st.Raw[key] = raw
		if err := st.decode(key, ref.Kind, raw); err != nil {
			if errors.Is(err, ErrNewerSchema) {
				st.ReadOnly = true
				continue
			}
			return nil, err
		}
	}
	if st.Problem == nil && !st.ReadOnly {
		return nil, corrupt("problem %s has no problem document", id)
	}
	return st, nil
}

func newState(h Head) *State {
	if h.Docs == nil {
		h.Docs = map[string]DocRef{}
	}
	if h.Receipts == nil {
		h.Receipts = map[string]ReceiptIndex{}
	}
	return &State{Head: h, Assemblies: map[string]*Assembly{}, Validation: map[string]*ValidationReport{},
		Decisions: map[string]*Decision{}, Runs: map[string]*ResearchRun{}, Views: map[string]*View{}, Raw: map[string][]byte{}}
}

// docKindFor maps a closure key to its document kind.
func docKindFor(key string) string {
	switch {
	case key == "problem":
		return DocProblem
	case key == "catalog":
		return DocCatalog
	case key == "evidence":
		return DocEvidence
	case key == "derived":
		return DocDerived
	case strings.HasPrefix(key, "assembly:"):
		return DocAssembly
	case strings.HasPrefix(key, "validation:"):
		return DocValidation
	case strings.HasPrefix(key, "decision:"):
		return DocDecision
	case strings.HasPrefix(key, "run:"):
		return DocRun
	case strings.HasPrefix(key, "view:"):
		return DocView
	}
	return ""
}

func (st *State) decode(key, kind string, raw []byte) error {
	if docKindFor(key) != kind {
		return corrupt("closure key %s holds a %s", key, kind)
	}
	suffix := ""
	if i := strings.IndexByte(key, ':'); i >= 0 {
		suffix = key[i+1:]
	}
	switch kind {
	case DocProblem:
		var p Problem
		if err := DecodeStrict(raw, kind, &p); err != nil {
			return err
		}
		st.Problem = &p
	case DocCatalog:
		var c Catalog
		if err := DecodeStrict(raw, kind, &c); err != nil {
			return err
		}
		st.Catalog = &c
	case DocEvidence:
		var e EvidenceBundle
		if err := DecodeStrict(raw, kind, &e); err != nil {
			return err
		}
		st.Evidence = &e
	case DocDerived:
		var d DerivedBundle
		if err := DecodeStrict(raw, kind, &d); err != nil {
			return err
		}
		st.Derived = &d
	case DocAssembly:
		var a Assembly
		if err := DecodeStrict(raw, kind, &a); err != nil {
			return err
		}
		if a.ID != suffix {
			return corrupt("assembly key %s holds %s", key, a.ID)
		}
		st.Assemblies[a.ID] = &a
	case DocValidation:
		var v ValidationReport
		if err := DecodeStrict(raw, kind, &v); err != nil {
			return err
		}
		st.Validation[v.AssemblyID] = &v
	case DocDecision:
		var d Decision
		if err := DecodeStrict(raw, kind, &d); err != nil {
			return err
		}
		st.Decisions[d.ID] = &d
	case DocRun:
		var r ResearchRun
		if err := DecodeStrict(raw, kind, &r); err != nil {
			return err
		}
		st.Runs[r.ID] = &r
	case DocView:
		var v View
		if err := DecodeStrict(raw, kind, &v); err != nil {
			return err
		}
		st.Views[v.ID] = &v
	default:
		return corrupt("unknown document kind %s", kind)
	}
	return nil
}

// docs enumerates every document of a state by closure key.
func (st *State) docs() map[string]any {
	out := map[string]any{}
	if st.Problem != nil {
		out["problem"] = st.Problem
	}
	if st.Catalog != nil {
		out["catalog"] = st.Catalog
	}
	if st.Evidence != nil {
		out["evidence"] = st.Evidence
	}
	if st.Derived != nil {
		out["derived"] = st.Derived
	}
	for id, a := range st.Assemblies {
		out["assembly:"+id] = a
	}
	for id, v := range st.Validation {
		out["validation:"+id] = v
	}
	for id, d := range st.Decisions {
		out["decision:"+id] = d
	}
	for id, r := range st.Runs {
		out["run:"+id] = r
	}
	for id, v := range st.Views {
		out["view:"+id] = v
	}
	return out
}

// envelopeOf returns a pointer to a document's envelope.
func envelopeOf(doc any) *Envelope {
	switch d := doc.(type) {
	case *Problem:
		return &d.Envelope
	case *Catalog:
		return &d.Envelope
	case *EvidenceBundle:
		return &d.Envelope
	case *DerivedBundle:
		return &d.Envelope
	case *Assembly:
		return &d.Envelope
	case *ValidationReport:
		return &d.Envelope
	case *Decision:
		return &d.Envelope
	case *ResearchRun:
		return &d.Envelope
	case *View:
		return &d.Envelope
	}
	return nil
}

// Clone deep-copies a state (JSON round trip of every document).
func (st *State) Clone() (*State, error) {
	out := newState(cloneHead(st.Head))
	out.ReadOnly = st.ReadOnly
	for key, doc := range st.docs() {
		b, err := json.Marshal(doc)
		if err != nil {
			return nil, err
		}
		if err := out.decode(key, docKindFor(key), b); err != nil {
			return nil, err
		}
	}
	for k, v := range st.Raw {
		out.Raw[k] = v
	}
	return out, nil
}

func cloneHead(h Head) Head {
	out := h
	out.Docs = map[string]DocRef{}
	for k, v := range h.Docs {
		out.Docs[k] = v
	}
	out.Receipts = map[string]ReceiptIndex{}
	for k, v := range h.Receipts {
		out.Receipts[k] = v
	}
	return out
}

// ---- listing ---------------------------------------------------------------------------

// ProblemSummary is one row of a subject's problem list.
type ProblemSummary struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Lifecycle    string `json:"lifecycle"`
	UpdatedAt    string `json:"updatedAt"`
	Generation   int64  `json:"generation"`
	Alternatives int    `json:"alternatives"`
	Inputs       int    `json:"inputs"`
	ReadOnly     bool   `json:"readOnly,omitempty"`
	Error        string `json:"error,omitempty"`
}

// List returns every problem bound to a subject, most recently updated first.
// A problem that fails to load is listed with its error, never hidden.
func (s *Store) List(sub SubjectRef) ([]ProblemSummary, error) {
	if err := ValidSubject(sub); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(s.projectDir(sub), "problems"))
	if errors.Is(err, os.ErrNotExist) {
		return []ProblemSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []ProblemSummary{}
	for _, e := range entries {
		if !e.IsDir() || !ValidID(KindProblem, e.Name()) {
			continue
		}
		st, err := s.Load(sub, e.Name())
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue // a creation that never reached its head
			}
			out = append(out, ProblemSummary{ID: e.Name(), Error: err.Error()})
			continue
		}
		row := ProblemSummary{ID: e.Name(), Generation: st.Head.Generation, ReadOnly: st.ReadOnly}
		row.UpdatedAt = st.Head.UpdatedAt
		if p := st.Problem; p != nil {
			row.Title, row.Lifecycle = p.Title, p.Lifecycle
			row.Alternatives, row.Inputs = len(p.Alternatives), len(p.Inputs)
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// ---- membership (the construction ACL) -------------------------------------------------

type member struct {
	ArtifactID string `json:"artifactId"`
	Revision   string `json:"revision"`
}

func (s *Store) membersPath(sub SubjectRef, id string) string {
	return filepath.Join(s.problemDir(sub, id), "members.jsonl")
}

// appendMembers durably records artifact revisions this problem may read.
// It runs before the head rename, so a crashed commit can only add members
// that belong to this same problem.
func (s *Store) appendMembers(sub SubjectRef, id string, ms []member) error {
	if len(ms) == 0 {
		return nil
	}
	have, _ := s.members(sub, id)
	var b bytes.Buffer
	for _, m := range ms {
		if have[m.ArtifactID+"@"+m.Revision] {
			continue
		}
		have[m.ArtifactID+"@"+m.Revision] = true
		line, _ := json.Marshal(m)
		b.Write(line)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return nil
	}
	p := s.membersPath(sub, id)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(b.Bytes())
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return syncDir(filepath.Dir(p))
}

func (s *Store) members(sub SubjectRef, id string) (map[string]bool, error) {
	out := map[string]bool{}
	raw, err := readNoFollow(s.membersPath(sub, id))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		var m member
		if json.Unmarshal(sc.Bytes(), &m) == nil && m.ArtifactID != "" && ValidToken(m.Revision) {
			out[m.ArtifactID+"@"+m.Revision] = true
		}
	}
	return out, nil
}

// IsMember reports whether a problem may read an artifact revision.
func (s *Store) IsMember(sub SubjectRef, id, artifactID, revision string) bool {
	if _, err := s.ReadHead(sub, id); err != nil && !errors.Is(err, ErrNewerSchema) {
		return false
	}
	m, err := s.members(sub, id)
	return err == nil && m[artifactID+"@"+revision]
}

// Content returns an artifact revision's bytes for one problem — only when
// that problem's membership lists it. A content hash alone grants nothing:
// identical bytes retained by another problem stay invisible from here.
func (s *Store) Content(sub SubjectRef, id, artifactID, revision string) ([]byte, error) {
	if !ValidToken(revision) || len(artifactID) != 16 {
		return nil, NotFound("no such artifact")
	}
	if !s.IsMember(sub, id, artifactID, revision) {
		return nil, NotFound("no such artifact")
	}
	a, ok := s.reg.Get(artifactID)
	if !ok {
		return nil, NotFound("no such artifact")
	}
	if _, ok := a.Revision(revision); !ok {
		return nil, NotFound("no such artifact")
	}
	b, err := s.reg.Content(revision)
	if err != nil {
		return nil, corrupt("artifact bytes missing: %v", err)
	}
	if artifacts.Hash(b) != revision {
		return nil, corrupt("artifact bytes do not match their revision")
	}
	return b, nil
}

// ---- commits -------------------------------------------------------------------------

// Tx is one in-flight commit: mutate Next; Base is the committed state.
type Tx struct {
	Base    *State
	Next    *State
	Actor   Actor
	Now     time.Time
	store   *Store
	subject SubjectRef
	ops     []OperationRecord
	native  []NativeRef
	inputs  []string
	outputs []string
	members []member
	summary []string
	events  []LedgerEvent
	// viewOnly marks commits that touch only views (no physical change).
	viewOnly bool
	// compiled holds this commit's freshly compiled assemblies (validate.go).
	compiled map[string]compiledAssembly
}

// Record notes one applied operation with canonical before/after values.
func (tx *Tx) Record(op, target string, before, after any) {
	enc := func(v any) json.RawMessage {
		if v == nil {
			return json.RawMessage("null")
		}
		b, err := Canonical(v)
		if err != nil {
			return json.RawMessage("null")
		}
		return b
	}
	tx.ops = append(tx.ops, OperationRecord{Op: op, Target: target, Before: enc(before), After: enc(after)})
}

// Summary appends a short human summary line (activity projection).
func (tx *Tx) Summary(s string) { tx.summary = append(tx.summary, s) }

// Event adds a ledger activity event for this commit.
func (tx *Tx) Event(kind, text string) {
	tx.events = append(tx.events, LedgerEvent{Kind: kind, Text: text})
}

// ViewOnly marks the commit as affecting views only.
func (tx *Tx) ViewOnly() { tx.viewOnly = true }

// Native records a native delivery reference consumed by this commit.
func (tx *Tx) Native(n NativeRef) { tx.native = append(tx.native, n) }

// Retain stores bytes as an immutable artifact owned by this problem and
// returns its (artifactId, revision). Used for inputs and derived files.
func (tx *Tx) Retain(kind, title string, content []byte) (string, string, error) {
	res, err := tx.store.reg.Retain(artifacts.Put{Kind: kind, Harness: "construction", Title: title, Content: content,
		Actor: tx.Actor.Principal, At: tx.Now, Provenance: artifacts.Provenance{Source: "construction"}})
	if err != nil {
		return "", "", err
	}
	tx.members = append(tx.members, member{ArtifactID: res.Artifact.ID, Revision: res.Revision.Hash})
	return res.Artifact.ID, res.Revision.Hash, nil
}

// Input/Output note artifact hashes consumed/produced.
func (tx *Tx) Input(hash string)  { tx.inputs = append(tx.inputs, hash) }
func (tx *Tx) Output(hash string) { tx.outputs = append(tx.outputs, hash) }

// Subject is the subject this transaction commits under.
func (tx *Tx) Subject() SubjectRef { return tx.subject }

// CommitRequest identifies one logical mutation.
type CommitRequest struct {
	RequestID   string
	PayloadHash string
	Actor       Actor
}

// Validator checks every changed document before anything is written; the
// assembly/geometry phases register theirs here (ValidateChanged).
var docValidators = map[string]func(st *State, key string, doc any) error{}

// Commit runs fn against a copy of the problem's state and, if it succeeds
// and every changed document validates, makes the result durable:
//
//	validate → stage immutable documents (fsync) → write the receipt →
//	record membership → atomically replace head.json (fsync file + dir) →
//	publish the ledger projection (best effort, recoverable).
//
// An identical replay of RequestID returns the original receipt; reuse with a
// different payload is a 409. A crash before the head rename leaves the old
// head valid; a crash after it is recovered by replaying the request.
func (s *Store) Commit(sub SubjectRef, id string, req CommitRequest, fn func(tx *Tx) error) (*State, *Receipt, error) {
	if !ValidRequestID(req.RequestID) {
		return nil, nil, Invalid("requestId must be 8–128 characters of [A-Za-z0-9_-]")
	}
	if !ValidToken(req.PayloadHash) {
		return nil, nil, Invalid("payload hash missing")
	}
	if errs := checkActor("actor", req.Actor); len(errs) > 0 {
		return nil, nil, Invalid(errs...)
	}
	if err := s.acquireWriter(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	base, err := s.Load(sub, id)
	if err != nil {
		return nil, nil, err
	}
	return s.commitLocked(sub, id, base, req, fn)
}

func (s *Store) commitLocked(sub SubjectRef, id string, base *State, req CommitRequest, fn func(tx *Tx) error) (*State, *Receipt, error) {
	if prior, ok := base.Head.Receipts[req.RequestID]; ok {
		if prior.PayloadHash != req.PayloadHash {
			return nil, nil, Conflict("request ID was already used for different content", nil)
		}
		rc, err := s.receipt(prior.Receipt)
		if err != nil {
			return nil, nil, err
		}
		return base, rc, nil
	}
	if base.ReadOnly {
		return nil, nil, ErrReadOnly
	}
	next, err := base.Clone()
	if err != nil {
		return nil, nil, err
	}
	now := s.now().UTC()
	tx := &Tx{Base: base, Next: next, Actor: req.Actor, Now: now, store: s, subject: sub}
	if err := fn(tx); err != nil {
		return nil, nil, err
	}
	return s.finish(sub, id, base, tx, req)
}

// preCommitHooks run after the transaction body and before change
// detection (e.g. compile assemblies, refuse blocking findings);
// postStampHooks run once every changed document has its final revision
// token and may add derived documents keyed to those tokens (validation
// reports). Registered by the domain files that own them.
var (
	preCommitHooks []func(tx *Tx) error
	postStampHooks []func(tx *Tx, tokens map[string]string) error
)

// stampChanged marks and stamps every document whose content differs from
// the base. Idempotent: a second call yields the same stamps.
func stampChanged(base, next *State, tx *Tx, stamp string) (map[string]bool, []string, error) {
	baseDocs, nextDocs := base.docs(), next.docs()
	for key := range baseDocs {
		if _, ok := nextDocs[key]; !ok {
			return nil, nil, Invalid("documents are never deleted: " + key)
		}
	}
	keys := make([]string, 0, len(nextDocs))
	for k := range nextDocs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// A document changes when its content changes; its revision stamp is
	// assigned here. The problem's updatedAt moves only with the problem's
	// own content, so an assembly edit never invalidates a pending
	// problem-level edit's expected revision (head.updatedAt moves always).
	changed := map[string]bool{}
	for _, key := range keys {
		if b, ok := baseDocs[key]; !ok || !sameDoc(b, nextDocs[key]) {
			changed[key] = true
		}
	}
	if changed["problem"] {
		next.Problem.UpdatedAt = stamp
	}
	for _, key := range keys {
		if !changed[key] {
			continue
		}
		env := envelopeOf(nextDocs[key])
		env.SchemaVersion = SchemaVersion
		env.Kind = docKindFor(key)
		env.CreatedAt = stamp
		env.Actor = tx.Actor
		if prev, ok := base.Head.Docs[key]; ok {
			env.RevisionNumber = prev.RevisionNumber + 1
			env.ParentRevision = prev.Revision
		} else {
			env.RevisionNumber = 1
			env.ParentRevision = ""
		}
	}
	return changed, keys, nil
}

func (s *Store) finish(sub SubjectRef, id string, base *State, tx *Tx, req CommitRequest) (*State, *Receipt, error) {
	next := tx.Next
	stamp := tx.Now.Format(time.RFC3339Nano)
	for _, h := range preCommitHooks {
		if err := h(tx); err != nil {
			return nil, nil, err
		}
	}
	changed, keys, err := stampChanged(base, next, tx, stamp)
	if err != nil {
		return nil, nil, err
	}
	if len(postStampHooks) > 0 {
		tokens := map[string]string{}
		nextDocs := next.docs()
		for _, key := range keys {
			if changed[key] {
				_, tok, err := TokenOf(nextDocs[key])
				if err != nil {
					return nil, nil, Invalid(key + ": " + err.Error())
				}
				tokens[key] = tok
			} else {
				tokens[key] = base.Head.Docs[key].Revision
			}
		}
		for _, h := range postStampHooks {
			if err := h(tx, tokens); err != nil {
				return nil, nil, err
			}
		}
		if changed, keys, err = stampChanged(base, next, tx, stamp); err != nil {
			return nil, nil, err
		}
	}
	nextDocs := next.docs()
	for _, key := range keys {
		if !changed[key] {
			continue
		}
		if err := validateDoc(next, key, nextDocs[key]); err != nil {
			return nil, nil, err
		}
	}
	// stage immutable documents
	head := cloneHead(base.Head)
	head.SchemaVersion, head.Kind, head.ProblemID, head.Subject = SchemaVersion, DocHead, id, sub
	var changes []DocChange
	for _, key := range keys {
		if !changed[key] {
			continue
		}
		raw, tok, err := TokenOf(nextDocs[key])
		if err != nil {
			return nil, nil, Invalid(key + ": " + err.Error())
		}
		limit := MaxDocumentBytes
		if strings.HasPrefix(key, "assembly:") {
			limit = MaxAssemblyBytes
		}
		if len(raw) > limit {
			return nil, nil, &Error{Status: 413, Kind: "too-large", Message: fmt.Sprintf("%s exceeds %d bytes", key, limit)}
		}
		res, err := s.reg.Retain(artifacts.Put{Kind: docKindFor(key), Harness: "construction", Title: key, Content: raw,
			Actor: tx.Actor.Principal, At: tx.Now, Provenance: artifacts.Provenance{Source: "construction"}})
		if err != nil {
			return nil, nil, err
		}
		if res.Revision.Hash != tok {
			return nil, nil, corrupt("staged %s under a different hash", key)
		}
		ref := DocRef{Kind: docKindFor(key), ArtifactID: res.Artifact.ID, Revision: tok, RevisionNumber: envelopeOf(nextDocs[key]).RevisionNumber}
		changes = append(changes, DocChange{Key: key, From: base.Head.Docs[key].Revision, To: tok})
		head.Docs[key] = ref
		tx.members = append(tx.members, member{ArtifactID: ref.ArtifactID, Revision: tok})
		next.Raw[key] = raw
	}
	if err := s.failpoint("after-stage-docs"); err != nil {
		return nil, nil, err
	}
	// the receipt (operation record) is itself an immutable document
	rc := &Receipt{
		Envelope: Envelope{SchemaVersion: SchemaVersion, Kind: DocReceipt, ID: NewID(KindOperation), RevisionNumber: 1,
			CreatedAt: stamp, Actor: tx.Actor},
		RequestID: req.RequestID, PayloadHash: req.PayloadHash, ProblemID: id, Subject: sub,
		BaseGeneration: base.Head.Generation, ResultGeneration: base.Head.Generation + 1,
		ParentCommit: base.Head.Commit.Revision, Summary: strings.Join(tx.summary, "; "),
		Operations: nonNilOps(tx.ops), Changes: nonNilChanges(changes), Status: "committed", ViewOnly: tx.viewOnly,
		Native: nonNilNative(tx.native), InputHashes: sortedUnique(tx.inputs), OutputHashes: sortedUnique(tx.outputs),
		Events: nonNilEvents(tx.events),
	}
	rraw, rtok, err := TokenOf(rc)
	if err != nil {
		return nil, nil, err
	}
	rres, err := s.reg.Retain(artifacts.Put{Kind: DocReceipt, Harness: "construction", Title: "receipt", Content: rraw,
		Actor: tx.Actor.Principal, At: tx.Now, Provenance: artifacts.Provenance{Source: "construction"}})
	if err != nil {
		return nil, nil, err
	}
	tx.members = append(tx.members, member{ArtifactID: rres.Artifact.ID, Revision: rtok})
	if err := s.failpoint("after-stage-receipt"); err != nil {
		return nil, nil, err
	}
	if err := s.ensureSubject(sub); err != nil {
		return nil, nil, err
	}
	if err := s.appendMembers(sub, id, tx.members); err != nil {
		return nil, nil, err
	}
	head.Generation = base.Head.Generation + 1
	head.Commit = DocRef{Kind: DocReceipt, ArtifactID: rres.Artifact.ID, Revision: rtok, RevisionNumber: 1}
	head.Receipts[req.RequestID] = ReceiptIndex{PayloadHash: req.PayloadHash, Generation: head.Generation, Receipt: rtok, ReceiptID: rc.ID}
	head.UpdatedAt = stamp
	hb, err := json.MarshalIndent(head, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	if err := s.failpoint("before-head-rename"); err != nil {
		return nil, nil, err
	}
	if err := writeAtomic(s.headPath(sub, id), append(hb, '\n')); err != nil {
		return nil, nil, err
	}
	next.Head = head
	if err := s.failpoint("after-head-rename"); err != nil {
		return nil, nil, err // durable, but the caller never hears: replay recovers
	}
	s.rawMu.Lock()
	s.rawCache[rtok] = rraw
	s.rawMu.Unlock()
	s.publishPending(sub, id, head)
	return next, rc, nil
}

func sameDoc(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ab, err1 := Canonical(stripEnvelopeStamp(a))
	bb, err2 := Canonical(stripEnvelopeStamp(b))
	return err1 == nil && err2 == nil && bytes.Equal(ab, bb)
}

// stripEnvelopeStamp compares documents by content: the revision stamp
// (number, parent, time, actor) is assigned by the commit, not by content.
func stripEnvelopeStamp(doc any) any {
	b, err := json.Marshal(doc)
	if err != nil {
		return doc
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return doc
	}
	for _, k := range []string{"revisionNumber", "parentRevision", "createdAt", "actor", "updatedAt"} {
		delete(m, k)
	}
	return m
}

func validateDoc(st *State, key string, doc any) error {
	if v, ok := docValidators[docKindFor(key)]; ok {
		return v(st, key, doc)
	}
	switch d := doc.(type) {
	case *Problem:
		return ValidateProblem(d)
	}
	return nil
}

func (s *Store) failpoint(point string) error {
	if s.fail == nil {
		return nil
	}
	return s.fail(point)
}

func (s *Store) ensureSubject(sub SubjectRef) error {
	p := filepath.Join(s.projectDir(sub), "subject.json")
	if raw, err := readNoFollow(p); err == nil {
		var have struct {
			Subject SubjectRef `json:"subject"`
		}
		if json.Unmarshal(raw, &have) != nil || have.Subject != sub {
			return corrupt("subject index mismatch for %s", ProjectKey(sub))
		}
		return nil
	}
	b, _ := json.MarshalIndent(map[string]any{"schemaVersion": SchemaVersion, "kind": DocSubject, "subject": sub}, "", "  ")
	return writeAtomic(p, append(b, '\n'))
}

// receipt loads a receipt document by token.
func (s *Store) receipt(tok string) (*Receipt, error) {
	raw, err := s.docBytes(DocRef{Kind: DocReceipt, Revision: tok})
	if err != nil {
		return nil, err
	}
	var rc Receipt
	if err := DecodeStrict(raw, DocReceipt, &rc); err != nil {
		return nil, err
	}
	return &rc, nil
}

// Receipt loads a problem's receipt by request id (replay / lost ACK).
func (s *Store) Receipt(sub SubjectRef, id, requestID string) (*Receipt, error) {
	h, err := s.ReadHead(sub, id)
	if err != nil {
		return nil, err
	}
	idx, ok := h.Receipts[requestID]
	if !ok {
		return nil, NotFound("no receipt for that request")
	}
	return s.receipt(idx.Receipt)
}

// History walks the commit chain from the head, newest first (bounded).
func (s *Store) History(sub SubjectRef, id string, limit int) ([]Receipt, error) {
	h, err := s.ReadHead(sub, id)
	if err != nil {
		return nil, err
	}
	var out []Receipt
	tok := h.Commit.Revision
	for tok != "" && (limit <= 0 || len(out) < limit) {
		rc, err := s.receipt(tok)
		if err != nil {
			return out, err
		}
		out = append(out, *rc)
		tok = rc.ParentCommit
	}
	return out, nil
}

// ---- creation -------------------------------------------------------------------------

type createIntent struct {
	RequestID   string `json:"requestId"`
	PayloadHash string `json:"payloadHash"`
	ProblemID   string `json:"problemId"`
}

// Create makes a new problem under a subject. The request id is bound to a
// problem id in a durable intent record before the first head is written, so
// a lost ACK or a crash mid-creation replays into the same problem.
func (s *Store) Create(sub SubjectRef, req CommitRequest, fn func(tx *Tx) error) (*State, *Receipt, error) {
	if err := ValidSubject(sub); err != nil {
		return nil, nil, err
	}
	if !ValidRequestID(req.RequestID) || !ValidToken(req.PayloadHash) {
		return nil, nil, Invalid("requestId and payload are required")
	}
	if errs := checkActor("actor", req.Actor); len(errs) > 0 {
		return nil, nil, Invalid(errs...)
	}
	if err := s.acquireWriter(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	intentPath := filepath.Join(s.projectDir(sub), "creates", req.RequestID+".json")
	var intent createIntent
	if raw, err := readNoFollow(intentPath); err == nil {
		if json.Unmarshal(raw, &intent) != nil || !ValidID(KindProblem, intent.ProblemID) {
			return nil, nil, corrupt("creation intent %s", req.RequestID)
		}
		if intent.PayloadHash != req.PayloadHash {
			return nil, nil, Conflict("request ID was already used for different content", nil)
		}
		if base, err := s.Load(sub, intent.ProblemID); err == nil {
			return s.commitLocked(sub, intent.ProblemID, base, req, fn)
		} else if !errors.Is(err, ErrNotFound) {
			return nil, nil, err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		intent = createIntent{RequestID: req.RequestID, PayloadHash: req.PayloadHash, ProblemID: NewID(KindProblem)}
		if err := s.ensureSubject(sub); err != nil {
			return nil, nil, err
		}
		b, _ := json.Marshal(intent)
		if err := writeAtomic(intentPath, b); err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, err
	}
	base := newState(Head{SchemaVersion: SchemaVersion, Kind: DocHead, ProblemID: intent.ProblemID, Subject: sub})
	return s.commitLocked(sub, intent.ProblemID, base, req, fn)
}

// ---- ledger projection ------------------------------------------------------------------

type ledgerState struct {
	PublishedGeneration int64 `json:"publishedGeneration"`
}

func (s *Store) ledgerPath(sub SubjectRef, id string) string {
	return filepath.Join(s.problemDir(sub, id), "ledger.json")
}

func (s *Store) publishedGeneration(sub SubjectRef, id string) int64 {
	raw, err := readNoFollow(s.ledgerPath(sub, id))
	if err != nil {
		return 0
	}
	var ls ledgerState
	_ = json.Unmarshal(raw, &ls)
	return ls.PublishedGeneration
}

// publishPending publishes every unpublished commit's events, oldest first.
func (s *Store) publishPending(sub SubjectRef, id string, head Head) {
	if s.publish == nil {
		return
	}
	_ = s.reconcileLedger(sub, id, head)
}

// ReconcileLedger republishes events of commits the ledger has not
// acknowledged (e.g. after a ledger append failed or a crash).
func (s *Store) ReconcileLedger(sub SubjectRef, id string) error {
	h, err := s.ReadHead(sub, id)
	if err != nil {
		return err
	}
	return s.reconcileLedger(sub, id, h)
}

func (s *Store) reconcileLedger(sub SubjectRef, id string, head Head) error {
	if s.publish == nil {
		return nil
	}
	published := s.publishedGeneration(sub, id)
	var pending []Receipt
	tok := head.Commit.Revision
	for tok != "" {
		rc, err := s.receipt(tok)
		if err != nil {
			return err
		}
		if rc.ResultGeneration <= published {
			break
		}
		pending = append(pending, *rc)
		tok = rc.ParentCommit
	}
	for i := len(pending) - 1; i >= 0; i-- {
		rc := pending[i]
		if len(rc.Events) > 0 {
			if err := s.publish(sub, id, rc.ResultGeneration, rc.Events); err != nil {
				return err
			}
		}
		b, _ := json.Marshal(ledgerState{PublishedGeneration: rc.ResultGeneration})
		if err := writeAtomic(s.ledgerPath(sub, id), b); err != nil {
			return err
		}
	}
	return nil
}

func nonNilOps(x []OperationRecord) []OperationRecord {
	if x == nil {
		return []OperationRecord{}
	}
	return x
}
func nonNilChanges(x []DocChange) []DocChange {
	if x == nil {
		return []DocChange{}
	}
	return x
}
func nonNilNative(x []NativeRef) []NativeRef {
	if x == nil {
		return []NativeRef{}
	}
	return x
}
func nonNilEvents(x []LedgerEvent) []LedgerEvent {
	if x == nil {
		return []LedgerEvent{}
	}
	return x
}
