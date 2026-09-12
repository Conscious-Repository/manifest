package reintake

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

const canaryScope = `{"tools":["none"],"mcp":"no_mcp","tool_calls":[],"fallback":false,"elapsed_ms":1}`

func TestClaudeCanaryAvailabilityAlwaysUnverified(t *testing.T) {
	for _, installed := range []bool{false, true} {
		// Even a discoverable executable or credential environment cannot grant
		// readiness: the checker neither resolves PATH nor reads credentials.
		path := t.TempDir()
		if installed {
			path = "/home/benjamin/.local/bin"
		}
		t.Setenv("PATH", path)
		t.Setenv("ANTHROPIC_API_KEY", "synthetic-not-a-secret")
		for _, fixture := range []bool{false, true} {
			r, err := ClaudeCanary(fixture)
			if err == nil || r.Status != "unsupported/unverified" || r.LiveUsageVerified || r.ProductionRouted || !r.PageOwnerRequired || r.Stop == "" || len(r.MissingEvidence) == 0 || r.FixtureValidated != fixture {
				t.Fatalf("invalid readiness: %+v %v", r, err)
			}
		}
	}
}

func TestClaudeCanaryReceiptRefusals(t *testing.T) {
	for _, raw := range []string{
		``, `null`, `{}`, `{"cost_usd":0}`, `{"total_cost_usd":0,"type":"result","subtype":"success"}`,
		`{"model":"claude-sonnet-5","provider":"claude-sub","completed":true,"cost_usd":0,"cost_usd":1,"steps":1}`,
	} {
		f := loadClaude(t)
		f.Usage = json.RawMessage(raw)
		if validateCanaryFixture(canaryAuthority(), f, []byte(canaryScope)) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, mutation := range []struct{ old, new string }{
		{`"cost_usd":0`, `"cost_usd":0.01`}, {`"cost_usd":0`, `"cost_usd":1e-999`},
		{`"cost_usd":0`, `"cost_usd":null`}, {`"cost_usd":0`, `"cost_usd":-1`},
		{`"completed":true`, `"completed":false`}, {`"steps":1`, `"steps":2`},
		{`claude-sub`, `anthropic`}, {`claude-sonnet-5`, `sonnet`},
		{`"steps":1`, `"steps":1,"tool_calls":["Read"]`},
		{`"steps":1`, `"steps":1,"fallback":true`},
	} {
		f := loadClaude(t)
		f.Usage = json.RawMessage(strings.Replace(`{"model":"claude-sonnet-5","provider":"claude-sub","completed":true,"cost_usd":0,"steps":1}`, mutation.old, mutation.new, 1))
		if validateCanaryFixture(canaryAuthority(), f, []byte(canaryScope)) == nil {
			t.Fatalf("accepted %s", f.Usage)
		}
	}
	for _, scope := range []string{`{}`, `null`, strings.Replace(canaryScope, `"none"`, `"Read"`, 1), strings.Replace(canaryScope, `no_mcp`, `portal`, 1), strings.Replace(canaryScope, `[]`, `null`, 1), strings.Replace(canaryScope, `[]`, `["Read"]`, 1), strings.Replace(canaryScope, `false`, `true`, 1), strings.Replace(canaryScope, `false`, `null`, 1), strings.Replace(canaryScope, `:1}`, `:30001}`, 1), strings.Replace(canaryScope, `:1}`, `:-1}`, 1), strings.Replace(canaryScope, `:1}`, `:null}`, 1), strings.Replace(canaryScope, `:1}`, `:1,"fallback":false}`, 1)} {
		if validateCanaryFixture(canaryAuthority(), loadClaude(t), []byte(scope)) == nil {
			t.Fatalf("accepted scope %s", scope)
		}
	}
}

func TestClaudeCanaryScopeAndAuthority(t *testing.T) {
	for _, kind := range []string{"documents", "proposals", "portal", "payload", "provider", "model", "tools", "mcp", "cost", "steps", "time"} {
		t.Run(kind, func(t *testing.T) {
			f, a := loadClaude(t), canaryAuthority()
			switch kind {
			case "documents":
				f.Request.Documents = append(f.Request.Documents, "second")
			case "proposals":
				f.Proposals = append(f.Proposals, f.Proposals[0])
			case "portal":
				f.Request.PortalVisible = true
			case "payload":
				f.Proposals[0].Body = "invalid"
			case "provider":
				a.Provider = "anthropic"
			case "model":
				a.Model = "sonnet"
			case "tools":
				a.Tools = []string{"web"}
			case "mcp":
				a.MCP = "portal"
			case "cost":
				*a.CeilingUSD = 1
			case "steps":
				a.MaxSteps = 2
			case "time":
				a.TimeoutSeconds = 31
			}
			if validateCanaryFixture(a, f, []byte(canaryScope)) == nil {
				t.Fatal("accepted drift")
			}
		})
	}
}

func TestClaudeCanaryCallGraphHasNoProductionIO(t *testing.T) {
	// Audit every call in the checker and its local validation helpers. Changes
	// introducing a file open, runner, writer or network call fail this allowlist.
	allowed := map[string]bool{
		"canaryAuthority": true, "claudeFixtureUsage": true, "json.RawMessage": true, "decodeStrict": true, "validateCanaryFixture": true,
		"validateAuthority": true, "compare": true, "refusal": true, "structural": true,
		"len": true, "string": true, "append": true, "make": true, "blank": true,
		"fixtures.ReadFile": true, "a.Validate": true, "hermes.VerifyDutyUsage": true,
		"res.DutyVerified": true, "strings.TrimSpace": true, "strings.Count": true,
		"approvals.ReContractPathAllowed": true, "mdfm.ExtractFencedBlock": true,
		"approvals.ParseReContractPayload": true, "payload.Validate": true,
		"reflect.DeepEqual": true, "json.NewDecoder": true, "bytes.NewReader": true,
		"d.Token": true, "d.More": true, "walk": true, "errors.New": true,
		"d.DisallowUnknownFields": true, "d.Decode": true,
	}
	for _, path := range []string{"claude_canary.go", "shadow.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || (path == "shadow.go" && !allowed[fn.Name.Name]) {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch c := call.Fun.(type) {
				case *ast.Ident:
					name = c.Name
				case *ast.SelectorExpr:
					if x, ok := c.X.(*ast.Ident); ok {
						name = x.Name + "." + c.Sel.Name
					}
				case *ast.ArrayType:
					return true // []byte conversion
				}
				if !allowed[name] {
					t.Errorf("unreviewed call in %s: %s", fn.Name.Name, name)
				}
				return true
			})
		}
	}
}

func loadClaude(t *testing.T) fixture {
	f := load(t, "single")
	f.Usage = claudeFixtureUsage()
	return f
}
