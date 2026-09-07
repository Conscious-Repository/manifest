package server

import "strings"

// Deployment policy: best defaults and accepted overrides live together here.
// Both defaults were successfully probed with the installed CLIs.
var codingModels = map[string]struct {
	best    string
	allowed []string
}{
	"codex":  {best: "gpt-6-astra", allowed: []string{"gpt-6-astra", "gpt-5.5"}},
	"claude": {best: "fable", allowed: []string{"fable", "opus", "sonnet"}},
}

func firstModel(models []string) string {
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

// Grammar: agent:name[::intent][::model:slug], either segment order.
// Duplicate/empty model segments or multiple intents invalidate the override.
// Preserve a single intent independently so model typos cannot turn plan into execution.
func parseAgentToken(tok string) (base, intent, model string) {
	if !strings.HasPrefix(tok, "agent:") {
		return tok, "", ""
	}
	parts := strings.Split(tok, "::")
	base = parts[0]
	seenModel, invalid := false, false
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, "model:") || part == "model" {
			if seenModel {
				invalid = true
			}
			seenModel = true
			model = strings.TrimPrefix(part, "model:")
			if model == "" || part == "model" {
				invalid = true
			}
		} else {
			if intent != "" || part == "" {
				invalid = true
			}
			if intent == "" || part == "plan" {
				intent = strings.TrimSpace(part)
			}
		}
	}
	if invalid {
		model = "invalid-token"
	}
	return
}

func codingModel(owner, requested string) (model, note string) {
	policy := codingModels[owner]
	if requested == "" || requested == "best" {
		return policy.best, ""
	}
	for _, allowed := range policy.allowed {
		if requested == allowed {
			return requested, ""
		}
	}
	return policy.best, "Unknown or malformed model override " + requested + "; using best (" + policy.best + ")."
}

func (se termSession) boardModel() string {
	if se.Model != "" {
		return se.Model
	} // pin even if deployment defaults change
	model, _ := codingModel(se.Kind, "")
	return model
}
