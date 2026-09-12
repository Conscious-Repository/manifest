package hermes

import "strings"

// SubscriptionOption names owner recovery choices, not configured providers or
// an ordered failover list. No subscription adapter is independently verified.
type SubscriptionOption string

const (
	ClaudeCodeSubscription SubscriptionOption = "claude-code-subscription"
	CodexSubscription      SubscriptionOption = "codex-subscription"
)

// FallbackChoice is inert owner input for a separately authorized experiment.
// These references are claims awaiting independent review, never runtime grants.
// No result/error creates a choice and no mutable recovery state is stored here.
type FallbackChoice struct {
	Option            SubscriptionOption
	Provider          string
	Model             string
	OwnerAction       string
	PrimaryResolution string // exactly clean-resolved; uncertainty cannot qualify
	PrimaryEvidence   string // complete, durable evidence reference for owner review
}

// RefuseFallback distinguishes malformed input from the future adapter boundary.
// Even a clean token always refuses today; no executable or provider is resolved.
func RefuseFallback(c *FallbackChoice) error {
	if c == nil || strings.TrimSpace(c.OwnerAction) == "" || strings.TrimSpace(c.PrimaryEvidence) == "" || c.PrimaryResolution != "clean-resolved" {
		return refuse("explicit owner choice and clean resolved primary evidence required; freeze lane and page Benjamin")
	}
	switch c.Option {
	case ClaudeCodeSubscription:
		if c.Provider != "claude-sub" || c.Model != "claude-sonnet-5" {
			return refuse("fallback identity must be exact")
		}
	case CodexSubscription:
		// No Codex model pin has been independently verified. Never invent one
		// or interpret a default/alias as a certified subscription identity.
		return refuse("codex subscription unsupported/unverified: no verified exact model or adapter")
	default:
		return refuse("missing or ambiguous fallback option")
	}
	return refuse("claude subscription unsupported/unverified: future adapter not implemented")
}
