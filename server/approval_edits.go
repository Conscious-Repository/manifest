package server

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"manifest/aion"
	"manifest/approvals"
)

// YOUR EDITS ARE THE LABELS (owner, 2026-10-06): a wrong owner or goal is
// never a rejection reason — "i would only ever edit this, so if i do, it
// should track that and learn off my behavior". Every pre-approval edit that
// changes a task's owner, goal (rock), kind or title is recorded here with
// the meeting it came from, so suggestions can learn which owner and goal you
// actually pick for what was said. DataDir only, never the vault.

// approvalEdit is one changed field on one card.
type approvalEdit struct {
	At      string `json:"at"`
	ID      string `json:"id"`
	Type    string `json:"type"`
	Meeting string `json:"meeting,omitempty"`
	Title   string `json:"title"`
	Field   string `json:"field"` // owner | rock | kind | title
	From    string `json:"from"`
	To      string `json:"to"`
}

var approvalEditsMu sync.Mutex

func (s *Server) approvalEditsPath() string {
	if s.jevAdviceDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.jevAdviceDir), "approval-learning", "edits.jsonl")
}

// recordApprovalEdits appends what changed between the card as it was and
// the payload you saved. Best effort: the edit itself never fails on it.
func (s *Server) recordApprovalEdits(before approvals.Proposal, after aion.ProposalPayload) {
	p := s.approvalEditsPath()
	if p == "" {
		return
	}
	fence := aion.PayloadFence
	if before.Type == approvals.TypeReBacklog || before.Type == approvals.TypeReResolve {
		fence = aion.REPayloadFence
	}
	prev, ok := aion.ParsePayloadFence(before.Body, fence)
	if !ok {
		return
	}
	meeting := ""
	if len(prev.Sources) > 0 {
		meeting = path.Base(strings.TrimSuffix(prev.Sources[0], ".md"))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var lines []string
	for _, f := range []struct{ name, from, to string }{
		{"owner", prev.Owner, after.Owner}, {"rock", prev.Rock, after.Rock},
		{"kind", prev.Kind, after.Kind}, {"title", prev.Title, after.Title},
	} {
		if strings.TrimSpace(f.from) == strings.TrimSpace(f.to) {
			continue
		}
		b, err := json.Marshal(approvalEdit{At: now, ID: before.ID, Type: before.Type, Meeting: meeting, Title: prev.Title, Field: f.name, From: f.from, To: f.to})
		if err == nil {
			lines = append(lines, string(b))
		}
	}
	if len(lines) == 0 {
		return
	}
	approvalEditsMu.Lock()
	defer approvalEditsMu.Unlock()
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(strings.Join(lines, "\n") + "\n")
}

// approvalEdits reads every recorded edit (oldest first).
func (s *Server) approvalEdits() []approvalEdit {
	p := s.approvalEditsPath()
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var out []approvalEdit
	for _, l := range strings.Split(string(b), "\n") {
		var e approvalEdit
		if json.Unmarshal([]byte(l), &e) == nil && e.Field != "" {
			out = append(out, e)
		}
	}
	return out
}

// editHint is a correction you have made before, offered again: never
// applied by itself — a one-tap chip on the card.
type editHint struct {
	Field string `json:"field"` // owner | rock
	To    string `json:"to"`
	Count int    `json:"count"` // how many times you made this change
	Of    int    `json:"of"`    // out of how many edits of this field from this value
	Scope string `json:"scope"` // the meeting series, or "any meeting"
}

var seriesDateRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s*(-\s*)?)+`)

// meetingSeries strips the leading date(s): "2026-09-11 standing waves" →
// "standing waves", so a recurring meeting is one series.
func meetingSeries(meeting string) string {
	return strings.TrimSpace(seriesDateRe.ReplaceAllString(strings.ToLower(meeting), ""))
}

// approvalEditHints proposes the owner/goal you have consistently changed
// this card's value to: within the same meeting series (twice, at least two
// thirds of the time), else across meetings (three times, three quarters).
func (s *Server) approvalEditHints(p approvals.Proposal) []editHint {
	if p.Type != approvals.TypeAionBacklog && p.Type != approvals.TypeReBacklog {
		return nil
	}
	fence := aion.PayloadFence
	if p.Type == approvals.TypeReBacklog {
		fence = aion.REPayloadFence
	}
	pl, ok := aion.ParsePayloadFence(p.Body, fence)
	if !ok {
		return nil
	}
	edits := s.approvalEdits()
	if len(edits) == 0 {
		return nil
	}
	series := ""
	if len(pl.Sources) > 0 {
		series = meetingSeries(path.Base(strings.TrimSuffix(pl.Sources[0], ".md")))
	}
	var out []editHint
	for _, f := range []struct{ name, cur string }{{"owner", pl.Owner}, {"rock", pl.Rock}} {
		pick := func(sameSeries bool, minCount int, minShare float64, scope string) bool {
			to := map[string]int{}
			n := 0
			for _, e := range edits {
				if e.Field != f.name || e.Type != p.Type || !strings.EqualFold(strings.TrimSpace(e.From), strings.TrimSpace(f.cur)) {
					continue
				}
				if sameSeries && (series == "" || meetingSeries(e.Meeting) != series) {
					continue
				}
				to[e.To]++
				n++
			}
			best, bestN := "", 0
			for v, c := range to {
				if c > bestN || (c == bestN && v < best) {
					best, bestN = v, c
				}
			}
			if bestN >= minCount && float64(bestN) >= minShare*float64(n) && !strings.EqualFold(best, f.cur) {
				out = append(out, editHint{Field: f.name, To: best, Count: bestN, Of: n, Scope: scope})
				return true
			}
			return false
		}
		if !pick(true, 2, 2.0/3, series) {
			pick(false, 3, 0.75, "any meeting")
		}
	}
	return out
}
