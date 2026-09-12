package reintake

import (
	"encoding/json"

	"manifest/hermes"
)

// ClaudeCanaryReport is never a live receipt. The checker has no filesystem,
// process, network, config, runner or writer dependency. Callers own stdout.
type ClaudeCanaryReport struct {
	Status            string               `json:"status"`
	Authority         hermes.DutyAuthority `json:"authority"`
	MaxDocuments      int                  `json:"maxDocuments"`
	MaxProposals      int                  `json:"maxProposals"`
	FixtureValidated  bool                 `json:"fixtureValidated"`
	LiveUsageVerified bool                 `json:"liveUsageVerified"`
	ProductionRouted  bool                 `json:"productionRouted"`
	PageOwnerRequired bool                 `json:"pageOwnerRequired"`
	MissingEvidence   []string             `json:"missingEvidence"`
	Stop              string               `json:"stop"`
}

func canaryAuthority() hermes.DutyAuthority {
	zero := 0.0
	return hermes.DutyAuthority{Provider: "claude-sub", Model: Model, Tools: []string{"none"}, MCP: "no_mcp", TimeoutSeconds: 30, MaxSteps: 1, CeilingUSD: &zero}
}

// ClaudeCanary always refuses live readiness, including when an explicitly
// requested synthetic fixture passes. Installed CLI JSON output is not proof of
// the subscription receipt contract. No launch seam or arbitrary input exists.
// This function creates no files; a persisted report must retain Stop.
func ClaudeCanary(testFixture bool) (ClaudeCanaryReport, error) {
	r := ClaudeCanaryReport{
		Status: "unsupported/unverified", Authority: canaryAuthority(),
		MaxDocuments: 1, MaxProposals: 1, PageOwnerRequired: true,
		MissingEvidence: []string{
			"verified bounded claude-sub launcher (Hermes rejects this provider)",
			"authenticated noninteractive subscription availability without secrets: unverified",
			"actual receipt provider=claude-sub and model=claude-sonnet-5",
			"actual receipt completed=true and explicit zero cost_usd (not subscription billing inference)",
			"actual receipt steps=1, elapsed time <=30s, tools=none, mcp=no_mcp, tool_calls=[], fallback=false",
		},
		Stop: "STOP: claude-sub canary refused; page owner with local evidence; no retry, fallback or cutover",
	}
	if testFixture {
		raw, _ := fixtures.ReadFile("fixtures/single.json")
		var f fixture
		if decodeStrict(raw, &f) != nil || validateCanaryFixture(r.Authority, f, []byte(`{"tools":["none"],"mcp":"no_mcp","tool_calls":[],"fallback":false,"elapsed_ms":1}`)) != nil {
			return r, refusal("canary fixture validation failed")
		}
		r.FixtureValidated = true
	}
	return r, &hermes.Refusal{Reason: r.Stop}
}

// The additional scope evidence is synthetic, never adapted from CLI output.
// Reuse the existing authority, strict usage and re-contract comparison gates.
func validateCanaryFixture(a hermes.DutyAuthority, f fixture, scope []byte) error {
	if err := validateAuthority(a); err != nil {
		return err
	}
	if *a.CeilingUSD != 0 || a.MaxSteps != 1 || a.TimeoutSeconds != 30 {
		return refusal("canary bounds differ")
	}
	var s struct {
		Tools    []string          `json:"tools"`
		MCP      string            `json:"mcp"`
		Calls    []json.RawMessage `json:"tool_calls"`
		Fallback *bool             `json:"fallback"`
		Elapsed  *int              `json:"elapsed_ms"`
	}
	if decodeStrict(scope, &s) != nil || len(s.Tools) != 1 || s.Tools[0] != "none" || s.MCP != "no_mcp" || s.Calls == nil || len(s.Calls) != 0 || s.Fallback == nil || *s.Fallback || s.Elapsed == nil || *s.Elapsed < 0 || *s.Elapsed > 30000 {
		return refusal("missing or uncertain canary scope evidence")
	}
	return compare(a, f)
}
