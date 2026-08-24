package approvals

import (
	"encoding/json"
	"strings"
)

// TypePortalProposal surfaces a team-portal proposal — "X proposed this task
// to Y" — in the owner's FEED as an APPROVABLE card rather than a notice he
// can only dismiss.
//
// Before this type existed a proposal did reach the FEED, through the portal
// bridge, as a `portal-item` notice titled "portal proposal for y@aion.bio"
// with two affordances: jump, and dismiss. Deciding it meant leaving the FEED
// for the AION tab or the portal itself. The owner's ask was to approve it
// "like any other card there" — so it becomes an actual approval and inherits
// the whole lane: the card, the Confirm/Reject buttons, the badge count, the
// inspector.
//
// Like TypeRunErrand it carries NO ApplyPath: Confirm writes no file, because
// the effect is not a vault write. Confirming records the decision (the folder
// move) and the SERVER dispatches teamportal.Store.Decide afterwards, which
// mints the item in the team store — and the materialization lane then carries
// it into the backlog like any other portal item. Keeping ApplyPath empty is
// what stops the file-apply machinery from ever being reached.
const TypePortalProposal = "portal-proposal"

// PortalProposalFence is the payload language. The client refuses to Confirm a
// card whose fence language disagrees with its declared type (a misfiled
// proposal), so this string is part of the contract, not decoration.
const PortalProposalFence = "portal"

// PortalProposalPayload identifies which proposal, in which portal, the card
// decides. It deliberately carries no item CONTENT: the team store is the
// record, and a copy here could disagree with it by the time the owner looks.
// Title and target ride along for the card's own rendering only.
type PortalProposalPayload struct {
	Portal string `json:"portal"` // team store name: "aion-portal" | "ooda-portal"
	PropID string `json:"prop_id"`
	Kind   string `json:"kind"`   // task | decision
	Title  string `json:"title"`  // display only
	Target string `json:"target"` // display only — the email it is aimed at
	By     string `json:"by"`     // display only — who proposed it
}

// ParsePortalProposal reads the payload out of a proposal body.
func ParsePortalProposal(p Proposal) (PortalProposalPayload, bool) {
	if p.Type != TypePortalProposal {
		return PortalProposalPayload{}, false
	}
	raw, ok := fencedBlock(p.Body, PortalProposalFence)
	if !ok {
		return PortalProposalPayload{}, false
	}
	var out PortalProposalPayload
	if json.Unmarshal([]byte(raw), &out) != nil {
		return PortalProposalPayload{}, false
	}
	return out, strings.TrimSpace(out.PropID) != ""
}

// RenderPortalProposalBody builds the proposal body: a human line naming what
// is being asked of whom, then the machine payload. The prose is what the
// owner reads on the card; the fence is what the confirm handler acts on.
func RenderPortalProposalBody(p PortalProposalPayload) string {
	who := p.Target
	if who == "" {
		who = "a teammate"
	}
	by := p.By
	if by == "" {
		by = "someone on the team"
	}
	kind := p.Kind
	if kind != "decision" {
		kind = "task"
	}
	body, _ := json.MarshalIndent(p, "", "  ")
	return "" +
		by + " proposed a " + kind + " for " + who + ".\n\n" +
		"> " + strings.TrimSpace(p.Title) + "\n\n" +
		"Approving creates it as " + who + "'s " + kind + " in the team store, and the\n" +
		"materialization lane writes it into the AION backlog. Rejecting closes the\n" +
		"proposal and nothing is created. Either way " + who + " sees the outcome in\n" +
		"the portal — the proposal is theirs to decide too, and whichever of you\n" +
		"acts first settles it.\n\n" +
		"```" + PortalProposalFence + "\n" + string(body) + "\n```\n"
}

// fencedBlock returns the contents of the first ```<lang> block in a body.
func fencedBlock(body, lang string) (string, bool) {
	open := "```" + lang
	i := strings.Index(body, open)
	if i < 0 {
		return "", false
	}
	rest := body[i+len(open):]
	if j := strings.Index(rest, "\n"); j >= 0 {
		rest = rest[j+1:]
	}
	end := strings.Index(rest, "```")
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[:end]), true
}
