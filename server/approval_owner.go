package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/goals"
	"manifest/typesafe"
)

// OWNER AND GOAL, FROM EVIDENCE (owner, 2026-10-06: "tasks tethered to the
// proper goals/milestones", names "added and accurately"). What each piece is
// allowed to do was measured on 91 tasks he approved (open/internal meetings):
//
//   - the quote and its diarized speaker are shown on the card — evidence,
//     not a decision: the speaker matched his final owner only 27 of 91
//     times ("Hannah, can you run X" is said by Benjamin), and diarization
//     can be wrong or "Other";
//   - Jev's owner pick is offered as a one-tap chip only at confidence ≥ 0.8
//     and only when it differs from the card: 11 of 12 such picks matched his
//     final owner (Jev said "unclear" 66 times rather than guess);
//   - a goal tether that names no goal in his goals (id or alias) is flagged —
//     plain code, no model. Jev's goal picks were NOT reliable enough to offer
//     (11 of 16 right at confidence ≥ 0.7), so no goal suggestion is made;
//     his own goal edits teach the hint chips instead (approval_edits.go).
//
// Open/internal meetings only reach Jev; nothing here edits a card.

const (
	ownerSuggestMinConf = 0.8
	jevKindOwner        = "owner"
)

type ownerView struct {
	Quote   string `json:"quote,omitempty"`
	Speaker string `json:"speaker,omitempty"` // as diarized — evidence, not the answer
	// Suggest is Jev's confident owner (initials) when it differs from the card.
	Suggest     string  `json:"suggest,omitempty"`
	SuggestName string  `json:"suggestName,omitempty"`
	SuggestConf float64 `json:"suggestConf,omitempty"`
	// RockMissing: the card's goal names no goal in the Aion area.
	RockMissing bool `json:"rockMissing,omitempty"`
}

// rawAionPayload keeps the extraction's quote (ProposalPayload drops it).
func rawAionPayload(body string) (map[string]any, bool) {
	start := strings.Index(body, "````aion\n")
	if start < 0 {
		return nil, false
	}
	rest := body[start+len("````aion\n"):]
	end := strings.Index(rest, "\n````")
	if end < 0 {
		return nil, false
	}
	var m map[string]any
	return m, json.Unmarshal([]byte(rest[:end]), &m) == nil
}

func (s *Server) approvalOwner(p approvals.Proposal) *ownerView {
	if p.Type != approvals.TypeAionBacklog {
		return nil
	}
	pl, ok := aion.ParsePayloadFence(p.Body, aion.PayloadFence)
	if !ok || pl.Kind == "heuristic" {
		return nil
	}
	v := &ownerView{}
	if raw, ok := rawAionPayload(p.Body); ok {
		v.Quote, _ = raw["quote"].(string)
	}
	src := ""
	if len(pl.Sources) > 0 {
		src = strings.TrimSuffix(pl.Sources[0], ".md")
	}
	var ex *quoteExcerpt
	if v.Quote != "" && strings.HasPrefix(src, "log/") {
		ex = s.quoteExcerpt(src, v.Quote)
		if ex != nil {
			v.Speaker = ex.Speaker
		}
	}
	if pl.Rock != "" && !s.aionRockKnown(pl.Rock) {
		v.RockMissing = true
	}
	if ex != nil && s.jevAutoOn() && s.screenSourceAllowed(src) {
		s.ownerSuggestion(p.ID, pl, src, v, ex)
	}
	if v.Quote == "" && v.Speaker == "" && v.Suggest == "" && !v.RockMissing {
		return nil
	}
	return v
}

// aionRockKnown: the tether names a goal or milestone in the Aion area, by
// id or by an alias of its last segment.
func (s *Server) aionRockKnown(rock string) bool {
	area := s.aionGoalsArea()
	if area == nil {
		return true // no goals wired: nothing to check against, so no flag
	}
	last := strings.ToLower(path.Base(rock))
	var walk func([]goals.GoalView) bool
	walk = func(gs []goals.GoalView) bool {
		for _, g := range gs {
			if strings.EqualFold(g.ID, rock) || strings.EqualFold(path.Base(g.ID), last) {
				return true
			}
			for _, a := range g.Aliases {
				if strings.EqualFold(strings.ReplaceAll(a, " ", "-"), last) {
					return true
				}
			}
			if walk(g.Children) {
				return true
			}
		}
		return false
	}
	return walk(area.Rocks)
}

// quoteExcerpt is where a quote sits in its transcript: the diarized speaker
// and the turns before it (never the whole transcript).
type quoteExcerpt struct {
	Speaker string   `json:"speaker"`
	Before  []string `json:"before"`
	Turn    string   `json:"turn"`
}

var (
	speakerTurn = regexp.MustCompile(`^\*\*([^*]{1,80}):\*\*\s*(.*)$`)
	excerptMu   sync.Mutex
	excerptMemo = map[string]*quoteExcerpt{}
)

func (s *Server) quoteExcerpt(src, quote string) *quoteExcerpt {
	if s.index == nil {
		return nil
	}
	abs := filepath.Join(s.index.VaultRoot(), filepath.FromSlash(src)+".md")
	st, err := os.Stat(abs)
	if err != nil {
		return nil
	}
	key := abs + "\x00" + st.ModTime().String() + "\x00" + quote
	excerptMu.Lock()
	if e, ok := excerptMemo[key]; ok {
		excerptMu.Unlock()
		return e
	}
	excerptMu.Unlock()
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	type turn struct{ who, text string }
	var turns []turn
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if m := speakerTurn.FindStringSubmatch(l); m != nil {
			turns = append(turns, turn{m[1], m[2]})
		} else if l != "" && len(turns) > 0 {
			turns[len(turns)-1].text += " " + l
		}
	}
	q := strings.ToLower(strings.TrimSpace(quote))
	if len(q) > 40 {
		q = q[:40]
	}
	var out *quoteExcerpt
	for i, t := range turns {
		if q != "" && strings.Contains(strings.ToLower(t.text), q) {
			out = &quoteExcerpt{Speaker: t.who, Turn: t.who + ": " + clip(t.text, 600)}
			for j := max(0, i-3); j < i; j++ {
				out.Before = append(out.Before, turns[j].who+": "+clip(turns[j].text, 300))
			}
			break
		}
	}
	excerptMu.Lock()
	excerptMemo[key] = out
	excerptMu.Unlock()
	return out
}

// ownerSuggestion fills Suggest from cached Jev advice (queueing it when
// missing): only a confident pick that differs from the card.
func (s *Server) ownerSuggestion(id string, pl aion.ProposalPayload, src string, v *ownerView, ex *quoteExcerpt) {
	people := s.aionPeopleOptions()
	if len(people) == 0 {
		return
	}
	text, _ := json.Marshal(map[string]any{"title": pl.Title, "kind": pl.Kind, "quote": v.Quote, "excerpt": ex, "people": people})
	hash := jevHash(string(text))
	e, hit := s.jevLookup(jevKindOwner, id, hash)
	if !hit {
		s.jevAdviseAsync(jevKindOwner, id, func() jevCached {
			return s.computeOwnerPick(context.Background(), id, hash, pl, v.Quote, src, ex, people)
		})
		return
	}
	if e.State != jevStateAdvised || e.Owner == nil {
		return
	}
	pick := *e.Owner
	if pick.Choice == "" || pick.Choice == "unclear" || pick.Confidence < ownerSuggestMinConf || strings.EqualFold(pick.Choice, pl.Owner) {
		return
	}
	v.Suggest, v.SuggestConf = pick.Choice, pick.Confidence
	if n, ok := people[pick.Choice]; ok {
		v.SuggestName = strings.SplitN(fmt.Sprint(n), " (", 2)[0]
	}
}

// jevOwnerPick is one cached owner judgment.
type jevOwnerPick struct {
	Choice     string  `json:"choice"`
	Confidence float64 `json:"confidence"`
}

func (s *Server) aionPeopleOptions() map[string]any {
	if s.aion == nil {
		return nil
	}
	out := map[string]any{}
	for _, p := range s.aion.LoadPeople().People() {
		if p.Initials == "" {
			continue
		}
		label := p.Name
		if p.Role != "" {
			label += " (" + p.Role + ")"
		}
		out[p.Initials] = label
	}
	return out
}

func (s *Server) computeOwnerPick(ctx context.Context, id, hash string, pl aion.ProposalPayload, quote, src string, ex *quoteExcerpt, people map[string]any) jevCached {
	e := jevCached{Kind: jevKindOwner, ID: id, Hash: hash, At: time.Now()}
	opts := map[string]any{"unclear": "the excerpt does not make clear who took this on"}
	for k, v := range people {
		opts[k] = v
	}
	ctx, cancel := context.WithTimeout(ctx, jevAutoTimeout)
	defer cancel()
	res, err := s.jevAdvisor().Eval.Evaluate(ctx, typesafe.Request{
		Model: s.jevAdvisor().Model,
		State: map[string]any{"item": map[string]any{"kind": pl.Kind, "title": pl.Title, "quote": quote, "meeting": path.Base(src)}, "excerpt": ex},
		Questions: map[string]typesafe.Question{"owner": typesafe.Choice(map[string]any{
			"task":  "Who on the team OWNS this backlog item — the person who took it on or was asked to do it, per the meeting excerpt (`item`, `excerpt`)?",
			"notes": []string{"The speaker of the quote is often NOT the owner: 'Hannah, can you run X' spoken by Benjamin is owned by Hannah.", "Speaker labels come from automatic diarization and can be wrong or 'Other'.", "Choose unclear rather than guess when nobody clearly takes it on."},
		}, opts)},
	})
	if err != nil {
		return jevErrState(jevKindOwner, id, hash, err)
	}
	a, ok := res.Answers["owner"]
	if !ok || a.Choice == "" {
		return jevErrState(jevKindOwner, id, hash, fmt.Errorf("no answer"))
	}
	e.State, e.Owner = jevStateAdvised, &jevOwnerPick{Choice: a.Choice, Confidence: a.Confidence}
	return e
}
