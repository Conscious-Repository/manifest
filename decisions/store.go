package decisions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/record"
)

// Store reads and writes decision notes under <vault>/<root>/<id>.md (root =
// "system/decisions"). Every write goes through the injected write func —
// main binds it to the narrow `decisions` vaultwriter capability; this
// package never opens a vault file to write, and a nil writer fails loudly.
// There is no index: List reads the directory (the files are the truth).
type Store struct {
	vaultRoot string
	root      string // vault-relative, slash form
	write     func(abs string, data []byte) error
	mu        sync.Mutex
}

// NewStore builds the store.
func NewStore(vaultRoot, root string, write func(string, []byte) error) *Store {
	if write == nil {
		write = func(string, []byte) error {
			return errors.New("decisions: no vault writer injected (§A3 write boundary)")
		}
	}
	return &Store{vaultRoot: vaultRoot, root: filepath.ToSlash(root), write: write}
}

// Root returns the vault-relative record root.
func (s *Store) Root() string { return s.root }

// Rel returns the vault-relative slash path of a decision's note.
func (s *Store) Rel(id string) string { return s.root + "/" + id + ".md" }

// Path returns the absolute path of a decision's note.
func (s *Store) Path(id string) string {
	return filepath.Join(s.vaultRoot, filepath.FromSlash(s.root), id+".md")
}

// ValidID is the guard that keeps an id a filename and never a path: the
// shape MintID produces (a slug, optionally with a -N suffix).
func ValidID(id string) bool {
	return id != "" && id == record.Slug(id, 0)
}

// MintID derives a fresh id from a title — the slug, suffixed -2, -3, … past
// the ids already taken (the aion-bl/<slug> rule, unprefixed: the directory is
// the namespace).
func MintID(title string, taken map[string]bool) string {
	base := record.Slug(title, 60)
	if base == "" {
		base = "decision"
	}
	id := base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// Load reads one note; ok=false when absent or the id is not a filename.
func (s *Store) Load(id string) (*Doc, bool) {
	if !ValidID(id) {
		return nil, false
	}
	b, err := os.ReadFile(s.Path(id))
	if err != nil {
		return nil, false
	}
	return Parse(string(b)), true
}

// Get projects one decision.
func (s *Store) Get(id string) (Decision, bool) {
	d, ok := s.Load(id)
	if !ok {
		return Decision{}, false
	}
	dec := d.Decision()
	dec.ID = id
	return dec, true
}

// List projects every note in the directory: newest captured first, ties by
// id so the order is stable.
func (s *Store) List() []Decision {
	out := []Decision{}
	for _, id := range s.ids() {
		if dec, ok := s.Get(id); ok {
			out = append(out, dec)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Captured != out[j].Captured {
			return out[i].Captured > out[j].Captured
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *Store) ids() []string {
	entries, _ := os.ReadDir(filepath.Join(s.vaultRoot, filepath.FromSlash(s.root)))
	var out []string
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".md")
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || !ValidID(id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func (s *Store) save(id string, d *Doc) error {
	return s.write(s.Path(id), []byte(Serialize(d)))
}

// Create lays down a new note. The id is minted from the title unless the
// caller supplies one; a title already open under another id is refused
// (recurring decisions legitimately re-add a title whose earlier note is
// decided/revisited). Status defaults open; Captured defaults to now.
func (s *Store) Create(dec Decision, now time.Time) (Decision, error) {
	dec.Title = strings.TrimSpace(dec.Title)
	dec.Status = strings.ToLower(strings.TrimSpace(dec.Status))
	if dec.Status == "" {
		dec.Status = StatusOpen
	}
	if err := Validate(dec); err != nil {
		return Decision{}, err
	}
	if dec.Captured == "" {
		dec.Captured = now.Format("2006-01-02")
	}
	if dec.Status == StatusDecided && dec.Decided == "" {
		dec.Decided = now.Format("2006-01-02")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	taken := map[string]bool{}
	for _, have := range s.List() {
		taken[have.ID] = true
		if strings.EqualFold(have.Title, dec.Title) && have.Status != StatusDecided && have.Status != StatusRevisited {
			return Decision{}, fmt.Errorf("already in the ledger: %q (%s)", dec.Title, have.ID)
		}
	}
	if dec.ID = strings.TrimSpace(dec.ID); dec.ID == "" {
		dec.ID = MintID(dec.Title, taken)
	} else if !ValidID(dec.ID) {
		return Decision{}, fmt.Errorf("id %q is not a slug", dec.ID)
	} else if taken[dec.ID] {
		return Decision{}, fmt.Errorf("decision %s already exists", dec.ID)
	}
	if err := s.save(dec.ID, New(dec)); err != nil {
		return Decision{}, err
	}
	got, _ := s.Get(dec.ID)
	return got, nil
}

// Patch is a partial update: a nil field is untouched. Status follows what
// happened when it is not set explicitly: recording an Outcome on an open /
// deliberating decision decides it (aion's Decide, which needs an outcome);
// recording an ActualOutcome on a decided one revisits it. Decided /
// Revisited are stamped on those transitions.
type Patch struct {
	Title, Owner, Status, Outcome, Why, ExpectedOutcome, ActualOutcome, NeededBy *string
	Evidence, Downstream                                                         *[]Link
	Alternatives                                                                 *[]Alternative
	Sources                                                                      *[]string
}

// Change reports what an Update did: the projection before and after, the
// fields that changed (frontmatter keys and section names), and the
// lifecycle transition, if any — "decided", "revisited", "reopened" — so the
// ledger line can say what happened rather than just "updated".
type Change struct {
	Before, After Decision
	Fields        []string
	Transition    string
}

// Update applies a patch to one note and saves it once. Nothing changed →
// no write (a replayed update is not a second event).
func (s *Store) Update(id string, p Patch, now time.Time) (Change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.Load(id)
	if !ok {
		return Change{}, fmt.Errorf("decision %q not found", id)
	}
	before := d.Decision()
	before.ID = id
	ch := Change{Before: before}
	today := now.Format("2006-01-02")

	set := func(field string, ptr *string, get func() string, apply func(string)) {
		if ptr == nil {
			return
		}
		v := strings.TrimSpace(*ptr)
		if v == get() {
			return
		}
		apply(v)
		ch.Fields = append(ch.Fields, field)
	}
	set("title", p.Title, func() string { return before.Title }, func(v string) { d.SetScalar("title", v) })
	set("owner", p.Owner, func() string { return before.Owner }, func(v string) { d.SetScalar("owner", v) })
	set("needed-by", p.NeededBy, func() string { return before.NeededBy }, func(v string) { d.SetScalar("needed-by", v) })
	set("outcome", p.Outcome, func() string { return before.Outcome }, func(v string) { d.SetScalar("outcome", v) })
	set("why", p.Why, func() string { return before.Why }, d.SetWhy)
	set("expected outcome", p.ExpectedOutcome, func() string { return before.ExpectedOutcome }, d.SetExpected)
	set("actual outcome", p.ActualOutcome, func() string { return before.ActualOutcome }, d.SetActual)
	if p.Evidence != nil && !sameLinks(before.Evidence, *p.Evidence) {
		d.SetEvidence(*p.Evidence)
		ch.Fields = append(ch.Fields, "evidence")
	}
	if p.Downstream != nil && !sameLinks(before.Downstream, *p.Downstream) {
		d.SetDownstream(*p.Downstream)
		ch.Fields = append(ch.Fields, "downstream")
	}
	if p.Alternatives != nil && !sameAlternatives(before.Alternatives, *p.Alternatives) {
		d.SetAlternatives(*p.Alternatives)
		ch.Fields = append(ch.Fields, "alternatives")
	}
	if p.Sources != nil && strings.Join(before.Sources, "\x00") != strings.Join(*p.Sources, "\x00") {
		d.SetSources(*p.Sources)
		ch.Fields = append(ch.Fields, "sources")
	}

	// status: explicit, or implied by what was recorded
	status := before.Status
	given := func(p *string) bool { return p != nil && strings.TrimSpace(*p) != "" }
	switch {
	case p.Status != nil:
		status = strings.ToLower(strings.TrimSpace(*p.Status))
		if !ValidStatus(status) {
			return Change{}, fmt.Errorf("status %q is not one of %s", status, strings.Join(Statuses, "/"))
		}
	case given(p.ActualOutcome) && before.Status == StatusDecided:
		status = StatusRevisited
	case given(p.Outcome) && (before.Status == StatusOpen || before.Status == StatusDeliberating):
		status = StatusDecided
	}
	if status != before.Status {
		d.SetScalar("status", status)
		ch.Fields = append(ch.Fields, "status")
		switch {
		case status == StatusDecided:
			ch.Transition = "decided"
			if before.Decided == "" {
				d.SetScalar("decided", today)
				ch.Fields = append(ch.Fields, "decided")
			}
		case status == StatusRevisited:
			ch.Transition = "revisited"
			if before.Decided == "" { // revisiting implies it was decided
				d.SetScalar("decided", today)
				ch.Fields = append(ch.Fields, "decided")
			}
			d.SetScalar("revisited", today)
			ch.Fields = append(ch.Fields, "revisited")
		case before.Status == StatusDecided || before.Status == StatusRevisited:
			ch.Transition = "reopened"
		}
	}
	if len(ch.Fields) == 0 {
		ch.After = before
		return ch, nil
	}
	if err := s.save(id, d); err != nil {
		return Change{}, err
	}
	after, _ := s.Get(id)
	ch.After = after
	return ch, nil
}

// Changed reports whether the update wrote anything.
func (c Change) Changed() bool { return len(c.Fields) > 0 }

func sameLinks(a, b []Link) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i].Ref) != strings.TrimSpace(b[i].Ref) || strings.TrimSpace(a[i].Note) != strings.TrimSpace(b[i].Note) {
			return false
		}
	}
	return true
}

func sameAlternatives(a, b []Alternative) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i].Option) != strings.TrimSpace(b[i].Option) || strings.TrimSpace(a[i].Tradeoff) != strings.TrimSpace(b[i].Tradeoff) {
			return false
		}
	}
	return true
}
