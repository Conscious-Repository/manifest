package realestate

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Slug   string  `json:"slug"` // property slug, or "admin:<entity-slug>" for the admin lane
	Amount float64 `json:"amount"`
	WorkID string  `json:"workId,omitempty"` // optional stage/todo tether (hard lane only)
	Cat    string  `json:"cat,omitempty"`    // budget category: soft | carry | acquisition (blank = hard)
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
	Assignments []Alloc `json:"assignments,omitempty"`
	Remembered  bool    `json:"remembered,omitempty"` // prefilled from vendor memory
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
// existing lot and of `ledgerKeys` (every ledger line across the portfolio).
// prefill maps lower(vendor) → (category, propertySlug) from vendor memory —
// remembered vendors land pre-assigned so the user's job is confirmation.
func (s *StatementStore) Ingest(label string, rows []StatementRow, ledgerKeys map[string]bool, prefillCat, prefillProp map[string]string) (added, dups int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	for _, r := range s.st.Rows {
		seen[DedupeKey(r.Date, r.Amount, r.Vendor)] = true
	}
	today := time.Now().Format("2006-01-02")
	for _, r := range rows {
		key := DedupeKey(r.Date, r.Amount, r.Vendor)
		if seen[key] || ledgerKeys[key] {
			dups++
			continue
		}
		seen[key] = true
		r.ID = stmtID(key, label)
		r.Statement = label
		r.Imported = today
		r.State = "pending"
		vk := strings.ToLower(strings.TrimSpace(r.Vendor))
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

// Update patches one row's category/assignments/state (the workbench's row
// interactions). Applied rows are immutable.
func (s *StatementStore) Update(id string, category *string, assignments *[]Alloc, state *string) (StatementRow, error) {
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
		if assignments != nil {
			r.Assignments = *assignments
		}
		if state != nil {
			switch *state {
			case "pending", "assigned", "split", "skipped":
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
		if strings.TrimSpace(r.Category) == "" {
			return nil, fmt.Errorf("row %s has no category", r.ID)
		}
		var sum float64
		for _, a := range r.Assignments {
			if strings.TrimSpace(a.Slug) == "" {
				return nil, fmt.Errorf("row %s has an empty property", r.ID)
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

func stmtID(key, label string) string {
	h := sha1.Sum([]byte(key + "|" + label))
	return "stmt-" + hex.EncodeToString(h[:6])
}
