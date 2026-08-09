package aion

import (
	"fmt"
	"strconv"
	"strings"

	"manifest/record"
)

// ParseHandoff reads an audited backfill handoff document (the
// aion-historic-backlog format: proposal records as bullet/checkbox lines
// with inline fields, each followed by an indented `- evidence: …;
// confidence: …` line) into proposal payloads for the SAME proposals lane
// the live pipeline uses — one code path, exercised at scale on day one
// (spec §7). Nothing here writes anywhere.
//
// Mapping rules (owner's decisions + the handoff's own instructions):
//   - evidence/confidence land in Quote/Confidence — kept in the PROPOSAL,
//     never in the persisted record line;
//   - [decides:: XX] canonicalizes to owner;
//   - [review:: confirm-open] is surfaced on the payload so the approval
//     card can ask "still open?";
//   - historic checked tasks default status done with done_on = captured;
//   - heuristics propose mode `new` (the audit already merged recurring
//     expressions; their several [source::]s become the reinforcements).
func ParseHandoff(raw string) ([]ProposalPayload, error) {
	var out []ProposalPayload
	var cur *ProposalPayload
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	inFence := false
	for _, line := range strings.Split(raw, "\n") {
		// the handoff documents its own record grammar inside fenced code
		// blocks — example lines are not records
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			flush()
			continue
		}
		if inFence {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		indent := record.IndentWidth(line[:len(line)-len(trimmed)])
		if indent == 0 {
			if _, it := parseBacklogItemLine(line); it != nil && it.Kind != "" {
				flush()
				p := payloadFromItem(it)
				cur = &p
				continue
			}
			flush()
			continue
		}
		if cur != nil && strings.HasPrefix(trimmed, "- evidence:") {
			quote, conf := parseEvidenceLine(strings.TrimPrefix(trimmed, "- evidence:"))
			cur.Quote, cur.Confidence = quote, conf
		}
	}
	flush()
	if len(out) == 0 {
		return nil, fmt.Errorf("no proposal records found — is this a handoff document?")
	}
	return out, nil
}

func payloadFromItem(it *BacklogItem) ProposalPayload {
	p := ProposalPayload{
		Kind: it.Kind, Title: it.Text, Owner: it.Owner,
		Rock: it.Rock, Due: it.Due, Status: it.Status, DoneOn: it.DoneOn,
		NeededBy: it.NeededBy, Decided: it.Decided, Outcome: it.Outcome,
		Sources: it.Sources, Captured: it.Captured,
	}
	for _, f := range it.Unknown {
		switch strings.ToLower(f.Key) {
		case "decides":
			if p.Owner == "" {
				p.Owner = f.Value
			}
		case "review":
			p.Review = f.Value
		case "first":
			if p.Captured == "" {
				p.Captured = f.Value
			}
		}
	}
	switch p.Kind {
	case KindHeuristic:
		p.Heuristic = HeuristicIntent{Mode: HeuristicModeNew}
		p.Status, p.DoneOn = "", ""
	case KindTask:
		if it.Checked && p.Status == "" {
			p.Status = StatusDone
		}
		if p.Status == StatusDone && p.DoneOn == "" {
			p.DoneOn = p.Captured
		}
	case KindDecision:
		if p.Status == "" {
			if p.Decided != "" {
				p.Status = StatusDecided
			} else {
				p.Status = StatusOpen
			}
		}
	}
	return p
}

// parseEvidenceLine reads `"quote text"; confidence: 0.96` (quotes optional).
func parseEvidenceLine(rest string) (string, float64) {
	rest = strings.TrimSpace(rest)
	quote := rest
	conf := 0.0
	if i := strings.LastIndex(rest, "; confidence:"); i >= 0 {
		quote = strings.TrimSpace(rest[:i])
		confStr := strings.TrimSpace(rest[i+len("; confidence:"):])
		// tolerate trailing commentary, e.g. "0.93 (speaker was JR; …)"
		if j := strings.IndexByte(confStr, ' '); j > 0 {
			confStr = confStr[:j]
		}
		if f, err := strconv.ParseFloat(confStr, 64); err == nil {
			conf = f
		}
	}
	quote = strings.Trim(quote, `"“”`)
	return quote, conf
}

// ProposalAction / ProposalBody build the proposal file content for a
// payload — the importer's analogue of the engine ritual's authoring rules
// (one-line action; body = summary + evidence line + ````aion fence).
func ProposalAction(p ProposalPayload) string {
	return "aion: " + p.Kind + " — " + p.Title
}

func ProposalBody(p ProposalPayload) string {
	var b strings.Builder
	b.WriteString(p.Title)
	if p.Review != "" {
		b.WriteString("\n\n> review: " + p.Review + " — confirm before accepting")
	}
	if p.Quote != "" {
		src := ""
		if len(p.Sources) > 0 {
			src = " — [[" + p.Sources[0] + "]]"
		}
		b.WriteString(fmt.Sprintf("\n\n> %q%s (confidence %.2f)", p.Quote, src, p.Confidence))
	}
	b.WriteString("\n\n" + RenderPayloadFence(p))
	return b.String()
}

// ProposalTarget maps a payload to its approvals type + apply path (the
// fixed conventions shared with the engine).
func ProposalTarget(p ProposalPayload) (typ, applyPath string) {
	if p.Kind == KindHeuristic {
		return "aion-heuristic", "system/aion/heuristics.md"
	}
	return "aion-backlog", "system/aion/backlog.md"
}
