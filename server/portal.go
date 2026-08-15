package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"manifest/teamportal"
)

// PortalOptions wires the team write layer (portal move, Phase 2–3) into the
// standalone portal listener. The zero value serves the Phase-1 static site
// unchanged (open read, no auth, no writes).
type PortalOptions struct {
	Auth       *teamportal.Auth  // Google OAuth + sessions (nil → no sign-in)
	Store      *teamportal.Store // team state on /shared (nil → no writes)
	AdminEmail string            // the portal owner — may decide any proposal
}

// PortalHandler serves the AION portal as a standalone site, rooted at the
// embedded web/portal subtree (the copy of the aionbio public/portal app).
//
// This is a SEPARATE mux from Handler(): it is mounted on its own listener
// (cfg.PortalPort, default 7778) and shares nothing with the dashboard's
// routes. GET / returns web/portal/index.html; every other path resolves
// against the portal's own assets (src/*, data/*, content/*), which the app
// requests with document-relative URLs — so the portal is self-contained at
// the root of its port.
//
// Phase 2–3 (2026-08-14): Google sign-in (@aion.bio only) now gates the WHOLE
// portal — view and write alike (requireSignIn below). Within a session, the
// write endpoints keep their finer locks — comments (any member), item PATCH
// (assignee only), team/ adds (self), and proposals for others (owner- or
// target-approved). Team state lives in opt.Store, merged client-side over the
// published data files.
func PortalHandler(opt PortalOptions) (http.Handler, error) {
	sub, err := fs.Sub(webFiles, "web/portal")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/", noCache(etagFor(sub), http.FileServer(http.FS(sub))))

	if opt.Auth != nil {
		mux.HandleFunc("GET /oauth2/login", opt.Auth.HandleLogin)
		mux.HandleFunc("GET /oauth2/callback", opt.Auth.HandleCallback)
		mux.HandleFunc("POST /oauth2/logout", opt.Auth.HandleLogout)
	}
	api := &portalAPI{opt: opt, people: portalPeople(sub), owners: portalOwners(sub)}
	mux.HandleFunc("GET /api/me", api.handleMe)
	if opt.Store != nil {
		mux.HandleFunc("GET /api/team/state", api.handleState)
		if opt.Auth != nil {
			mux.HandleFunc("POST /api/team/comment", api.handleComment)
			mux.HandleFunc("DELETE /api/team/comment", api.handleDeleteComment)
			mux.HandleFunc("PATCH /api/team/item/{id...}", api.handlePatch)
			mux.HandleFunc("POST /api/team/items", api.handleAdd)
			mux.HandleFunc("POST /api/team/proposals", api.handlePropose)
			mux.HandleFunc("POST /api/team/proposals/decide", api.handleDecide)
		}
	}
	loginPage, _ := fs.ReadFile(sub, "login.html") // the anonymous landing (nil → fall back to a redirect)
	return requireSignIn(opt, mux, loginPage), nil
}

// requireSignIn gates the WHOLE portal behind Google sign-in: the only way in
// is an @aion.bio session (2026-08-14 — "login to view", tightening the earlier
// open-read stance). The /oauth2/* endpoints stay anonymous — they ARE the way
// in and out — but the app shell, its assets, and every /api route now need a
// valid session. A signed-out browser navigation is bounced to Google; a
// signed-out asset/XHR fetch gets a plain 401 (no redirect for a data request).
//
// When Auth is nil (no OAuth client on this host) the gate is a no-op: with no
// way to sign in, locking would brick the site — the same graceful degradation
// as the zero-value "serve the static Phase-1 site" path.
func requireSignIn(opt PortalOptions, inner http.Handler, loginPage []byte) http.Handler {
	if opt.Auth == nil {
		return inner
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The oauth endpoints AND the portal's public branding assets (logo,
		// favicon, fonts, colour tokens) are the only anonymous surface — the
		// landing page shows the logo to signed-out visitors by design, so those
		// files aren't secret. Everything else needs a session.
		if strings.HasPrefix(r.URL.Path, "/oauth2/") ||
			(r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/assets/")) {
			inner.ServeHTTP(w, r)
			return
		}
		if _, ok := opt.Auth.Identify(r); ok {
			inner.ServeHTTP(w, r)
			return
		}
		// Anonymous browser navigation → the branded landing page (logo + a
		// Sign in button), NOT an auto-redirect to Google. Auto-redirecting let
		// Google silently re-authenticate a still-live session, so a signed-out
		// user could never stay out; the landing keeps sign-in a deliberate
		// click. A data/XHR fetch still gets a plain 401.
		if isBrowserNav(r) {
			if len(loginPage) > 0 {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(http.StatusOK)
				w.Write(loginPage)
				return
			}
			http.Redirect(w, r, "/oauth2/login", http.StatusFound) // fallback if the page is missing
			return
		}
		http.Error(w, "sign in with your @"+teamportal.Domain+" account to view this portal", http.StatusUnauthorized)
	})
}

// isBrowserNav reports whether r is a top-level document navigation (rather than
// an XHR/fetch or a sub-resource load) — only those get the 302-to-Google, so
// data fetches receive a clean 401 the app can handle. Sec-Fetch-Mode is the
// reliable signal on modern browsers; Accept: text/html is the fallback.
func isBrowserNav(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if m := r.Header.Get("Sec-Fetch-Mode"); m != "" {
		return m == "navigate"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// portalAPI is the team write surface. It resolves emails to the roster's
// initials (people.json + optional emails.json overrides in the team dir) and
// enforces the assignee lock against the published backlog + team items.
type portalAPI struct {
	opt    PortalOptions
	people []portalPerson
	owners map[string]string // published item id → owner initials
}

type portalPerson struct {
	Initials string `json:"initials"`
	Name     string `json:"name"`
}

// portalPeople parses the embedded people.json roster (nil-safe on drift).
func portalPeople(sub fs.FS) []portalPerson {
	var doc struct {
		People []portalPerson `json:"people"`
	}
	if b, err := fs.ReadFile(sub, "data/people.json"); err == nil {
		_ = json.Unmarshal(b, &doc)
	}
	return doc.People
}

// portalOwners maps published backlog item ids to their owner initials.
func portalOwners(sub fs.FS) map[string]string {
	var doc struct {
		Items []struct {
			ID    string `json:"id"`
			Owner string `json:"owner"`
		} `json:"items"`
	}
	out := map[string]string{}
	if b, err := fs.ReadFile(sub, "data/backlog.json"); err == nil {
		_ = json.Unmarshal(b, &doc)
	}
	for _, it := range doc.Items {
		out[it.ID] = it.Owner
	}
	return out
}

// initialsFor resolves an @aion.bio email to roster initials: the hand-edited
// emails.json override wins; else the email local part must equal a person's
// first name or their initials (benjamin@ → Benjamin Anderson → BA). Empty
// when unmapped — such a member can comment and add, but owns nothing
// published.
func (p *portalAPI) initialsFor(email string) string {
	local := strings.ToLower(strings.TrimSpace(strings.SplitN(email, "@", 2)[0]))
	if local == "" {
		return ""
	}
	if p.opt.Store != nil {
		if ini, ok := p.opt.Store.EmailOverrides()[strings.ToLower(strings.TrimSpace(email))]; ok {
			return ini
		}
	}
	for _, per := range p.people {
		first := strings.ToLower(strings.SplitN(strings.TrimSpace(per.Name), " ", 2)[0])
		if local == first || strings.EqualFold(local, per.Initials) {
			return per.Initials
		}
	}
	return ""
}

// ownerToken is the owner written onto items a member creates: their roster
// initials, or the email local part when unmapped (still identifiable).
func (p *portalAPI) ownerToken(email string) string {
	if ini := p.initialsFor(email); ini != "" {
		return ini
	}
	return strings.SplitN(email, "@", 2)[0]
}

// ownerOf finds an item's assignee: published backlog first, then team items.
func (p *portalAPI) ownerOf(itemID string) (string, bool) {
	if o, ok := p.owners[itemID]; ok {
		return o, true
	}
	if p.opt.Store != nil {
		return p.opt.Store.TeamOwner(itemID)
	}
	return "", false
}

func (p *portalAPI) isAdmin(email string) bool {
	return p.opt.AdminEmail != "" && strings.EqualFold(email, p.opt.AdminEmail)
}

// identify authenticates a write request; 401 (with a clear message) when
// anonymous or expired.
func (p *portalAPI) identify(w http.ResponseWriter, r *http.Request) (teamportal.Identity, bool) {
	if p.opt.Auth == nil {
		http.Error(w, "sign-in is not configured", http.StatusServiceUnavailable)
		return teamportal.Identity{}, false
	}
	id, ok := p.opt.Auth.Identify(r)
	if !ok {
		http.Error(w, "sign in with your @"+teamportal.Domain+" account to write", http.StatusUnauthorized)
		return teamportal.Identity{}, false
	}
	return id, true
}

// handleMe reports the caller's identity to the portal UI (open endpoint —
// anonymous readers get {anon:true}).
func (p *portalAPI) handleMe(w http.ResponseWriter, r *http.Request) {
	if p.opt.Auth == nil {
		writeJSON(w, map[string]any{"anon": true, "authConfigured": false})
		return
	}
	id, ok := p.opt.Auth.Identify(r)
	if !ok {
		writeJSON(w, map[string]any{"anon": true, "authConfigured": p.opt.Auth.Enabled()})
		return
	}
	writeJSON(w, map[string]any{
		"email": id.Email, "name": id.Name,
		"initials": p.initialsFor(id.Email),
		"admin":    p.isAdmin(id.Email),
	})
}

// handleState serves the whole team overlay — open read, like the site.
func (p *portalAPI) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, p.opt.Store.Ext())
}

func (p *portalAPI) handleComment(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct{ Item, Text string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, exists := p.ownerOf(b.Item); !exists {
		http.Error(w, "unknown item", http.StatusNotFound)
		return
	}
	c, err := p.opt.Store.AddComment(id, b.Item, b.Text, time.Now())
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, c)
}

// handleDeleteComment removes a comment. The store enforces the permission
// (author or admin); we map its sentinels to 404/403 so the UI can tell "gone"
// from "not yours".
func (p *portalAPI) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct{ Item, ID string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	switch err := p.opt.Store.DeleteComment(id, b.Item, b.ID, p.isAdmin(id.Email), time.Now()); {
	case err == nil:
		writeJSON(w, map[string]any{"deleted": b.ID})
	case errors.Is(err, teamportal.ErrCommentNotFound):
		http.Error(w, "comment not found", http.StatusNotFound)
	case errors.Is(err, teamportal.ErrNotYours):
		http.Error(w, err.Error(), http.StatusForbidden)
	default:
		httpError(w, errBadRequest(err.Error()))
	}
}

// handlePatch is the owner-lock write: ONLY the item's assignee may change its
// status/fields (the admin included — no override lane, decided 2026-08-13).
func (p *portalAPI) handlePatch(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	itemID := r.PathValue("id")
	owner, exists := p.ownerOf(itemID)
	if !exists {
		http.Error(w, "unknown item", http.StatusNotFound)
		return
	}
	mine := p.ownerToken(id.Email)
	if owner == "" || !strings.EqualFold(owner, mine) {
		http.Error(w, "only the assignee ("+orDash(owner)+") can change this item", http.StatusForbidden)
		return
	}
	var fields map[string]string
	if err := decode(r, &fields); err != nil {
		httpError(w, err)
		return
	}
	ov, err := p.opt.Store.Patch(id, itemID, fields, time.Now())
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]any{"item": itemID, "override": ov})
}

// handleAdd creates the caller's own team/ item (direct, no approval).
func (p *portalAPI) handleAdd(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct{ Kind, Title, Rock, Due string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	it, err := p.opt.Store.AddItem(id, p.ownerToken(id.Email), b.Kind, b.Title, b.Rock, b.Due, time.Now())
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, it)
}

// handlePropose files an item for someone else — a proposal until the portal
// owner or the target approves it.
func (p *portalAPI) handlePropose(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct{ Target, Kind, Title, Rock, Due string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	prop, err := p.opt.Store.Propose(id, b.Target, p.ownerToken(b.Target), b.Kind, b.Title, b.Rock, b.Due, time.Now())
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, prop)
}

// handleDecide resolves a proposal. Authorized deciders: the portal owner
// (admin) or the proposal's target — either suffices (approvals-model mirror).
func (p *portalAPI) handleDecide(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
	}
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("proposal id is required"))
		return
	}
	target := ""
	for _, prop := range p.opt.Store.Ext().Proposals {
		if prop.ID == b.ID {
			target = prop.Target
			break
		}
	}
	if target == "" {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}
	if !p.isAdmin(id.Email) && !strings.EqualFold(id.Email, target) {
		http.Error(w, "only "+target+" or the portal owner can decide this", http.StatusForbidden)
		return
	}
	prop, err := p.opt.Store.Decide(id, b.ID, b.Approve, time.Now())
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, prop)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
