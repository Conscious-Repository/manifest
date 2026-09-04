package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"manifest/feed"
	"manifest/hermes"
	"manifest/ledger"
	"manifest/spirits"
)

// "dig →" on the do-bot's own cards. Alfred is the owner's Hermes agent on
// this box — the daily domain scout since the 2026-08-24 takeover (the
// aion-domain-scout skill in ~/.hermes/skills). His cards carry
// `agent: alfred` and there is no excalibur spirit behind them (domain-scout's
// rituals are paused), so a dig cannot be spooled as a ritual run-now; it goes
// to the same agent that wrote the card, over the runner the @hermes
// delegation lane already uses. The deliverable keeps the retired
// domain-scout/targeted contract so the loop still closes in the feed: ONE
// brief in artifacts/library/ and ONE artifact card pointing at it.
//
// Scope: the turn runs with the runner's DEFAULT toolsets, not the read-only
// plan/comment scope. artifacts/feed and artifacts/library in the harness tree
// are Alfred's established write surface — his daily cron writes there with the
// same toolsets — and a feed card is a proposal the owner verdicts, not a world
// change. The vault stays closed to him by his skill's own rule.

// alfredAgent is the do-bot's name on the feed (SOUL.md; the skill brands its
// cards with it).
const alfredAgent = "alfred"

// digSkills mirrors the daily cron job's preload, so a dig is the same scan
// aimed at one item rather than a bare model turn. The runner names them at
// the top of the prompt (hermes -z has no working --skills flag) and Hermes
// loads them on demand.
const digSkills = "aion-domain-scout,aion-biosciences"

// hermesDigAgent reports whether a feed card's agent is the do-bot — its dig
// runs a Hermes turn rather than a spirit ritual. Only while the runner is
// wired; otherwise the card falls through to the ritual path's honest 422.
func (s *Server) hermesDigAgent(agent string) bool {
	if s.hermes == nil {
		return false
	}
	return strings.EqualFold(agent, alfredAgent) || strings.EqualFold(agent, "hermes")
}

// startHermesDig kicks off the turn in the background. It coalesces per card:
// a second dig while one is in flight returns spirits.ErrAlreadyActive so the
// handler's 409 branch is shared with the ritual path.
func (s *Server) startHermesDig(h Harness, it feed.Item) error {
	s.hermes.mu.Lock()
	if s.hermes.digging[it.ID] {
		s.hermes.mu.Unlock()
		return spirits.ErrAlreadyActive
	}
	s.hermes.digging[it.ID] = true
	s.hermes.mu.Unlock()
	go s.runHermesDig(h, it)
	return nil
}

// runHermesDig invokes the CLI and ledgers the outcome. The card count is
// VERIFIED against the feed dir rather than taken from the reply — narrating a
// write without making it is the exact failure that retired the old scout.
func (s *Server) runHermesDig(h Harness, it feed.Item) {
	defer func() {
		s.hermes.mu.Lock()
		delete(s.hermes.digging, it.ID)
		s.hermes.mu.Unlock()
	}()
	before := feedIDs(h)
	actor := agentIdentity(it.Agent).ID
	meta := map[string]any{"feed": it.ID}
	obj := ledger.Object{Kind: ledger.ObjFeed, ID: it.ID} // the dig is about the card
	// Each dig is a fresh Hermes session; the prompt carries the card in full,
	// so a re-dig has everything it needs without a resume.
	res, err := s.hermes.runner.Run(context.Background(), hermes.Request{
		Prompt: hermesDigPrompt(h.Spirits.Root(), it),
		Skills: digSkills,
	})
	if err != nil {
		log.Printf("feed dig %s (%s): %v", it.ID, it.Agent, err)
		s.ledger(ledger.Entry{Source: "run", Kind: "run.failed", Actor: actor, Object: obj, Harness: "hermes",
			Text: "dig on " + ledger.Snip(it.Title, 120) + " — " + err.Error(), Meta: meta})
		return
	}
	written := 0
	for _, n := range h.Spirits.Feed.List(feed.Filter{}, time.Now()) {
		if !before[n.ID] && strings.EqualFold(n.Agent, it.Agent) {
			written++
		}
	}
	if written == 0 {
		log.Printf("feed dig %s (%s): the turn finished but no card landed in the feed — reply: %s", it.ID, it.Agent, ledger.Snip(res.Reply, 200))
	}
	meta["itemsWritten"], meta["spentUsd"], meta["model"], meta["sessionId"] = written, res.SpentUSD, res.Model, res.SessionID
	s.ledger(ledger.Entry{Source: "run", Kind: "run.completed", Actor: actor, Object: obj, Harness: "hermes",
		Text: fmt.Sprintf("dig on %s → %d card(s): %s", ledger.Snip(it.Title, 120), written, ledger.Snip(res.Reply, 280)), Meta: meta})
}

// feedIDs snapshots the harness feed (every non-discarded card) so the dig's
// yield can be counted honestly afterwards.
func feedIDs(h Harness) map[string]bool {
	out := map[string]bool{}
	for _, n := range h.Spirits.Feed.List(feed.Filter{}, time.Now()) {
		out[n.ID] = true
	}
	return out
}

// hermesDigPrompt composes the work order: the card in full (the agent's only
// unambiguous handle on what to dig), then the targeted-brief method and the
// two-file deliverable with absolute paths into the card's own harness tree.
func hermesDigPrompt(root string, it feed.Item) string {
	var b strings.Builder
	b.WriteString("DIG (the owner pressed \"dig →\" on one of your cards in the manifest feed). Go deeper on this item you scouted:\n\n")
	fmt.Fprintf(&b, "  id: %s\n  type: %s\n  title: %s\n", it.ID, it.Type, it.Title)
	if it.Why != "" {
		fmt.Fprintf(&b, "  why: %s\n", it.Why)
	}
	if it.Link != "" {
		fmt.Fprintf(&b, "  link: %s\n", it.Link)
	}
	if it.Source != "" {
		fmt.Fprintf(&b, "  source: %s\n", it.Source)
	}
	if it.Domain != "" {
		fmt.Fprintf(&b, "  domain: %s\n", it.Domain)
	}
	if len(it.Tags) > 0 {
		fmt.Fprintf(&b, "  tags: [%s]\n", strings.Join(it.Tags, ", "))
	}
	if body := strings.TrimSpace(it.Body); body != "" {
		fmt.Fprintf(&b, "\n  %s\n", strings.ReplaceAll(body, "\n", "\n  "))
	}
	fmt.Fprintf(&b, `
Work it like a targeted brief, not a daily scan: 3–6 searches from genuinely
different angles until the picture stops changing — the primary literature AND
at least one X angle (site:x.com …), since labs and founders say it there
first. Ground relevance in AION's actual thesis (aion-biosciences). Never
invent URLs; cite only pages a search returned this run.

Deliver exactly TWO files into the harness tree (never the vault):

1. ONE brief at %[1]s/artifacts/library/<slug>.md with the sections
   Context (the card and the question, restated so the brief stands alone),
   Findings (most load-bearing first, source link inline on each),
   Sources, and So what (one line: what it means for Benjamin's decision).
2. ONE feed card at %[1]s/artifacts/feed/<slug>-<shorthash>.md in your usual
   frontmatter shape — type: artifact, agent: %[2]s, profile: targeted,
   link: artifacts/library/<slug>.md (the brief; no external URL on this
   card), title = the question in a few words, why = the So-what line, body =
   a two-to-three sentence digest, tags may reuse the original's, status: new,
   date: RFC3339 UTC now, confidence: your call.

No second brief, no extra cards, no padding. If the dig turns up nothing beyond
the original, the brief says so and the card still lands — that is how the
loop closes in the feed. Do not edit the original card. Verify both files
exist before you answer, and never claim a write you did not make.
Reply with ONE line: the So-what and the card's filename.
`, root, it.Agent)
	return b.String()
}
