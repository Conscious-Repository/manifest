package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Share credentials are generated with 192 bits of entropy. Only hashes persist;
// short-lived browser sessions are memory-only and become invalid on restart.
type dealShare struct {
	ID           string    `json:"id"`
	Slug         string    `json:"slug"`
	Label        string    `json:"label"`
	Created      time.Time `json:"created"`
	Expires      time.Time `json:"expires"`
	Revoked      bool      `json:"revoked"`
	PasswordHash string    `json:"passwordHash,omitempty"`
}
type dealShareSession struct {
	ID      string
	Expires time.Time
}
type dealShareAttempt struct {
	Count int
	Until time.Time
}
type dealShareStore struct {
	mu       sync.Mutex
	file     string
	shares   map[string]dealShare
	sessions map[string]dealShareSession
	attempts map[string]dealShareAttempt
}

func newDealShareStore(dir string) (*dealShareStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	st := &dealShareStore{file: filepath.Join(dir, "deal-shares.json"), shares: map[string]dealShare{}, sessions: map[string]dealShareSession{}, attempts: map[string]dealShareAttempt{}}
	b, err := os.ReadFile(st.file)
	if err == nil {
		err = json.Unmarshal(b, &st.shares)
	} else if os.IsNotExist(err) {
		err = nil
	}
	return st, err
}
func (s *Server) UseDealShares(dir string) error {
	st, err := newDealShareStore(filepath.Join(dir, "deal-sharing"))
	if err == nil {
		s.dealShares = st
	}
	return err
}
func shareSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func shareHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (st *dealShareStore) saveLocked() error {
	b, err := json.Marshal(st.shares)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(st.file), ".shares-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		err = os.Rename(f.Name(), st.file)
	}
	return err
}
func shareActive(sh dealShare, now time.Time) bool {
	return sh.ID != "" && !sh.Revoked && now.Before(sh.Expires)
}
func (s *Server) handleDealShares(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	st := s.dealShares
	if st == nil {
		http.Error(w, "Deal sharing unavailable", 503)
		return
	}
	slug := r.PathValue("slug")
	if _, ok := s.dealBySlug(slug); !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == "GET" {
		st.mu.Lock()
		defer st.mu.Unlock()
		items := []dealShare{}
		for _, sh := range st.shares {
			if sh.Slug == slug {
				sh.PasswordHash = ""
				items = append(items, sh)
			}
		}
		writeJSON(w, map[string]any{"shares": items})
		return
	}
	if !shareSameOrigin(r) {
		http.Error(w, "Invalid origin", 403)
		return
	}
	if r.Method == "DELETE" {
		st.mu.Lock()
		defer st.mu.Unlock()
		id := r.PathValue("id")
		old, ok := st.shares[id]
		if !ok || old.Slug != slug {
			http.NotFound(w, r)
			return
		}
		sh := old
		sh.Revoked = true
		st.shares[id] = sh
		if err := st.saveLocked(); err != nil {
			st.shares[id] = old
			http.Error(w, "Could not revoke link", 500)
			return
		}
		w.WriteHeader(204)
		return
	}
	var input struct {
		Label string `json:"label"`
		Days  int    `json:"days"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || strings.TrimSpace(input.Label) == "" || len(input.Label) > 120 || input.Days < 1 || input.Days > 365 {
		http.Error(w, "Enter a label and expiration of 1–365 days", 400)
		return
	}
	if _, err := s.buildDealUnderwriting(slug); err != nil {
		http.Error(w, "Deal package unavailable", 503)
		return
	}
	password := shareSecret(24)
	sh := dealShare{ID: shareSecret(16), Slug: slug, Label: strings.TrimSpace(input.Label), Created: time.Now().UTC(), Expires: time.Now().UTC().Add(time.Duration(input.Days) * 24 * time.Hour), PasswordHash: shareHash(password)}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.shares[sh.ID] = sh
	if err := st.saveLocked(); err != nil {
		delete(st.shares, sh.ID)
		http.Error(w, "Could not create link", 500)
		return
	}
	sh.PasswordHash = ""
	writeJSON(w, map[string]any{"share": sh, "password": password, "path": "/lender/" + sh.ID + "/"})
}
func shareSameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

// This wrapper is mounted only on OODA's listener. Nothing falls through from
// /lender to a team route, including after a successful lender authentication.
func (s *Server) DealShareHandler(team http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /lender/{id}/", s.handleLender)
	mux.HandleFunc("POST /lender/{id}/unlock", s.handleLender)
	mux.HandleFunc("POST /lender/{id}/logout", s.handleLender)
	mux.HandleFunc("GET /lender/{id}/underwriting", s.handleLender)
	mux.HandleFunc("GET /lender/{id}/underwriting/document", s.handleLender)
	mux.HandleFunc("GET /lender-assets/{asset}", func(w http.ResponseWriter, r *http.Request) {
		assets := map[string]string{"screening.js": "web/ooda/src/re-screening.js", "underwriting.js": "web/ooda/src/deal-underwriting.js", "underwriting.css": "web/ooda/src/deal-underwriting.css", "base.css": "web/ooda/src/ooda.css", "app.js": "web/lender/app.js", "shell.css": "web/lender/shell.css"}
		p, ok := assets[r.PathValue("asset")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		b, err := webFiles.ReadFile(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(p, ".js") {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/lender/") || strings.HasPrefix(r.URL.Path, "/lender-assets/") {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Cache-Control", "no-store")
			mux.ServeHTTP(w, r)
			return
		}
		team.ServeHTTP(w, r)
	})
}
func (s *Server) handleLender(w http.ResponseWriter, r *http.Request) {
	st := s.dealShares
	if st == nil {
		http.NotFound(w, r)
		return
	}
	id := r.PathValue("id")
	base := "/lender/" + id + "/"
	now := time.Now()
	st.mu.Lock()
	sh := st.shares[id]
	st.mu.Unlock()
	if !shareActive(sh, now) {
		http.Error(w, "This link is expired, revoked, or unavailable. Ask the sender for a new link.", 404)
		return
	}
	// All API-like operations require an exact path, not the trailing-slash fallback.
	if r.URL.Path == base {
		b, _ := webFiles.ReadFile("web/lender/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Write(b)
		return
	}
	cookieName := "lender_session"
	if r.URL.Path == base+"unlock" && r.Method == "POST" {
		if !shareSameOrigin(r) {
			http.Error(w, "Invalid origin", 403)
			return
		}
		var input struct {
			Password string `json:"password"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil {
			http.Error(w, "Enter the link password", 400)
			return
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		sh = st.shares[id]
		if !shareActive(sh, now) {
			http.NotFound(w, r)
			return
		}
		a := st.attempts[id]
		if now.After(a.Until) {
			a = dealShareAttempt{Until: now.Add(time.Minute)}
		}
		if a.Count >= 10 {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too many attempts. Try again in a minute.", 429)
			return
		}
		a.Count++
		st.attempts[id] = a
		if subtle.ConstantTimeCompare([]byte(shareHash(input.Password)), []byte(sh.PasswordHash)) != 1 {
			http.Error(w, "Incorrect password", 401)
			return
		}
		token := shareSecret(32)
		for k, v := range st.sessions {
			if !now.Before(v.Expires) {
				delete(st.sessions, k)
			}
		}
		st.sessions[shareHash(token)] = dealShareSession{ID: id, Expires: now.Add(8 * time.Hour)}
		delete(st.attempts, id)
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: base, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 28800})
		w.WriteHeader(204)
		return
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		http.Error(w, "Enter the link password to continue", 401)
		return
	}
	st.mu.Lock()
	session, ok := st.sessions[shareHash(c.Value)]
	st.mu.Unlock()
	if !ok || session.ID != id || !now.Before(session.Expires) {
		http.Error(w, "Session expired. Enter the link password again.", 401)
		return
	}
	if r.URL.Path == base+"logout" && r.Method == "POST" {
		if !shareSameOrigin(r) {
			http.Error(w, "Invalid origin", 403)
			return
		}
		st.mu.Lock()
		delete(st.sessions, shareHash(c.Value))
		st.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: cookieName, Path: base, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.WriteHeader(204)
		return
	}
	r.SetPathValue("slug", sh.Slug)
	if r.URL.Path == base+"underwriting/document" {
		s.handleDealUnderwritingDoc(w, r)
		return
	}
	if r.URL.Path != base+"underwriting" {
		http.NotFound(w, r)
		return
	}
	out, err := s.buildDealUnderwriting(sh.Slug)
	if err != nil {
		http.Error(w, "Could not load this deal", 503)
		return
	}
	view, err := lenderProjection(out)
	if err != nil {
		http.Error(w, "Could not prepare this deal", 500)
		return
	}
	writeJSON(w, view)
}

// Explicit allowlists keep owner-only correspondence, source notes, task chats,
// bank import metadata and unrelated portfolio records out of the lender JSON.
func sharePick(v any, keys string) map[string]any {
	m, _ := v.(map[string]any)
	out := map[string]any{}
	for _, k := range strings.Fields(keys) {
		if x, ok := m[k]; ok {
			out[k] = x
		}
	}
	return out
}
func shareRows(v any, keys string) []any {
	rows := []any{}
	a, _ := v.([]any)
	for _, x := range a {
		rows = append(rows, sharePick(x, keys))
	}
	return rows
}
func lenderProjection(out *dealUnderwriting) (map[string]any, error) {
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err = json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	src := sharePick(raw["source"], "rent_growth opex_growth hold_years selling_cost_pct exit_cap_rate")
	original, _ := raw["source"].(map[string]any)
	basis := original["deal_underwriting"]
	if basis == nil {
		basis = original["lender_diligence"]
	}
	clean := sharePick(basis, "title contingencyPct fundEquityShare partnerEquityShare structure repayment constructionRate reserveMonths termMonths refinanceRate refinanceAmortYears refinanceLtvLow refinanceLtvHigh")
	bm, _ := basis.(map[string]any)
	clean["properties"] = shareRows(bm["properties"], "slug acquisition hardCostsIncludingContingency softCosts baseLoan interestReserve phase units")
	clean["presentationFinancing"] = sharePick(bm["presentationFinancing"], "enabled constructionLtc constructionRate reserveMonths termMonths refinanceRate refinanceAmortYears refinanceLtvLow refinanceLtvHigh")
	if bm["packageAssumptions"] != nil {
		clean["packageAssumptions"] = sharePick(bm["packageAssumptions"], "name values dates valuationBasis")
	}
	facts := map[string]any{}
	if fm, ok := bm["documentFacts"].(map[string]any); ok {
		for ref, v := range fm {
			if out.allowed[ref] {
				facts[ref] = sharePick(v, "type sheetDate sheetCount existingUnits proposedUnits grossAboveGradeSF grossBasementSF indexedSheetsNotIncluded sourceSheets drafterDesignation")
			}
		}
	}
	clean["documentFacts"] = facts
	evidence := map[string]any{}
	if em, ok := bm["diligenceEvidence"].(map[string]any); ok {
		for slug, v := range em {
			if _, member := out.Docs[slug]; !member {
				continue
			}
			entries := map[string]any{}
			if m, ok := v.(map[string]any); ok {
				for key, value := range m {
					entries[key] = sharePick(value, "path")
				}
			}
			evidence[slug] = entries
		}
	}
	clean["diligenceEvidence"] = evidence
	clean["supportingDocuments"] = shareRows(bm["supportingDocuments"], "title path")
	clean["unmatchedPayments"] = shareRows(bm["unmatchedPayments"], "description amount")
	src["deal_underwriting"] = clean
	members := []any{}
	for _, v := range raw["members"].([]any) {
		m := v.(map[string]any)
		p := sharePick(m, "slug short address entity status units rentMonthly unitMix")
		p["ledger"] = shareRows(m["ledger"], "date type category cat vendor amount status doc")
		p["unitMix"] = shareRows(m["unitMix"], "label beds baths sqft rent")
		phases := shareRows(m["work"], "text checked done estTotal weeks fields")
		for _, phase := range phases {
			pm := phase.(map[string]any)
			fields := []any{}
			if fs, ok := pm["fields"].([]any); ok {
				for _, field := range fs {
					fm, _ := field.(map[string]any)
					if fm["key"] == "soft-budget" {
						fields = append(fields, sharePick(fm, "key value"))
					}
				}
			}
			pm["fields"] = fields
		}
		p["work"] = phases
		members = append(members, p)
	}
	sources := map[string]any{}
	for k, v := range raw["sources"].(map[string]any) {
		sources[k] = sharePick(v, "total_units avg_rent_per_unit purchase_price closing_costs hard_costs carry_cost phase_costs_include_contingency")
	}
	view := map[string]any{"deal": sharePick(raw["deal"], "name slug"), "source": src, "members": members, "sources": sources, "docs": raw["docs"], "contracts": shareRows(raw["contracts"], "name status allocations doc"), "assumptions": raw["assumptions"], "revision": out.Revision}
	return view, nil
}
