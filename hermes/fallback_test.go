package hermes

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"strings"
	"testing"
)

func TestFallbackRefusesWithoutLaunch(t *testing.T) {
	old := successorCommand
	t.Cleanup(func() { successorCommand = old })
	successorCommand = func(context.Context, string) *exec.Cmd { t.Fatal("provider launch reachable"); return nil }
	clean := FallbackChoice{Option: ClaudeCodeSubscription, Provider: "claude-sub", Model: "claude-sonnet-5", OwnerAction: "synthetic-owner-record", PrimaryResolution: "clean-resolved", PrimaryEvidence: "synthetic-complete-receipt"}
	if err := RefuseFallback(nil); err == nil {
		t.Fatal("missing choice accepted")
	}
	for _, kind := range []string{"clean", "missing", "auto", "ambiguous", "owner", "evidence", "timeout", "partial-output", "missing-receipt", "missing-usage", "nonzero-cost", "model-drift", "provider-drift", "tools", "mcp", "provider-error", "uncertain", "crash-after-effect", "model", "provider", "codex"} {
		t.Run(kind, func(t *testing.T) {
			c := clean
			switch kind {
			case "clean":
			case "missing":
				c.Option = ""
			case "auto":
				c.Option = "auto"
			case "ambiguous":
				c.Option = "claude-code-subscription,codex-subscription"
			case "owner":
				c.OwnerAction = ""
			case "evidence":
				c.PrimaryEvidence = ""
			case "model":
				c.Model = "default"
			case "provider":
				c.Provider = "auto"
			case "codex":
				c.Option = CodexSubscription
				c.Provider = "codex-sub"
				c.Model = "gpt-6-astra"
			default:
				c.PrimaryResolution = kind
			}
			for _, duty := range []string{"", "extractor/re-intake"} {
				r := NewRunner(Config{Enabled: true, Bin: "/never-execute", Duties: map[string]DutyAuthority{duty: successorAuthority()}})
				res, err := r.Run(t.Context(), Request{MigratedDuty: duty, Fallback: &c, Prompt: "synthetic"})
				if err == nil || res.Reply != "" || res.DutyVerified() {
					t.Fatal("fallback executable")
				}
				if kind == "clean" && !strings.Contains(err.Error(), "future adapter not implemented") {
					t.Fatal(err)
				}
				if kind != "clean" && strings.Contains(err.Error(), "future adapter not implemented") {
					t.Fatal("unclean token reached adapter boundary")
				}
			}
		})
	}
}

func TestFallbackGateHasNoIO(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "fallback.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := c.Fun.(type) {
		case *ast.Ident:
			if fn.Name == "refuse" {
				return true
			}
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok && x.Name == "strings" && fn.Sel.Name == "TrimSpace" {
				return true
			}
		}
		t.Error("unreviewed call in inert fallback gate")
		return true
	})
}
