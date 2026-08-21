package approvals

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"manifest/mdfm"
	"manifest/realestate"
	"manifest/record"
	"manifest/tasks"
)

// The RE-CONTRACT proposal type (overhaul §5 — the intake ritual's output):
// the extractor parses an uploaded bid/contract/estimate document into ONE
// structured proposal — contractor match-or-create, total, property
// allocation split, node mapping (with new milestones where the tree lacks a
// scope), extracted tasks + decisions, and the terms/exclusions/risk prose.
// The owner adjusts amounts on the card (the spirit proposes, never
// decides); Confirm writes the records through the approved-proposal
// capability: contractor (when new) → per-property tree additions →
// the contract record. Audited, git-trailed.
const TypeReContract = "re-contract"

// ReContractFence is the body's payload fence language tag.
const ReContractFence = "re-contract"

// reContractPathRe is the ONLY apply-path shape: one new record directly
// under the contracts folder. Byte-identical to the engine's audit predicate.
var reContractPathRe = regexp.MustCompile(`^system/realestate/contracts/[a-z0-9][a-z0-9-]*\.md$`)

// ReContractPathAllowed is the re-contract apply-path allow-list.
func ReContractPathAllowed(rel string) bool { return reContractPathRe.MatchString(rel) }

// ReContractAllocation is one slice of the proposed split.
type ReContractAllocation struct {
	Property string  `json:"property"` // property slug
	Node     string  `json:"node"`     // rock/milestone/task work id (may name a new_milestones entry)
	Amount   float64 `json:"amount"`
	Reason   string  `json:"reason,omitempty"` // the split reasoning, shown on the card
}

// ReContractMilestone is a proposed new milestone (a scope the tree lacks).
type ReContractMilestone struct {
	Property string `json:"property"`
	Rock     string `json:"rock"` // rock id or text
	Name     string `json:"name"` // milestone text; its node id becomes <rock-id>/<slug(name)>
}

// ReContractTask is one extracted task or decision.
type ReContractTask struct {
	Property string `json:"property"`
	Parent   string `json:"parent,omitempty"` // rock/milestone id or rock text ("" = current rock)
	Text     string `json:"text"`
	Decision bool   `json:"decision,omitempty"`
	Owner    string `json:"owner,omitempty"`
}

// ReContractPayload is the ````re-contract fence's JSON object.
type ReContractPayload struct {
	Kind             string                 `json:"kind"`                        // bid | contract | estimate
	Contractor       string                 `json:"contractor,omitempty"`        // existing record slug
	ContractorCreate string                 `json:"contractor_create,omitempty"` // display name when no record matches
	Name             string                 `json:"name"`
	Total            float64                `json:"total"`
	Date             string                 `json:"date,omitempty"`
	Expires          string                 `json:"expires,omitempty"`
	Doc              string                 `json:"doc,omitempty"` // sha256:… CAS ref
	Allocations      []ReContractAllocation `json:"allocations"`
	NewMilestones    []ReContractMilestone  `json:"new_milestones,omitempty"`
	Tasks            []ReContractTask       `json:"tasks,omitempty"`
	Terms            []string               `json:"terms,omitempty"`
	Exclusions       []string               `json:"exclusions,omitempty"`
	RiskItems        []string               `json:"risk_items,omitempty"`
}

// Validate checks the shape invariants (shared by filing, edit, and apply).
func (p *ReContractPayload) Validate() error {
	switch p.Kind {
	case "bid", "contract", "estimate":
	default:
		return fmt.Errorf("kind must be bid|contract|estimate")
	}
	if strings.TrimSpace(p.Contractor) == "" && strings.TrimSpace(p.ContractorCreate) == "" {
		return fmt.Errorf("contractor (slug) or contractor_create (name) is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if p.Total <= 0 {
		return fmt.Errorf("total must be positive")
	}
	if len(p.Allocations) == 0 {
		return fmt.Errorf("at least one allocation is required")
	}
	var sum float64
	for _, a := range p.Allocations {
		if strings.TrimSpace(a.Property) == "" || strings.TrimSpace(a.Node) == "" {
			return fmt.Errorf("every allocation needs property and node")
		}
		if a.Amount <= 0 {
			return fmt.Errorf("allocation amounts must be positive")
		}
		sum += a.Amount
	}
	if diff := sum - p.Total; diff > 0.01 || diff < -0.01 {
		return fmt.Errorf("Σ allocations (%.2f) must equal total (%.2f)", sum, p.Total)
	}
	for _, t := range p.Tasks {
		if strings.TrimSpace(t.Text) == "" || strings.TrimSpace(t.Property) == "" {
			return fmt.Errorf("every task needs property and text")
		}
	}
	return nil
}

// Status maps the document kind to the contract lifecycle: a signed contract
// commits money; bids and estimates arrive proposed.
func (p *ReContractPayload) Status() string {
	if p.Kind == "contract" {
		return "accepted"
	}
	return "proposed"
}

// ParseReContractPayload extracts the fence from a proposal body.
func ParseReContractPayload(body string) (ReContractPayload, bool) {
	raw, ok := mdfm.ExtractFencedBlock(body, ReContractFence)
	if !ok {
		return ReContractPayload{}, false
	}
	var p ReContractPayload
	if json.Unmarshal([]byte(raw), &p) != nil {
		return ReContractPayload{}, false
	}
	return p, true
}

// SetReContractPayload rewrites the pending proposal's fence with the
// owner-edited payload (the card's adjust-amounts affordance — decision §5:
// the owner always adjusts; the spirit proposes, never decides).
func (s *Store) SetReContractPayload(id string, payload ReContractPayload) error {
	src := filepath.Join(s.dir, "pending", id+".md")
	p, err := s.parse(src)
	if err != nil {
		return err
	}
	if p.Type != TypeReContract {
		return fmt.Errorf("proposal %s is not a re-contract proposal", id)
	}
	if err := payload.Validate(); err != nil {
		return err
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	body, ok := replaceFencedBlock(p.Body, ReContractFence, string(out))
	if !ok {
		return fmt.Errorf("proposal %s carries no %s payload fence", id, ReContractFence)
	}
	p.Body = body
	return os.WriteFile(src, []byte(serialize(p)), 0o644)
}

// replaceFencedBlock swaps the CONTENT of the first ````<lang> fence.
func replaceFencedBlock(body, lang, content string) (string, bool) {
	lines := strings.Split(body, "\n")
	start := -1
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "````"+lang) || strings.HasPrefix(t, "```"+lang) {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	marker := "````"
	if !strings.HasPrefix(strings.TrimSpace(lines[start]), "````") {
		marker = "```"
	}
	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == marker {
			end = i
			break
		}
	}
	if end < 0 {
		return "", false
	}
	out := append([]string{}, lines[:start+1]...)
	out = append(out, strings.Split(strings.TrimRight(content, "\n"), "\n")...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n"), true
}

// applyReContract writes the confirmed intake: contractor record (when new),
// per-property tree additions (new milestones + extracted tasks/decisions),
// then the contract record — everything through the approved-proposal
// capability, refusing any drift from the allow-list.
func (s *Store) applyReContract(p Proposal) error {
	if !ReContractPathAllowed(p.ApplyPath) {
		return fmt.Errorf("apply refused: %q is not a contracts-folder record path", p.ApplyPath)
	}
	if s.vw == nil || s.reCap == "" || s.vaultRoot == "" {
		return fmt.Errorf("apply refused: real-estate applies are not configured")
	}
	payload, ok := ParseReContractPayload(p.Body)
	if !ok {
		return fmt.Errorf("apply refused: re-contract %s carries no payload fence", p.ID)
	}
	if err := payload.Validate(); err != nil {
		return fmt.Errorf("apply refused: %w", err)
	}
	// A second bid for the SAME scope is the normal case, not an error: the
	// slug is derived from contractor+scope+property, so competing bids
	// collide by construction, and the workbench is built to hold several and
	// accept one. Refusing outright stranded two real $38,500 WM Electric
	// bids behind $42,150 records from a month earlier (2026-08-21). So:
	// same source document → a true re-extraction, refuse; different document
	// → a competing bid, land it beside the first.
	applyPath, err := s.freeContractPath(p.ApplyPath, payload)
	if err != nil {
		return err
	}

	// 1) contractor — an existing slug must resolve; a create writes a thin
	// record (idempotent: an existing file just gets referenced)
	ctrDir := "system/realestate/contractors"
	slug := strings.TrimSpace(payload.Contractor)
	if slug == "" {
		name := strings.TrimSpace(payload.ContractorCreate)
		slug = record.Slug(name, 60)
		rel := path.Join(ctrDir, slug+".md")
		if _, err := os.Stat(filepath.Join(s.vaultRoot, filepath.FromSlash(rel))); err != nil {
			content := "---\ncategories: [contractor]\nname: " + name + "\n---\n\n# " + name + "\n"
			if err := s.vw.WriteCap(s.reCap, rel, []byte(content)); err != nil {
				return fmt.Errorf("apply refused: contractor create: %w", err)
			}
		}
	} else if _, err := os.Stat(filepath.Join(s.vaultRoot, filepath.FromSlash(path.Join(ctrDir, slug+".md")))); err != nil {
		return fmt.Errorf("apply refused: no contractor record %q", slug)
	}

	// 2) per-property tree additions — one surgical section write per property
	type propWork struct {
		milestones []ReContractMilestone
		tasks      []ReContractTask
	}
	perProp := map[string]*propWork{}
	touchProp := func(slug string) *propWork {
		if w, ok := perProp[slug]; ok {
			return w
		}
		w := &propWork{}
		perProp[slug] = w
		return w
	}
	for _, m := range payload.NewMilestones {
		touchProp(m.Property).milestones = append(touchProp(m.Property).milestones, m)
	}
	for _, t := range payload.Tasks {
		touchProp(t.Property).tasks = append(touchProp(t.Property).tasks, t)
	}
	for _, a := range payload.Allocations {
		touchProp(a.Property) // existence check below, even with no tree writes
	}
	today := time.Now().Format("2006-01-02")
	for propSlug, work := range perProp {
		rel := path.Join("system/realestate/properties", propSlug+".md")
		raw, err := os.ReadFile(filepath.Join(s.vaultRoot, filepath.FromSlash(rel)))
		if err != nil {
			return fmt.Errorf("apply refused: no property record %q", propSlug)
		}
		if len(work.milestones) == 0 && len(work.tasks) == 0 {
			continue
		}
		_, body := mdfm.Split(string(raw))
		section, lines := rockSection(body)
		stages := realestate.ParseWork(lines)
		list := &realestate.PropertyTaskList{Stages: stages, Section: section}
		for _, m := range work.milestones {
			t := &tasks.Task{Text: strings.TrimSpace(m.Name)}
			t.Fields = append(t.Fields, tasks.Field{Key: "milestone", Value: ""})
			node := list.Append(t, strings.TrimSpace(m.Rock))
			_ = node
		}
		list.Stages = realestate.ParseWork(strings.Split(strings.TrimRight(realestate.EmitWork(list.Stages), "\n"), "\n"))
		for _, tk := range work.tasks {
			t := &tasks.Task{Text: strings.TrimSpace(tk.Text), Added: today, Owner: strings.TrimSpace(tk.Owner)}
			if tk.Decision {
				t.Fields = append(t.Fields, tasks.Field{Key: "decision", Value: ""})
			}
			list.Append(t, strings.TrimSpace(tk.Parent))
		}
		if err := s.vw.ReplaceSectionCap(s.reCap, rel, section, realestate.EmitWork(list.Stages)); err != nil {
			return fmt.Errorf("apply refused: tree write on %s: %w", propSlug, err)
		}
	}

	// 3) the contract record itself
	c := realestate.Contract{
		Slug: strings.TrimSuffix(path.Base(applyPath), ".md"),
		Name: payload.Name, Contractor: slug, Status: payload.Status(),
		Total: payload.Total, Date: payload.Date, Expires: payload.Expires, Doc: payload.Doc,
		Terms: payload.Terms, Exclusions: payload.Exclusions, RiskItems: payload.RiskItems,
	}
	for _, a := range payload.Allocations {
		c.Allocations = append(c.Allocations, realestate.ContractAllocation{
			Property: a.Property, NodeID: a.Node, Amount: a.Amount,
		})
	}
	if err := s.vw.WriteCap(s.reCap, applyPath, []byte(realestate.NewContractRecord(c))); err != nil {
		return fmt.Errorf("apply refused: contract write: %w", err)
	}
	return nil
}

// freeContractPath resolves where a confirmed contract record actually lands.
// The happy path is the proposal's own apply-path. When that is taken it asks
// whether this is the SAME document arriving twice (refuse — re-extracting one
// bid must never write it twice) or a DIFFERENT one (a competing bid — give it
// the next free -N slug). Identity is the document hash; with no hash on
// either side it falls back to total+date, which is what distinguishes two
// bids in practice.
func (s *Store) freeContractPath(applyPath string, payload ReContractPayload) (string, error) {
	full := func(rel string) string { return filepath.Join(s.vaultRoot, filepath.FromSlash(rel)) }
	if _, err := os.Stat(full(applyPath)); err != nil {
		return applyPath, nil // free
	}
	base := strings.TrimSuffix(applyPath, ".md")
	for n := 1; n <= 20; n++ {
		rel := applyPath
		if n > 1 {
			rel = fmt.Sprintf("%s-%d.md", base, n)
		}
		b, err := os.ReadFile(full(rel))
		if err != nil {
			if !ReContractPathAllowed(rel) { // a suffix must stay inside the allow-list
				return "", fmt.Errorf("apply refused: %q is not a contracts-folder record path", rel)
			}
			return rel, nil // this slug is free
		}
		if sameContractDocument(string(b), payload) {
			return "", fmt.Errorf("apply refused: %s already holds this same document — "+
				"it was imported before (re-extracting one bid must not write it twice)", rel)
		}
	}
	return "", fmt.Errorf("apply refused: %s and 20 numbered siblings are all taken", applyPath)
}

// sameContractDocument reports whether an existing contract record came from
// the same source document as this payload.
func sameContractDocument(existing string, payload ReContractPayload) bool {
	fm, _ := mdfm.Split(existing)
	have := strings.TrimSpace(strings.Trim(fm["doc"], `"`))
	want := strings.TrimSpace(payload.Doc)
	if have != "" && want != "" {
		return strings.EqualFold(have, want) // the hash is the identity
	}
	// no hash to compare — two bids differing in money or date are different
	// bids; identical on both is the same one arriving twice
	return strings.TrimSpace(fm["total"]) == strings.TrimSpace(fmtContractTotal(payload.Total)) &&
		strings.TrimSpace(fm["date"]) == strings.TrimSpace(payload.Date)
}

func fmtContractTotal(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

// rockSection picks the tree's section: `## rocks`, tolerating the legacy
// `## work` heading (pre-migration records).
func rockSection(body string) (string, []string) {
	sec := func(name string) ([]string, bool) {
		var out []string
		in, seen := false, false
		for _, ln := range strings.Split(body, "\n") {
			t := strings.TrimRight(ln, " \t")
			if strings.HasPrefix(t, "## ") && !strings.HasPrefix(t, "### ") {
				in = strings.EqualFold(t, "## "+name)
				if in {
					seen = true
				}
				continue
			}
			if in {
				out = append(out, ln)
			}
		}
		return out, seen
	}
	if lines, ok := sec("rocks"); ok {
		return "rocks", lines
	}
	lines, _ := sec("work")
	return "work", lines
}
