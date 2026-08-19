package realestate

import (
	"encoding/json"
	"strconv"
	"strings"

	"manifest/record"
	"manifest/tasks"
)

// The `## rocks` section (realestate-overhaul, 2026-08-18): the property's
// management core — ROCKS (the schedule-bearing chunks, carrying [done-by::]
// dates) holding MILESTONES (scope groupings like "roofing") and TASKS /
// DECISIONS, with ledger rows tethered by `[work:: id]` tokens. Grammar is the
// proven goals ladder re-roled and extended ONE level: checkbox bullets,
// positional nesting (4 spaces/level, three levels), `[key:: value]` inline
// fields, fixpoint serialization (parse→emit is byte-identical, so hand edits
// in Obsidian and app writes coexist). Legacy `## work` sections read through
// the same parser (tolerant-read doctrine); the migration renames the heading.
//
// Line roles by depth: 0 = rock · 1 = milestone OR task/decision · 2 = task/
// decision under a milestone. A depth-1 line is a milestone iff it has
// children or carries [milestone::]. A line carrying [decision::] is a
// decision: renders in the decisions lane, never ages, resolves with
// [resolution:: note]. Rock lines keep the source-order field grammar
// (existing files put [weeks::] before [done::] — byte fixpoint wins); node
// lines parse/emit through the SHARED tasks line grammar (one grammar, one
// file — kernel doctrine §3), so [owner::]/[added::]/[waiting::]/[since::]
// ride the tasks conventions verbatim.
//
// Money is ALWAYS a computed rollup from the ledger — never stored here.

// WorkField is a preserved `[key:: value]` annotation (unknown keys round-trip).
type WorkField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// WorkNode is one tree line below a rock: a milestone, task, or decision.
// The line itself lives in Task (shared grammar); the node adds the work id,
// role flags, and derived money.
type WorkNode struct {
	Task     *tasks.Task // the parsed line: text · checked · typed task fields · unknown fields
	ID       string      // hierarchical work id (money tether target): rock/slug or rock/milestone/slug
	Explicit bool        // id is frozen in the file via [work:: id]

	Milestone  bool   // has children or carries [milestone::]
	Decision   bool   // carries [decision::] — decisions lane, never ages
	Resolution string // [resolution:: note]

	Est         float64 // own [est:: N]
	EstTotal    float64 // own + Σ child ests
	Unestimated int     // open non-decision leaves carrying no est

	Children []*WorkNode
	Extra    []string // verbatim non-checkbox lines beneath (preserved)

	// derived money (JoinWorkLedger) — never stored
	Committed    float64
	Paid         float64
	Recognized   float64
	Unreconciled float64
	Receipted    bool
	Bids         []WorkBid      // legacy bid chips (pre-contract records only)
	Contracts    []WorkContract // accepted-contract slices targeting this node
	// OpenBids are PROPOSED contract records targeting this node — money that
	// is quoted but not committed. They carry no budget weight (only an
	// accepted contract commits); they exist so a bid is visible where the
	// work is, and can be accepted from there.
	OpenBids []WorkContract
}

// WorkContract is one accepted allocation chip on a node.
type WorkContract struct {
	Slug       string  `json:"slug"`
	Contractor string  `json:"contractor"`
	Amount     float64 `json:"amount"`
	Date       string  `json:"date,omitempty"`    // the quote's date — how competing bids are read
	Expires    string  `json:"expires,omitempty"` // a bid goes stale; say when
}

// TaskID is the node's identity on the unified task surface: the explicit
// [todo:: id] pin when present (thread/plan continuity across rewording),
// else the hierarchical work id.
func (n *WorkNode) TaskID() string {
	if n.Task != nil {
		if id := n.Task.ExplicitID(); id != "" {
			return id
		}
	}
	return n.ID
}

// MarshalJSON flattens the line + node into one client object (the property
// page consumes st.tasks[j].text/checked/est/… directly).
func (n *WorkNode) MarshalJSON() ([]byte, error) {
	t := n.Task
	if t == nil {
		t = &tasks.Task{}
	}
	obj := map[string]any{
		"id":     n.ID,
		"taskId": n.TaskID(),
		"text":   t.Text,
	}
	obj["checked"] = t.Checked
	set := func(k, v string) {
		if v != "" {
			obj[k] = v
		}
	}
	set("owner", t.Owner)
	set("added", t.Added)
	set("done", t.Done)
	set("waiting", t.Waiting)
	set("since", t.Since)
	set("rank", t.Rank)
	set("resolution", n.Resolution)
	if n.Milestone {
		obj["milestone"] = true
	}
	if n.Decision {
		obj["decision"] = true
	}
	obj["est"] = n.Est
	obj["estTotal"] = n.EstTotal
	if n.Unestimated > 0 {
		obj["unestimated"] = n.Unestimated
	}
	if len(n.Children) > 0 {
		obj["children"] = n.Children
	}
	obj["committed"] = n.Committed
	obj["paid"] = n.Paid
	obj["recognized"] = n.Recognized
	if n.Unreconciled > 0 {
		obj["unreconciled"] = n.Unreconciled
	}
	if n.Receipted {
		obj["receipted"] = true
	}
	if len(n.Bids) > 0 {
		obj["bids"] = n.Bids
	}
	if len(n.Contracts) > 0 {
		obj["contracts"] = n.Contracts
	}
	if len(n.OpenBids) > 0 {
		obj["openBids"] = n.OpenBids
	}
	return json.Marshal(obj)
}

// WorkBid is a tethered ledger bid, surfaced for the node's chips. Row carries
// the full source ledger row so chip actions (accept/decline) can exact-match
// mutate without a second lookup.
type WorkBid struct {
	Who    string    `json:"who"`
	Amount float64   `json:"amount"`
	Status string    `json:"status"`
	Row    LedgerRow `json:"row"`
}

// WorkStage is one top-level ROCK line. (Type name kept from the `## work`
// era — the whole package and client speak "stage"; the UI says rock.)
type WorkStage struct {
	ID           string      `json:"id"`
	Explicit     bool        `json:"-"`
	Text         string      `json:"text"`
	Checked      bool        `json:"checked"`
	Ready        bool        `json:"ready"` // all nodes checked (and at least one)
	Current      bool        `json:"current"`
	Est          float64     `json:"est"`         // the rock's OWN [est::] (not-yet-broken-down remainder)
	EstTotal     float64     `json:"estTotal"`    // own + Σ node est totals
	Unestimated  int         `json:"unestimated"` // open leaves carrying no est
	Weeks        float64     `json:"weeks"`       // [weeks:: N] — done-by prefill hint (templates)
	Done         string      `json:"done"`        // [done:: YYYY-MM-DD] — stamped at rock check
	DoneBy       string      `json:"doneBy"`      // [done-by:: YYYY-MM-DD] — the schedule IS these dates
	Fields       []WorkField `json:"fields,omitempty"`
	Extra        []string    `json:"-"`
	Tasks        []*WorkNode `json:"tasks"` // depth-1 nodes (milestones + loose tasks/decisions)
	Committed    float64     `json:"committed"`
	Paid         float64     `json:"paid"`
	Recognized   float64     `json:"recognized"`             // Σ node recognized + rock-level recognition
	Unreconciled float64     `json:"unreconciled,omitempty"` // Σ done-but-unlinked firm money
}

var (
	workLineRe  = record.CheckboxRe // the kernel checkbox grammar
	workFieldRe = record.FieldRe    // the kernel field grammar
)

// parseWorkFields strips every [key:: value] out of a rock line's text,
// keeping source order (rock lines round-trip byte-exact — existing files
// order [weeks::] before [done::], which the tasks canonical emit would flip).
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

// nodeHasField reports a field key's presence on a node line (a bare
// [decision::] marker has an empty value — presence is the signal).
func nodeHasField(t *tasks.Task, key string) bool {
	for _, f := range t.Fields {
		if strings.EqualFold(f.Key, key) {
			return true
		}
	}
	return false
}

// ParseWork reads a `## rocks` (or legacy `## work`) section's lines into the
// rock tree. Depth 0 checkbox lines are rocks, depth 1 nodes of the last
// rock, depth ≥2 children of the last depth-1 node; every non-checkbox line
// is preserved verbatim on the preceding entity (fixpoint).
func ParseWork(lines []string) []WorkStage {
	var stages []WorkStage
	var lastNode *WorkNode // last depth-1 node
	var lastLeaf *WorkNode // deepest node emitted last (Extra target)
	attachExtra := func(ln string) {
		if lastLeaf != nil {
			lastLeaf.Extra = append(lastLeaf.Extra, ln)
			return
		}
		st := &stages[len(stages)-1]
		st.Extra = append(st.Extra, ln)
	}
	for _, ln := range lines {
		m := workLineRe.FindStringSubmatch(ln)
		if m == nil {
			if t := strings.TrimRight(ln, " \t"); t != "" {
				if len(stages) == 0 {
					continue // stray prose before the first rock — dropped like pre-## prose
				}
				attachExtra(ln)
			}
			continue
		}
		checked := strings.EqualFold(m[2], "x")
		depth := workIndentWidth(m[1]) / 4
		if depth < 1 || len(stages) == 0 {
			text, fields := parseWorkFields(m[3])
			stages = append(stages, WorkStage{Text: text, Checked: checked, Fields: fields})
			lastNode, lastLeaf = nil, nil
			continue
		}
		node := &WorkNode{Task: tasks.ParseLine(checked, m[3])}
		st := &stages[len(stages)-1]
		if depth >= 2 && lastNode != nil {
			lastNode.Children = append(lastNode.Children, node)
		} else {
			st.Tasks = append(st.Tasks, node)
			lastNode = node
		}
		lastLeaf = node
	}
	assignWorkIDs(stages)
	// derive current (first unchecked) + ready + typed fields + est rollups
	cur := false
	for i := range stages {
		st := &stages[i]
		if st.Tasks == nil {
			st.Tasks = []*WorkNode{} // marshal as [] — a nil slice is null in JSON and crashes .forEach client-side
		}
		st.Est = fieldFloat(st.Fields, "est")
		st.Weeks = fieldFloat(st.Fields, "weeks")
		st.Done = fieldValue(st.Fields, "done")
		st.DoneBy = fieldValue(st.Fields, "done-by")
		st.EstTotal = st.Est
		st.Ready = len(st.Tasks) > 0
		for _, n := range st.Tasks {
			deriveNode(n)
			st.EstTotal += n.EstTotal
			st.Unestimated += n.Unestimated
			if !nodeDeepChecked(n) {
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

// deriveNode computes a node's typed projections + est rollup (post-order).
func deriveNode(n *WorkNode) {
	t := n.Task
	n.Est = taskFieldFloat(t, "est")
	n.Decision = nodeHasField(t, "decision")
	n.Resolution = t.FieldValue("resolution")
	n.Milestone = len(n.Children) > 0 || nodeHasField(t, "milestone")
	n.EstTotal = n.Est
	n.Unestimated = 0
	for _, c := range n.Children {
		deriveNode(c)
		n.EstTotal += c.EstTotal
		n.Unestimated += c.Unestimated
	}
	if len(n.Children) == 0 && !n.Milestone && !n.Decision && !t.Checked && n.Est == 0 {
		n.Unestimated = 1
	}
}

// nodeDeepChecked: the node and every descendant is checked.
func nodeDeepChecked(n *WorkNode) bool {
	if !n.Task.Checked {
		return false
	}
	for _, c := range n.Children {
		if !nodeDeepChecked(c) {
			return false
		}
	}
	return true
}

func taskFieldFloat(t *tasks.Task, key string) float64 {
	v := strings.ReplaceAll(strings.ReplaceAll(t.FieldValue(key), "$", ""), ",", "")
	if v == "" {
		return 0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return f
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

// WalkNodes visits every node in the tree depth-first (milestones before
// their children), with the owning rock.
func WalkNodes(stages []WorkStage, fn func(st *WorkStage, n *WorkNode)) {
	for i := range stages {
		var walk func(n *WorkNode)
		walk = func(n *WorkNode) {
			fn(&stages[i], n)
			for _, c := range n.Children {
				walk(c)
			}
		}
		for _, n := range stages[i].Tasks {
			walk(n)
		}
	}
}

// FindWorkNode returns the node whose work id OR task id matches, with its
// owning rock (nil when absent).
func FindWorkNode(stages []WorkStage, id string) (*WorkStage, *WorkNode) {
	var st *WorkStage
	var out *WorkNode
	WalkNodes(stages, func(s *WorkStage, n *WorkNode) {
		if out == nil && (n.ID == id || n.TaskID() == id) {
			st, out = s, n
		}
	})
	return st, out
}

// SetWorkField updates/inserts/removes a [key:: value] on the rock or node
// with the given id (empty value removes). On nodes, keys the shared tasks
// grammar types (done, added, owner, waiting, since, rank) set the typed
// field; everything else rides the unknown-field list. The typed projections
// re-derive on re-parse.
func SetWorkField(stages []WorkStage, id, key, value string) bool {
	for i := range stages {
		if stages[i].ID == id {
			upsertWorkField(&stages[i].Fields, key, value)
			return true
		}
	}
	_, n := FindWorkNode(stages, id)
	if n == nil {
		return false
	}
	SetNodeField(n, key, value)
	return true
}

// SetNodeField sets one field on a node line, routing typed tasks-grammar
// keys to their struct fields.
func SetNodeField(n *WorkNode, key, value string) {
	t := n.Task
	switch strings.ToLower(key) {
	case "done":
		t.Done = value
	case "added":
		t.Added = value
	case "owner":
		t.Owner = value
	case "waiting":
		t.Waiting = value
	case "since":
		t.Since = value
	case "rank":
		t.Rank = value
	case "stage":
		t.Stage = value
	default:
		var fs []tasks.Field
		found := false
		for _, f := range t.Fields {
			if strings.EqualFold(f.Key, key) {
				found = true
				if value == "" {
					continue // remove
				}
				f.Value = value
			}
			fs = append(fs, f)
		}
		if !found && value != "" {
			fs = append(fs, tasks.Field{Key: key, Value: value})
		}
		t.Fields = fs
	}
}

func upsertWorkField(fs *[]WorkField, key, value string) {
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
	var assign func(parentID string, n *WorkNode)
	assign = func(parentID string, n *WorkNode) {
		if ex := n.Task.FieldValue("work"); ex != "" {
			n.ID, n.Explicit = ex, true
			seen[ex] = true
		} else {
			n.ID = uniq(parentID + "/" + workSlug(n.Task.Text))
		}
		for _, c := range n.Children {
			assign(n.ID, c)
		}
	}
	for i := range stages {
		st := &stages[i]
		if ex := fieldValue(st.Fields, "work"); ex != "" {
			st.ID, st.Explicit = ex, true
			seen[ex] = true
		} else {
			st.ID = uniq(workSlug(st.Text))
		}
		for _, n := range st.Tasks {
			assign(st.ID, n)
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
	sl := record.Slug(s, 40)
	if sl == "" {
		sl = "item"
	}
	return sl
}

// EmitWork serializes the tree back to section lines — 4-space indents,
// `- [ ]`/`- [x]`, rock fields in source order, node lines through the shared
// tasks emit, Extra lines verbatim. Fixpoint with ParseWork.
func EmitWork(stages []WorkStage) string {
	var b strings.Builder
	var emitNode func(depth int, n *WorkNode)
	emitNode = func(depth int, n *WorkNode) {
		b.WriteString(strings.Repeat("    ", depth))
		b.WriteString(tasks.EmitLine(n.Task))
		b.WriteString("\n")
		for _, ex := range n.Extra {
			b.WriteString(ex + "\n")
		}
		for _, c := range n.Children {
			emitNode(depth+1, c)
		}
	}
	for _, st := range stages {
		if st.Checked {
			b.WriteString("- [x] ")
		} else {
			b.WriteString("- [ ] ")
		}
		b.WriteString(st.Text)
		for _, f := range st.Fields {
			if f.Value == "" {
				b.WriteString(" [" + f.Key + "::]")
			} else {
				b.WriteString(" [" + f.Key + ":: " + f.Value + "]")
			}
		}
		b.WriteString("\n")
		for _, ex := range st.Extra {
			b.WriteString(ex + "\n")
		}
		for _, n := range st.Tasks {
			emitNode(1, n)
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
	}
	changed := false
	WalkNodes(stages, func(_ *WorkStage, n *WorkNode) {
		if !changed && n.ID == id && !n.Explicit {
			n.Task.Fields = append(n.Task.Fields, tasks.Field{Key: "work", Value: id})
			n.Explicit = true
			changed = true
		}
	})
	return changed
}

// JoinWorkLedger attaches the derived money to the tree: a ledger row's
// WorkID matches any node (rolls up through its milestone into its rock) or a
// rock id directly. COMMITTED comes from accepted-contract allocations
// (overhaul decision 4) — draw-aware: committed = max(Σ allocations,
// Σ expenses) per node; paid = expenses. Legacy fallback: with no contract
// allocations at all, accepted BID rows still commit (pre-migration records
// keep working — live rehabs must never break). Never stored.
// JoinWorkBids attaches PROPOSED contract slices to their node. Separate from
// JoinWorkLedger on purpose: an open bid is not money, it is an option — it
// must never reach a sum.
func JoinWorkBids(stages []WorkStage, bids []NodeAllocation) {
	nodeByID := map[string]*WorkNode{}
	WalkNodes(stages, func(_ *WorkStage, n *WorkNode) { nodeByID[n.ID] = n })
	for _, b := range bids {
		n, ok := nodeByID[b.NodeID]
		if !ok {
			continue // a bid on a node that no longer exists stays on the contract
		}
		n.OpenBids = append(n.OpenBids, WorkContract{
			Slug: b.Contract, Contractor: b.Contractor, Amount: b.Amount, Date: b.Date, Expires: b.Expires,
		})
	}
}

func JoinWorkLedger(stages []WorkStage, ledger []LedgerRow, allocs []NodeAllocation) {
	type acc struct{ acceptedSum, expenseSum float64 }
	legacyBids := len(allocs) == 0
	// index every tether target
	nodeByID := map[string]*WorkNode{}
	stageByID := map[string]*WorkStage{}
	nodeStage := map[string]*WorkStage{}
	for i := range stages {
		stageByID[stages[i].ID] = &stages[i]
	}
	WalkNodes(stages, func(st *WorkStage, n *WorkNode) {
		nodeByID[n.ID] = n
		nodeStage[n.ID] = st
	})
	// per-tether-id own sums; expenses against a committed node are a DRAW
	// against that contract — committed = max(Σ committed, Σ expenses).
	perID := map[string]*acc{}
	touch := func(id string) *acc {
		if a, ok := perID[id]; ok {
			return a
		}
		a := &acc{}
		perID[id] = a
		return a
	}
	for _, a := range allocs {
		n, isNode := nodeByID[a.NodeID]
		_, isStage := stageByID[a.NodeID]
		if !isNode && !isStage {
			continue // allocation to a deleted node — money stays on the contract, no crash
		}
		touch(a.NodeID).acceptedSum += a.Amount
		if isNode {
			n.Contracts = append(n.Contracts, WorkContract{
				Slug: a.Contract, Contractor: a.Contractor, Amount: a.Amount,
			})
		}
	}
	for _, r := range ledger {
		if r.WorkID == "" {
			continue
		}
		n, isNode := nodeByID[r.WorkID]
		_, isStage := stageByID[r.WorkID]
		if !isNode && !isStage {
			continue // tether to a deleted node — money stays in the ledger, no crash
		}
		if normalizeCat(r.Cat) == CatOperating {
			// operating money never tethers into rehab nodes. This guard must
			// stay symmetric with ComputeProjectBudget's exclusion: a tethered
			// operating row in node Paid but not paid[hard] would corrupt the
			// hard accrual (rec + paid[hard] - tethCash) — negative SPENT.
			continue
		}
		isExpense := strings.EqualFold(r.Type, "expense")
		accepted := strings.EqualFold(r.Type, "bid") && strings.EqualFold(r.Status, "accepted")
		if isExpense {
			touch(r.WorkID).expenseSum += r.Amount
		} else if accepted && legacyBids {
			touch(r.WorkID).acceptedSum += r.Amount
		}
		if isNode && legacyBids && strings.EqualFold(r.Type, "bid") {
			who := r.Contractor
			if who == "" {
				who = r.Vendor
			}
			n.Bids = append(n.Bids, WorkBid{Who: who, Amount: r.Amount, Status: r.Status, Row: r})
		}
	}
	drawAware := func(a *acc) float64 {
		if a == nil {
			return 0
		}
		if a.acceptedSum > a.expenseSum {
			return a.acceptedSum
		}
		return a.expenseSum
	}
	// receipt evidence: every committed slice on the id carries a doc —
	// contract mode reads the contracts' doc refs, legacy mode the bid rows'
	receipted := func(id string) bool {
		if !legacyBids {
			any := false
			for _, a := range allocs {
				if a.NodeID != id {
					continue
				}
				if a.Doc == "" {
					return false
				}
				any = true
			}
			return any
		}
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
	// post-order fold: own sums + child rollups
	var fold func(n *WorkNode)
	fold = func(n *WorkNode) {
		a := perID[n.ID]
		var ownPaid, firm float64
		if a != nil {
			ownPaid, firm = a.expenseSum, a.acceptedSum
		}
		n.Paid = ownPaid
		n.Committed = drawAware(a)
		ownRec := ownPaid
		if n.Task.Checked && firm > ownPaid {
			ownRec = firm
			if receipted(n.ID) {
				n.Receipted = true
			} else {
				n.Unreconciled = firm - ownPaid
			}
		}
		n.Recognized = ownRec
		for _, c := range n.Children {
			fold(c)
			n.Paid += c.Paid
			n.Committed += c.Committed
			n.Recognized += c.Recognized
			n.Unreconciled += c.Unreconciled
		}
	}
	for i := range stages {
		st := &stages[i]
		a := perID[st.ID]
		var ownPaid, firm float64
		if a != nil {
			ownPaid, firm = a.expenseSum, a.acceptedSum
		}
		st.Paid = ownPaid
		st.Committed = drawAware(a)
		rec := ownPaid
		if st.Checked && firm > rec {
			if !receipted(st.ID) {
				st.Unreconciled += firm - rec
			}
			rec = firm
		}
		st.Recognized = rec
		for _, n := range st.Tasks {
			fold(n)
			st.Paid += n.Paid
			st.Committed += n.Committed
			st.Recognized += n.Recognized
			st.Unreconciled += n.Unreconciled
		}
	}
}

// Rock templates (research consensus). Seeds only — never enforced; delete
// five rocks in ten seconds for a cosmetic flip.
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
