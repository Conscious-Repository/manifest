package recruiting

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"manifest/recruiting/sources"
)

// The source-run substrate (plan §4.9, D14). A run is one adapter executed
// over one explicit Scope. Its trace, the adapter's raw response and the
// draft queue live under <dataDir>/recruiting/runs/<run-id>/ — OUTSIDE the
// vault and outside git, mode 0600 — because a run is a cache of a search,
// not a record. Nothing a run holds reaches the vault until the owner
// accepts ONE draft, and the accepted record keeps the citation (url, quote,
// kind, date) so it outlives the cache when the sweep takes the raw payload.
//
// Layout of one run directory:
//
//	run.json        RunState — source, scope, counts, pinned, triaged, expires
//	response.json   the adapter's drafts exactly as emitted, before dedupe
//	drafts.json     the review queue: each draft with its status
//
// This is the ONE place the recruiting package writes a file directly. The
// run root is not a vault path, the vaultwriter has no capability over it,
// and the constructor refuses a root inside the vault it serves.

// RunTTL is how long a fully-triaged, unpinned run's cache survives (D14).
const RunTTL = 30 * 24 * time.Hour

// Draft statuses. `new` is the only state accept and reject act on; every
// other state is terminal for that draft.
const (
	DraftNew       = "new"
	DraftDuplicate = "duplicate"
	DraftAccepted  = "accepted"
	DraftRejected  = "rejected"
)

// Run caps: a scope with no max gets DefaultRunMax; nothing may ask for more
// than MaxRunMax in one run. The blast radius is legible before it starts.
const (
	DefaultRunMax = 25
	MaxRunMax     = 100
)

// RunCounts are the numbers the sources panel paints. `Fetched` is what the
// adapter returned; the other four partition it by what happened next.
type RunCounts struct {
	Fetched   int `json:"fetched"`
	New       int `json:"new"`
	Duplicate int `json:"duplicate"`
	Accepted  int `json:"accepted"`
	Rejected  int `json:"rejected"`
}

// RunState is run.json: the trace without the drafts.
type RunState struct {
	ID        string        `json:"id"`
	Source    string        `json:"source"`
	Scope     sources.Scope `json:"scope"`
	StartedAt time.Time     `json:"startedAt"`
	Counts    RunCounts     `json:"counts"`
	Cursor    string        `json:"cursor,omitempty"`
	Pinned    bool          `json:"pinned"`
	// TriagedAt is set once nothing in the queue is still `new` — for every
	// run alike, since a preview's drafts accept too. It starts the D14 clock.
	TriagedAt time.Time `json:"triagedAt,omitzero"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
}

// Draft is one review-queue entry: the adapter's draft plus what the owner
// decided about it. CandidateID is the vault record it became (accepted) or
// matched (duplicate).
type Draft struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	Reason      string    `json:"reason,omitempty"`
	CandidateID string    `json:"candidateId,omitempty"`
	DecidedAt   time.Time `json:"decidedAt,omitzero"`
	// LookedUpAt stamps the deterministic cross-source pass (lookup.go), so
	// the queue shows which drafts have already been asked about and a second
	// press is a deliberate refresh rather than an accident.
	LookedUpAt time.Time              `json:"lookedUpAt,omitzero"`
	Draft      sources.CandidateDraft `json:"draft"`
	// Paths are the ranked intro routes from the owner seeds to this person
	// over the network's claims (Phase 3): to the record once accepted, to
	// the draft's external keys while it is still queued. DERIVED on every
	// read and never written to drafts.json — the network edges own the fact.
	Paths []PathClaim `json:"paths,omitempty"`
}

// Run is the full projection of one run: state plus queue.
type Run struct {
	RunState
	Drafts []Draft `json:"drafts"`
}

// SourceInfo is what the UI's adapter rail lists for one registered source.
type SourceInfo struct {
	ID     string               `json:"id"`
	Kind   sources.Kind         `json:"kind"`
	Fields []sources.ScopeField `json:"fields"`
}

// RunRequest is the body of POST …/sources/run.
type RunRequest struct {
	Source string            `json:"source"`
	Role   string            `json:"role"`
	Query  string            `json:"query"`
	Max    int               `json:"max"`
	DryRun bool              `json:"dryRun"`
	Fields map[string]string `json:"fields,omitempty"`
}

// RunStore owns the run directories and the registered adapters. It holds
// the record Store because accept IS a record write — through the same
// converter path QuickAdd uses, one draft at a time.
type RunStore struct {
	root     string
	store    *Store
	adapters map[string]sources.Adapter
	order    []string
	mu       sync.Mutex
}

// NewRunStore roots the cache at root (<dataDir>/recruiting/runs). It
// refuses a root inside the record store's vault: derived data never goes in
// the vault, and this is the one write path the package holds directly.
func NewRunStore(root string, store *Store) (*RunStore, error) {
	if store == nil {
		return nil, errors.New("recruiting: a run store needs the record store")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if vault, err := filepath.Abs(store.vaultRoot); err == nil && vault != "" {
		rel, err := filepath.Rel(vault, absRoot)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errf("recruiting: run cache %q must live outside the vault %q (D14)", absRoot, vault)
		}
	}
	return &RunStore{root: absRoot, store: store, adapters: map[string]sources.Adapter{}}, nil
}

// Register adds one adapter to the rail. Registering the same id twice
// replaces it in place, keeping the rail's order stable.
func (r *RunStore) Register(a sources.Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := strings.TrimSpace(a.ID())
	if _, have := r.adapters[id]; !have {
		r.order = append(r.order, id)
	}
	r.adapters[id] = a
}

// Sources lists the registered adapters in registration order.
func (r *RunStore) Sources() []SourceInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SourceInfo, 0, len(r.order))
	for _, id := range r.order {
		a := r.adapters[id]
		fields := a.Scope()
		if fields == nil {
			fields = []sources.ScopeField{}
		}
		out = append(out, SourceInfo{ID: id, Kind: a.Kind(), Fields: fields})
	}
	return out
}

// Root returns the run cache root.
func (r *RunStore) Root() string { return r.root }

// ---- run ----

// Execute runs one adapter over one scope, writes the run under the cache
// root, and dedupes every draft against the vault. It never writes a record:
// a run only ever produces a queue, which is why a preview and a live run are
// the same thing and `DryRun` now gates nothing (see Accept).
func (r *RunStore) Execute(ctx context.Context, req RunRequest, now time.Time) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	adapter, ok := r.adapters[strings.TrimSpace(req.Source)]
	if !ok {
		return Run{}, errf("unknown source %q", req.Source)
	}
	role := strings.TrimSpace(req.Role)
	if role != "" && !r.store.roleExists(role) {
		return Run{}, errf("unknown role %q", role)
	}
	max := req.Max
	if max <= 0 {
		max = DefaultRunMax
	}
	if max > MaxRunMax {
		max = MaxRunMax
	}
	scope := sources.Scope{
		Role: role, Query: strings.TrimSpace(req.Query), Max: max, DryRun: req.DryRun,
	}
	if len(req.Fields) > 0 {
		scope.Fields = map[string]string{}
		for k, v := range req.Fields {
			scope.Fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	// A run must SAY what it is scoped to — but a query is no longer the only
	// way to say it. Naming one paper, one repo or one feed is a scope too,
	// and a stricter rule here refused exactly those runs ("a run needs a
	// query" on a DOI). The adapter still refuses a scope IT cannot use.
	if scope.Query == "" && len(scope.Fields) == 0 {
		return Run{}, errf("a run needs a query, or one thing to scope it to")
	}

	drafts, err := adapter.Search(ctx, scope)
	if err != nil {
		return Run{}, errf("%s: %v", adapter.ID(), err)
	}
	if len(drafts) > max {
		drafts = drafts[:max]
	}
	// the raw response is kept verbatim (D14) — before dedupe, before any
	// sanitising, so a later dispute can be settled against what the source
	// actually said
	raw := append([]sources.CandidateDraft(nil), drafts...)

	run := Run{RunState: RunState{
		ID: newRunID(adapter.ID(), now), Source: adapter.ID(), Scope: scope, StartedAt: now.UTC(),
	}}
	run.Counts.Fetched = len(drafts)
	existing := r.store.candidateIndex()
	for i, d := range drafts {
		d.SourceID = adapter.ID()
		if d.Role == "" {
			d.Role = role
		}
		// a source that knows what its link IS has already said so; this
		// sorts the rest by host so every queue entry carries the labelled
		// view, whichever adapter produced it
		d = sources.ClassifyLinks(d)
		entry := Draft{ID: "d" + strconv.Itoa(i+1), Status: DraftNew, Draft: d}
		if id, why := existing.match(d); id != "" {
			entry.Status, entry.CandidateID, entry.Reason = DraftDuplicate, id, why
			entry.Draft.Dedupe = sources.DedupeHint{CandidateID: id, Reason: why}
			run.Counts.Duplicate++
		} else {
			run.Counts.New++
		}
		run.Drafts = append(run.Drafts, entry)
	}
	if run.Drafts == nil {
		run.Drafts = []Draft{}
	}
	run.triage(now)
	if err := r.writeRun(run, raw); err != nil {
		return Run{}, err
	}
	return r.project(run, nil), nil
}

// project decorates a run for the wire: every draft carries the intro paths
// the network currently derives for it (Draft.Paths). A finder built by the
// caller is reused across runs; nil builds one. Nothing here is stored.
func (r *RunStore) project(run Run, f *PathFinder) Run {
	if f == nil {
		f = r.pathFinder()
	}
	for i := range run.Drafts {
		d := &run.Drafts[i]
		targets := []string{d.CandidateID}
		if d.CandidateID == "" {
			targets = extKeysOfDraft(d.Draft)
		}
		d.Paths = f.Paths(targets, nil, DefaultTopPaths)
		if len(d.Paths) == 0 {
			d.Paths = nil
		}
		// a queue looked up before Topics existed (Phase 1) holds the chips
		// only inside its author-record snippet; read them back out so the
		// card (and an accept's knowledge claims) see what the evidence says
		if len(d.Draft.Topics) == 0 {
			d.Draft.Topics = topicsFromEvidence(d.Draft)
		}
	}
	return run
}

// topicsFromEvidence recovers a draft's topics from its publication rows'
// verbatim "topics: a; b; c" segment (the OpenAlex author record — the same
// author-canonical provenance Phase 1 stores in Topics), deduped by topicKey
// and capped like a lookup. Nil when no row names any. Pure.
func topicsFromEvidence(d sources.CandidateDraft) []string {
	var out []string
	seen := map[string]bool{}
	for _, ev := range d.Evidence {
		if ev.Kind != sources.EvidencePublication {
			continue
		}
		for _, part := range strings.Split(ev.Snippet, " · ") {
			part = strings.TrimSpace(part)
			if len(part) < len("topics:") || !strings.EqualFold(part[:len("topics:")], "topics:") {
				continue
			}
			for _, t := range strings.Split(part[len("topics:"):], ";") {
				t = strings.TrimSpace(t)
				key := topicKey(t)
				if key == "" || seen[key] || len(out) >= lookupTopicsMax {
					continue
				}
				seen[key] = true
				out = append(out, t)
			}
		}
	}
	return out
}

func (r *RunStore) pathFinder() *PathFinder {
	return NewPathFinder(r.store.LoadNetworkPeople().People(), r.store.LoadEdges().Edges())
}

// triage stamps the D14 clock once nothing is left to decide — for every run
// alike, since a preview's drafts are acceptable too.
func (run *Run) triage(now time.Time) {
	if !run.TriagedAt.IsZero() {
		return
	}
	// every run holds a queue worth triaging now that a preview accepts too,
	// so none of them starts its expiry clock with drafts still pending
	for _, d := range run.Drafts {
		if d.Status == DraftNew {
			return
		}
	}
	run.TriagedAt = now.UTC()
	run.ExpiresAt = run.TriagedAt.Add(RunTTL)
}

// Accept promotes exactly ONE draft into a candidate record through the same
// converter path QuickAdd takes, and a draft leaves `new` exactly once.
//
// ⚠ A PREVIEW RUN IS NOT A LESSER RUN (owner decision 2026-09-04). This used
// to refuse when Scope.DryRun was set — "that is what the checkbox meant" —
// but the checkbox never protected anything: Execute writes no record either
// way, the drafts are identical, and accept is already a deliberate,
// one-at-a-time gesture with no accept-all behind it. All the gate did was
// force an identical second call to someone else's API before the owner could
// keep a person he was already looking at. The safety that matters — nothing
// reaches the vault without a per-record click — is unchanged.
func (r *RunStore) Accept(runID, draftID string, now time.Time) (Run, Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.load(runID)
	if err != nil {
		return Run{}, Candidate{}, err
	}
	i, err := run.find(draftID)
	if err != nil {
		return Run{}, Candidate{}, err
	}
	d := &run.Drafts[i]
	switch d.Status {
	case DraftNew:
	case DraftDuplicate:
		return Run{}, Candidate{}, errf("draft %s is already on the board as %s", draftID, d.CandidateID)
	default:
		return Run{}, Candidate{}, errf("draft %s is already %s", draftID, d.Status)
	}
	c, err := r.store.AcceptDraft(d.Draft, now)
	if err != nil {
		return Run{}, Candidate{}, err
	}
	d.Status, d.CandidateID, d.DecidedAt = DraftAccepted, c.ID, now.UTC()
	run.Counts.Accepted++
	run.triage(now)
	if err := r.writeRun(run, nil); err != nil {
		return Run{}, Candidate{}, err
	}
	return r.project(run, nil), c, nil
}

// Reject marks one draft rejected. Nothing is written to the vault.
func (r *RunStore) Reject(runID, draftID string, now time.Time) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.load(runID)
	if err != nil {
		return Run{}, err
	}
	i, err := run.find(draftID)
	if err != nil {
		return Run{}, err
	}
	d := &run.Drafts[i]
	if d.Status != DraftNew {
		return Run{}, errf("draft %s is already %s", draftID, d.Status)
	}
	d.Status, d.DecidedAt = DraftRejected, now.UTC()
	run.Counts.Rejected++
	run.triage(now)
	if err := r.writeRun(run, nil); err != nil {
		return Run{}, err
	}
	return r.project(run, nil), nil
}

// Unreject reverses a pass (Phase 3): the draft returns to `new` exactly as
// it was before the decision — no decided-at, the rejected count back down,
// and the run's D14 expiry clock cleared, since a queue with a `new` draft
// in it is not triaged. Only a rejected draft can come back: an accepted one
// is a record now, and a duplicate never left. Nothing touches the vault.
func (r *RunStore) Unreject(runID, draftID string, now time.Time) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.load(runID)
	if err != nil {
		return Run{}, err
	}
	i, err := run.find(draftID)
	if err != nil {
		return Run{}, err
	}
	d := &run.Drafts[i]
	if d.Status != DraftRejected {
		return Run{}, errf("draft %s is %s, not passed — only a pass can be undone", draftID, d.Status)
	}
	d.Status, d.DecidedAt, d.Reason = DraftNew, time.Time{}, ""
	if run.Counts.Rejected > 0 {
		run.Counts.Rejected--
	}
	run.TriagedAt, run.ExpiresAt = time.Time{}, time.Time{}
	if err := r.writeRun(run, nil); err != nil {
		return Run{}, err
	}
	return r.project(run, nil), nil
}

// Pin exempts a run's cache from the sweep (D14), or puts it back.
func (r *RunStore) Pin(runID string, pinned bool) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.load(runID)
	if err != nil {
		return Run{}, err
	}
	run.Pinned = pinned
	if err := r.writeRun(run, nil); err != nil {
		return Run{}, err
	}
	return r.project(run, nil), nil
}

// Get loads one run.
func (r *RunStore) Get(runID string) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.load(runID)
	if err != nil {
		return Run{}, err
	}
	return r.project(run, nil), nil
}

// Runs lists every run newest-first, sweeping expired ones on the way: the
// listing is the sweeper, so there is no background timer to forget.
func (r *RunStore) Runs(now time.Time) []Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweep(now)
	out := []Run{}
	// one network graph for the whole listing, not one per draft
	finder := r.pathFinder()
	for _, id := range r.ids() {
		run, err := r.load(id)
		if err != nil {
			continue
		}
		out = append(out, r.project(run, finder))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// Sweep removes every unpinned run whose expiry has passed and reports what
// went. An untriaged run has no expiry and is never swept.
func (r *RunStore) Sweep(now time.Time) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sweep(now)
}

func (r *RunStore) sweep(now time.Time) []string {
	var gone []string
	for _, id := range r.ids() {
		run, err := r.load(id)
		if err != nil {
			continue
		}
		if run.Pinned || run.ExpiresAt.IsZero() || !now.After(run.ExpiresAt) {
			continue
		}
		if err := os.RemoveAll(r.dir(id)); err == nil {
			gone = append(gone, id)
		}
	}
	return gone
}

// ---- files ----

var runIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,80}$`)
var draftIDRe = regexp.MustCompile(`^d[0-9]{1,6}$`)

// newRunID is sortable by start time, names its source, and carries enough
// randomness that two runs in the same second do not collide.
func newRunID(source string, now time.Time) string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	src := strings.ToLower(strings.Map(func(c rune) rune {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			return c
		}
		return '-'
	}, source))
	return now.UTC().Format("20060102-150405") + "-" + src + "-" + hex.EncodeToString(b[:])
}

func (r *RunStore) dir(id string) string { return filepath.Join(r.root, id) }

func (r *RunStore) ids() []string {
	ents, err := os.ReadDir(r.root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() && runIDRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func (r *RunStore) load(id string) (Run, error) {
	if !runIDRe.MatchString(id) {
		return Run{}, errf("run %q not found", id)
	}
	var run Run
	if err := readJSON(filepath.Join(r.dir(id), "run.json"), &run.RunState); err != nil {
		return Run{}, errf("run %q not found", id)
	}
	if err := readJSON(filepath.Join(r.dir(id), "drafts.json"), &run.Drafts); err != nil {
		return Run{}, errf("run %q has no draft queue", id)
	}
	if run.Drafts == nil {
		run.Drafts = []Draft{}
	}
	return run, nil
}

func (run *Run) find(draftID string) (int, error) {
	if !draftIDRe.MatchString(draftID) {
		return 0, errf("draft %q not found", draftID)
	}
	for i := range run.Drafts {
		if run.Drafts[i].ID == draftID {
			return i, nil
		}
	}
	return 0, errf("draft %q not found", draftID)
}

// writeRun persists run.json and drafts.json (and response.json when raw is
// given — only on the first write). Directories are 0700 and files 0600:
// this is a cache of a search for named people, readable by the owner and
// nothing else.
func (r *RunStore) writeRun(run Run, raw []sources.CandidateDraft) error {
	if err := os.MkdirAll(r.root, 0o700); err != nil {
		return err
	}
	dir := r.dir(run.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if raw != nil {
		if err := writeJSON0600(filepath.Join(dir, "response.json"), raw); err != nil {
			return err
		}
	}
	// derived paths never reach the cache: the network edges own that fact,
	// and a stale route on disk would outlive the edge it came from
	drafts := make([]Draft, len(run.Drafts))
	for i, d := range run.Drafts {
		d.Paths = nil
		drafts[i] = d
	}
	if err := writeJSON0600(filepath.Join(dir, "drafts.json"), drafts); err != nil {
		return err
	}
	return writeJSON0600(filepath.Join(dir, "run.json"), run.RunState)
}

func writeJSON0600(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	// WriteFile only applies the mode on create; make it true for an existing
	// tmp too, then swap in atomically
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeJSONDir0600 is the sandboxed variant of writeJSON0600 that also creates
// the parent directory. Every direct file write in the recruiting package is
// rooted here (in runs.go) so the "only runs.go may open a file to write"
// invariant the writeless test enforces holds for the derived Ashby sync state
// (<dataDir>/recruiting/ashby.json) too.
func writeJSONDir0600(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeJSON0600(path, v)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// ---- dedupe ----

// candidateIndex is the vault's candidates keyed the two ways a draft can
// match one: by external id (source:externalId) and by normalized name.
type candidateIndex struct {
	byRef  map[string]string   // "openalex:A123" → cand id
	byName map[string][]string // normalized name → cand ids
	orgs   map[string]string   // cand id → normalized org
}

func (s *Store) candidateIndex() candidateIndex {
	idx := candidateIndex{byRef: map[string]string{}, byName: map[string][]string{}, orgs: map[string]string{}}
	for _, slug := range s.CandidateSlugs() {
		doc := s.LoadCandidate(slug)
		id := doc.Get("id")
		if id == "" {
			id = CandidateID(slug)
		}
		if ref := strings.TrimSpace(doc.Get("source_ref")); ref != "" {
			idx.byRef[ref] = id
		}
		if name := normalizeKey(doc.Get("name")); name != "" {
			idx.byName[name] = append(idx.byName[name], id)
		}
		idx.orgs[id] = normalizeKey(doc.Profile()["org"])
	}
	return idx
}

// match reports the candidate a draft duplicates, and why: external id
// first when the draft carries one, then normalized name + org. A name match
// against a record with no org (or a draft with none) still counts — the
// store refuses a same-name record anyway, and a duplicate that says so is
// better than an accept that fails.
func (idx candidateIndex) match(d sources.CandidateDraft) (string, string) {
	if ref := SourceRef(d); ref != "" {
		if id, ok := idx.byRef[ref]; ok {
			return id, "external id " + ref
		}
	}
	name := normalizeKey(d.Name)
	if name == "" {
		return "", ""
	}
	org := normalizeKey(d.Org)
	for _, id := range idx.byName[name] {
		have := idx.orgs[id]
		if org == "" || have == "" || org == have {
			if org != "" && org == have {
				return id, "same name and org"
			}
			return id, "same name"
		}
	}
	return "", ""
}

// SourceRef is the record-side key for an adapter's external id:
// "<source>:<externalId>", or "" when the draft has none.
func SourceRef(d sources.CandidateDraft) string {
	ext := strings.TrimSpace(d.ExternalID)
	if ext == "" {
		return ""
	}
	return strings.TrimSpace(d.SourceID) + ":" + ext
}

// normalizeKey folds a name or org for comparison: lowercase, letters and
// digits only, single spaces.
func normalizeKey(s string) string {
	var b strings.Builder
	space := false
	for _, c := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c > 127 && !isSpaceOrPunct(c):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(c)
		default:
			space = true
		}
	}
	return b.String()
}

func isSpaceOrPunct(c rune) bool {
	return strings.ContainsRune(" \t\n\r.,;:'\"()[]{}-_/\\|·•", c)
}

// Adapter returns one registered adapter by id. The intake's preview needs to
// ask ONE source about ONE reference without recording a run — a preview is
// not a search, so it leaves no cache behind.
func (r *RunStore) Adapter(id string) (sources.Adapter, bool) {
	a, ok := r.adapters[strings.TrimSpace(id)]
	return a, ok
}
