package sources

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 2, 15, 4, 5, 0, time.UTC)

// Rule 1 — an adapter never writes. Structural, not aspirational: no adapter
// implementation may carry a func field (an injected writer), a pointer to a
// store, or anything that could persist. It returns DTOs and stops.
func TestNoAdapterHoldsAWriter(t *testing.T) {
	for _, a := range []Adapter{Manual{}, OpenAlex{}} {
		ty := reflect.TypeOf(a)
		for ty.Kind() == reflect.Ptr {
			ty = ty.Elem()
		}
		if ty.Kind() != reflect.Struct {
			continue
		}
		for i := 0; i < ty.NumField(); i++ {
			f := ty.Field(i)
			switch f.Type.Kind() {
			case reflect.Func, reflect.Ptr, reflect.Interface, reflect.Chan:
				t.Errorf("%s.%s is a %s — an adapter that can hold a writer is one "+
					"that can write. The adapter returns DTOs; recruiting decides "+
					"what becomes a record.", ty.Name(), f.Name, f.Type.Kind())
			}
		}
	}
}

// The import-boundary half of rule 1: this package must not import
// `recruiting`, or an adapter could reach the store transitively. One
// direction only.
func TestSourcesDoesNotImportRecruiting(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if path == "manifest/recruiting" || strings.HasPrefix(path, "manifest/recruiting/") {
					t.Errorf("%s imports %s — an adapter must not be able to reach the "+
						"vault store, even transitively", name, path)
				}
				if path == "manifest/aion" || strings.HasPrefix(path, "manifest/aion/") {
					t.Errorf("%s imports %s — recruiting is private; aion is the public "+
						"export contract", name, path)
				}
			}
		}
	}
	if len(pkgs) == 0 {
		t.Fatal("parsed no packages — the boundary check is not running")
	}
}

// Rule 2 — every emitted fact carries its source.
func TestManualDraftCarriesItsProvenance(t *testing.T) {
	d, err := Manual{Owner: "benjamin"}.Draft(Entry{
		Text: "https://example.test/people/dana — met at a conference",
		Role: "role/mri-engineer", Now: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.SourceID != "manual" || len(d.Evidence) != 1 {
		t.Fatalf("draft: %+v", d)
	}
	ev := d.Evidence[0]
	if ev.SourceID != "manual" || ev.URLOrFile != "https://example.test/people/dana" ||
		!ev.RetrievedAt.Equal(testNow) || !ev.Cited() {
		t.Fatalf("evidence: %+v", ev)
	}
	if ev.Snippet != "https://example.test/people/dana — met at a conference" {
		t.Errorf("the owner's line was not preserved verbatim: %q", ev.Snippet)
	}
	if d.Name != "dana" {
		t.Errorf("provisional name from the url: %q", d.Name)
	}
}

// A bare name is still cited — by the owner's own words. "Benjamin said so"
// is a real provenance; an absent one is not.
func TestManualDraftCitesTheOwnersWords(t *testing.T) {
	d, err := Manual{Owner: "rj"}.Draft(Entry{Text: "Marlow Finch", Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	ev := d.Evidence[0]
	if ev.Kind != EvidenceOwnerNote || ev.URLOrFile != "" || !ev.Cited() {
		t.Fatalf("evidence: %+v", ev)
	}
	if _, err := (Manual{}).Draft(Entry{Text: "   ", Now: testNow}); err == nil {
		t.Error("an empty entry produced a draft")
	}
}

// The owner's asserted relationship is the strongest edge in the system, and
// it arrives with its basis, its confidence, and inferred explicitly false.
func TestManualEdgesAreOwnerAsserted(t *testing.T) {
	d, err := Manual{Owner: "benjamin"}.Draft(Entry{
		Text: "Marlow Finch", Known: true, KnownVia: "aion-net/ben-anderson", Now: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	edges, err := Manual{}.GraphEdges(context.Background(), d)
	if err != nil || len(edges) != 1 {
		t.Fatalf("edges=%+v err=%v", edges, err)
	}
	e := edges[0]
	if e.Type != EdgeDirectKnown || e.Confidence != OwnerConfidence || e.Inferred ||
		e.Basis == "" || e.SourceID != "owner" {
		t.Fatalf("edge: %+v", e)
	}
	if !ValidEdgeType(e.Type) {
		t.Error("the manual adapter emitted an edge type outside the closed set")
	}
	if _, err := (Manual{}).Draft(Entry{Text: "x", Known: true, Now: testNow}); err == nil {
		t.Error("a known-person entry with no asserting node produced a draft")
	}
}

// Rule 3 — no adapter emits an email or a phone number (D15). The manual
// adapter has no path to one at all: a URL that looks like a mailto is not a
// link it will follow onto the profile.
func TestManualNeverEmitsContactDetails(t *testing.T) {
	d, err := Manual{}.Draft(Entry{
		Text: "Marlow Finch mailto:marlow@example.test +1 555 0100", Now: testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Contact) != 0 {
		t.Fatalf("the manual adapter set contact fields: %+v", d.Contact)
	}
	for _, l := range d.Links {
		if strings.HasPrefix(l, "mailto:") {
			t.Errorf("a mailto reached the links: %q", l)
		}
	}
}

// Enrich is a no-op by design: inventing a field here is exactly the
// enrichment D15 refuses.
func TestManualEnrichChangesNothing(t *testing.T) {
	d, err := Manual{}.Draft(Entry{Text: "Marlow Finch", Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Manual{}.Enrich(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, d) {
		t.Errorf("Enrich changed the draft:\n%+v\n%+v", d, got)
	}
}

// Search honours the scope's cap and makes no network call (there is nothing
// to call — the owner IS the source).
func TestManualSearchHonoursTheScope(t *testing.T) {
	got, err := Manual{Owner: "benjamin"}.Search(context.Background(), Scope{
		Role: "role/mri-engineer", Query: "Marlow Finch", Max: 25, DryRun: true,
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("drafts=%+v err=%v", got, err)
	}
	if got[0].Role != "role/mri-engineer" {
		t.Errorf("the run's role did not reach the draft: %+v", got[0])
	}
	if fields := (Manual{}).Scope(); len(fields) == 0 {
		t.Error("an adapter must declare what the UI has to collect")
	}
}

// The vocabulary the records key off is a closed set, and nothing outside it
// may be serialized.
func TestEdgeTypeSetIsClosed(t *testing.T) {
	for _, ty := range EdgeTypes {
		if !ValidEdgeType(ty) {
			t.Errorf("%q is in EdgeTypes but not valid", ty)
		}
	}
	for _, bad := range []EdgeType{"", "friend", "DIRECT_KNOWN", "linkedin_connection"} {
		if ValidEdgeType(bad) {
			t.Errorf("%q passed as an edge type", bad)
		}
	}
}

// Guard against a future adapter file quietly acquiring an os write path.
func TestNoAdapterFileWritesToDisk(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	banned := map[string]bool{"WriteFile": true, "Create": true, "OpenFile": true,
		"MkdirAll": true, "Rename": true, "Remove": true, "RemoveAll": true}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok || ident.Name != "os" || !banned[sel.Sel.Name] {
					return true
				}
				t.Errorf("%s calls os.%s — an adapter holds no write path of any kind",
					name, sel.Sel.Name)
				return true
			})
		}
	}
}
