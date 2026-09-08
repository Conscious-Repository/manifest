package server

import (
	"bytes"
	"encoding/json"
	"manifest/realestate"
	"manifest/vaultwriter"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLenderShareAccessScopeAndRevocation(t *testing.T) {
	f := oodaPortalFixtureFull(t)
	f.srv.UseVault(vaultwriter.New(f.vault))
	dir := t.TempDir()
	if err := f.srv.UseDealShares(dir); err != nil {
		t.Fatal(err)
	}
	write := func(p, b string) {
		t.Helper()
		p = filepath.Join(f.vault, "system/realestate", p)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(b), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("deals/duo.md", "---\ncategories: [deal]\n---\n# Duo\n")
	write("deals/elsewhere.md", "---\ncategories: [deal]\n---\n# Elsewhere\n")
	write("properties/one.md", "---\ncategories: [property]\naddress: One\ndeal: '[[duo]]'\n---\n")
	write("properties/other.md", "---\ncategories: [property]\naddress: Other\ndeal: '[[elsewhere]]'\n---\n")
	write("docs/one/plan.pdf", "member plan")
	write("docs/one/attachment.html", "<script>document.body.textContent=\"unsafe\"</script>")
	write("docs/other/secret.pdf", "other deal secret")
	if _, err := f.srv.index.Rebuild(); err != nil {
		t.Fatal(err)
	}
	private := http.NewServeMux()
	private.HandleFunc("POST /api/deals/{slug}/shares", f.srv.handleDealShares)
	private.HandleFunc("GET /api/deals/{slug}/shares", f.srv.handleDealShares)
	private.HandleFunc("DELETE /api/deals/{slug}/shares/{id}", f.srv.handleDealShares)
	do := func(h http.Handler, method, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	created := do(private, "POST", "/api/deals/duo/shares", `{"label":"Lender","days":30}`, nil)
	if created.Code != 200 {
		t.Fatal(created.Code, created.Body)
	}
	var result struct {
		Share    dealShare `json:"share"`
		Password string    `json:"password"`
		Path     string    `json:"path"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Share.PasswordHash != "" || len(result.Password) != 48 {
		t.Fatal("bad generated secret response")
	}
	disk, err := os.ReadFile(filepath.Join(dir, "deal-sharing/deal-shares.json"))
	if err != nil || bytes.Contains(disk, []byte(result.Password)) {
		t.Fatal("password persisted in plaintext")
	}
	list := do(private, "GET", "/api/deals/duo/shares", "", nil)
	if strings.Contains(list.Body.String(), "password") {
		t.Fatal("list exposed password material")
	}
	pub := f.srv.DealShareHandler(f.h)
	base := result.Path
	endpoint := base + "underwriting"
	for _, path := range []string{endpoint, endpoint + "/document?ref=system/realestate/docs/one/plan.pdf"} {
		if w := do(pub, "GET", path, "", nil); w.Code != 401 {
			t.Fatal("anonymous access", w.Code)
		}
	}
	if w := do(pub, "POST", base+"unlock", `{"password":"wrong"}`, nil); w.Code != 401 {
		t.Fatal("bad password accepted")
	}
	passwordJSON, _ := json.Marshal(map[string]string{"password": result.Password})
	w := do(pub, "POST", base+"unlock", string(passwordJSON), nil)
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session")
	}
	c := cookies[0]
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != base {
		t.Fatal("weak cookie")
	}
	w = do(pub, "GET", endpoint, "", c)
	if w.Code != 200 || strings.Contains(w.Body.String(), "other deal secret") {
		t.Fatal("scoped data", w.Code, w.Body)
	}
	if w = do(pub, "GET", endpoint+"/document?ref=system/realestate/docs/one/plan.pdf", "", c); w.Code != 200 || w.Body.String() != "member plan" {
		t.Fatal("member document", w.Code, w.Body)
	}
	if w = do(pub, "GET", endpoint+"/document?ref=system/realestate/docs/one/attachment.html", "", c); w.Code != 200 || w.Header().Get("Content-Security-Policy") != "sandbox allow-downloads; default-src 'none'" {
		t.Fatal("active attachment not sandboxed", w.Code, w.Header())
	}
	if w = do(pub, "GET", endpoint+"/document?ref=system/realestate/docs/other/secret.pdf", "", c); w.Code != 404 {
		t.Fatal("cross-deal document")
	}
	if w = do(pub, "GET", "/api/ooda/portfolio", "", c); w.Code == 200 {
		t.Fatal("lender session became team identity")
	}
	another := do(private, "POST", "/api/deals/elsewhere/shares", `{"label":"Other","days":1}`, nil)
	var other struct {
		Path string `json:"path"`
	}
	json.Unmarshal(another.Body.Bytes(), &other)
	if w = do(pub, "GET", other.Path+"underwriting", "", c); w.Code != 401 {
		t.Fatal("cross-link session")
	}
	write("deals/duo.md", "---\ncategories: [deal]\nslug: renamed-deal\n---\n# Renamed Deal\n")
	write("properties/one.md", "---\ncategories: [property]\naddress: One\ndeal: '[[renamed-deal]]'\n---\n")
	if _, err := f.srv.index.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if w = do(pub, "GET", endpoint, "", c); w.Code != 200 || !strings.Contains(w.Body.String(), "Renamed Deal") {
		t.Fatal("renamed deal broke existing share", w.Code, w.Body)
	}
	if w = do(private, "GET", "/api/deals/renamed-deal/shares", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), result.Share.ID) {
		t.Fatal("renamed deal lost share management", w.Code, w.Body)
	}
	if w = do(private, "DELETE", "/api/deals/elsewhere/shares/"+result.Share.ID, "", nil); w.Code != 404 {
		t.Fatal("cross-deal revoke")
	}
	if w = do(private, "DELETE", "/api/deals/duo/shares/"+result.Share.ID, "", nil); w.Code != 204 {
		t.Fatal("revoke", w.Code)
	}
	for _, path := range []string{endpoint, endpoint + "/document?ref=system/realestate/docs/one/plan.pdf"} {
		if w = do(pub, "GET", path, "", c); w.Code != 404 {
			t.Fatal("revoked link still works")
		}
	}
	restarted, err := newDealShareStore(filepath.Join(dir, "deal-sharing"))
	if err != nil || !restarted.shares[result.Share.ID].Revoked {
		t.Fatal("revocation not durable")
	}
}
func TestLenderProjectionExcludesInternalFields(t *testing.T) {
	out := &dealUnderwriting{Source: json.RawMessage(`{"private":"PRIVATE_MARKER","deal_underwriting":{"source":"PRIVATE_MARKER","openItems":["PRIVATE_MARKER"],"title":"Deal","properties":[],"packageAssumptions":{"name":"Ask","values":{"rent_growth":0.03}},"corrections":["PRIVATE_MARKER"]}}`)}
	// JSON projection accepts the empty-member shape from the builder.
	out.Members = []realestate.Property{}
	out.Sources = map[string]json.RawMessage{}
	out.Contracts = []realestate.Contract{}
	v, err := lenderProjection(out)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	if bytes.Contains(b, []byte("PRIVATE_MARKER")) {
		t.Fatal("private metadata escaped")
	}
	if !bytes.Contains(b, []byte("rent_growth")) {
		t.Fatal("lost assumptions")
	}
}
func TestLenderShareExpiry(t *testing.T) {
	now := time.Now()
	if shareActive(dealShare{ID: "x", Expires: now}, now) || shareActive(dealShare{ID: "x", Expires: now.Add(time.Hour), Revoked: true}, now) {
		t.Fatal("expired/revoked share active")
	}
}
