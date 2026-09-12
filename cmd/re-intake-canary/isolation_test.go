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
			return true
		})
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

var reviewedSuccessorSources = map[string]string{
	"../../hermes/runner.go":    "c6c9f3eadf85cf8244b3cb20d6c8b2c8a498a8739d8527c8a1f44372d9357839",
	"../../hermes/successor.go": "9fe3e973fc52accd9eae33bd17cf34deb5311bbb91779412807c88672c343293",
	"../../hermes/successor.py": "a3f4902cfee4fbcb90201300bed034e79dfe12ef164c6b2adfb8bb470b7beca2",
	"../../hermes/authority.go": "807fef078582c1aeb344c45d2d4f5295e073f6e4c2dfa698115dd7d8cf4baaa9",
	"../../hermes/fallback.go":  "361d54087bd0eb76a98ee1014717e86632c7f037ab5dab4643528f91142f98e6",
}
