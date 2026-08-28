// Goals placement proposals (telegram→feed lane, §12 amendment 2026-08-19):
// the pure accept transform behind the `goals-item` approval type. The owner
// authors the words (on Telegram, in a thread); the agent only PLACES them —
// and nothing here runs until the owner confirms the card in FEED.
//
// This file is deliberately I/O-free: ApplyPlacement is bytes→bytes, the
// approvals store owns the read and the capability-gated write, exactly like
// aion.AppendBacklogItem. All mutation goes through the Doc's own semantic
// methods (EditGoal, MoveGoal) so pin-before-rename, alias-on-rename and the
// only-rocks-take-children rule are inherited, never re-implemented.
package goals

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"manifest/mdfm"
)

// PayloadFence is the fenced-block language tag a goals proposal's body
// carries: one ```goals block holding one JSON object.
const PayloadFence = "goals"

// Placement modes and levels — closed vocabularies.
const (
	ModeAdd  = "add"
	ModeEdit = "edit"
	ModeMove = "move"

	LevelRock      = "rock"
	LevelMilestone = "milestone" // depth 1 under a rock
	LevelTask      = "task"      // under ANY existing goal, at any depth
)

// PlacementPayload is the ```goals fence's JSON object. quote/confidence are
// proposal-display only and never reach goals.md.
type PlacementPayload struct {
	Mode  string `json:"mode"`  // add | edit | move
	Level string `json:"level"` // rock | milestone | task
	Area  string `json:"area"`  // "## <Area>" heading text, e.g. "Home"

	ParentID string `json:"parentId,omitempty"` // milestone add/move: the rock that takes it; task add: any existing goal ("" on move = promote to rock)
	TargetID string `json:"targetId,omitempty"` // edit/move: the goal being changed
	// AnchorText is the staleness guard on edit/move: the target's CURRENT
	// text as the proposer saw it. A mismatch refuses the apply — the file
	// moved under the proposal, and the owner re-reviews rather than the app
	// guessing. (New invention: aion-resolve matches by title-derived id and
	// has nothing like it.)
	AnchorText string `json:"anchorText,omitempty"`

	Title string `json:"title,omitempty"` // add: the new line; edit: the new text ("" = keep)
	Owner string `json:"owner,omitempty"`

	// Rock-only extras — all optional. The quarter is never carried: a new
	// rock always stamps the CURRENT quarter (owner call 2026-08-19).
	Start  string   `json:"start,omitempty"`
	Due    string   `json:"due,omitempty"`
	Serves []string `json:"serves,omitempty"`

	// Proposal-display only.
	Source     string  `json:"source,omitempty"`
	Quote      string  `json:"quote,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// Validate checks the closed vocabularies and per-mode required fields.
func (p *PlacementPayload) Validate() error {
	switch p.Mode {
	case ModeAdd, ModeEdit, ModeMove:
	default:
		return fmt.Errorf("mode must be add, edit or move (got %q)", p.Mode)
	}
	switch p.Level {
	case LevelRock, LevelMilestone, LevelTask:
	default:
		return fmt.Errorf("level must be rock, milestone or task (got %q)", p.Level)
	}
	if strings.TrimSpace(p.Area) == "" {
		return errors.New("area is required")
	}
	switch p.Mode {
	case ModeAdd:
		if strings.TrimSpace(p.Title) == "" {
			return errors.New("add needs a title")
		}
		if p.Level != LevelRock && strings.TrimSpace(p.ParentID) == "" {
			return fmt.Errorf("a %s add needs the goal it goes under (parentId)", p.Level)
		}
	case ModeEdit, ModeMove:
		if strings.TrimSpace(p.TargetID) == "" {
			return fmt.Errorf("%s needs targetId", p.Mode)
		}
		if strings.TrimSpace(p.AnchorText) == "" {
			return fmt.Errorf("%s needs anchorText (the target's current text)", p.Mode)
		}
	}
	if p.Level != LevelRock && (p.Start != "" || p.Due != "" || len(p.Serves) > 0) {
		return errors.New("start/due/serves are rock-only fields")
	}
	for _, d := range []struct{ name, v string }{{"start", p.Start}, {"due", p.Due}} {
		if d.v != "" && !isISO(d.v) {
			return fmt.Errorf("%s must be YYYY-MM-DD (got %q)", d.name, d.v)
		}
	}
	return nil
}

func isISO(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ParsePlacement extracts the ```goals fence from a proposal body.
func ParsePlacement(body string) (PlacementPayload, bool) {
	raw, ok := mdfm.ExtractFencedBlock(body, PayloadFence)
	if !ok {
		return PlacementPayload{}, false
	}
	var p PlacementPayload
	if json.Unmarshal([]byte(raw), &p) != nil {
		return PlacementPayload{}, false
	}
	return p, true
}

// ApplyPlacement is the pure accept transform: current goals.md bytes + one
// confirmed payload → next bytes. Every refusal returns an error and leaves
// nothing to write.
func ApplyPlacement(current string, p PlacementPayload, now time.Time) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	// Precondition: the current file must already be a Serialize fixpoint.
	// Serialize canonicalizes on first pass (field order, indent, dropped
	// no-op fields) — applying a one-line change to a non-canonical file
	// would rewrite the whole document, which the minimal-diff budget below
	// exists to forbid. The owner's file is canonical in practice; if it ever
	// is not, one by-hand save in the app canonicalizes it under the
	// user-action lane, where a full rewrite is legitimate.
	doc := Parse(current)
	if Serialize(doc) != current {
		return "", errors.New("goals.md is not in canonical form — open GOALS and make any edit by hand once, then re-confirm")
	}

	area := doc.FindArea(p.Area)
	if area == nil {
		return "", fmt.Errorf("no area %q in goals.md (a proposal never creates areas)", p.Area)
	}

	switch p.Mode {
	case ModeAdd:
		if err := applyAdd(doc, area, p, now); err != nil {
			return "", err
		}
	case ModeEdit:
		if err := applyEdit(doc, p); err != nil {
			return "", err
		}
	case ModeMove:
		if err := applyMove(doc, area, p); err != nil {
			return "", err
		}
	}

	next := Serialize(doc)
	if err := assertMinimalDiff(current, next, p.Mode); err != nil {
		return "", err
	}
	return next, nil
}

func applyAdd(doc *Doc, area *Area, p PlacementPayload, now time.Time) error {
	title := strings.TrimSpace(p.Title)
	// duplicate guard: same title (or an alias of it) anywhere live in the area
	tslug := slug(title)
	for _, g := range areaLiveGoals(area) {
		if strings.EqualFold(strings.TrimSpace(g.Text), title) || slug(g.Text) == tslug {
			return fmt.Errorf("%q already exists in %s", title, area.Name)
		}
		for _, al := range g.Aliases {
			if slug(al) == tslug {
				return fmt.Errorf("%q matches an alias of %q in %s", title, g.Text, area.Name)
			}
		}
	}
	parentID := strings.TrimSpace(p.ParentID)
	// the depth rule, checked before AddGoal ever runs. It is level-shaped: a
	// milestone still hangs only off a top-level rock (MoveGoal enforces the
	// same for moves); a TASK hangs off any goal that ALREADY EXISTS in the
	// same area. Neither ever creates the structure it needs — a proposal
	// places one line into a shape the owner already wrote.
	switch p.Level {
	case LevelRock:
		parentID = ""
	case LevelMilestone:
		rock := doc.RockOf(parentID)
		if rock == nil || rock.ID != parentID {
			return fmt.Errorf("parent %q is not a top-level rock — milestones hang off rocks only", parentID)
		}
	case LevelTask:
		pArea, parent := doc.FindGoal(parentID)
		if parent == nil {
			return fmt.Errorf("no goal %q in goals.md — a task hangs off a goal that already exists", parentID)
		}
		if pArea.Name != area.Name {
			return fmt.Errorf("parent %q is not in area %s", parentID, area.Name)
		}
	}
	g, ok := doc.AddGoal(area.Name, parentID, "rocks", title, p.Owner)
	if !ok {
		return fmt.Errorf("could not place %q under %q", title, parentID)
	}
	if p.Level == LevelRock {
		g.Quarter = CurrentQuarter(now) // a new rock always lands in the current quarter
		g.Start, g.Due = stripBracket(p.Start), stripBracket(p.Due)
		g.Serves = dedupeNonEmpty(p.Serves)
	}
	doc.assignIDs()
	return nil
}

func applyEdit(doc *Doc, p PlacementPayload) error {
	g, err := anchoredTarget(doc, p)
	if err != nil {
		return err
	}
	e := GoalEdit{}
	if t := strings.TrimSpace(p.Title); t != "" && t != g.Text {
		e.Text = &t
	}
	if p.Owner != "" {
		e.Owner = &p.Owner
	}
	if p.Level == LevelRock {
		if p.Start != "" {
			e.Start = &p.Start
		}
		if p.Due != "" {
			e.Due = &p.Due
		}
		if len(p.Serves) > 0 {
			s := p.Serves
			e.Serves = &s
		}
	}
	// EditGoal owns pin-before-rename and alias-on-rename — inherited, not
	// rebuilt here.
	if !doc.EditGoal(g.ID, e) {
		return fmt.Errorf("edit of %q failed", p.TargetID)
	}
	return nil
}

func applyMove(doc *Doc, area *Area, p PlacementPayload) error {
	g, err := anchoredTarget(doc, p)
	if err != nil {
		return err
	}
	if a, _ := doc.FindGoal(g.ID); a == nil || a.Name != area.Name {
		return fmt.Errorf("%q is not in area %s", p.TargetID, area.Name)
	}
	// a move changes the derived id's parent path — freeze identity first so
	// every reference ([rock::], daily [goal::], serves) survives the move
	g.pin()
	if !doc.MoveGoal(g.ID, strings.TrimSpace(p.ParentID)) {
		if p.ParentID == "" {
			return fmt.Errorf("could not promote %q to a rock (already top-level?)", p.TargetID)
		}
		return fmt.Errorf("could not move %q under %q — only a top-level rock in the same area may take it", p.TargetID, p.ParentID)
	}
	doc.assignIDs()
	return nil
}

// anchoredTarget resolves TargetID and enforces the staleness anchor.
func anchoredTarget(doc *Doc, p PlacementPayload) (*Goal, error) {
	_, g := doc.FindGoal(strings.TrimSpace(p.TargetID))
	if g == nil {
		return nil, fmt.Errorf("no goal %q in goals.md — it may have been renamed since this was proposed", p.TargetID)
	}
	if !strings.EqualFold(strings.TrimSpace(g.Text), strings.TrimSpace(p.AnchorText)) {
		return nil, fmt.Errorf("goal %q now reads %q, not %q — the file moved under this proposal; re-review it", p.TargetID, g.Text, p.AnchorText)
	}
	return g, nil
}

// areaLiveGoals: every live goal in the area (annuals + rocks + children).
// Frozen history lines are text, not goals, and never participate.
func areaLiveGoals(a *Area) []*Goal {
	var out []*Goal
	var walk func(gs []*Goal)
	walk = func(gs []*Goal) {
		for _, g := range gs {
			out = append(out, g)
			walk(g.Children)
		}
	}
	walk(a.Annuals)
	walk(a.Rocks)
	return out
}

// assertMinimalDiff is the structural budget: a confirmed placement is ONE
// line added (add), one line replaced in position (edit), or one line
// relocated (move). Anything wider means the transform did more than the
// owner approved, and the write is refused.
func assertMinimalDiff(current, next, mode string) error {
	if next == current {
		return errors.New("the placement changed nothing")
	}
	cur, nxt := strings.Split(current, "\n"), strings.Split(next, "\n")
	removed, added := lineDelta(cur, nxt)
	switch mode {
	case ModeAdd:
		if removed != 0 || added != 1 {
			return fmt.Errorf("add must be exactly one new line (got +%d/-%d)", added, removed)
		}
	case ModeEdit:
		// an edit rewrites the target line; pin-before-rename may also grow
		// that same line, so the budget is one-for-one
		if removed > 1 || added > 1 {
			return fmt.Errorf("edit must touch exactly one line (got +%d/-%d)", added, removed)
		}
	case ModeMove:
		// one line leaves its position, one (possibly re-indented, possibly
		// newly pinned) arrives elsewhere
		if removed > 1 || added > 1 {
			return fmt.Errorf("move must relocate exactly one line (got +%d/-%d)", added, removed)
		}
		if len(cur) != len(nxt) {
			return fmt.Errorf("move must preserve the line count (%d → %d)", len(cur), len(nxt))
		}
	}
	return nil
}

// lineDelta counts lines unique to each side (multiset difference) — cheap
// and order-blind, which is exactly what a budget needs.
func lineDelta(cur, nxt []string) (removed, added int) {
	count := map[string]int{}
	for _, l := range cur {
		count[l]++
	}
	for _, l := range nxt {
		count[l]--
	}
	for _, n := range count {
		if n > 0 {
			removed += n
		} else if n < 0 {
			added -= n
		}
	}
	return
}
