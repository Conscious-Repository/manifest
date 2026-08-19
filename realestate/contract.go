package realestate

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"manifest/mdfm"
	"manifest/vaultindex"
)

// Contract records (overhaul §3.2): the source of COMMITTED money. One
// record per contract under system/realestate/contracts/, categories:
// [contract]. Money is procured by trade package — a contract allocates its
// total across one or more (property, node) targets, so a $25.5k masonry
// contract spanning two properties is ONE record pointing at ONE document.
// The ledger stays pure cash facts; expenses draw a contract down via the
// [contract:: slug] note token. Terms/exclusions/risk items ride as body
// prose (display-only). Frontmatter is scalar-line (kernel style); the
// allocations list is inline flow: ["prop | node | amount", …].

// ContractStatuses is the status enum, in lifecycle order.
var ContractStatuses = []string{"proposed", "accepted", "declined", "expired", "closed"}

// ContractAllocation is one slice of a contract's total: property → node.
type ContractAllocation struct {
	Property string  `json:"property"` // property slug
	NodeID   string  `json:"nodeId"`   // rock/milestone/task work id
	Amount   float64 `json:"amount"`
}

// Contract is one parsed record + its derived draw picture.
type Contract struct {
	Path        string               `json:"path"`
	Slug        string               `json:"slug"`
	Name        string               `json:"name"`       // first `# ` heading, else slug
	Contractor  string               `json:"contractor"` // contractor slug ([[wikilink]] unwrapped)
	Status      string               `json:"status"`     // proposed | accepted | declined | expired | closed
	Total       float64              `json:"total"`
	Date        string               `json:"date,omitempty"`
	Expires     string               `json:"expires,omitempty"`
	Doc         string               `json:"doc,omitempty"` // CAS ref (sha256:…) or legacy path
	Allocations []ContractAllocation `json:"allocations"`
	Terms       []string             `json:"terms,omitempty"`      // ## terms bullets
	Exclusions  []string             `json:"exclusions,omitempty"` // ## exclusions bullets
	RiskItems   []string             `json:"riskItems,omitempty"`  // ## risk items bullets
	Changes     []string             `json:"changes,omitempty"`    // ## changes log lines
	// derived (never stored)
	Drawn float64 `json:"drawn"` // Σ expenses carrying [contract:: slug]
}

// Accepted reports whether the contract commits money.
func (c *Contract) Accepted() bool { return strings.EqualFold(c.Status, "accepted") }

// AllocTotal is Σ allocations (must equal Total — validated at write).
func (c *Contract) AllocTotal() float64 {
	var n float64
	for _, a := range c.Allocations {
		n += a.Amount
	}
	return n
}

// ParseContract reads one contract record (tolerant — missing fields omit).
func ParseContract(rel, slug, raw string) Contract {
	fm, body := mdfm.Split(raw)
	c := Contract{
		Path: rel, Slug: slug, Name: slug,
		Status:  strings.ToLower(strings.TrimSpace(fm["status"])),
		Date:    strings.TrimSpace(fm["date"]),
		Expires: strings.TrimSpace(fm["expires"]),
		Doc:     unquote(fm["doc"]),
	}
	c.Total, _ = parseMoney(unquote(fm["total"]))
	ctr := unquote(fm["contractor"])
	if m := wikilinkRe.FindStringSubmatch(ctr); m != nil {
		ctr = strings.TrimSpace(m[1])
	}
	c.Contractor = ctr
	for _, item := range quotedList(fm["allocations"]) {
		if a, ok := parseAllocation(item); ok {
			c.Allocations = append(c.Allocations, a)
		}
	}
	for _, ln := range strings.Split(body, "\n") {
		if strings.HasPrefix(ln, "# ") {
			c.Name = strings.TrimSpace(ln[2:])
			break
		}
	}
	secs := parseSections(body)
	c.Terms = parseLog(secs["terms"])
	c.Exclusions = parseLog(secs["exclusions"])
	c.RiskItems = parseLog(secs["risk items"])
	c.Changes = parseLog(secs["changes"])
	return c
}

// ParseAllocationItem reads one "property-slug | node-id | amount" item
// (the server's create/update payload uses the same string shape as the
// frontmatter list).
func ParseAllocationItem(s string) (ContractAllocation, bool) { return parseAllocation(s) }

// PatchContractFrontmatter updates only named scalar lines (nil = remove),
// retaining unknown fields, their order, and the complete body byte-for-byte
// (the fundraising patch idiom).
func PatchContractFrontmatter(src []byte, updates map[string]*string) []byte {
	lines := strings.Split(string(src), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return src
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return src
	}
	done := map[string]bool{}
	out := []string{"---"}
	for _, ln := range lines[1:end] {
		k, _, ok := strings.Cut(ln, ":")
		key := strings.TrimSpace(k)
		v, wanted := updates[key]
		if ok && wanted {
			done[key] = true
			if v != nil {
				out = append(out, key+": "+*v)
			}
			continue
		}
		out = append(out, ln)
	}
	keys := make([]string, 0, len(updates))
	for k := range updates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !done[k] && updates[k] != nil {
			out = append(out, k+": "+*updates[k])
		}
	}
	out = append(out, lines[end:]...)
	return []byte(strings.Join(out, "\n"))
}

// parseAllocation reads "property-slug | node-id | amount".
func parseAllocation(s string) (ContractAllocation, bool) {
	parts := strings.Split(s, "|")
	if len(parts) != 3 {
		return ContractAllocation{}, false
	}
	amt, err := parseMoney(parts[2])
	if err != nil {
		return ContractAllocation{}, false
	}
	return ContractAllocation{
		Property: strings.TrimSpace(parts[0]),
		NodeID:   strings.TrimSpace(parts[1]),
		Amount:   amt,
	}, true
}

// EmitAllocation renders one allocation item (the frontmatter list element).
func EmitAllocation(a ContractAllocation) string {
	return a.Property + " | " + a.NodeID + " | " + strconv.FormatFloat(a.Amount, 'f', -1, 64)
}

// EmitAllocations renders the inline flow list for the frontmatter line.
func EmitAllocations(allocs []ContractAllocation) string {
	items := make([]string, 0, len(allocs))
	for _, a := range allocs {
		items = append(items, strconv.Quote(EmitAllocation(a)))
	}
	return "[" + strings.Join(items, ", ") + "]"
}

// NewContractRecord renders a fresh record's full bytes (create path — the
// migration and the manual/proposal flows both write through this).
func NewContractRecord(c Contract) string {
	var b strings.Builder
	b.WriteString("---\ncategories: [contract]\n")
	b.WriteString("contractor: \"[[" + c.Contractor + "]]\"\n")
	st := c.Status
	if st == "" {
		st = "proposed"
	}
	b.WriteString("status: " + st + "\n")
	b.WriteString("total: " + strconv.FormatFloat(c.Total, 'f', -1, 64) + "\n")
	if c.Date != "" {
		b.WriteString("date: " + c.Date + "\n")
	}
	if c.Expires != "" {
		b.WriteString("expires: " + c.Expires + "\n")
	}
	if c.Doc != "" {
		b.WriteString("doc: \"" + c.Doc + "\"\n")
	}
	b.WriteString("allocations: " + EmitAllocations(c.Allocations) + "\n")
	b.WriteString("---\n\n# " + c.Name + "\n")
	section := func(name string, lines []string) {
		if len(lines) == 0 {
			return
		}
		b.WriteString("\n## " + name + "\n")
		for _, ln := range lines {
			b.WriteString("- " + ln + "\n")
		}
	}
	section("terms", c.Terms)
	section("exclusions", c.Exclusions)
	section("risk items", c.RiskItems)
	section("changes", c.Changes)
	return b.String()
}

// Contracts returns every contract record (category "contract"), sorted by
// date desc then name. Drawn is NOT filled here (needs every ledger — the
// server layer aggregates it).
func (s *Service) Contracts() []Contract {
	refs, err := s.ix.Category("contract", vaultindex.SortNameAsc)
	if err != nil {
		return nil
	}
	var out []Contract
	for _, r := range refs {
		raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(r.Path)))
		if err != nil {
			continue
		}
		out = append(out, ParseContract(r.Path, r.Name, string(raw)))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// GetContract returns the record at slug, freshly parsed.
func (s *Service) GetContract(slug string) (Contract, bool) {
	for _, c := range s.Contracts() {
		if strings.EqualFold(c.Slug, slug) {
			return c, true
		}
	}
	return Contract{}, false
}

// NodeAllocation is one ACCEPTED contract slice targeting a node of one
// property — the committed-money source the joins consume (overhaul
// decision 4: contracts replace accepted bids).
type NodeAllocation struct {
	Contract   string  `json:"contract"` // contract slug
	Contractor string  `json:"contractor"`
	NodeID     string  `json:"nodeId"`
	Amount     float64 `json:"amount"`
	Doc        string  `json:"doc,omitempty"`     // the contract's doc ref — receipt evidence
	Date       string  `json:"date,omitempty"`    // the quote's date
	Expires    string  `json:"expires,omitempty"` // when a bid goes stale
}

// AllocationsFor projects the accepted contracts' slices for ONE property.
func AllocationsFor(contracts []Contract, propertySlug string) []NodeAllocation {
	return allocationsWhere(contracts, propertySlug, func(c Contract) bool { return c.Accepted() })
}

// ProposedFor projects the OPEN BIDS for one property — proposed records, the
// options on the table. They are deliberately not in AllocationsFor: a bid is
// not committed money and must not reach a budget.
func ProposedFor(contracts []Contract, propertySlug string) []NodeAllocation {
	return allocationsWhere(contracts, propertySlug, func(c Contract) bool {
		return strings.EqualFold(c.Status, "proposed")
	})
}

func allocationsWhere(contracts []Contract, propertySlug string, keep func(Contract) bool) []NodeAllocation {
	var out []NodeAllocation
	for _, c := range contracts {
		if !keep(c) {
			continue
		}
		for _, a := range c.Allocations {
			if !strings.EqualFold(a.Property, propertySlug) {
				continue
			}
			out = append(out, NodeAllocation{
				Contract: c.Slug, Contractor: c.Contractor,
				NodeID: a.NodeID, Amount: a.Amount, Doc: c.Doc,
				Date: c.Date, Expires: c.Expires,
			})
		}
	}
	return out
}
