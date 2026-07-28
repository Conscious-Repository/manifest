package realestate

import (
	"regexp"
	"strconv"
	"strings"

	"manifest/record"
)

// The `## work` section (properties-pass-4 plan): the property's management
// core — STAGES (standard-ish, editable) holding TODOS (free-text SOW lines),
// with ledger rows tethered by `[work:: id]` tokens. Grammar copied from the
// proven goals ladder: checkbox bullets, positional nesting (4 spaces/level),
// `[key:: value]` inline fields, fixpoint serialization (parse→emit is
// byte-identical, so hand edits in Obsidian and app writes coexist).
//
// Research-backed rules: the stage list with a current marker IS the schedule
// (no separate object); current stage = first unchecked stage; checking a
// stage is explicit (all-todos-done only marks it Ready); money is ALWAYS a
// computed rollup from the ledger — never stored here.

// WorkField is a preserved `[key:: value]` annotation (unknown keys round-trip).
type WorkField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// WorkTodo is one action line under a stage.
type WorkTodo struct {
	ID        string      `json:"id"` // explicit [work:: id] else derived stage/todo slug
	Explicit  bool        `json:"-"`  // id is frozen in the file
	Text      string      `json:"text"`
	Checked   bool        `json:"checked"`
	Est       float64     `json:"est"` // [est:: N] — the work list IS the hard-cost budget (0 = unestimated)
	Fields    []WorkField `json:"fields,omitempty"`
	Extra     []string    `json:"-"`         // verbatim non-todo lines beneath (preserved)
	Committed float64     `json:"committed"` // derived: tethered expenses + accepted bids
	Paid      float64     `json:"paid"`      // derived: tethered expenses (cash)
	// accrual recognition: a DONE todo's firm price counts as spent before the
	// bank transaction lands; the gap is the ⚑ unreconciled amount.
	Recognized   float64   `json:"recognized"`             // checked ? max(firm, paid) : paid
	Unreconciled float64   `json:"unreconciled,omitempty"` // checked ? max(0, firm − paid) : 0 (unless receipted)
	Receipted    bool      `json:"receipted,omitempty"`    // firm price carries a receipt doc — evidence without cash
	Bids         []WorkBid `json:"bids,omitempty"`
}

// WorkBid is a tethered ledger bid, surfaced for the todo's chips. Row carries
// the full source ledger row so chip actions (accept/decline) can exact-match
// mutate without a second lookup.
type WorkBid struct {
	Who    string    `json:"who"`
	Amount float64   `json:"amount"`
	Status string    `json:"status"`
	Row    LedgerRow `json:"row"`
}

// WorkStage is one top-level stage line.
type WorkStage struct {
	ID           string      `json:"id"`
	Explicit     bool        `json:"-"`
	Text         string      `json:"text"`
	Checked      bool        `json:"checked"`
	Ready        bool        `json:"ready"` // all todos checked (and at least one)
	Current      bool        `json:"current"`
	Est          float64     `json:"est"`         // the stage's OWN [est::] (not-yet-broken-down remainder)
	EstTotal     float64     `json:"estTotal"`    // Σ todo est + own est
	Unestimated  int         `json:"unestimated"` // open todos carrying no est
	Weeks        float64     `json:"weeks"`       // [weeks:: N] — schedule duration (§3)
	Done         string      `json:"done"`        // [done:: YYYY-MM-DD] — stamped at stage check
	Fields       []WorkField `json:"fields,omitempty"`
	Extra        []string    `json:"-"`
	Todos        []WorkTodo  `json:"todos"`
	Committed    float64     `json:"committed"`
	Paid         float64     `json:"paid"`
	Recognized   float64     `json:"recognized"`             // Σ todo recognized + stage-level recognition
	Unreconciled float64     `json:"unreconciled,omitempty"` // Σ done-but-unlinked firm money
}

var (
	workLineRe  = record.CheckboxRe // the kernel checkbox grammar
	workFieldRe = record.FieldRe    // the kernel field grammar
	workSlugRe  = regexp.MustCompile(`[^a-z0-9]+`)
)

// parseWorkFields strips every [key:: value] out of a bullet's text.
func parseWorkFields(raw string) (text string, fields []WorkField) {
	text = workFieldRe.ReplaceAllStringFunc(raw, func(m string) string {
		g := workFieldRe.FindStringSubmatch(m)
		fields = append(fields, WorkField{Key: g[1], Value: strings.TrimSpace(g[2])})
		return ""
	})
	return strings.Join(strings.Fields(text), " "), fields
}

// workIndentWidth is the kernel indent rule (tab = 4).
func workIndentWidth(s string) int { return record.IndentWidth(s) }

// ParseWork reads a `## work` section's lines into stages. Depth 0 checkbox
// lines are stages, depth ≥1 are todos of the last stage; every non-checkbox
// line is preserved verbatim on the preceding node (fixpoint).
func ParseWork(lines []string) []WorkStage {
	var stages []WorkStage
	for _, ln := range lines {
		m := workLineRe.FindStringSubmatch(ln)
		if m == nil {
			if t := strings.TrimRight(ln, " \t"); t != "" {
				if len(stages) == 0 {
					continue // stray prose before the first stage — dropped like pre-## prose
				}
				st := &stages[len(stages)-1]
				if len(st.Todos) > 0 {
					st.Todos[len(st.Todos)-1].Extra = append(st.Todos[len(st.Todos)-1].Extra, ln)
				} else {
					st.Extra = append(st.Extra, ln)
				}
			}
			continue
		}
		checked := strings.EqualFold(m[2], "x")
		text, fields := parseWorkFields(m[3])
		if workIndentWidth(m[1]) < 4 || len(stages) == 0 {
			stages = append(stages, WorkStage{Text: text, Checked: checked, Fields: fields})
		} else {
			st := &stages[len(stages)-1]
			st.Todos = append(st.Todos, WorkTodo{Text: text, Checked: checked, Fields: fields})
		}
	}
	assignWorkIDs(stages)
	// derive current (first unchecked) + ready + typed fields + est rollups
	cur := false
	for i := range stages {
		st := &stages[i]
		if st.Todos == nil {
			st.Todos = []WorkTodo{} // marshal as [] — a nil slice is null in JSON and crashes .forEach client-side
		}
		st.Est = fieldFloat(st.Fields, "est")
		st.Weeks = fieldFloat(st.Fields, "weeks")
		st.Done = fieldValue(st.Fields, "done")
		st.EstTotal = st.Est
		st.Ready = len(st.Todos) > 0
		for j := range st.Todos {
			td := &st.Todos[j]
			td.Est = fieldFloat(td.Fields, "est")
			st.EstTotal += td.Est
			if td.Est == 0 && !td.Checked {
				st.Unestimated++
			}
			if !td.Checked {
				st.Ready = false
			}
		}
		if !st.Checked && !cur {
			st.Current = true
			cur = true
		}
	}
	return stages
}

func fieldFloat(fs []WorkField, key string) float64 {
	v := strings.ReplaceAll(strings.ReplaceAll(fieldValue(fs, key), "$", ""), ",", "")
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return f
}

// SetWorkField updates/inserts/removes a [key:: value] on the node with the
// given id (empty value removes). The typed projections re-derive on re-parse.
func SetWorkField(stages []WorkStage, id, key, value string) bool {
	upsert := func(fs *[]WorkField) {
		for i := range *fs {
			if strings.EqualFold((*fs)[i].Key, key) {
				if value == "" {
					*fs = append((*fs)[:i], (*fs)[i+1:]...)
				} else {
					(*fs)[i].Value = value
				}
				return
			}
		}
		if value != "" {
			*fs = append(*fs, WorkField{Key: key, Value: value})
		}
	}
	for i := range stages {
		if stages[i].ID == id {
			upsert(&stages[i].Fields)
			return true
		}
		for j := range stages[i].Todos {
			if stages[i].Todos[j].ID == id {
				upsert(&stages[i].Todos[j].Fields)
				return true
			}
		}
	}
	return false
}

// assignWorkIDs: explicit [work:: id] wins; else hierarchical slug with -2/-3
// collision suffixes (goals id.go pattern).
func assignWorkIDs(stages []WorkStage) {
	seen := map[string]bool{}
	uniq := func(base string) string {
		id := base
		for n := 2; seen[id]; n++ {
			id = base + "-" + itoaWork(n)
		}
		seen[id] = true
		return id
	}
	for i := range stages {
		st := &stages[i]
		if ex := fieldValue(st.Fields, "work"); ex != "" {
			st.ID, st.Explicit = ex, true
			seen[ex] = true
		} else {
			st.ID = uniq(workSlug(st.Text))
		}
		for j := range st.Todos {
			td := &st.Todos[j]
			if ex := fieldValue(td.Fields, "work"); ex != "" {
				td.ID, td.Explicit = ex, true
				seen[ex] = true
			} else {
				td.ID = uniq(st.ID + "/" + workSlug(td.Text))
			}
		}
	}
}

func fieldValue(fs []WorkField, key string) string {
	for _, f := range fs {
		if strings.EqualFold(f.Key, key) {
			return f.Value
		}
	}
	return ""
}

func workSlug(s string) string {
	sl := strings.Trim(workSlugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if sl == "" {
		sl = "item"
	}
	if len(sl) > 40 {
		sl = strings.Trim(sl[:40], "-")
	}
	return sl
}

// EmitWork serializes stages back to section lines — 4-space indents, `- [ ]`,
// fields re-appended in order, Extra lines verbatim. Fixpoint with ParseWork.
func EmitWork(stages []WorkStage) string {
	var b strings.Builder
	line := func(indent int, checked bool, text string, fields []WorkField) {
		b.WriteString(strings.Repeat("    ", indent))
		if checked {
			b.WriteString("- [x] ")
		} else {
			b.WriteString("- [ ] ")
		}
		b.WriteString(text)
		for _, f := range fields {
			b.WriteString(" [" + f.Key + ":: " + f.Value + "]")
		}
		b.WriteString("\n")
	}
	for _, st := range stages {
		line(0, st.Checked, st.Text, st.Fields)
		for _, ex := range st.Extra {
			b.WriteString(ex + "\n")
		}
		for _, td := range st.Todos {
			line(1, td.Checked, td.Text, td.Fields)
			for _, ex := range td.Extra {
				b.WriteString(ex + "\n")
			}
		}
	}
	return b.String()
}

// FreezeWorkID writes the node's derived id as an explicit [work:: id] field
// (the tether target must survive renames). Returns true when it changed.
func FreezeWorkID(stages []WorkStage, id string) bool {
	for i := range stages {
		st := &stages[i]
		if st.ID == id && !st.Explicit {
			st.Fields = append(st.Fields, WorkField{Key: "work", Value: id})
			st.Explicit = true
			return true
		}
		for j := range st.Todos {
			td := &st.Todos[j]
			if td.ID == id && !td.Explicit {
				td.Fields = append(td.Fields, WorkField{Key: "work", Value: id})
				td.Explicit = true
				return true
			}
		}
	}
	return false
}

// JoinWorkLedger attaches the derived money to stages/todos: a ledger row's
// WorkID matches a todo id (rolls into its stage too) or a stage id directly.
// committed = expenses + ACCEPTED bids; paid = expenses. One source of truth —
// the ledger; these numbers are never stored.
func JoinWorkLedger(stages []WorkStage, ledger []LedgerRow) {
	type slot struct {
		si, ti int // ti = -1 → stage-level
	}
	index := map[string]slot{}
	for i := range stages {
		index[stages[i].ID] = slot{i, -1}
		for j := range stages[i].Todos {
			index[stages[i].Todos[j].ID] = slot{i, j}
		}
	}
	// pass-5 accounting: an expense tethered to a work item that carries an
	// ACCEPTED bid is a DRAW against that contract — it moves paid, not
	// committed (no double count; contracted-to-go reaches 0 when fully paid).
	// Per node: committed = max(Σ accepted bids, Σ expenses) when a bid exists,
	// else Σ expenses.
	type acc struct{ acceptedSum, expenseSum float64 }
	perNode := map[string]*acc{}
	touch := func(id string) *acc {
		if a, ok := perNode[id]; ok {
			return a
		}
		a := &acc{}
		perNode[id] = a
		return a
	}
	for _, r := range ledger {
		if r.WorkID == "" {
			continue
		}
		sl, ok := index[r.WorkID]
		if !ok {
			continue // tether to a deleted node — money stays in the ledger, no crash
		}
		st := &stages[sl.si]
		isExpense := strings.EqualFold(r.Type, "expense")
		accepted := strings.EqualFold(r.Type, "bid") && strings.EqualFold(r.Status, "accepted")
		if isExpense {
			st.Paid += r.Amount
			touch(r.WorkID).expenseSum += r.Amount
		} else if accepted {
			touch(r.WorkID).acceptedSum += r.Amount
		}
		if sl.ti >= 0 {
			td := &st.Todos[sl.ti]
			if isExpense {
				td.Paid += r.Amount
			}
			if strings.EqualFold(r.Type, "bid") {
				who := r.Contractor
				if who == "" {
					who = r.Vendor
				}
				td.Bids = append(td.Bids, WorkBid{Who: who, Amount: r.Amount, Status: r.Status, Row: r})
			}
		}
	}
	// fold per-node committed (draw-aware) back onto todos + stages
	for id, a := range perNode {
		committed := a.expenseSum
		if a.acceptedSum > 0 && a.acceptedSum > a.expenseSum {
			committed = a.acceptedSum
		}
		sl := index[id]
		stages[sl.si].Committed += committed
		if sl.ti >= 0 {
			stages[sl.si].Todos[sl.ti].Committed = committed
		}
	}
	// recognition (accrual): checking a node with a firm price recognizes it as
	// SPENT before the bank transaction lands; the gap is ⚑ unreconciled until
	// the workbench tethers real payments covering it OR a receipt is attached
	// to the accepted bid (bank match OR receipt — either evidence clears it).
	receipted := func(id string) bool {
		any := false
		for _, r := range ledger {
			if r.WorkID != id || !strings.EqualFold(r.Type, "bid") || !strings.EqualFold(r.Status, "accepted") {
				continue
			}
			if r.Doc == "" {
				return false
			}
			any = true
		}
		return any
	}
	for i := range stages {
		st := &stages[i]
		for j := range st.Todos {
			td := &st.Todos[j]
			firm := 0.0
			if a, ok := perNode[td.ID]; ok {
				firm = a.acceptedSum
			}
			td.Recognized = td.Paid
			if td.Checked && firm > td.Paid {
				td.Recognized = firm
				if receipted(td.ID) {
					td.Receipted = true
				} else {
					td.Unreconciled = firm - td.Paid
				}
			}
			st.Recognized += td.Recognized
			st.Unreconciled += td.Unreconciled
		}
		if a, ok := perNode[st.ID]; ok { // rows tethered to the stage itself
			rec := a.expenseSum
			if st.Checked && a.acceptedSum > rec {
				if !receipted(st.ID) {
					st.Unreconciled += a.acceptedSum - rec
				}
				rec = a.acceptedSum
			}
			st.Recognized += rec
		}
	}
}

// Stage templates (research consensus). Seeds only — never enforced; delete
// five stages in ten seconds for a cosmetic flip.
var RehabStages = []string{
	"Pre-development", "Exterior & structural", "Demo",
	"Rough-in", "Insulation & drywall", "Finishes", "Punch & final inspection", "Exit",
}

var NewBuildStages = []string{
	"Pre-construction", "Site work", "Foundation", "Framing & dry-in",
	"MEP rough-in", "Insulation & drywall", "Finishes", "Final inspections & CO", "Close-out",
}

func itoaWork(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}
