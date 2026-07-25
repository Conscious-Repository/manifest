package realestate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ImportMemory persists the bank-CSV import's learned state under DataDir
// (outside both trees, like feed-signals.json): vendor→category so a vendor's
// category auto-fills on the next import, and header-signature→column-mapping so
// the same bank's export never needs re-mapping. Mirrors signals.Store (mutex +
// JSON save()).
type ImportMemory struct {
	path string
	mu   sync.Mutex
	st   importState
}

type importState struct {
	VendorCategory map[string]string            `json:"vendorCategory"` // lower(vendor) → category
	Mappings       map[string]map[string]string `json:"mappings"`       // header signature → {field: column}
}

func NewImportMemory(dataDir string) *ImportMemory {
	m := &ImportMemory{
		path: filepath.Join(dataDir, "realestate-import.json"),
		st:   importState{VendorCategory: map[string]string{}, Mappings: map[string]map[string]string{}},
	}
	if b, err := os.ReadFile(m.path); err == nil {
		_ = json.Unmarshal(b, &m.st)
		if m.st.VendorCategory == nil {
			m.st.VendorCategory = map[string]string{}
		}
		if m.st.Mappings == nil {
			m.st.Mappings = map[string]map[string]string{}
		}
	}
	return m
}

// Signature normalizes a header row into the mapping key.
func Signature(headers []string) string {
	h := make([]string, len(headers))
	for i, s := range headers {
		h[i] = strings.ToLower(strings.TrimSpace(s))
	}
	return strings.Join(h, "|")
}

// Lookup returns the remembered mapping for a header signature (nil if none)
// and the vendor→category map (for auto-filling categories).
func (m *ImportMemory) Lookup(sig string) (map[string]string, map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var mapping map[string]string
	if mm, ok := m.st.Mappings[sig]; ok {
		mapping = map[string]string{}
		for k, v := range mm {
			mapping[k] = v
		}
	}
	vc := map[string]string{}
	for k, v := range m.st.VendorCategory {
		vc[k] = v
	}
	return mapping, vc
}

// Remember stores a used column mapping and the vendor→category pairs from an
// applied import.
func (m *ImportMemory) Remember(sig string, mapping map[string]string, vendorCats map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sig != "" && len(mapping) > 0 {
		m.st.Mappings[sig] = mapping
	}
	for v, c := range vendorCats {
		v = strings.ToLower(strings.TrimSpace(v))
		c = strings.TrimSpace(c)
		if v != "" && c != "" {
			m.st.VendorCategory[v] = c
		}
	}
	m.save()
}

func (m *ImportMemory) save() {
	b, err := json.MarshalIndent(m.st, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(m.path, b, 0o644)
}

// SuggestMapping guesses field→column from header names (used when no mapping
// is remembered). Fields: date, amount, vendor, note.
func SuggestMapping(headers []string) map[string]string {
	out := map[string]string{}
	pick := func(field string, needles ...string) {
		if _, done := out[field]; done {
			return
		}
		for _, n := range needles {
			for _, h := range headers {
				if strings.Contains(strings.ToLower(h), n) {
					out[field] = h
					return
				}
			}
		}
	}
	pick("date", "transaction date", "posted", "date")
	pick("amount", "amount", "debit", "value")
	pick("vendor", "description", "payee", "merchant", "vendor", "name")
	pick("note", "memo", "note", "category")
	return out
}

// VendorList returns the distinct vendors across a ledger, sorted (for the
// quick-add autocomplete datalist).
func VendorList(rows []LedgerRow) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		v := strings.TrimSpace(r.Vendor)
		if v != "" && !seen[strings.ToLower(v)] {
			seen[strings.ToLower(v)] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
