package aion

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Store reads and writes the seven corpus files under <vault>/<root>/
// (root = "system/aion"). Every write goes through the injected write func
// (§A3 boundary — main binds it to the vaultwriter "aion" user-action
// capability; this package never opens a file to write).
type Store struct {
	vaultRoot string
	root      string // vault-relative, slash form ("system/aion")
	write     func(abs string, data []byte) error
}

func NewStore(vaultRoot, root string, write func(string, []byte) error) *Store {
	if write == nil {
		write = func(string, []byte) error {
			return fmt.Errorf("aion: no vault writer injected (§A3 write boundary)")
		}
	}
	return &Store{vaultRoot: vaultRoot, root: root, write: write}
}

// Root returns the vault-relative corpus dir ("system/aion").
func (s *Store) Root() string { return s.root }

// Path returns the absolute path of a corpus file.
func (s *Store) Path(name string) string {
	return filepath.Join(s.vaultRoot, filepath.FromSlash(s.root), name)
}

// Rel returns the vault-relative slash path of a corpus file.
func (s *Store) Rel(name string) string { return s.root + "/" + name }

func (s *Store) raw(name string) string {
	b, _ := os.ReadFile(s.Path(name))
	return string(b)
}

func (s *Store) save(name, content string) error {
	return s.write(s.Path(name), []byte(content))
}

// ---- typed load/save pairs ----

func (s *Store) LoadPeople() *PeopleDoc { return ParsePeople(s.raw("people.md")) }
func (s *Store) SavePeople(d *PeopleDoc) error {
	return s.save("people.md", SerializePeople(d))
}

func (s *Store) LoadVTO() *VTODoc { return ParseVTO(s.raw("vto.md")) }
func (s *Store) SaveVTO(d *VTODoc) error {
	return s.save("vto.md", SerializeVTO(d))
}

func (s *Store) LoadBacklog() *BacklogDoc { return ParseBacklog(s.raw("backlog.md")) }
func (s *Store) SaveBacklog(d *BacklogDoc) error {
	return s.save("backlog.md", SerializeBacklog(d))
}

func (s *Store) LoadHeuristics() *HeuristicsDoc { return ParseHeuristics(s.raw("heuristics.md")) }
func (s *Store) SaveHeuristics(d *HeuristicsDoc) error {
	return s.save("heuristics.md", SerializeHeuristics(d))
}

func (s *Store) LoadHiring() *HiringDoc { return ParseHiring(s.raw("hiring.md")) }
func (s *Store) SaveHiring(d *HiringDoc) error {
	return s.save("hiring.md", SerializeHiring(d))
}

func (s *Store) LoadReferences() *ReferencesDoc {
	return ParseReferences(s.raw("references.md"))
}
func (s *Store) SaveReferences(d *ReferencesDoc) error {
	return s.save("references.md", SerializeReferences(d))
}
func (s *Store) RawFile(name string) string { return s.raw(name) }

func (s *Store) LoadFinances() *FinancesDoc { return ParseFinances(s.raw("finances.md")) }
func (s *Store) SaveFinances(d *FinancesDoc) error {
	return s.save("finances.md", SerializeFinances(d))
}

// ---- semantic actions (invariants live here, not in clients) ----

// AddItem appends a new task/decision authored in the UI.
func (s *Store) AddItem(it *BacklogItem) error {
	if it.Kind != KindTask && it.Kind != KindDecision {
		return fmt.Errorf("kind must be task or decision")
	}
	if strings.TrimSpace(it.Text) == "" {
		return fmt.Errorf("title is required")
	}
	doc := s.LoadBacklog()
	it.ID = ItemID(it.Kind, it.Text)
	if doc.Find(it.ID) != nil {
		return fmt.Errorf("already in backlog: %q", it.Text)
	}
	if it.Status == "" {
		it.Status = StatusOpen
	}
	doc.AppendItem(it)
	return s.SaveBacklog(doc)
}

// UpdateItem edits a task/decision's editable fields. Status transitions:
// open ↔ in_progress ↔ done (done stamps done_on, reopening clears it);
// `decided` is unreachable here — only Decide sets it.
func (s *Store) UpdateItem(id string, set map[string]string, now time.Time) error {
	doc := s.LoadBacklog()
	it := doc.Find(id)
	if it == nil {
		return fmt.Errorf("item %q not found", id)
	}
	// only a decided DECISION is permanent — a task that arrived with a stray
	// [status:: decided] (extraction drift) must stay editable, or it locks
	// forever behind an error about decisions
	if it.Kind == KindDecision && it.Status == StatusDecided {
		return fmt.Errorf("a decided decision is permanent")
	}
	for key, val := range set {
		switch key {
		case "title":
			if strings.TrimSpace(val) == "" {
				return fmt.Errorf("title cannot be empty")
			}
			it.Text = val
			it.ID = ItemID(it.Kind, it.Text)
		case "owner":
			it.Owner = val
		case "rock":
			it.Rock = val
		case "due":
			it.Due = val
		case "needed_by":
			it.NeededBy = val
		case "rank":
			if val != "" {
				if _, err := strconv.Atoi(val); err != nil {
					return fmt.Errorf("rank must be a whole number")
				}
			}
			it.Rank = val
		case "status":
			switch val {
			case StatusOpen, StatusInProgress, StatusDone:
			default:
				return fmt.Errorf("status must be open, in_progress or done")
			}
			if it.Kind == KindDecision && val == StatusDone {
				return fmt.Errorf("decisions are decided, not done")
			}
			it.Status = val
			if it.Kind == KindTask {
				if val == StatusDone {
					it.Checked = true
					if it.DoneOn == "" {
						it.DoneOn = now.Format("2006-01-02")
					}
				} else {
					it.Checked = false
					it.DoneOn = ""
				}
			}
		default:
			return fmt.Errorf("unknown field %q", key)
		}
	}
	return s.SaveBacklog(doc)
}

// SetRanks writes [rank:: n] on many items in ONE backlog save (the unified
// todos drag-to-rank batch — redesign stage 4). Unknown ids are skipped;
// empty rank clears the field. No-op (no write) when nothing changed.
func (s *Store) SetRanks(ranks map[string]string) error {
	if len(ranks) == 0 {
		return nil
	}
	doc := s.LoadBacklog()
	changed := false
	for id, rank := range ranks {
		it := doc.Find(id)
		if it == nil || (it.Kind == KindDecision && it.Status == StatusDecided) {
			continue
		}
		if it.Rank != rank {
			it.Rank = rank
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.SaveBacklog(doc)
}

// LinkageReport summarizes a BackfillLinkage pass (for the cmd's dry-run).
type LinkageReport struct {
	RocksRewritten int
	RocksNulled    int
	DecidedFilled  int
	OwnersFixed    int
	Changes        []string // human lines: "<title> · rock <old>→<new>"
}

// BackfillLinkage is the one-time linkage migration (aion-linkage-scope):
// it rewrites free-text rocks to canonical ids or null per rockMap (a raw
// value → id, "" = drop the rock), fills missing [decided::] on decided
// decisions from [captured::], and normalizes owner initials per ownerMap
// (token → replacement, "" = drop the token). One parse → edit →
// serialize; the caller's injected write keeps it audited + fixpoint-safe.
// A rock value absent from rockMap is left untouched (canonical aion/* ids
// and alias-resolved values stay as-is). dry reports without saving.
func (s *Store) BackfillLinkage(rockMap map[string]string, ownerMap map[string]string, dry bool) (LinkageReport, error) {
	var rep LinkageReport
	doc := s.LoadBacklog()
	for _, it := range allBacklogItems(doc) {
		if newRock, ok := rockMap[it.Rock]; ok && newRock != it.Rock {
			rep.Changes = append(rep.Changes, it.Text+" · rock "+orDash(it.Rock)+"→"+orDash(newRock))
			it.Rock = newRock
			if newRock == "" {
				rep.RocksNulled++
			} else {
				rep.RocksRewritten++
			}
		}
		if it.Kind == KindDecision && it.Status == StatusDecided && it.Decided == "" && it.Captured != "" {
			it.Decided = it.Captured
			rep.DecidedFilled++
			rep.Changes = append(rep.Changes, it.Text+" · decided ←captured "+it.Captured)
		}
		if newOwner, changed := remapOwner(it.Owner, ownerMap); changed {
			rep.Changes = append(rep.Changes, it.Text+" · owner "+orDash(it.Owner)+"→"+orDash(newOwner))
			it.Owner = newOwner
			rep.OwnersFixed++
		}
	}
	if dry {
		return rep, nil
	}
	return rep, s.SaveBacklog(doc)
}

// allBacklogItems flattens top-level items + their children (the migration
// touches nested items too).
func allBacklogItems(d *BacklogDoc) []*BacklogItem {
	var out []*BacklogItem
	var rec func(items []*BacklogItem)
	rec = func(items []*BacklogItem) {
		for _, it := range items {
			out = append(out, it)
			rec(it.Children)
		}
	}
	rec(d.Items())
	return out
}

// remapOwner applies ownerMap token-wise to a slash-separated owner value
// ("BA/YS" → "BA/Y"); "" replacement drops the token. Returns the new value
// and whether anything changed.
func remapOwner(owner string, ownerMap map[string]string) (string, bool) {
	if owner == "" || len(ownerMap) == 0 {
		return owner, false
	}
	var kept []string
	changed := false
	for _, tok := range strings.Split(owner, "/") {
		tok = strings.TrimSpace(tok)
		if repl, ok := ownerMap[tok]; ok {
			changed = true
			if repl != "" {
				kept = append(kept, repl)
			}
			continue
		}
		if tok != "" {
			kept = append(kept, tok)
		}
	}
	if !changed {
		return owner, false
	}
	return strings.Join(kept, "/"), true
}

func orDash(s string) string {
	if s == "" {
		return "∅"
	}
	return s
}

// LinkEdit is one reconcile edit: set rock/decided/owner on an item by id.
// A nil field is left unchanged; an empty-string field clears that value.
// Only these three linkage fields are touched — never status/outcome/title
// — so it applies to DECIDED decisions too (setting a decision's rock is
// metadata, not re-deciding it; UpdateItem's permanence guard doesn't apply).
type LinkEdit struct {
	ID       string  `json:"id"`
	Rock     *string `json:"rock,omitempty"`
	Decided  *string `json:"decided,omitempty"`
	Owner    *string `json:"owner,omitempty"`
	NeededBy *string `json:"neededBy,omitempty"` // decision deadline (ISO) → portal diamond
}

// BatchLink applies a set of LinkEdits in one parse → edit → serialize →
// audited write. Returns how many items changed. Unknown ids are skipped.
func (s *Store) BatchLink(edits []LinkEdit) (int, error) {
	doc := s.LoadBacklog()
	byID := map[string]*BacklogItem{}
	for _, it := range allBacklogItems(doc) {
		byID[it.ID] = it
	}
	n := 0
	for _, e := range edits {
		it := byID[e.ID]
		if it == nil {
			continue
		}
		changed := false
		if e.Rock != nil && *e.Rock != it.Rock {
			it.Rock = strings.TrimSpace(*e.Rock)
			changed = true
		}
		if e.Owner != nil && *e.Owner != it.Owner {
			it.Owner = strings.TrimSpace(*e.Owner)
			changed = true
		}
		if e.Decided != nil && it.Kind == KindDecision && *e.Decided != it.Decided {
			it.Decided = strings.TrimSpace(*e.Decided)
			// a dated decision is a decided one — keep status coherent
			if it.Decided != "" && it.Status != StatusDecided {
				it.Status = StatusDecided
			}
			changed = true
		}
		if e.NeededBy != nil && it.Kind == KindDecision && *e.NeededBy != it.NeededBy {
			// deadline on an OPEN decision — does NOT flip status (unlike decided)
			it.NeededBy = strings.TrimSpace(*e.NeededBy)
			changed = true
		}
		if changed {
			n++
		}
	}
	if n == 0 {
		return 0, nil
	}
	return n, s.SaveBacklog(doc)
}

// Decide resolves an open decision — append-only: the line is never
// removed, it gains [decided::] + [outcome::] and status decided.
func (s *Store) Decide(id, outcome string, now time.Time) error {
	if strings.TrimSpace(outcome) == "" {
		return fmt.Errorf("an outcome is required")
	}
	doc := s.LoadBacklog()
	it := doc.Find(id)
	if it == nil {
		return fmt.Errorf("decision %q not found", id)
	}
	if it.Kind != KindDecision {
		return fmt.Errorf("only a decision can be decided")
	}
	if it.Status == StatusDecided {
		return fmt.Errorf("already decided %s", it.Decided)
	}
	it.Status = StatusDecided
	it.Decided = now.Format("2006-01-02")
	it.Outcome = outcome
	return s.SaveBacklog(doc)
}

// EditHeuristic rewrites a statement (identity follows the text).
func (s *Store) EditHeuristic(id, statement string) error {
	if strings.TrimSpace(statement) == "" {
		return fmt.Errorf("statement cannot be empty")
	}
	doc := s.LoadHeuristics()
	h := doc.FindLive(id)
	if h == nil {
		return fmt.Errorf("heuristic %q not found", id)
	}
	h.Text = statement
	h.ID = HeuristicID(statement)
	return s.SaveHeuristics(doc)
}

// MergeHeuristics unions `from` into `into` and removes `from` (the
// prune/synthesize loop). Sources are never lost.
func (s *Store) MergeHeuristics(intoID, fromID string) error {
	doc := s.LoadHeuristics()
	if !doc.Merge(intoID, fromID) {
		return fmt.Errorf("merge needs two distinct live heuristics")
	}
	return s.SaveHeuristics(doc)
}

// ReorderHeuristics applies a full permutation of the live list.
func (s *Store) ReorderHeuristics(ids []string) error {
	doc := s.LoadHeuristics()
	if !doc.Reorder(ids) {
		return fmt.Errorf("reorder must be a permutation of the live heuristics")
	}
	return s.SaveHeuristics(doc)
}

// RetireHeuristic moves an entry to ## retired (never deletes).
func (s *Store) RetireHeuristic(id string) error {
	doc := s.LoadHeuristics()
	if !doc.Retire(id) {
		return fmt.Errorf("heuristic %q not found", id)
	}
	return s.SaveHeuristics(doc)
}

// SetFinances upserts the recognized frontmatter keys; the private body is
// untouched.
func (s *Store) SetFinances(values map[string]string) error {
	doc := s.LoadFinances()
	for _, key := range FinanceKeys {
		if v, ok := values[key]; ok {
			doc.Set(key, v)
		}
	}
	return s.SaveFinances(doc)
}
