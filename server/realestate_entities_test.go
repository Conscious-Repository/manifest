package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"manifest/realestate"
)

// GET /api/realestate/entities treats the statement import memory as the
// optional state it is everywhere else: a server composed with the
// real-estate service but without import memory (UseRealestate sets both;
// the construction fixture sets only the service) answers with empty
// bindings instead of dereferencing nil, and with memory present the
// bindings come from it. The handler is called directly, so a panic fails
// this test instead of being recovered by net/http.
func TestRealestateEntitiesListWithoutImportMemory(t *testing.T) {
	f := constructionFixture(t)
	if f.srv.realestate == nil || f.srv.reImport != nil {
		t.Fatal("the fixture composes the real-estate service without import memory")
	}
	get := func() map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/realestate/entities", nil))
		if rec.Code != 200 {
			t.Fatalf("entities list: %d %s", rec.Code, rec.Body)
		}
		var m map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := get()
	if b, ok := m["bindings"].(map[string]any); !ok || len(b) != 0 {
		t.Fatalf("without import memory the bindings are empty: %v", m["bindings"])
	}
	for _, k := range []string{"entities", "partners", "lenders", "tenants", "contractors"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("the list still carries %s: %v", k, m)
		}
	}
	f.srv.reImport = realestate.NewImportMemory(t.TempDir())
	f.srv.reImport.BindLabel("Synthetic Checking 1234", "fixture-entity")
	if b, _ := get()["bindings"].(map[string]any); len(b) != 1 || b["synthetic checking 1234"] != "fixture-entity" {
		t.Fatalf("with import memory the bindings come from it: %v", b)
	}
	f.assertSourcesUntouched(t)
}
