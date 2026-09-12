package reintake

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"manifest/hermes"
)

func TestProductionInitialReceiptPrecedesExecutionAndBlocksOverlap(t *testing.T) {
	dir, cfg, a, c, reply := productionFixture(t)
	_, _, err := runStaged(context.Background(), dir, cfg, a, c, func(context.Context, hermes.DutyAuthority, string) (boundedCompletion, error) {
		rs := readReceipt(t, dir)
		if len(rs) != 1 || rs[0].State != "uncertain" || !rs[0].PageOwnerRequired {
			t.Fatal("provider called before durable uncertainty")
		}
		_, _, err := runStaged(context.Background(), dir, cfg, a, c, func(context.Context, hermes.DutyAuthority, string) (boundedCompletion, error) {
			t.Fatal("overlap reached provider")
			return boundedCompletion{}, nil
		})
		if err == nil {
			t.Fatal("overlap accepted")
		}
		return boundedCompletion{reply: reply, verified: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProductionInterruptedOrUnsafeReceiptNeverLaunches(t *testing.T) {
	for _, kind := range []string{"empty", "partial", "uncertain", "symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			dir, cfg, a, c, _ := productionFixture(t)
			name := filepath.Join(dir, ProductionPath, "run.jsonl")
			switch kind {
			case "empty":
				os.WriteFile(name, nil, 0600)
			case "partial":
				os.WriteFile(name, []byte(`{"state":`), 0600)
			case "uncertain":
				os.WriteFile(name, []byte(`{"state":"uncertain"}`), 0600)
			case "symlink":
				os.Symlink(filepath.Join(t.TempDir(), "secret"), name)
			case "directory":
				os.Mkdir(name, 0700)
			}
			_, _, err := runStaged(context.Background(), dir, cfg, a, c, func(context.Context, hermes.DutyAuthority, string) (boundedCompletion, error) {
				t.Fatal("prior receipt reached provider")
				return boundedCompletion{}, nil
			})
			if err == nil {
				t.Fatal("prior receipt accepted")
			}
		})
	}
}

func TestProductionLostFinalReceiptErasesCandidate(t *testing.T) {
	dir, cfg, a, c, reply := productionFixture(t)
	p, r, err := runStaged(context.Background(), dir, cfg, a, c, func(context.Context, hermes.DutyAuthority, string) (boundedCompletion, error) {
		if err := os.Remove(filepath.Join(dir, ProductionPath, "run.jsonl")); err != nil {
			t.Fatal(err)
		}
		return boundedCompletion{reply: reply, verified: true}, nil
	})
	if err == nil || p.Body != "" || r.State != "uncertain" || !r.PageOwnerRequired {
		t.Fatal("lost receipt allowed candidate", r, err)
	}
	_, _, retryErr := runStaged(context.Background(), dir, cfg, a, c, func(context.Context, hermes.DutyAuthority, string) (boundedCompletion, error) {
		t.Fatal("lost receipt retried")
		return boundedCompletion{}, nil
	})
	if retryErr == nil {
		t.Fatal("lost receipt latch missing")
	}
	f, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if appendProductionReceipt(f, productionReceipt("verified", "candidate only")) == nil {
		t.Fatal("closed receipt accepted")
	}
}

func TestProductionAdapterNoWriterAndOnlyCanonicalActivation(t *testing.T) {
	// Review the small adapter's import/capability boundary. approvals contributes
	// only its Proposal value and existing pure re-contract validators.
	file, err := parser.ParseFile(token.NewFileSet(), "production.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	allowedImports := map[string]bool{"context": true, "crypto/sha256": true, "encoding/json": true, "errors": true, "fmt": true, "io": true, "os": true, "path/filepath": true, "strings": true, "syscall": true, "time": true, "unicode/utf8": true, "manifest/approvals": true, "manifest/hermes": true}
	for _, imp := range file.Imports {
		name, _ := strconv.Unquote(imp.Path.Value)
		if !allowedImports[name] {
			t.Error("unreviewed capability", name)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if pkg.Name == "approvals" {
			switch sel.Sel.Name {
			case "Proposal", "ReContractPayload", "TypeReContract", "ReContractPathAllowed", "ParseReContractPayload":
			default:
				t.Error("approval store capability", sel.Sel.Name)
			}
		}
		return true
	})
	// Only the canonical intake handoff may configure or invoke the adapter.
	for _, base := range []string{"../server", "../cmd"} {
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			for _, call := range []string{"reintake.RunStaged(", "reintake.ValidateProductionRoute("} {
				if strings.Contains(string(b), call) && filepath.ToSlash(path) != "../server/intake_production.go" {
					t.Error("unexpected activation", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	main, err := os.ReadFile("../main.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(main), "reintake.RunStaged(") {
		t.Fatal("main activated adapter")
	}
	routes, err := os.ReadFile("../server/server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(routes), `mux.HandleFunc("POST /api/realestate/intake", s.handleREIntake)`) {
		t.Fatal("existing intake ownership changed")
	}
	intake, err := os.ReadFile("../server/intake.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(intake), `s.spirits.SpoolRunNow("extractor", "re-intake", req, "")`) {
		t.Fatal("Excalibur handoff changed")
	}
}
