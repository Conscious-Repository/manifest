package main

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Audit entrypoint calls/imports, and pin the reviewed transitive implementation.
// Fixed MigratedDuty takes Runner.Run's early runSuccessor return. Its reachable
// effects are one temporary usage file, isolated Python, and one fixed HTTP POST.
// No legacy CLI, proposals parser, production package, or writer is reachable.
// Hash changes require repeating that branch-sensitive review, not blind refresh.
func TestCanarySourceCallGraphIsolation(t *testing.T) {
	for path, allowed := range map[string]string{
		"main.go":                "os.Exit run len emit hermes.PrimaryCanaryRefusal os.OpenRoot root.Close once context.Background root.OpenFile f.Close json.NewEncoder Encode f.Sync root.Open dir.Sync dir.Close invoke",
		"../../hermes/canary.go": "PrimaryCanaryRefusal NewRunner runner.Run res.DutyVerified",
	} {
		tree, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		imports := map[string]bool{"context": true, "encoding/json": true, "io": true, "os": true, "manifest/hermes": true}
		for _, imp := range tree.Imports {
			name, _ := strconv.Unquote(imp.Path.Value)
			if !imports[name] {
				t.Fatalf("unreviewed import %s", name)
			}
		}
		calls := map[string]bool{}
		receiptOpens, directoryOpens := 0, 0
		for _, name := range strings.Fields(allowed) {
			calls[name] = true
		}
		ast.Inspect(tree, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch f := call.Fun.(type) {
			case *ast.Ident:
				name = f.Name
			case *ast.SelectorExpr:
				name = f.Sel.Name
				if id, ok := f.X.(*ast.Ident); ok {
					name = id.Name + "." + name
				}
			}
			if !calls[name] {
				t.Errorf("unreviewed call in %s: %s", path, name)
			}
			// Together with the call/import allowlist, pin every command file
			// open: the old attempt cannot even be opened for reading.
			if path == "main.go" {
				switch name {
				case "root.OpenFile":
					receiptOpens++
					arg, ok := call.Args[0].(*ast.Ident)
					if !ok || arg.Name != "receiptName" {
						t.Error("file open must target only the new fixed receipt")
					}
				case "root.Open":
					directoryOpens++
					arg, ok := call.Args[0].(*ast.BasicLit)
					if !ok || arg.Value != `"."` {
						t.Error("directory sync must not open a receipt")
					}
				case "os.OpenRoot":
					arg, ok := call.Args[0].(*ast.Ident)
					if !ok || arg.Name != "receiptDirectory" {
						t.Error("root must use fixed staging directory")
					}
				}
			}
			return true
		})
		if path == "main.go" && (receiptOpens != 1 || directoryOpens != 1) {
			t.Fatal("expected exactly one receipt open and one directory sync open")
		}
	}
	for path, want := range reviewedSuccessorSources {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(b)) != want {
			t.Errorf("re-audit successor call graph: %s", path)
		}
	}
	// No production source may reference the canary API or command. Tests and this
	// dedicated command are the only consumers; inspect source without executing it.
	err := filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".hermes" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(path, "cmd/re-intake-canary/") || path == "../../hermes/canary.go" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, name := range []string{"RunPrimaryCanary", "PrimaryCanaryRefusal", "cmd/re-intake-canary"} {
			if strings.Contains(string(b), name) {
				t.Errorf("production reference %s: %s", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Re-audited for policy-aware receipt 36: the early successor branch only
// creates a temporary usage file and runs isolated Python; authority and usage
// checks are pure, fallback still refuses, and no production writer is reachable.
var reviewedSuccessorSources = map[string]string{
	"../../hermes/runner.go":    "f97d593a97fdc53e17c80b676a0fdf34b3171dcdc1f1649ccbb601e1e7e0a841",
	"../../hermes/successor.go": "27d4d63b5ce75faa2e1be6e837cf117f1d45a3e1fdc0e0ff1026231f695aa65a",
	"../../hermes/successor.py": "a7737229609b18c627c720f466858045b7aa06b4e03ed29ce1c0774d8385ae5c",
	"../../hermes/authority.go": "e70c31d863b4858d6d4733bc05e9bde7ed0cbac0942c095494c5dcb72ba02092",
	"../../hermes/fallback.go":  "361d54087bd0eb76a98ee1014717e86632c7f037ab5dab4643528f91142f98e6",
}
