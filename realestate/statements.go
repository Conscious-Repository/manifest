package realestate

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The statement workbench's parking lot (admin-portal plan §G): bank-statement
// rows live here — categorized, assigned to properties (or split across
// several), then applied into per-property ledger csvs. Persisted under
// <dataDir>/realestate/statements.json so half-finished categorization work
// survives sessions. States: pending → assigned|split → applied; skipped is the
// explicit parked-forever lane (personal spend), reversible.

type Alloc struct {
	Slug     string  `json:"slug"` // property slug, or "admin:<entity-slug>" for the admin lane
	Amount   float64 `json:"amount"`
	WorkID   string  `json:"workId,omitempty"`   // optional rock-tree tether (hard lane only)
	Contract string  `json:"contract,omitempty"` // optional contract draw-down (overhaul §7 — the third hop)
	Cat      string  `json:"cat,omitempty"`      // budget category: soft | acquisition (blank = hard); inflows: rent | capital
}

type StatementRow struct {
	ID          string  `json:"id"`
	Statement   string  `json:"statement"` // source file label, e.g. "chase-july.csv"
	Imported    string  `json:"imported"`  // YYYY-MM-DD
	Date        string  `json:"date"`
	Vendor      string  `json:"vendor"`
	Note        string  `json:"note"`
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Entity      string  `json:"entity,omitempty"` // paying entity (bound at upload)
	State       string  `json:"state"`            // pending | assigned | split | applied | skipped
	Inflow      bool    `json:"inflow,omitempty"` // deposit (rent / capital / transfer) — not an expense
	Reason      string  `json:"reason,omitempty"` // dismiss reason when skipped: personal | transfer | duplicate | other:<note>
	Assignments []Alloc `json:"assignments,omitempty"`
	Remembered  bool    `json:"remembered,omitempty"` // prefilled from vendor memory
	Source      string  `json:"source,omitempty"`     // "feed" = bank-feed sync (badge); "" = csv upload
	// MerchantKey is DERIVED (MerchantKey(vendor)), stamped on the copies List
	// returns and never persisted — the stored rows keep it zero. One
	// definition of "same merchant" serves the client's grouping.
	MerchantKey string `json:"merchantKey,omitempty"`
}

type StatementStore struct {
	path string
	mu   sync.Mutex
	st   stmtState
}

type stmtState struct {
	Rows       []StatementRow `json:"rows"`
	LastImport string         `json:"lastImport"`
}

func NewStatementStore(dataDir string) *StatementStore {
	s := &StatementStore{path: filepath.Join(dataDir, "realestate", "statements.json")}
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, &s.st)
	}
	return s
}

// DedupeKey matches the ledger import key: date|amount|lower(vendor).
func DedupeKey(date string, amount float64, vendor string) string {
	return strings.TrimSpace(date) + "|" + fmt.Sprintf("%.2f", amount) + "|" + strings.ToLower(strings.TrimSpace(vendor))
}

// Ingest adds parsed statement rows to the lot, skipping duplicates of the
// existing lot and of `ledgerKeys` (a COUNT per key — every ledger line across
// the portfolio). prefill maps lower(vendor) → (category, propertySlug) from
// vendor memory — remembered vendors land pre-assigned so the user's job is
// confirmation.
//
// Dedupe is by MULTIPLICITY, not by presence: a bank legitimately posts the
// same charge to the same payee twice in a day (proven by the running balance
// in the owner's export), and a statement lists each of them, so a batch
// carrying a key twice may add a second copy when the lot holds only one.
// Re-uploading that same file still adds nothing, because by then the lot
// holds both.
func (s *StatementStore) Ingest(label string, rows []StatementRow, ledgerKeys map[string]int, prefillCat, prefillProp map[string]string) (added, dups int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	have := map[string]int{}
	for k, n := range ledgerKeys {
		have[k] += n
	}
	for _, r := range s.st.Rows {
		have[DedupeKey(r.Date, r.Amount, r.Vendor)]++
	}
	batch := map[string]int{}
	today := time.Now().Format("2006-01-02")
	for _, r := range rows {
		key := DedupeKey(r.Date, r.Amount, r.Vendor)
		batch[key]++
		if batch[key] <= have[key] {
			dups++
			continue
		}
		r.ID = stmtID(key, label, batch[key]-1)
		r.Statement = label
		r.Imported = today
		r.State = "pending"
		vk := strings.ToLower(strings.TrimSpace(r.Vendor))
		if !r.Inflow { // vendor memory is expense memory — never prefill deposits
			if c, ok := prefillCat[vk]; ok && r.Category == "" {
				r.Category = c
				r.Remembered = true
			}
			if p, ok := prefillProp[vk]; ok {
				r.Assignments = []Alloc{{Slug: p, Amount: r.Amount}}
				if r.Category != "" {
					r.State = "assigned"
				}
				r.Remembered = true
			}
		}
		s.st.Rows = append(s.st.Rows, r)
		added++
	}
	s.st.LastImport = today
	s.save()
	return added, dups
}

// List returns the lot, newest imports first, pending-ish before applied.
func (s *StatementStore) List() ([]StatementRow, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]StatementRow(nil), s.st.Rows...)
	for i := range out {
		out[i].MerchantKey = MerchantKey(out[i].Vendor)
	}
	rank := map[string]int{"pending": 0, "assigned": 1, "split": 1, "skipped": 2, "applied": 3}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := rank[out[i].State], rank[out[j].State]; a != b {
			return a < b
		}
		if out[i].Imported != out[j].Imported {
			return out[i].Imported > out[j].Imported
		}
		return out[i].Date > out[j].Date
	})
	return out, s.st.LastImport
}

// SetCategory stamps one category across many rows at once — the "categorize
// N more like this" gesture. It only touches rows still in play (pending /
// assigned / split): applied rows are immutable here as everywhere, and a
// skipped row was dismissed for a reason. It never files — the rows are being
// categorized in bulk, not reviewed.
func (s *StatementStore) SetCategory(ids []string, category string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	category = strings.TrimSpace(category)
	n := 0
	for i := range s.st.Rows {
		r := &s.st.Rows[i]
		if !want[r.ID] || (r.State != "pending" && r.State != "assigned" && r.State != "split") {
			continue
		}
		r.Category = category
		n++
	}
	if n > 0 {
		s.save()
	}
	return n
}

// Update patches one row's category/note/assignments/state/reason (the
// workbench's row interactions). The note starts as the bank memo; the owner
// overwrites it freely before apply (bank-accounts plan §5) — it becomes the
// ledger note verbatim. Applied rows are immutable. Skipping REQUIRES a
// reason — every statement item must end assigned or dismissed-with-reason.
func (s *StatementStore) Update(id string, category, note *string, assignments *[]Alloc, state, reason *string) (StatementRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Rows {
		r := &s.st.Rows[i]
		if r.ID != id {
			continue
		}
		if r.State == "applied" {
			return *r, fmt.Errorf("row already applied")
		}
		if category != nil {
			r.Category = strings.TrimSpace(*category)
		}
		if note != nil {
			r.Note = strings.TrimSpace(*note)
		}
		if assignments != nil {
			r.Assignments = *assignments
		}
		if reason != nil {
			r.Reason = strings.TrimSpace(*reason)
		}
		if state != nil {
			switch *state {
			case "pending", "assigned", "split":
				r.State = *state
				r.Reason = ""
			case "skipped":
				if r.Reason == "" {
					return *r, fmt.Errorf("a dismiss reason is required (personal · transfer · duplicate · other)")
				}
				r.State = *state
			default:
				return *r, fmt.Errorf("bad state %q", *state)
			}
		}
		// derive state when not explicitly set: assignments present → assigned/split
		if state == nil && r.State != "skipped" {
			switch len(r.Assignments) {
			case 0:
				r.State = "pending"
			case 1:
				r.State = "assigned"
			default:
				r.State = "split"
			}
		}
		s.save()
		return *r, nil
	}
	return StatementRow{}, fmt.Errorf("row not found")
}

// Get returns one row by id.
func (s *StatementStore) Get(id string) (StatementRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.st.Rows {
		if r.ID == id {
			return r, true
		}
	}
	return StatementRow{}, false
}

// Unfile returns an APPLIED row to the lot (assigned, edits allowed again).
// Callers delete the written ledger rows FIRST — this only flips state.
func (s *StatementStore) Unfile(id string) (StatementRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Rows {
		r := &s.st.Rows[i]
		if r.ID != id {
			continue
		}
		if r.State != "applied" {
			return *r, fmt.Errorf("row is %s — only applied rows unfile", r.State)
		}
		r.State = "assigned"
		s.save()
		return *r, nil
	}
	return StatementRow{}, fmt.Errorf("row not found")
}

// UpdateApplied patches category/note/assignment-tethers on an APPLIED row —
// the filed-edit lane (owner call 2026-08-19: history rows edit in place).
// Assignment IDENTITY (slugs + amounts) must not change here; callers rewrote
// the ledger rows first and moving money means unfile → refile.
func (s *StatementStore) UpdateApplied(id string, category, note *string, assignments *[]Alloc) (StatementRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Rows {
		r := &s.st.Rows[i]
		if r.ID != id {
			continue
		}
		if r.State != "applied" {
			return *r, fmt.Errorf("row is %s — not filed", r.State)
		}
		if category != nil {
			r.Category = strings.TrimSpace(*category)
		}
		if note != nil {
			r.Note = strings.TrimSpace(*note)
		}
		if assignments != nil {
			if len(*assignments) != len(r.Assignments) {
				return *r, fmt.Errorf("filed assignments can't change shape — unfile first")
			}
			for j, a := range *assignments {
				old := r.Assignments[j]
				if a.Slug != old.Slug || a.Amount != old.Amount {
					return *r, fmt.Errorf("filed targets/amounts can't change — unfile first")
				}
			}
			r.Assignments = *assignments
		}
		s.save()
		return *r, nil
	}
	return StatementRow{}, fmt.Errorf("row not found")
}

// Applicable validates and returns the rows ready to write: state
// assigned/split, category set, allocations sum to the amount (±1¢).
func (s *StatementStore) Applicable(ids []string) ([]StatementRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []StatementRow
	for _, r := range s.st.Rows {
		if !want[r.ID] {
			continue
		}
		if r.State != "assigned" && r.State != "split" {
			return nil, fmt.Errorf("row %s is %s — not applicable", r.ID, r.State)
		}
		if strings.TrimSpace(r.Category) == "" && !r.Inflow {
			return nil, fmt.Errorf("row %s has no category", r.ID)
		}
		var sum float64
		for _, a := range r.Assignments {
			if strings.TrimSpace(a.Slug) == "" {
				return nil, fmt.Errorf("row %s has an empty property", r.ID)
			}
			// a $0 slice on a split is always a mistake — the editor seeds
			// new slices at 0 and the Σ check alone can't see one left behind
			// (total + 0 still sums)
			if len(r.Assignments) > 1 && a.Amount == 0 {
				return nil, fmt.Errorf("row %s has a $0 slice for %s — set every amount or use ÷ even", r.ID, a.Slug)
			}
			sum += a.Amount
		}
		if diff := sum - r.Amount; diff > 0.01 || diff < -0.01 {
			return nil, fmt.Errorf("row %s allocations sum %.2f ≠ %.2f", r.ID, sum, r.Amount)
		}
		out = append(out, r)
	}
	return out, nil
}

// MarkApplied flips rows to applied after their ledger writes succeeded.
func (s *StatementStore) MarkApplied(ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	for i := range s.st.Rows {
		if want[s.st.Rows[i].ID] {
			s.st.Rows[i].State = "applied"
		}
	}
	s.save()
}

func (s *StatementStore) save() {
	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(s.path, b, 0o644)
}

// stmtID is content-derived so an id survives a restart. seq disambiguates the
// genuine same-day repeats the multiplicity rule now admits; seq 0 hashes
// exactly as it always did, so every id already in the lot is unchanged.
func stmtID(key, label string, seq int) string {
	src := key + "|" + label
	if seq > 0 {
		src += "|" + strconv.Itoa(seq)
	}
	h := sha1.Sum([]byte(src))
	return "stmt-" + hex.EncodeToString(h[:6])
}
