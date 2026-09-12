package hermes

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"strings"
)

// DutyAuthority is mandatory for successor duties only. Legacy callers do not
// acquire authority by inheriting the owner's interactive defaults.
type DutyAuthority struct {
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	Tools          []string `json:"tools"`
	MCP            string   `json:"mcp"` // no_mcp or one explicit server name
	TimeoutSeconds int      `json:"timeoutSeconds"`
	MaxSteps       int      `json:"maxSteps"`
	CeilingUSD     *float64 `json:"ceilingUsd"` // nil is absent; zero is an explicit bound
}

type Refusal struct{ Reason string }

func (e *Refusal) Error() string { return "refused: " + e.Reason }
func refuse(reason string) error { return &Refusal{Reason: reason} }

// Validate checks declared authority; it does not certify the mutable Hermes
// installation or grant filesystem access. No prompt is consulted for grants.
func (a DutyAuthority) Validate() error {
	if strings.TrimSpace(a.Model) == "" {
		return refuse("missing model pin")
	}
	if strings.TrimSpace(a.Provider) == "" || a.Provider == "auto" {
		return refuse("missing provider pin")
	}
	if len(a.Tools) == 0 {
		return refuse("missing explicit tool authority")
	}
	// These are potentially read-only names, not a certification of their
	// implementations. A verified launch mechanism is still required below.
	for _, tool := range a.Tools {
		switch tool {
		case "none", "web", "session_search", "vision":
		default:
			return refuse("tool authority not permitted for proposal-only duty")
		}
	}
	if strings.TrimSpace(a.MCP) == "" || strings.ContainsAny(a.MCP, ", *\t\n") || a.MCP == "all" {
		return refuse("missing explicit MCP authority")
	}
	if a.TimeoutSeconds <= 0 || a.MaxSteps <= 0 || a.CeilingUSD == nil || math.IsNaN(*a.CeilingUSD) || math.IsInf(*a.CeilingUSD, 0) || *a.CeilingUSD < 0 {
		return refuse("missing finite execution bounds")
	}
	return nil
}

// VerifyDutyResult is the mandatory acceptance boundary before ParseProposals
// or approvals.Propose. Refused text is erased so callers cannot accept it even
// if they accidentally continue after an error. Only known usage is accepted.
func VerifyDutyResult(a DutyAuthority, res Result, reportedProvider string, usagePresent bool) (Result, error) {
	reject := func(reason string) (Result, error) { return Result{}, refuse(reason) }
	if err := a.Validate(); err != nil {
		return Result{}, err
	}
	if !usagePresent {
		return reject("missing usage evidence")
	}
	if res.Model != a.Model {
		return reject("model drift")
	}
	if reportedProvider != a.Provider {
		return reject("provider drift")
	}
	if math.IsNaN(res.SpentUSD) || math.IsInf(res.SpentUSD, 0) || res.SpentUSD < 0 || res.SpentUSD > *a.CeilingUSD {
		return reject("cost bound exceeded")
	}
	return res, nil
}

func (r *Runner) dutyAuthority(req Request) (DutyAuthority, error) {
	if req.Fallback != nil {
		return DutyAuthority{}, RefuseFallback(req.Fallback)
	}
	a, ok := r.cfg.Duties[req.MigratedDuty]
	if !ok {
		return a, refuse("missing duty authority")
	}
	if err := a.Validate(); err != nil {
		return a, err
	}
	if req.Model != "" && req.Model != a.Model {
		return a, refuse("model override differs from duty pin")
	}
	if req.Toolsets != "" && req.Toolsets != strings.Join(append(append([]string{}, a.Tools...), a.MCP), ",") {
		return a, refuse("tool override differs from duty authority")
	}
	if a.Provider != "deepseek-local" || a.Model != "deepseek-v4.1-flash" || *a.CeilingUSD != 0 {
		return a, refuse("unsupported zero-cost successor identity")
	}
	if len(a.Tools) != 1 || a.Tools[0] != "none" || a.MCP != "no_mcp" {
		return a, refuse("successor requires exact tool-free scope")
	}
	if a.TimeoutSeconds > 3600 || a.MaxSteps > 1000 {
		return a, refuse("successor bounds exceed implementation limits")
	}
	return a, nil
}

// VerifyDutyUsageFile consumes the same --usage-file contract as Run, but a
// successor must not treat a missing or malformed report as zero-cost success.
func VerifyDutyUsageFile(a DutyAuthority, res Result, path string) (Result, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Result{}, refuse("missing usage evidence")
	}
	return VerifyDutyUsage(a, res, b)
}

// VerifyDutyUsage is the pure acceptance check shared by the runner and frozen
// shadow replay. It does not mint a DutyVerified receipt or authorize execution.
func VerifyDutyUsage(a DutyAuthority, res Result, b []byte) (Result, error) {
	var u usageReport
	fields, fieldsOK := strictUsageFields(b)
	valid := json.Unmarshal(b, &u) == nil && fieldsOK
	if valid {
		valid = u.Completed != nil && *u.Completed && !u.Failed
		found := false
		for _, key := range []string{"cost_usd", "estimated_cost_usd"} {
			if raw, ok := fields[key]; ok {
				var cost *float64
				if json.Unmarshal(raw, &cost) != nil || cost == nil || math.IsNaN(*cost) || math.IsInf(*cost, 0) || *cost < 0 || a.CeilingUSD == nil || *cost > *a.CeilingUSD {
					valid = false
				}
				// Float underflow must not turn a reported nonzero charge into
				// authorized zero. Inspect the JSON number's significand too.
				if a.CeilingUSD != nil && *a.CeilingUSD == 0 {
					significand, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(string(raw))), "e")
					if strings.Trim(significand, "-0.") != "" {
						valid = false
					}
				}
				found = true
			}
		}
		valid = valid && found
	}
	res.Model, res.SpentUSD, res.SessionID = u.Model, u.usd(), u.SessionID
	return VerifyDutyResult(a, res, u.Provider, valid)
}

// Duplicate fields can conceal a nonzero cost or a drifted identity. Reject
// ambiguous reports rather than relying on encoding/json's last-value rule.
func strictUsageFields(b []byte) (map[string]json.RawMessage, bool) {
	if len(b) > 64000 {
		return nil, false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, false
		}
		if _, exists := fields[key]; exists {
			return nil, false
		}
		var raw json.RawMessage
		if d.Decode(&raw) != nil {
			return nil, false
		}
		fields[key] = raw
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, false
	}
	return fields, true
}
