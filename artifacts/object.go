package artifacts

// FIRST-CLASS ARTIFACTS (manifest P1 Phase 1). An Artifact is the versioned,
// addressable OBJECT behind what used to be only a harness-relative path
// (delegate/feed/signals ArtifactRef): a run's brief, a plan, a report, an
// imported document. The plan's trunk is "artifacts over chats" — agents
// consume and emit these; a task binds to them by id (tasks.Task Outputs /
// Inputs); the ledger records their lifecycle.
//
// Three rules, in the order they matter:
//
//  1. Content identity is the address. Every revision's bytes are sha256'd
//     and kept in the same pool the chat attachments use (Store.Save — the
//     samizdat attachments/<sha256>.<ext> convention: a hash file is never
//     rewritten, identical bytes land once). A revision's Hash IS where its
//     bytes are; the Ref is merely where they were last seen in a harness.
//  2. Versioned, never mutated. A new version APPENDS a Revision that names
//     its parent's hash; the object file is rewritten whole, but no revision
//     is ever edited or dropped, and every old revision's bytes stay readable
//     through Content(hash). Put with identical bytes is a no-op.
//  3. One file per artifact, objects/<id>.json, tmp+rename — the file is the
//     truth (there is no index to drift; List reads the directory). The id is
//     derived from the first revision (kind · harness · ref · hash), so
//     replaying a registration yields the same object, not a duplicate.
//
// The package stays UI-agnostic and stdlib-only; the ledger event for a
// create/revise is the SERVER's job (it owns the ledger), fed by PutResult.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Artifact kinds — an open vocabulary (a writer may stamp its own); these are
// the ones the platform knows today.
const (
	KindDocument = "document" // a note or write-up
	KindReport   = "report"   // a run report / analysis
	KindPlan     = "plan"     // a task plan
	KindBrief    = "brief"    // a run's library brief (artifacts/library/…)
	KindLoom     = "loom"     // a woven / composed surface
	KindFile     = "file"     // anything else — the default
)

// idLen is the hex length of an artifact id (64 bits of the derivation hash:
// filename-safe, short enough to paste into a [outputs::] field by hand).
const idLen = 16

// Revision is one immutable version of an artifact's bytes.
type Revision struct {
	N      int       `json:"n"`                // 1-based version number
	Hash   string    `json:"hash"`             // sha256 hex — the content address (pool key)
	Size   int64     `json:"size"`             //
	Ref    string    `json:"ref,omitempty"`    // harness-relative path the bytes were seen at
	At     time.Time `json:"at"`               // UTC
	Actor  string    `json:"actor,omitempty"`  // who produced this version
	Parent string    `json:"parent,omitempty"` // previous revision's hash ("" on the first)
	Note   string    `json:"note,omitempty"`   // one line: why this version exists
}

// Provenance is where an artifact came from — the same refs the ledger uses,
// so an artifact's origin and its event history name the same things.
type Provenance struct {
	Source  string   `json:"source,omitempty"`  // "run" | "delegate" | "chat" | "import" | "manual" | …
	Task    string   `json:"task,omitempty"`    // the task that produced it (composite todo id)
	Run     string   `json:"run,omitempty"`     // the harness run that wrote it
	Session string   `json:"session,omitempty"` // the chat session it fell out of
	Inputs  []string `json:"inputs,omitempty"`  // artifact ids consumed to make it
}

// IsZero reports an unset provenance.
func (p Provenance) IsZero() bool {
	return p.Source == "" && p.Task == "" && p.Run == "" && p.Session == "" && len(p.Inputs) == 0
}

// merge fills the empty halves of p from q and unions Inputs — a later
// revision may learn where the artifact came from, never unlearn it.
func (p Provenance) merge(q Provenance) Provenance {
	if p.Source == "" {
		p.Source = q.Source
	}
	if p.Task == "" {
		p.Task = q.Task
	}
	if p.Run == "" {
		p.Run = q.Run
	}
	if p.Session == "" {
		p.Session = q.Session
	}
	p.Inputs = unionIDs(p.Inputs, q.Inputs)
	return p
}

// Artifact is the versioned object. Head is the current revision's hash; the
// chain is Revisions in order, each naming its parent.
type Artifact struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Title      string     `json:"title,omitempty"`
	Harness    string     `json:"harness,omitempty"` // which harness tree Ref is relative to ("" = primary)
	Ref        string     `json:"ref,omitempty"`     // current harness-relative path (the head's)
	Created    time.Time  `json:"created"`           // UTC — the first revision's At
	Actor      string     `json:"actor,omitempty"`   // who created it
	Provenance Provenance `json:"provenance,omitzero"`
	Head       string     `json:"head"` // hash of the current revision
	Revisions  []Revision `json:"revisions"`
}

// Version is the current version number (the revision count).
func (a Artifact) Version() int { return len(a.Revisions) }

// HeadRevision is the current revision (zero when the object is empty).
func (a Artifact) HeadRevision() Revision {
	if n := len(a.Revisions); n > 0 {
		return a.Revisions[n-1]
	}
	return Revision{}
}

// Revision finds a version by its hash.
func (a Artifact) Revision(hash string) (Revision, bool) {
	for _, r := range a.Revisions {
		if r.Hash == hash {
			return r, true
		}
	}
	return Revision{}, false
}

// Updated is when the artifact last changed (the head's At).
func (a Artifact) Updated() time.Time { return a.HeadRevision().At }

// Hash is the content address of b — sha256 hex, the pool's key.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// IDFor derives an artifact's stable id from its first registration. Two
// registrations of the same bytes at the same place are the same artifact;
// the same bytes registered elsewhere (a copy) are a different one.
func IDFor(kind, harness, ref, hash string) string {
	sum := sha256.Sum256([]byte("artifact\x00" + kind + "\x00" + harness + "\x00" + ref + "\x00" + hash))
	return hex.EncodeToString(sum[:])[:idLen]
}

// ValidID reports whether s has the shape IDFor produces (lowercase hex of
// idLen) — the guard that keeps an id a filename and never a path.
func ValidID(s string) bool { return len(s) == idLen && isLowerHex(s) }

// ValidHash reports a well-formed sha256 hex (the pool key shape).
func ValidHash(s string) bool { return len(s) == 64 && isLowerHex(s) }

func isLowerHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// unionIDs appends the ids of b missing from a (trimmed, order kept, no dups).
func unionIDs(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, id := range list {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// --- the registry ------------------------------------------------------------

// Registry is the artifact object store: objects/<id>.json beside the blob
// pool that holds every revision's bytes.
type Registry struct {
	pool *Store
	dir  string
	mu   sync.Mutex
}

// NewRegistry opens the registry beside pool (objects/ under the pool dir).
func NewRegistry(pool *Store) (*Registry, error) {
	if pool == nil {
		return nil, errors.New("artifacts: registry needs a pool")
	}
	dir := filepath.Join(pool.dir, "objects")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Registry{pool: pool, dir: dir}, nil
}

// Put registers content as an artifact — or as a new revision of one.
type Put struct {
	ID         string     // revise THIS artifact; "" resolves by Harness+Ref, else creates
	Kind       string     // KindFile when empty (ignored on a revision)
	Title      string     // set/refreshed when non-empty
	Harness    string     // the tree Ref is relative to
	Ref        string     // harness-relative path the bytes live at (may be empty: pool-only)
	Content    []byte     // the bytes — required, non-empty
	Actor      string     // who is putting
	Note       string     // one line on this version
	At         time.Time  // now when zero
	Provenance Provenance // merged into the artifact's (never unlearned)
}

// PutResult reports what Put did. Created: a new object. Changed: a revision
// was appended (false with Created false = identical bytes, nothing written).
type PutResult struct {
	Artifact Artifact
	Revision Revision
	Created  bool
	Changed  bool
}

// Put stores the bytes in the pool (deduped by hash), then appends the
// revision. Bytes go first so a crash between the two leaves an orphan blob,
// never an object whose content is missing.
func (r *Registry) Put(p Put) (PutResult, error) {
	if len(p.Content) == 0 {
		return PutResult{}, errors.New("artifacts: empty content")
	}
	if p.ID != "" && !ValidID(p.ID) {
		return PutResult{}, errors.New("artifacts: bad id")
	}
	p.Ref = cleanRef(p.Ref)
	p.Harness = strings.TrimSpace(p.Harness)
	p.Kind = strings.ToLower(strings.TrimSpace(p.Kind))
	if p.Kind == "" {
		p.Kind = KindFile
	}
	if p.At.IsZero() {
		p.At = time.Now()
	}
	p.At = p.At.UTC()
	hash := Hash(p.Content)

	r.mu.Lock()
	defer r.mu.Unlock()

	// resolve the target: explicit id, else the artifact already at this ref
	var cur Artifact
	have := false
	switch {
	case p.ID != "":
		if cur, have = r.get(p.ID); !have {
			return PutResult{}, errors.New("artifacts: no such artifact " + p.ID)
		}
	case p.Ref != "":
		cur, have = r.byRef(p.Harness, p.Ref)
	}
	if !have {
		id := IDFor(p.Kind, p.Harness, p.Ref, hash)
		if a, ok := r.get(id); ok { // the same registration, replayed
			return PutResult{Artifact: a, Revision: a.HeadRevision()}, nil
		}
		if err := r.keep(p.Content, p.Ref, hash); err != nil {
			return PutResult{}, err
		}
		rev := Revision{N: 1, Hash: hash, Size: int64(len(p.Content)), Ref: p.Ref, At: p.At, Actor: p.Actor, Note: p.Note}
		a := Artifact{
			ID: id, Kind: p.Kind, Title: p.Title, Harness: p.Harness, Ref: p.Ref,
			Created: p.At, Actor: p.Actor, Provenance: p.Provenance,
			Head: hash, Revisions: []Revision{rev},
		}
		if err := r.save(a); err != nil {
			return PutResult{}, err
		}
		return PutResult{Artifact: a, Revision: rev, Created: true, Changed: true}, nil
	}
	if cur.Head == hash { // identical bytes: nothing to version
		return PutResult{Artifact: cur, Revision: cur.HeadRevision()}, nil
	}
	if err := r.keep(p.Content, orStr(p.Ref, cur.Ref), hash); err != nil {
		return PutResult{}, err
	}
	rev := Revision{
		N: len(cur.Revisions) + 1, Hash: hash, Size: int64(len(p.Content)),
		Ref: orStr(p.Ref, cur.Ref), At: p.At, Actor: p.Actor, Parent: cur.Head, Note: p.Note,
	}
	next := cur
	next.Revisions = append(append([]Revision{}, cur.Revisions...), rev)
	next.Head = hash
	next.Ref = rev.Ref
	if p.Title != "" {
		next.Title = p.Title
	}
	next.Provenance = cur.Provenance.merge(p.Provenance)
	if err := r.save(next); err != nil {
		return PutResult{}, err
	}
	return PutResult{Artifact: next, Revision: rev, Changed: true}, nil
}

// keep puts the bytes in the pool under their hash (the pool dedupes).
func (r *Registry) keep(content []byte, ref, hash string) error {
	got, err := r.pool.Save(bytes.NewReader(content), filepath.Base(orStr(ref, hash)), "")
	if err != nil {
		return err
	}
	if got.Hash != hash {
		return errors.New("artifacts: pool hash mismatch")
	}
	return nil
}

// Get reads one artifact by id.
func (r *Registry) Get(id string) (Artifact, bool) {
	if !ValidID(id) {
		return Artifact{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.get(id)
}

// ByRef finds the artifact registered at a harness-relative path — the
// bridge from a legacy ArtifactRef row to its object ("" harness = primary).
func (r *Registry) ByRef(harness, ref string) (Artifact, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byRef(strings.TrimSpace(harness), cleanRef(ref))
}

// Content reads a revision's bytes from the pool by hash — any version, not
// just the head. The harness file at Ref may since have moved on; the pool
// has not.
func (r *Registry) Content(hash string) ([]byte, error) {
	if !ValidHash(hash) {
		return nil, errors.New("artifacts: bad hash")
	}
	p := r.pool.BlobPath(hash)
	if p == "" {
		return nil, errors.New("artifacts: no content for " + hash[:12])
	}
	return os.ReadFile(p)
}

// Filter narrows List; every set field must match.
type Filter struct {
	Kind    string
	Task    string // Provenance.Task
	Run     string // Provenance.Run
	Harness string
	Ref     string
}

func (f Filter) matches(a Artifact) bool {
	if f.Kind != "" && a.Kind != f.Kind {
		return false
	}
	if f.Task != "" && a.Provenance.Task != f.Task {
		return false
	}
	if f.Run != "" && a.Provenance.Run != f.Run {
		return false
	}
	if f.Harness != "" && a.Harness != f.Harness {
		return false
	}
	if f.Ref != "" && a.Ref != cleanRef(f.Ref) {
		return false
	}
	return true
}

// List reads every artifact matching f, most recently changed first.
func (r *Registry) List(f Filter) []Artifact {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Artifact{}
	for _, a := range r.all() {
		if f.matches(a) {
			out = append(out, a)
		}
	}
	return out
}

// --- files --------------------------------------------------------------------

func (r *Registry) path(id string) string { return filepath.Join(r.dir, id+".json") }

func (r *Registry) get(id string) (Artifact, bool) {
	b, err := os.ReadFile(r.path(id))
	if err != nil {
		return Artifact{}, false
	}
	var a Artifact
	if json.Unmarshal(b, &a) != nil || a.ID != id {
		return Artifact{}, false
	}
	return a, true
}

func (r *Registry) byRef(harness, ref string) (Artifact, bool) {
	if ref == "" {
		return Artifact{}, false
	}
	for _, a := range r.all() {
		if a.Ref == ref && a.Harness == harness {
			return a, true
		}
	}
	return Artifact{}, false
}

// all reads the directory: every well-formed object, newest change first
// (ties by id so the order is stable).
func (r *Registry) all() []Artifact {
	entries, _ := os.ReadDir(r.dir)
	var out []Artifact
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".json")
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || !ValidID(id) {
			continue
		}
		if a, ok := r.get(id); ok {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ui, uj := out[i].Updated(), out[j].Updated()
		if !ui.Equal(uj) {
			return ui.After(uj)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// save writes the object whole, atomically.
func (r *Registry) save(a Artifact) error {
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	p := r.path(a.ID)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// cleanRef normalizes a harness-relative ref: slash-separated, trimmed,
// never absolute and never climbing ("" when it would).
func cleanRef(ref string) string {
	ref = strings.TrimSpace(strings.ReplaceAll(ref, `\`, "/"))
	if ref == "" {
		return ""
	}
	clean := filepath.ToSlash(filepath.Clean(ref))
	if clean == "." || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// --- task ↔ artifact binding (derived) -------------------------------------------

// Binding is one task's artifact references as its line carries them —
// [outputs:: id, id] / [inputs:: id, id] on the thin tasks.Task. The package
// does not know tasks; the caller projects them into this shape.
type Binding struct {
	Task    string
	Outputs []string
	Inputs  []string
}

// Links is one artifact's task graph, derived: who produced it (its
// provenance task plus every task listing it as an output) and who consumes
// it (every task listing it as an input).
type Links struct {
	Producers []string `json:"producers,omitempty"`
	Consumers []string `json:"consumers,omitempty"`
}

// LinkIndex inverts the bindings over the artifacts: artifact id → Links.
// An id bound by a task but unknown to the registry still gets its links
// (the artifact may live in another registry, or not yet) — never dropped.
func LinkIndex(bindings []Binding, arts []Artifact) map[string]Links {
	idx := map[string]Links{}
	for _, a := range arts {
		if t := strings.TrimSpace(a.Provenance.Task); t != "" {
			l := idx[a.ID]
			l.Producers = unionIDs(l.Producers, []string{t})
			idx[a.ID] = l
		}
	}
	for _, b := range bindings {
		if strings.TrimSpace(b.Task) == "" {
			continue
		}
		for _, id := range b.Outputs {
			if id = strings.TrimSpace(id); id != "" {
				l := idx[id]
				l.Producers = unionIDs(l.Producers, []string{b.Task})
				idx[id] = l
			}
		}
		for _, id := range b.Inputs {
			if id = strings.TrimSpace(id); id != "" {
				l := idx[id]
				l.Consumers = unionIDs(l.Consumers, []string{b.Task})
				idx[id] = l
			}
		}
	}
	return idx
}

// TaskArtifacts answers "what did task X produce / consume": its bound
// outputs plus every artifact whose provenance names it, and its bound inputs.
func TaskArtifacts(task string, bindings []Binding, arts []Artifact) (outputs, inputs []string) {
	task = strings.TrimSpace(task)
	if task == "" {
		return nil, nil
	}
	for _, b := range bindings {
		if b.Task == task {
			outputs = unionIDs(outputs, b.Outputs)
			inputs = unionIDs(inputs, b.Inputs)
		}
	}
	for _, a := range arts {
		if a.Provenance.Task == task {
			outputs = unionIDs(outputs, []string{a.ID})
		}
	}
	return outputs, inputs
}
