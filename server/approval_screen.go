package server

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/typesafe"
)

// THE "PROBABLY NOT" SCREEN (owner, 2026-10-06). Half of all decided
// proposals were rejections, and for extracted aion tasks the owner's reasons
// are "duplicate / already tracked" and "not worth tracking". The screen
// folds such cards under a collapsed, counted "Probably not" section in the
// approvals lane — never rejects, never hides: every folded card shows why it
// folded and keeps its full Approve / Reject / edit controls.
//
// Two signals, each measured on the owner's own history before shipping:
//
//   - duplicate (local text, no model): the title closely matches an aion
//     backlog item from a DIFFERENT meeting — open ("already tracked") or done
//     ("already done"). Ratcliff/Obershelp similarity ≥ 0.7, the same measure
//     the backtest used.
//   - worth tracking (Jev, open/internal sources only — the owner's rule for
//     what TypeSafe may read): P(he would track it), with his own 25 latest
//     approvals and 25 latest rejections as examples (refreshed daily; a
//     rejection he marked Duplicate or Already done is not an example of
//     "not worth it"), fundraising meetings held to his stricter bar. Folds
//     below 0.2: on the held-out half of his history that folded 0 of 41
//     approved tasks and caught 9 of 70 rejected.
//
// Held, untiered and non-meeting sources get no Jev judgment (the card is
// simply not screened by it). Without Jev the duplicate signal still runs.

const (
	screenDupThreshold   = 0.70
	screenWorthThreshold = 0.20
	screenExamplesEach   = 25
	jevKindScreen        = "screen"
)

// screenView rides an aion-backlog approval row.
type screenView struct {
	Fold    bool     `json:"fold"`
	Reasons []string `json:"reasons,omitempty"`
	// Worth is P(he would track it), when Jev judged the card.
	Worth *float64 `json:"worth,omitempty"`
	// State of the Jev half: advised | pending | disabled | skipped | error.
	State  string `json:"state"`
	Advice string `json:"advice"` // what still decides: always the owner
}

// approvalScreen is nil for anything but a pending aion-backlog card.
func (s *Server) approvalScreen(p approvals.Proposal, store *approvals.Store) *screenView {
	if p.Type != approvals.TypeAionBacklog || s.aion == nil {
		return nil
	}
	pl, ok := aion.ParsePayloadFence(p.Body, aion.PayloadFence)
	if !ok || strings.TrimSpace(pl.Title) == "" {
		return nil
	}
	v := &screenView{Advice: "you decide; folding never rejects"}
	src := ""
	if len(pl.Sources) > 0 {
		src = strings.TrimSuffix(pl.Sources[0], ".md")
	}
	if why := s.screenDuplicate(pl.Title, src); why != "" {
		v.Fold, v.Reasons = true, append(v.Reasons, why)
	}
	v.State = s.screenWorth(p, pl, src, store, v)
	return v
}

// screenDupMemo remembers each card's verdict until the backlog changes:
// the feed asks on every poll, and comparing one title against ~500 items
// cost ~30 ms a card (0.4 s per feed request, 2026-10-07).
var (
	screenDupMu   sync.Mutex
	screenDupMemo = map[string]string{}
	screenDupRaw  string
)

// screenDuplicate names an aion backlog item from another meeting that this
// title restates, or "".
func (s *Server) screenDuplicate(title, src string) string {
	raw := s.aion.RawFile("backlog.md")
	key := title + "\x00" + src
	screenDupMu.Lock()
	if raw != screenDupRaw {
		screenDupRaw, screenDupMemo = raw, map[string]string{}
	}
	if v, ok := screenDupMemo[key]; ok {
		screenDupMu.Unlock()
		return v
	}
	screenDupMu.Unlock()
	v := s.screenDuplicateCompute(title, src, aion.ParseBacklog(raw))
	screenDupMu.Lock()
	if raw == screenDupRaw {
		screenDupMemo[key] = v
	}
	screenDupMu.Unlock()
	return v
}

func (s *Server) screenDuplicateCompute(title, src string, doc *aion.BacklogDoc) string {
	best, bestItem := 0.0, (*aion.BacklogItem)(nil)
	a := screenNorm(title)
	var ah [256]int
	for i := 0; i < len(a); i++ {
		ah[a[i]]++
	}
	for _, it := range doc.Items() {
		other := ""
		if len(it.Sources) > 0 {
			other = strings.TrimSuffix(it.Sources[0], ".md")
		}
		if src != "" && other == src {
			continue // the same meeting is not a duplicate of itself
		}
		b := screenNorm(it.Text)
		// matched characters can never exceed the characters the two titles
		// share (their histograms' overlap): skip, exactly, what cannot reach
		// the threshold before the expensive comparison
		if sum := len(a) + len(b); sum == 0 || 2*float64(charOverlap(&ah, b))/float64(sum) < screenDupThreshold {
			continue
		}
		if r := ratcliff(a, b); r > best {
			best, bestItem = r, it
		}
	}
	if best < screenDupThreshold || bestItem == nil {
		return ""
	}
	if bestItem.Checked {
		done := ""
		if bestItem.DoneOn != "" {
			done = " on " + bestItem.DoneOn
		}
		return fmt.Sprintf("already done%s: “%s” (%.0f%% match)", done, bestItem.Text, best*100)
	}
	return fmt.Sprintf("already tracked: “%s” (%.0f%% match)", bestItem.Text, best*100)
}

// screenWorth sets Worth (and folds) from cached Jev advice, queueing the
// judgment when missing. Returns the state.
func (s *Server) screenWorth(p approvals.Proposal, pl aion.ProposalPayload, src string, store *approvals.Store, v *screenView) string {
	if !s.jevAutoOn() {
		return jevStateDisabled
	}
	if !s.screenSourceAllowed(src) {
		return "skipped" // held, untiered or not a meeting: TypeSafe never reads it
	}
	examples := s.screenExamples(store)
	text := screenText(pl, src)
	hash := jevHash(text + "\x00" + examples.version)
	e, hit := s.jevLookup(jevKindScreen, p.ID, hash)
	if !hit {
		s.jevAdviseAsync(jevKindScreen, p.ID, func() jevCached {
			return s.computeScreenWorth(context.Background(), p.ID, pl, src, examples, hash)
		})
		return jevStatePending
	}
	if e.State != jevStateAdvised || e.Screen == nil {
		return e.State
	}
	w := *e.Screen
	v.Worth = &w
	if w < screenWorthThreshold {
		v.Fold = true
		v.Reasons = append(v.Reasons, fmt.Sprintf("unlikely you'd track it (%.0f%%, judged against your past decisions)", w*100))
	}
	return jevStateAdvised
}

func (s *Server) screenSourceAllowed(src string) bool {
	if !strings.HasPrefix(src, "log/") {
		return false
	}
	tm := s.aionTierMap()
	if tm == nil {
		var err error
		if tm, err = aion.LoadTierMap(); err != nil {
			return false
		}
	}
	t, ok := tm.SourceTier(src)
	return ok && (t == aion.TierOpen || t == aion.TierInternal)
}

func screenText(pl aion.ProposalPayload, src string) string {
	return strings.Join([]string{pl.Kind, pl.Title, pl.Owner, pl.Rock, path.Base(src)}, "\n")
}

// screenExCache holds the owner's recent decisions as Jev examples, per
// approval store and per day.
var (
	screenExMu    sync.Mutex
	screenExCache = map[*approvals.Store]*screenExampleSet{}
)

// screenExampleSet is the owner's recent decisions, as Jev examples.
type screenExampleSet struct {
	tracked, declined []string
	version           string
}

var rejectedReasonRe = regexp.MustCompile(`(?m)^> rejected: (.+)$`)

// screenExamples: his latest decided aion tasks from open/internal meetings.
// Cached by day so the fold does not reshuffle while he reviews.
func (s *Server) screenExamples(store *approvals.Store) screenExampleSet {
	day := time.Now().UTC().Format("2006-01-02")
	screenExMu.Lock()
	defer screenExMu.Unlock()
	if c := screenExCache[store]; c != nil && c.version == day {
		return *c
	}
	type ex struct {
		at, line string
	}
	collect := func(status string, keep func(approvals.Proposal) bool) []ex {
		var out []ex
		for _, q := range store.List(status) {
			if q.Type != approvals.TypeAionBacklog || !keep(q) {
				continue
			}
			pl, ok := aion.ParsePayloadFence(q.Body, aion.PayloadFence)
			if !ok || pl.Title == "" || len(pl.Sources) == 0 {
				continue
			}
			src := strings.TrimSuffix(pl.Sources[0], ".md")
			if !s.screenSourceAllowed(src) {
				continue
			}
			out = append(out, ex{q.Created, fmt.Sprintf("%s — %s (from “%s”)", pl.Kind, pl.Title, path.Base(src))})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].at > out[j].at })
		if len(out) > screenExamplesEach {
			out = out[:screenExamplesEach]
		}
		return out
	}
	notUnworthy := func(q approvals.Proposal) bool {
		m := rejectedReasonRe.FindStringSubmatch(q.Body)
		if m == nil {
			return true
		}
		r := strings.ToLower(m[1])
		return !strings.HasPrefix(r, "duplicate") && !strings.HasPrefix(r, "already done")
	}
	set := screenExampleSet{version: day}
	for _, e := range collect("approved", func(approvals.Proposal) bool { return true }) {
		set.tracked = append(set.tracked, e.line)
	}
	for _, e := range collect("rejected", notUnworthy) {
		set.declined = append(set.declined, e.line)
	}
	screenExCache[store] = &set
	return set
}

// computeScreenWorth asks Jev once (no cache) and maps every outcome.
func (s *Server) computeScreenWorth(ctx context.Context, id string, pl aion.ProposalPayload, src string, ex screenExampleSet, hash string) jevCached {
	e := jevCached{Kind: jevKindScreen, ID: id, Hash: hash, At: time.Now()}
	ctx, cancel := context.WithTimeout(ctx, jevAutoTimeout)
	defer cancel()
	q := typesafe.Noul(map[string]any{
		"task":              "A startup founder reviews items an assistant extracted from a team meeting before they enter the company's operational backlog (`item`, from the meeting `meeting`). Judge whether the founder would want this item TRACKED in the backlog.",
		"track_when":        "a concrete commitment or decision someone will act on, with a clear deliverable, that matters to the company's goals (`item.rock` names the goal it was tethered to)",
		"do_not_track_when": []string{"a passing remark, idea or aspiration nobody took on", "routine or trivial", "too vague to act on", "a status observation rather than work to do"},
		"this_founders_past_decisions": map[string]any{"tracked": ex.tracked, "declined": ex.declined,
			"how_to_use": "These are this founder's own earlier decisions on similar items. Match his bar, not a generic one."},
		"fundraising_meetings": "From investor, fundraising or pitch conversations he tracks ONLY explicit commitments with an owner (a promised follow-up, a deliverable to an investor); investor feedback, advice, opinions and pitch ideas are not tracked.",
	}, "he would track it", "he would not track it")
	res, err := s.jevAdvisor().Eval.Evaluate(ctx, typesafe.Request{
		Model:     s.jevAdvisor().Model,
		State:     map[string]any{"item": map[string]any{"kind": pl.Kind, "title": pl.Title, "owner": pl.Owner, "rock": pl.Rock, "due": pl.Due}, "meeting": path.Base(src)},
		Questions: map[string]typesafe.Question{"worth": q},
	})
	if err != nil {
		return jevErrState(jevKindScreen, id, hash, err)
	}
	a, ok := res.Answers["worth"]
	if !ok || a.Noul == nil {
		return jevErrState(jevKindScreen, id, hash, fmt.Errorf("no answer"))
	}
	w := *a.Noul
	e.State, e.Screen = jevStateAdvised, &w
	return e
}

var screenNonWord = regexp.MustCompile(`[^a-z0-9 ]+`)

func screenNorm(s string) string {
	return strings.Join(strings.Fields(screenNonWord.ReplaceAllString(strings.ToLower(s), " ")), " ")
}

// charOverlap counts the characters b shares with the histogram ah — an
// upper bound on the characters Ratcliff/Obershelp can match.
func charOverlap(ah *[256]int, b string) int {
	var bh [256]int
	n := 0
	for i := 0; i < len(b); i++ {
		c := b[i]
		if bh[c] < ah[c] {
			n++
		}
		bh[c]++
	}
	return n
}

// ratcliff is the Ratcliff/Obershelp similarity (Python difflib's ratio):
// twice the matched characters over the total length.
func ratcliff(a, b string) float64 {
	if len(a)+len(b) == 0 {
		return 1
	}
	return 2 * float64(rcMatches(a, b)) / float64(len(a)+len(b))
}

func rcMatches(a, b string) int {
	if a == "" || b == "" {
		return 0
	}
	// longest common substring
	best, ai, bi := 0, 0, 0
	prev := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
				if cur[j] > best {
					best, ai, bi = cur[j], i-cur[j], j-cur[j]
				}
			}
		}
		prev = cur
	}
	if best == 0 {
		return 0
	}
	return best + rcMatches(a[:ai], b[:bi]) + rcMatches(a[ai+best:], b[bi+best:])
}
