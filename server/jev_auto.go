package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/jev"
	"manifest/mdfm"
)

// Jev, wired into the decisions that already exist (owner ask 2026-10-03:
// "the defaults … set automatically, no change in the user interface").
//
//   - Transcript visibility card: an unmapped aion transcript's card default
//     is Jev's tier advice after jev.AdviseTier's conservative policy, instead
//     of a flat held. The tier map stays the one authority: nothing is written
//     until the owner confirms the card, through the same
//     aionRecordVisibility → aion.WriteTierMapEntry path as before.
//   - Approval cards (manifest operations): Jev's risk category rides the row
//     as advisory metadata (jevRisk); the existing gate still decides.
//   - Alfred's task tiers: the clarify gate's judgment is appended to the
//     work order as an advisory line; the agent turn still picks the tier.
//   - Coding-run results: an evidence + goal-state sidecar (jev-advice.json)
//     beside result.json; the run's outcome stays the result.json status.
//
// Without a key (or before the advice store is wired) every path behaves
// exactly as it did before: held default, no risk row, no advisory line, no
// sidecar, no network call. Advice is computed off the request path (sweep
// + one background worker per key) and cached by content hash, so a card list
// never waits on Jev.

// UseJevAdvice wires the advice store (<dataDir>/jev). Without it (and without
// an injected judge) automatic advice is off.
func (s *Server) UseJevAdvice(dir string) { s.jevAdviceDir = dir }

// jevAutoConfigured: automatic advice is wired at all (store or test judge).
func (s *Server) jevAutoConfigured() bool { return s.jevJudge != nil || s.jevAdviceDir != "" }

// jevAutoOn: wired AND a key (or an injected judge) is available right now.
func (s *Server) jevAutoOn() bool { return s.jevAutoConfigured() && s.jevEnabled() }

// jevAutoOffReason names why automatic advice is not running.
func (s *Server) jevAutoOffReason() string {
	if !s.jevAutoConfigured() {
		return "Jev advice is not wired on this server"
	}
	return "TYPESAFE_API_KEY is not set (Settings › Portals › TypeSafe) — Jev was not asked"
}

// Advice states.
const (
	jevStateAdvised  = "advised"  // Jev answered; the default is its policy tier/category
	jevStatePending  = "pending"  // queued; the conservative default stands meanwhile
	jevStateDisabled = "disabled" // no key / not wired; nothing was sent
	jevStateOversize = "oversize" // refused before sending; manual review
	jevStateEmpty    = "empty"    // nothing to judge
	jevStateError    = "error"    // Jev failed or timed out; retried later
)

// jevErrorRetry: a failed judgment is retried after this long.
const jevErrorRetry = 10 * time.Minute

// jevAutoTimeout bounds one background judgment.
const jevAutoTimeout = jevTimeout

// jevCached is one stored judgment: which object, over which content, and
// the answer (no source text is stored).
type jevCached struct {
	Kind   string            `json:"kind"`
	ID     string            `json:"id"`
	Hash   string            `json:"hash"`
	State  string            `json:"state"`
	Reason string            `json:"reason,omitempty"`
	At     time.Time         `json:"at"`
	Tier   *jev.TierAdvice   `json:"tier,omitempty"`
	Risk   *jev.ApprovalRisk `json:"risk,omitempty"`
	Screen *float64          `json:"screen,omitempty"` // P(he would track it) — approval_screen.go
	Owner  *jevOwnerPick     `json:"owner,omitempty"`  // approval_owner.go
}

type jevAdviceCache struct {
	mu       sync.Mutex
	mem      map[string]jevCached
	inflight map[string]bool
}

func (s *Server) jevCache() *jevAdviceCache {
	s.jevCacheOnce.Do(func() {
		s.jevAdvice = &jevAdviceCache{mem: map[string]jevCached{}, inflight: map[string]bool{}}
	})
	return s.jevAdvice
}

func jevHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (s *Server) jevCacheFile(kind, id string) string {
	if s.jevAdviceDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(s.jevAdviceDir, kind, hex.EncodeToString(sum[:12])+".json")
}

// jevLookup returns the stored judgment for (kind, id) over content hash.
// A stale hash (the content changed) or an expired error is a miss.
func (s *Server) jevLookup(kind, id, hash string) (jevCached, bool) {
	c := s.jevCache()
	c.mu.Lock()
	e, ok := c.mem[kind+"/"+id]
	c.mu.Unlock()
	if !ok {
		if f := s.jevCacheFile(kind, id); f != "" {
			if raw, err := os.ReadFile(f); err == nil && json.Unmarshal(raw, &e) == nil && e.ID == id {
				ok = true
				c.mu.Lock()
				c.mem[kind+"/"+id] = e
				c.mu.Unlock()
			}
		}
	}
	if !ok || e.Hash != hash {
		return jevCached{}, false
	}
	if e.State == jevStateError && time.Since(e.At) > jevErrorRetry {
		return jevCached{}, false
	}
	return e, true
}

func (s *Server) jevStore(e jevCached) {
	c := s.jevCache()
	c.mu.Lock()
	c.mem[e.Kind+"/"+e.ID] = e
	c.mu.Unlock()
	if e.State == jevStateError { // memory only: a restart retries
		return
	}
	f := s.jevCacheFile(e.Kind, e.ID)
	if f == "" {
		return
	}
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		log.Printf("jev advice: %v", err)
		return
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err == nil {
		_ = os.Rename(tmp, f)
	}
}

// jevAdviseAsync runs compute once per key in the background (deduped).
func (s *Server) jevAdviseAsync(kind, id string, compute func() jevCached) {
	c := s.jevCache()
	key := kind + "/" + id
	c.mu.Lock()
	if c.inflight[key] {
		c.mu.Unlock()
		return
	}
	c.inflight[key] = true
	c.mu.Unlock()
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.inflight, key)
			c.mu.Unlock()
		}()
		s.jevStore(compute())
	}()
}

// jevErrState is the error entry for a failed judgment (the key never appears
// in a TypeSafe error; the message is the API's or the transport's).
func jevErrState(kind, id, hash string, err error) jevCached {
	return jevCached{Kind: kind, ID: id, Hash: hash, State: jevStateError, Reason: "Jev failed: " + err.Error(), At: time.Now()}
}

// --------------------------------------------------- transcript tier default

const jevKindTier = "tier"

// jevTierView is the machine-readable audit of a card's default: what Jev
// said, what policy made of it, and that the map — not this — is the tier.
type jevTierView struct {
	State         string          `json:"state"`
	Reason        string          `json:"reason,omitempty"`
	Advisory      bool            `json:"advisory"`
	Applied       bool            `json:"applied"` // always false: the map changes only on confirm
	SourceOfTruth string          `json:"sourceOfTruth"`
	Tier          aion.Tier       `json:"tier"` // the default the card shows
	Advice        *jev.TierAdvice `json:"advice,omitempty"`
}

// tierAdviceText is the text Jev judges for a proposed transcript note: the
// note body without its frontmatter (connector ids carry no meaning).
func tierAdviceText(p approvals.Proposal) string {
	_, body := mdfm.Split(p.Proposed)
	return strings.TrimSpace(body)
}

// jevTierComputable: the proposal is one the tier advice is for — a synced
// transcript the converter already tagged aion. A note the owner tags aion
// only on the card is never sent out; it keeps the held default.
func jevTierComputable(p approvals.Proposal) (src string, ok bool) {
	if p.Type != approvals.TypeCreateVaultNote {
		return "", false
	}
	fm, _ := mdfm.Split(p.Proposed)
	src = transcriptSource(fm)
	return src, src != "" && noteHasAionCategory(p.Proposed)
}

// computeTierAdvice asks Jev (no cache) and maps every outcome to an entry.
func (s *Server) computeTierAdvice(ctx context.Context, p approvals.Proposal, src, name, text string) jevCached {
	hash := jevHash(text)
	e := jevCached{Kind: jevKindTier, ID: p.ID, Hash: hash, At: time.Now()}
	switch {
	case text == "":
		e.State, e.Reason = jevStateEmpty, "the note has no body to judge — held"
		return e
	case len(text) > jev.MaxTierText:
		e.State = jevStateOversize
		e.Reason = fmt.Sprintf("transcript is %d bytes, over Jev's %d-byte limit — not sent (never truncated); held for manual review", len(text), jev.MaxTierText)
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, jevAutoTimeout)
	defer cancel()
	adv, err := s.jevAdvisor().AdviseTier(ctx, jev.TierInput{Name: name, Source: src, Text: text})
	if err != nil {
		return jevErrState(jevKindTier, p.ID, hash, err)
	}
	e.State, e.Tier = jevStateAdvised, adv
	return e
}

// jevTierDefault returns the card's Jev view for an UNMAPPED transcript and
// the default tier it implies. A cache miss queues the judgment and answers
// pending/held now. Never blocks on Jev; never writes the map.
func (s *Server) jevTierDefault(p approvals.Proposal, name string) *jevTierView {
	v := &jevTierView{Advisory: true, SourceOfTruth: aion.TierMapPath, Tier: aion.TierHeld}
	src, ok := jevTierComputable(p)
	if !ok {
		return nil
	}
	if !s.jevAutoOn() {
		v.State, v.Reason = jevStateDisabled, s.jevAutoOffReason()+"; held default"
		return v
	}
	text := tierAdviceText(p)
	e, hit := s.jevLookup(jevKindTier, p.ID, jevHash(text))
	if !hit {
		s.jevAdviseAsync(jevKindTier, p.ID, func() jevCached {
			return s.computeTierAdvice(context.Background(), p, src, name, text)
		})
		v.State, v.Reason = jevStatePending, "Jev has not answered yet; held meanwhile"
		return v
	}
	return tierViewOf(e)
}

func tierViewOf(e jevCached) *jevTierView {
	v := &jevTierView{State: e.State, Reason: e.Reason, Advisory: true, SourceOfTruth: aion.TierMapPath, Tier: aion.TierHeld}
	if e.State == jevStateAdvised && e.Tier != nil {
		v.Advice = e.Tier
		switch e.Tier.Tier {
		case aion.TierOpen, aion.TierInternal, aion.TierHeld:
			v.Tier = e.Tier.Tier
		default:
			v.Reason = "unrecognised advice — held"
		}
	}
	return v
}

// ------------------------------------------------------- approval-risk rows

const jevKindRisk = "risk"

// jevRiskView rides a manifest-operation approval row as advisory metadata.
type jevRiskView struct {
	State         string            `json:"state"`
	Reason        string            `json:"reason,omitempty"`
	Advisory      bool              `json:"advisory"`
	Authoritative string            `json:"authoritative"` // what still decides
	Category      string            `json:"category,omitempty"`
	Result        *jev.ApprovalRisk `json:"result,omitempty"`
}

func approvalRiskText(p approvals.Proposal) string {
	return strings.TrimSpace(strings.TrimSpace(p.Action) + "\n\n" + strings.TrimSpace(p.Body))
}

func (s *Server) computeApprovalRisk(ctx context.Context, p approvals.Proposal, text string) jevCached {
	hash := jevHash(text)
	e := jevCached{Kind: jevKindRisk, ID: p.ID, Hash: hash, At: time.Now()}
	if text == "" {
		e.State, e.Reason = jevStateEmpty, "nothing to judge"
		return e
	}
	if len(text) > jev.MaxAgentInput {
		e.State, e.Reason = jevStateOversize, fmt.Sprintf("action is %d bytes, over %d — not sent", len(text), jev.MaxAgentInput)
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, jevAutoTimeout)
	defer cancel()
	r, err := s.jevAdvisor().ApprovalRisk(ctx, jev.ApprovalInput{Action: text, Context: "A pending approval card in the owner's approvals inbox (type " + p.Type + ")."})
	if err != nil {
		return jevErrState(jevKindRisk, p.ID, hash, err)
	}
	e.State, e.Risk = jevStateAdvised, r
	return e
}

// jevApprovalRisk annotates a pending manifest-operation row; nil when off
// (the row is then exactly what it was before).
func (s *Server) jevApprovalRisk(p approvals.Proposal) *jevRiskView {
	if p.Type != approvals.TypeManifestOperation || !s.jevAutoOn() {
		return nil
	}
	v := &jevRiskView{Advisory: true, Authoritative: "the existing approval gate"}
	text := approvalRiskText(p)
	e, hit := s.jevLookup(jevKindRisk, p.ID, jevHash(text))
	if !hit {
		s.jevAdviseAsync(jevKindRisk, p.ID, func() jevCached { return s.computeApprovalRisk(context.Background(), p, text) })
		v.State = jevStatePending
		return v
	}
	v.State, v.Reason = e.State, e.Reason
	if e.Risk != nil {
		v.Result, v.Category = e.Risk, e.Risk.Category
	}
	return v
}

// ------------------------------------------------------------------- sweep

// jevAutoSweep computes missing advice for every pending card that takes it,
// so the default is already set when the owner opens the card. Sequential;
// one call at a time. Map-known transcripts are skipped (the map answers).
func (s *Server) jevAutoSweep(ctx context.Context) {
	if !s.jevAutoOn() {
		return
	}
	tm := s.aionTierMap()
	if tm == nil {
		tm, _ = aion.LoadTierMap()
	}
	for _, h := range s.eachHarness() {
		if h.Approvals == nil {
			continue
		}
		for _, p := range h.Approvals.List("pending") {
			if ctx.Err() != nil {
				return
			}
			switch p.Type {
			case approvals.TypeCreateVaultNote:
				src, ok := jevTierComputable(p)
				if !ok {
					continue
				}
				name := tierMapNoteName(p.ApplyPath)
				if tm != nil {
					if _, known := tm.Tier(name); known {
						continue
					}
				}
				text := tierAdviceText(p)
				if _, hit := s.jevLookup(jevKindTier, p.ID, jevHash(text)); !hit {
					s.jevStore(s.computeTierAdvice(ctx, p, src, name, text))
				}
			case approvals.TypeAionBacklog:
				s.approvalScreen(p, h.Approvals) // queues the Jev half when missing
				s.approvalOwner(p)
			case approvals.TypeManifestOperation:
				text := approvalRiskText(p)
				if _, hit := s.jevLookup(jevKindRisk, p.ID, jevHash(text)); !hit {
					s.jevStore(s.computeApprovalRisk(ctx, p, text))
				}
			}
		}
	}
}

// jevAutoSweepAsync runs one sweep at a time off the ticker.
func (s *Server) jevAutoSweepAsync() {
	if !s.jevAutoOn() || !s.jevSweeping.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.jevSweeping.Store(false)
		s.jevAutoSweep(context.Background())
	}()
}

// -------------------------------------------------------------- clarify gate

// jevClarifyTimeout bounds the clarify call on the work-order path (it sits
// in front of a spool; a slow Jev must not hold the owner's comment up).
const jevClarifyTimeout = 8 * time.Second

// jevClarifyTierHint maps a gate decision onto Alfred's existing tiers.
var jevClarifyTierHint = map[string]string{
	"act_now":              "leans Tier 1 — if the ask is low-stakes, act now and state the assumption you made",
	"ask_user":             "leans Tier 2 with a leading # questions — ask the one question that separates the readings",
	"inspect_source_first": "look the missing detail up first in files, records or tools you can actually read; ask only if that lookup is unavailable or comes back empty",
	"stop_due_to_risk":     "leans Tier 2 PLAN-GATE — do not execute; state the risk and wait for explicit authorization",
}

// clarifyAdvisoryLine renders a gate result as one work-order line ("" for
// an unknown decision).
func clarifyAdvisoryLine(g *jev.ClarifyGate) string {
	hint, ok := jevClarifyTierHint[g.Decision]
	if !ok {
		return ""
	}
	line := fmt.Sprintf("JEV ADVISORY (clarify gate — advisory only; the tiers and authorization gates above still decide): %s (confidence %.2f, risk %.1f/4) — %s.",
		g.Decision, g.Confidence, g.Risk.Score, hint)
	if len(g.Cautions) > 0 {
		line += " Cautions: " + strings.Join(g.Cautions, "; ") + "."
	}
	return line + "\n"
}

// jevClarifyLine asks the clarify gate about an owner comment on the Alfred
// tier path. "" whenever Jev is off, slow, or failing — the work order is
// then byte-for-byte what it was before.
func (s *Server) jevClarifyLine(request, context_ string) string {
	request = strings.TrimSpace(request)
	if request == "" || !s.jevAutoOn() {
		return ""
	}
	if budget := jev.MaxAgentInput - len(request) - 64; len(context_) > budget {
		if budget <= 0 {
			return ""
		}
		context_ = context_[len(context_)-budget:] // keep the newest dialog
	}
	ctx, cancel := context.WithTimeout(context.Background(), jevClarifyTimeout)
	defer cancel()
	g, err := s.jevAdvisor().ClarifyGate(ctx, jev.ClarifyInput{Request: request, Context: context_})
	if err != nil {
		log.Printf("jev clarify gate: %v (work order sent without advice)", err)
		return ""
	}
	return clarifyAdvisoryLine(g)
}

// ------------------------------------------------------ coding-run evidence

// jevRunAdviceFile sits beside result.json; the run's outcome stays result.json's.
const jevRunAdviceFile = "jev-advice.json"

type jevRunAdvice struct {
	Advisory      bool               `json:"advisory"`
	Authoritative string             `json:"authoritative"`
	Hash          string             `json:"hash"`
	Status        string             `json:"status"` // result.json's own status, verbatim
	At            time.Time          `json:"at"`
	Evidence      *jev.EvidenceCheck `json:"evidence,omitempty"`
	Goal          *jev.GoalState     `json:"goal,omitempty"`
	Errors        []string           `json:"errors,omitempty"`
}

// jevRunAdvise writes the evidence/goal sidecar for one ingested coding
// result. Synchronous; callers run it in the background.
func (s *Server) jevRunAdvise(ctx context.Context, dir, task string, result codingResult) {
	if !s.jevAutoOn() {
		return
	}
	hash := jevHash(result.Status + "\n" + result.Summary + "\n" + result.ArtifactURL)
	path := filepath.Join(dir, jevRunAdviceFile)
	var prev jevRunAdvice
	if raw, err := os.ReadFile(path); err == nil && json.Unmarshal(raw, &prev) == nil && prev.Hash == hash {
		return
	}
	out := jevRunAdvice{Advisory: true, Authoritative: "result.json status", Hash: hash, Status: result.Status, At: time.Now()}
	evidence := "status: " + result.Status
	if result.ArtifactURL != "" {
		evidence += "\nartifact: " + result.ArtifactURL
	} else {
		evidence += "\nartifact: (none)"
	}
	ctx, cancel := context.WithTimeout(ctx, jevAutoTimeout)
	defer cancel()
	j := s.jevAdvisor()
	if ev, err := j.EvidenceCheck(ctx, jev.EvidenceInput{Claim: result.Summary, Evidence: evidence, Context: "TASK: " + task}); err == nil {
		out.Evidence = ev
	} else {
		out.Errors = append(out.Errors, "evidence: "+err.Error())
	}
	if g, err := j.GoalState(ctx, jev.GoalInput{Goal: task, Status: result.Summary, Context: evidence}); err == nil {
		out.Goal = g
	} else {
		out.Errors = append(out.Errors, "goal: "+err.Error())
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	_ = boardWrite(path, raw)
}
