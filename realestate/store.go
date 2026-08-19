package realestate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"manifest/mdfm"
	"manifest/record"
	"manifest/vaultindex"
)

// Service reads property records from the vault index + their frontmatter,
// sections, and ledger sidecar from disk. Index-backed (like reading.Service) so
// the running index watcher keeps it live with no dedicated watcher.
type Service struct{ ix *vaultindex.Index }

func New(ix *vaultindex.Index) *Service { return &Service{ix: ix} }

var wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// Properties returns every property record, sorted by entity then address so the
// Board's entity grouping is stable. The frontend re-sorts/filters as needed.
func (s *Service) Properties() ([]Property, error) {
	refs, err := s.ix.Category("property", vaultindex.SortNameAsc)
	if err != nil {
		return nil, err
	}
	contracts := s.Contracts() // one load for the whole portfolio's committed source
	out := make([]Property, 0, len(refs))
	for _, r := range refs {
		if p, ok := s.parse(r.Path, r.Name, contracts); ok {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := groupKey(out[i].Entity), groupKey(out[j].Entity); a != b {
			return a < b
		}
		return strings.ToLower(out[i].Address) < strings.ToLower(out[j].Address)
	})
	intel := s.parcelIntel()
	for i := range out {
		out[i].Intel = intel[AddrKey(out[i].Address)]
	}
	return out, nil
}

// Get returns the single property record at slug (basename), freshly parsed.
func (s *Service) Get(slug string) (Property, bool) {
	for _, r := range mustRefs(s.ix.Category("property", vaultindex.SortNameAsc)) {
		if strings.EqualFold(r.Name, slug) {
			p, ok := s.parse(r.Path, r.Name, s.Contracts())
			if ok {
				p.Intel = s.parcelIntel()[AddrKey(p.Address)]
			}
			return p, ok
		}
	}
	return Property{}, false
}

func mustRefs(refs []vaultindex.NoteRef, _ error) []vaultindex.NoteRef { return refs }

// parse reads one record's frontmatter + `## budget`/`## log` sections + its
// ledger csv sidecar into a Property, then derives the rollup (committed from
// the accepted-contract allocations targeting this property). Tolerant: missing
// or garbled fields are simply omitted (a hand edit in Obsidian reads back the same).
func (s *Service) parse(rel, name string, contracts []Contract) (Property, bool) {
	full := filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(rel))
	raw, err := os.ReadFile(full)
	if err != nil {
		return Property{}, false
	}
	fm, body := mdfm.Split(string(raw))
	p := Property{
		Path: rel, Slug: name, Name: name,
		Address: unquote(fm["address"]),
		Entity:  unquote(fm["entity"]),
		Status:  strings.ToLower(strings.TrimSpace(fm["status"])),
		Kind:    strings.ToLower(strings.TrimSpace(fm["kind"])),
		Control: strings.ToLower(strings.TrimSpace(fm["control"])),
		Hidden:  strings.EqualFold(strings.TrimSpace(fm["hidden"]), "true"),
	}
	p.Short = strings.TrimSpace(strings.SplitN(p.Address, ",", 2)[0])
	if m := wikilinkRe.FindStringSubmatch(fm["deal"]); m != nil {
		p.Deal = strings.TrimSpace(m[1])
	}
	p.Owner = unquote(fm["owner"])
	p.OwnerAddr = unquote(fm["owner-addr"])
	p.OwnerSince = strings.TrimSpace(fm["owner-since"])
	p.From = unquote(fm["from"])
	p.Until = unquote(fm["until"])
	p.Drive = unquote(fm["drive"])
	p.Agc = unquote(fm["agc"])
	p.Lat, _ = strconv.ParseFloat(strings.TrimSpace(fm["lat"]), 64)
	p.Lng, _ = strconv.ParseFloat(strings.TrimSpace(fm["lng"]), 64)
	p.WorkStart = strings.TrimSpace(fm["work-start"])
	sections := parseSections(body)
	p.Budget = parseBudget(sections["budget"])
	p.Log = parseLog(sections["log"])
	if len(p.Log) > 0 {
		p.LastLog = p.Log[0]
	}
	if led, err := os.ReadFile(ledgerPath(full)); err == nil {
		p.Ledger = parseLedger(led)
	}
	p.Units = sourceUnits(record.Sidecar(full, record.SidecarSource))
	// measurables (overhaul §3.1): the frontmatter unit mix wins the unit
	// count once present; free numeric keys read as measurables
	p.UnitMix = ParseUnits(fm["units"])
	if len(p.UnitMix) > 0 {
		p.Units = len(p.UnitMix)
		p.RentMonthly = RentMonthly(p.UnitMix)
	}
	p.Measurables = ParseMeasurables(fm)
	p.Locked = strings.TrimSpace(fm["locked"])
	p.Underwrite = ReadUnderwriteLock(record.Sidecar(full, record.SidecarUnderwrite))
	// the rock tree: `## rocks`, tolerant-reading the legacy `## work` heading
	// (writes go back to whichever heading the file has — RocksSection)
	p.RocksSection = "rocks"
	rockLines, okRocks := sections["rocks"]
	if !okRocks {
		if legacy, okWork := sections["work"]; okWork {
			rockLines, p.RocksSection = legacy, "work"
		}
	}
	p.Work = ParseWork(rockLines)
	view := &PropertyTaskList{Stages: p.Work, Legacy: ParsePropertyTasks(sections["tasks"])}
	p.Tasks = view.Tasks()
	allocs := AllocationsFor(contracts, name)
	// a draw carrying [contract::] but no [work::] attributes to the
	// contract's first allocated node here (the workbench writes both; this
	// covers hand-written rows deterministically — no double count)
	for i := range p.Ledger {
		if p.Ledger[i].Contract == "" || p.Ledger[i].WorkID != "" {
			continue
		}
		for _, a := range allocs {
			if strings.EqualFold(a.Contract, p.Ledger[i].Contract) {
				p.Ledger[i].WorkID = a.NodeID
				break
			}
		}
	}
	JoinWorkLedger(p.Work, p.Ledger, allocs)
	JoinWorkBids(p.Work, ProposedFor(contracts, name)) // options, not money
	// the work list IS the hard-cost budget — the triplet derives from work
	// est + contracts + the ledger. (parseBudget/computeRollup survive for
	// migration.)
	p.Rollup = computeMoneyRollup(p.Work, p.Ledger, allocs)
	p.Project = ComputeProjectBudget(
		sourceMoney(record.Sidecar(full, record.SidecarSource)),
		p.Work, p.Ledger, p.Control == "owned", allocs)
	p.Schedule = DeriveSchedule(p.WorkStart, p.Work)
	for _, st := range p.Work {
		if st.Current {
			p.CurrentStage = st.Text
			break
		}
	}
	return p, true
}

// sourceUnits pulls total_units from the source sidecar — either the
// property-slice shape or a single-parcel full-deal shape (properties[0]).
func sourceUnits(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var v struct {
		TotalUnits float64 `json:"total_units"`
		Properties []struct {
			TotalUnits float64 `json:"total_units"`
		} `json:"properties"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return 0
	}
	if v.TotalUnits > 0 {
		return int(v.TotalUnits)
	}
	if len(v.Properties) > 0 {
		return int(v.Properties[0].TotalUnits)
	}
	return 0
}

// Deal is one deal-bundle record (categories: [deal]) — a group of parcels that
// underwrites together. Pass 1 surfaces them as a quiet Board lane + map groups.
type Deal struct {
	Path       string   `json:"path"`
	Slug       string   `json:"slug"`
	Name       string   `json:"name"`       // first `# ` heading, else the slug
	Status     string   `json:"status"`     // deal enum (incl. "opportunity")
	Properties []string `json:"properties"` // member addresses (frontmatter list)
}

// Deals returns every deal record, sorted by name.
func (s *Service) Deals() ([]Deal, error) {
	refs, err := s.ix.Category("deal", vaultindex.SortNameAsc)
	if err != nil {
		return nil, err
	}
	out := make([]Deal, 0, len(refs))
	for _, r := range refs {
		raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(r.Path)))
		if err != nil {
			continue
		}
		fm, body := mdfm.Split(string(raw))
		d := Deal{Path: r.Path, Slug: r.Name, Name: r.Name,
			Status: strings.ToLower(strings.TrimSpace(fm["status"]))}
		for _, ln := range strings.Split(body, "\n") {
			if strings.HasPrefix(ln, "# ") {
				d.Name = strings.TrimSpace(ln[2:])
				break
			}
		}
		for _, p := range quotedList(fm["properties"]) {
			if m := wikilinkRe.FindStringSubmatch(p); m != nil {
				p = strings.TrimSpace(m[1])
			}
			d.Properties = append(d.Properties, p)
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// ledgerPath is the csv sidecar beside a property's .md (…/<slug>.md → …/<slug>.ledger.csv).
func ledgerPath(mdFull string) string { return record.Sidecar(mdFull, record.SidecarLedger) }

// LedgerRel maps a vault-relative .md path to its vault-relative ledger csv path.
func LedgerRel(mdRel string) string { return record.Sidecar(mdRel, record.SidecarLedger) }

// Template is a budget-mix template (categories: [budget-template]) — a
// user-editable record whose `## budget` table seeds new properties at creation.
type Template struct {
	Slug   string       `json:"slug"`
	Name   string       `json:"name"`
	Budget []BudgetLine `json:"budget"`
	Stages []WorkStage  `json:"stages,omitempty"` // ## stages — seeds work plans (§3)
}

// Templates returns the budget-mix templates, sorted by name.
func (s *Service) Templates() []Template {
	refs, err := s.ix.Category("budget-template", vaultindex.SortNameAsc)
	if err != nil {
		return nil
	}
	var out []Template
	for _, r := range refs {
		raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(r.Path)))
		if err != nil {
			continue
		}
		_, body := mdfm.Split(string(raw))
		secs := parseSections(body)
		tplLines, okTpl := secs["rocks"]
		if !okTpl {
			tplLines = secs["stages"] // legacy template heading
		}
		t := Template{Slug: r.Name, Name: r.Name, Budget: parseBudget(secs["budget"]),
			Stages: ParseWork(tplLines)}
		for _, ln := range strings.Split(body, "\n") {
			if strings.HasPrefix(ln, "# ") {
				t.Name = strings.TrimSpace(ln[2:])
				break
			}
		}
		out = append(out, t)
	}
	return out
}

// quotedList parses a frontmatter list whose items may be quoted strings
// CONTAINING commas (deal member addresses: `["4924 Fountain Ave, Saint Louis,
// MO", …]`) — mdfm.List splits on every comma, which shreds those. Splits on
// commas only outside quotes; unquoted items pass through like mdfm.List.
func quotedList(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	var out []string
	var cur strings.Builder
	inQ := byte(0)
	flush := func() {
		s := strings.TrimSpace(cur.String())
		s = strings.Trim(s, `"'`)
		if s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case inQ != 0:
			if c == inQ {
				inQ = 0
			}
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			inQ = c
			cur.WriteByte(c)
		case c == ',':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// groupKey sorts blank entities ("unassigned") last on the Board.
func groupKey(entity string) string {
	if strings.TrimSpace(entity) == "" {
		return "￿" // sorts after any real name
	}
	return strings.ToLower(entity)
}

// unquote strips a YAML-quoted scalar (addresses carry commas the importer may quote).
func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"') {
		return strings.ReplaceAll(v[1:len(v)-1], `\"`, `"`)
	}
	if len(v) >= 2 && (v[0] == '\'' && v[len(v)-1] == '\'') {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}
