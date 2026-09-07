package server

import (
	"strings"
	"time"
	"unicode"
)

// Classification belongs to the existing agent turn, not a keyword router or
// a second planner. The result tag lets ingestion preserve plan completeness.
const alfredTierProtocol = `ALFRED TASK TIERS (takes precedence over persona reply-shape defaults):
Lean aggressive: get as far as you can without Benjamin.
Tier 1 — low-ambiguity, low-stakes task asks: AUTO-EXECUTE now in this turn.
Do not propose a plan or wait for a fire. For example, "look up this candidate's
publications and attach a short brief" means perform the lookup and deliver it.
Use the existing manifest operation prepare/execute tools: standing_authorization
operations may execute immediately; human_approval operations retain their existing
gate and byte-exact preview. Never self-approve or claim pending effects happened.
Return [tier:: executed] on its own first line, then a result summary of at most
3 short sentences and 280 words, with an artifact/source link. Save long findings
as an artifact and link it. Do not merely promise to do the work.
Tier 2 — consequential or ambiguous work: PLAN-GATE. Get as far as authorized,
then return [tier:: plan] followed by the COMPLETE, concrete, unbounded plan
(or a leading # questions when an answer is essential). Do not execute consequential
changes without the existing authorization. The comment brevity cap does NOT apply
to this plan deliverable. Explicit plan/fire requests keep their normal phase.
Tier 3 — pure inquiries: answer only, prefixed [tier:: answer]. Do not manufacture
a task, plan, or operation. Keep the existing task-comment brevity.
PROTOCOL: reply in ONE library brief that IS your answer, beginning with the tier tag.
`

func alfredTier(brief string) (string, string) {
	for _, tier := range []string{"executed", "plan", "answer"} {
		prefix := "[tier:: " + tier + "]"
		if strings.HasPrefix(strings.TrimSpace(brief), prefix) {
			return tier, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(brief), prefix))
		}
	}
	return "", brief
}

func capTierOneSummary(text string) string {
	text = capTaskComment("comment", text)
	sentences := 0
	for i, r := range text {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		next := i + 1
		if next < len(text) && !unicode.IsSpace(rune(text[next])) {
			continue
		}
		sentences++
		if sentences == 3 {
			return strings.TrimSpace(text[:next])
		}
	}
	return text
}

// Tier-1 results use precisely the harness artifact/run path and Review
// projection that fired work uses; the full artifact survives the summary cap.
func (s *Server) materializeAlfredAuto(task, brief string) bool {
	h := s.findHarness("hermes")
	if h == nil || h.Spirits == nil {
		return false
	}
	run := boardRunID()
	if boardArtifact(h, run, brief) != nil {
		return false
	}
	return boardReport(h, run, task, "go", "auto", "completed", brief, time.Now()) == nil
}
