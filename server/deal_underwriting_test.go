package server

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"manifest/vaultwriter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDealUnderwritingAssetsMatch(t *testing.T) {
	for _, pair := range [][2]string{{"web/js/86-deal-diligence.js", "web/ooda/src/deal-underwriting.js"}, {"web/css/86-deal-diligence.css", "web/ooda/src/deal-underwriting.css"}} {
		a, _ := fs.ReadFile(webFiles, pair[0])
		b, _ := fs.ReadFile(webFiles, pair[1])
		if len(a) == 0 || !bytes.Equal(a, b) {
			t.Fatalf("Shared underwriting assets differ: %v", pair)
		}
	}
}
func TestDealUnderwritingScopeAndLiveRefresh(t *testing.T) {
	f := oodaPortalFixtureFull(t)
	f.srv.UseVault(vaultwriter.New(f.vault))
	write := func(rel, body string) {
		full := filepath.Join(f.vault, "system/realestate", rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("deals/duo.md", "---\ncategories: [deal]\n---\n# Duo\n")
	write("properties/748-n-euclid.md", "---\ncategories: [property]\naddress: 748 N Euclid\ndeal: '[[duo]]'\ncontrol: owned\nstatus: construction\n---\n")
	write("properties/other.md", "---\ncategories: [property]\naddress: Other\ndeal: '[[elsewhere]]'\n---\n")
	write("docs/748-n-euclid/plan.pdf", "member plan")
	write("docs/other/secret.pdf", "other deal")
	if _, err := f.srv.index.Rebuild(); err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/ooda/deal/duo/underwriting"
	if r := oodaDo(t, f.h, nil, "GET", endpoint, ""); r.Code == 200 {
		t.Fatal("anonymous underwriting access")
	}
	first := oodaDoAs(t, f, "brian@ooda.group", "Brian", "GET", endpoint, "")
	if first.Code != 200 || bytes.Contains(first.Body.Bytes(), []byte("secret.pdf")) {
		t.Fatalf("scope: %d %s", first.Code, first.Body)
	}
	before := first.Header().Get("ETag")
	if before == "" {
		t.Fatal("missing revision")
	}
	ref := "system/realestate/docs/748-n-euclid/plan.pdf"
	if r := oodaDoAs(t, f, "brian@ooda.group", "Brian", "GET", endpoint+"/document?ref="+ref, ""); r.Code != 200 || r.Body.String() != "member plan" {
		t.Fatalf("member document: %d %s; bundle=%s", r.Code, r.Body, first.Body)
	}
	if r := oodaDoAs(t, f, "brian@ooda.group", "Brian", "GET", endpoint+"/document?ref=system/realestate/docs/other/secret.pdf", ""); r.Code != http.StatusNotFound {
		t.Fatal("cross-deal file exposed")
	}
	write("properties/748-n-euclid.ledger.csv", "date,type,category,vendor,amount,status,note\n2026-09-08,expense,labor,Test vendor,123,paid,\n")
	if _, err := f.srv.index.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after := oodaDoAs(t, f, "brian@ooda.group", "Brian", "GET", endpoint, "")
	if after.Code != 200 || after.Header().Get("ETag") == before || !bytes.Contains(after.Body.Bytes(), []byte("Test vendor")) {
		t.Fatalf("new expense not reflected: %d %s", after.Code, after.Body)
	}
}

func TestDealPackageAssumptionSave(t *testing.T) {
	f := oodaPortalFixtureFull(t)
	f.srv.UseVault(vaultwriter.New(f.vault))
	if err := os.MkdirAll(filepath.Join(f.vault, "system/realestate/deals"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.vault, "system/realestate/deals/duo.md"), []byte("---\ncategories: [deal]\n---\n# Duo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.index.Rebuild(); err != nil {
		t.Fatal(err)
	}
	d, err := f.srv.buildDealUnderwriting("duo")
	if err != nil {
		t.Fatal(err)
	}
	send := func(rev string, values map[string]any) int {
		raw, _ := json.Marshal(map[string]any{"revision": rev, "name": "Bank review", "values": values})
		r := httptest.NewRequest("POST", "/", bytes.NewReader(raw))
		r.SetPathValue("slug", "duo")
		w := httptest.NewRecorder()
		f.srv.handleDealPackageAssumptions(w, r)
		return w.Code
	}
	if code := send(d.Revision, map[string]any{"vacancy_rate": 1.2}); code != 400 {
		t.Fatalf("invalid ratio: %d", code)
	}
	if code := send("stale", map[string]any{"rent_growth": .03}); code != 409 {
		t.Fatalf("stale save: %d", code)
	}
	if code := send(d.Revision, map[string]any{"rent_growth": .03, "replacement_reserve": nil}); code != 200 {
		t.Fatalf("save: %d", code)
	}
	after, err := f.srv.buildDealUnderwriting("duo")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(after.Source, []byte(`"packageAssumptions"`)) || !bytes.Contains(after.Source, []byte(`"replacement_reserve": null`)) {
		t.Fatal("assumptions not preserved")
	}
	if after.Revision == d.Revision {
		t.Fatal("revision did not change")
	}
}
