package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
	"time"

	"manifest/spirits"
	"manifest/teamportal"
)

// PortalOptions wires the team write layer (portal move, Phase 2–3) into the
// standalone portal listener. The zero value serves the Phase-1 static site
// unchanged (open read, no auth, no writes).
//
// The agent-loop callbacks (kairos plan Phases C/D) bridge the portal into
// the cockpit Server's delegation machinery — both listeners live in one
// process, so these are plain closures wired in main. All nil-safe: a nil
// callback means the route isn't registered / the hook is skipped.
type PortalOptions struct {
	Auth   *teamportal.Auth       // Google OAuth + sessions (nil → no sign-in)
	Tokens *teamportal.TokenStore // revocable per-user API tokens (nil → no API access panel)
	Store  *teamportal.Store      // team state on /shared (nil → no writes)
	Live   PortalLive             // live base + team overlay (nil → embedded fallback)
	// WebRoot is the embedded subtree this portal's app shell is served from
	// ("" → "web/portal"). login.html and the anonymous /assets/ exemption
	// follow it automatically.
	WebRoot string
	// ReadRoutes registers this portal's OWN read surface ("" → the AION
	// data-file routes). The WRITE routes below are registered unconditionally
	// and are never duplicated per portal — one copy of the permission model.
	ReadRoutes func(mux *http.ServeMux, opt PortalOptions)
	AdminEmail string // the portal owner — may decide any proposal
	// OnComment runs after a member's comment is stored — the dialog hook
	// (mention → auto-assign + relay; assigned item → every comment relays).
	OnComment func(itemID string, mentions []string, text string)
	// Agents lists the TEAM-surface roster ({id,name,harness,personas}).
	Agents func() []map[string]any
	// Panel returns an item's plan record + delegation state.
	Panel func(itemID string) map[string]any
	// Assign assigns an agent (or clears); actor = the member, for attribution.
	Assign func(itemID, owner, memberEmail, memberName string) error
	// Fire executes the plan; returns spirits.ErrAlreadyActive when mid-run.
	Fire func(itemID, memberEmail, memberName string) error
	// Activity returns one item's team activity trail (portal v2 stream).
	Activity func(itemID string) []map[string]any
	// Panel plan-section write (portal v2 plan editor); handler gates
	// assignee/admin before calling.
	PlanWrite func(itemID, section, text string) error
	// FileBlob resolves a comment-attachment hash to a served file path.
	FileBlob func(hash string) string
	// --- native chat with kairos (chat-kairos handoff) ---
	// ChatThreads returns threads + messages + the engine snapshot (heartbeat,
	// active claim, pending). ChatThread is create/rename/rescope/archive/reopen.
	// ChatAsk spools an ask/delegate run. ChatEngine is the sidebar snapshot.
	// ChatProposal applies/discards a proposal (assignee/admin gated in the
	// bridge). All nil-safe — chat routes register only when wired.
	ChatThreads  func() map[string]any
	ChatThread   func(op, id, title, rock, memberEmail, memberName string) (map[string]any, error)
	ChatAsk      func(thread, text, ritual string, context []string, memberEmail, memberName string) error
	ChatEngine   func() map[string]any
	ChatProposal func(thread, msg string, index int, apply bool, memberEmail, memberName, memberInitials string, admin bool) error
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
// target-approved). Team state lives in opt.Store and AionLive composes it
// server-side over the owner-authored base.
func PortalHandler(opt PortalOptions) (http.Handler, error) {
	// A typed-nil projection (a nil *AionLive stored in the interface) is
	// NOT nil to `!= nil`, so it would pass the read-route check below and
	// panic on the first request. Normalize it away here rather than trusting
	// every caller to (ooda-portal plan, Stage A step 5).
	opt.Live = liveOrNil(opt.Live)
	webRoot := opt.WebRoot
	if webRoot == "" {
		webRoot = "web/portal"
	}
	sub, err := fs.Sub(webFiles, webRoot)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/", noCache(etagFor(sub), http.FileServer(http.FS(sub))))
	if opt.Live != nil {
		if opt.ReadRoutes != nil {
			opt.ReadRoutes(mux, opt)
		} else {
			aionReadRoutes(mux, opt.Live)
		}
	}

	if opt.Auth != nil {
		mux.HandleFunc("GET /oauth2/login", opt.Auth.HandleLogin)
		mux.HandleFunc("GET /oauth2/callback", opt.Auth.HandleCallback)
		mux.HandleFunc("POST /oauth2/logout", opt.Auth.HandleLogout)
	}
	api := &portalAPI{
		opt: opt, people: portalPeople(sub), owners: portalOwners(sub),
		published: portalPublishedState(sub),
	}
	mux.HandleFunc("GET /api/me", api.handleMe)
	if opt.Auth != nil && opt.Tokens != nil {
		mux.HandleFunc("GET /api/tokens", api.handleTokens)
		mux.HandleFunc("POST /api/tokens", api.handleCreateToken)
		mux.HandleFunc("DELETE /api/tokens/{id}", api.handleRevokeToken)
	}
	if opt.Store != nil {
		mux.HandleFunc("GET /api/team/state", api.handleState)
		mux.HandleFunc("GET /api/team/snapshot", api.handleSnapshot)
		if opt.Auth != nil {
			mux.HandleFunc("POST /api/team/comment", api.handleComment)
			mux.HandleFunc("DELETE /api/team/comment", api.handleDeleteComment)
			mux.HandleFunc("PATCH /api/team/item/{id...}", api.handlePatch)
			mux.HandleFunc("POST /api/team/items", api.handleAdd)
			mux.HandleFunc("POST /api/team/proposals", api.handlePropose)
			mux.HandleFunc("POST /api/team/proposals/decide", api.handleDecide)
			// the agent loop on the portal (kairos plan Phases C/D) — routes
			// exist only when the cockpit bridges are wired. Item ids carry
			// slashes (team/<slug>), so ids ride the body/query, never the path.
			if opt.Agents != nil {
				mux.HandleFunc("GET /api/team/agents", api.handleAgents)
			}
			if opt.Panel != nil {
				mux.HandleFunc("GET /api/team/panel", api.handlePanel)
			}
			if opt.Assign != nil {
				mux.HandleFunc("POST /api/team/assign", api.handleAssign)
			}
			if opt.Fire != nil {
				mux.HandleFunc("POST /api/team/fire", api.handleFire)
			}
			// portal v2: item activity trail, plan-section writes, attachments
			if opt.Activity != nil {
				mux.HandleFunc("GET /api/team/activity", api.handleActivity)
			}
			if opt.PlanWrite != nil {
				mux.HandleFunc("POST /api/team/plan", api.handlePlanWrite)
			}
			if opt.FileBlob != nil {
				mux.HandleFunc("GET /api/team/file/{hash}", api.handleFileBlob)
			}
			// native chat with kairos (chat-kairos handoff)
			if opt.ChatThreads != nil {
				mux.HandleFunc("GET /api/chat/threads", api.handleChatThreads)
			}
			if opt.ChatThread != nil {
				mux.HandleFunc("POST /api/chat/thread", api.handleChatThread)
			}
			if opt.ChatAsk != nil {
				mux.HandleFunc("POST /api/chat/ask", api.handleChatAsk)
			}
			if opt.ChatEngine != nil {
				mux.HandleFunc("GET /api/chat/engine", api.handleChatEngine)
			}
			if opt.ChatProposal != nil {
				mux.HandleFunc("POST /api/chat/proposal", api.handleChatProposal)
			}
		}
	}
	loginPage, _ := fs.ReadFile(sub, "login.html") // the anonymous landing (nil → fall back to a redirect)
	return requireSignIn(opt, mux, loginPage), nil
}

// requireSignIn gates the WHOLE portal behind an @aion.bio identity: browsers
// use a Google-backed session, while scripts may use a per-user bearer token.
// The /oauth2/* endpoints stay anonymous — they are the browser's way in and
// out — but the app shell, its assets, and every /api route need a valid
// identity. A signed-out browser navigation gets the login page; a signed-out
// asset/XHR fetch gets a plain 401 (no redirect for a data request).
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
		if _, ok := opt.Auth.IdentifyRequest(r); ok {
			inner.ServeHTTP(w, r)
			return
		}
		// Authorization headers are API attempts, never browser navigation. A
		// bad token must receive a machine-readable status, not login HTML.
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "invalid or revoked bearer token", http.StatusUnauthorized)
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
		http.Error(w, "sign in with your @"+opt.Auth.Domain()+" account to view this portal", http.StatusUnauthorized)
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

func handleAionLiveFile(live PortalLive, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, rev, err := live.File(path)
		if err != nil {
			http.Error(w, "live Aion contract unavailable", http.StatusServiceUnavailable)
			return
		}
		etag := `"` + rev + `"`
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if strings.HasSuffix(path, ".json") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		}
		_, _ = w.Write(b)
	}
}

func handleAionLiveRevision(live PortalLive) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := live.Status()
		etag := `"` + st.EffectiveRevision + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeJSON(w, st)
	}
}

// portalAPI is the team write surface. It resolves emails to the roster's
// initials (people.json + optional emails.json overrides in the team dir) and
// enforces the assignee lock against the published backlog + team items.
type portalAPI struct {
	opt       PortalOptions
	people    []portalPerson
	owners    map[string]string // published item id → owner initials
	published map[string]any    // immutable embedded backlog/goals/people/meta
}

type portalPerson struct {
	Initials string `json:"initials"`
	Name     string `json:"name"`
	Email    string `json:"email,omitempty"`
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
	full := strings.ToLower(strings.TrimSpace(email))
	local := strings.ToLower(strings.TrimSpace(strings.SplitN(email, "@", 2)[0]))
	if local == "" {
		return ""
	}
	// hand-edited emails.json override wins (per-machine, top priority)
	if p.opt.Store != nil {
		if ini, ok := p.opt.Store.EmailOverrides()[full]; ok {
			return ini
		}
	}
	// the roster's own email field — the deterministic association (people.md)
	for _, per := range p.peopleList() {
		if per.Email != "" && strings.EqualFold(strings.TrimSpace(per.Email), full) {
			return per.Initials
		}
	}
	// fallback for accounts with no explicit email: first-name / initials match
	for _, per := range p.peopleList() {
		first := strings.ToLower(strings.SplitN(strings.TrimSpace(per.Name), " ", 2)[0])
		if local == first || strings.EqualFold(local, per.Initials) {
			return per.Initials
		}
	}
	return ""
}

// personName resolves an email to the roster person's display name ("" when
// unmapped) — the portal's "signed in as" confirmation.
func (p *portalAPI) personName(email string) string {
	ini := p.initialsFor(email)
	if ini == "" {
		return ""
	}
	for _, per := range p.peopleList() {
		if strings.EqualFold(per.Initials, ini) {
			return per.Name
		}
	}
	return ""
}

func (p *portalAPI) peopleList() []portalPerson {
	if p.opt.Live != nil {
		return p.opt.Live.People()
	}
	return p.people
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
	if p.opt.Live != nil {
		return p.opt.Live.OwnerOf(itemID)
	}
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
	return PortalIdentify(p.opt, w, r)
}

// PortalIdentify authenticates a portal request exactly the way every shared
// write route does (cookie first, then bearer), writing the 401/503 itself.
// Exported so a portal's own ReadRoutes can gate its routes without
// re-implementing auth — the one thing that must never be copied.
func PortalIdentify(opt PortalOptions, w http.ResponseWriter, r *http.Request) (teamportal.Identity, bool) {
	if opt.Auth == nil {
		http.Error(w, "sign-in is not configured", http.StatusServiceUnavailable)
		return teamportal.Identity{}, false
	}
	id, ok := opt.Auth.IdentifyRequest(r)
	if !ok {
		http.Error(w, "sign in with your @"+opt.Auth.Domain()+" account to write", http.StatusUnauthorized)
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
	id, ok := p.opt.Auth.IdentifyRequest(r)
	if !ok {
		writeJSON(w, map[string]any{"anon": true, "authConfigured": p.opt.Auth.Enabled()})
		return
	}
	writeJSON(w, map[string]any{
		"email": id.Email, "name": id.Name,
		"initials": p.initialsFor(id.Email),
		"person":   p.personName(id.Email),
		"admin":    p.isAdmin(id.Email),
		// canFire is server-side policy the drawer trusts: team-wide today
		// (owner decision 2026-08-16); a single line here tightens it later.
		"canFire": p.opt.Fire != nil,
	})
}

// identifyCookie is intentionally narrower than identify: API tokens may use
// portal capabilities but may not mint or revoke credentials.
func (p *portalAPI) identifyCookie(w http.ResponseWriter, r *http.Request) (teamportal.Identity, bool) {
	if p.opt.Auth == nil {
		http.Error(w, "sign-in is not configured", http.StatusServiceUnavailable)
		return teamportal.Identity{}, false
	}
	id, ok := p.opt.Auth.Identify(r)
	if !ok {
		http.Error(w, "sign in to the portal to manage API tokens", http.StatusUnauthorized)
		return teamportal.Identity{}, false
	}
	return id, true
}

type portalTokenView struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Created  time.Time  `json:"created"`
	LastUsed *time.Time `json:"last_used,omitempty"`
	Revoked  bool       `json:"revoked"`
}

func tokenView(rec teamportal.TokenRecord) portalTokenView {
	return portalTokenView{ID: rec.ID, Label: rec.Label, Created: rec.Created, LastUsed: rec.LastUsed, Revoked: rec.Revoked}
}

func (p *portalAPI) handleTokens(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := p.identifyCookie(w, r)
	if !ok {
		return
	}
	recs := p.opt.Tokens.List(id.Email)
	out := make([]portalTokenView, 0, len(recs))
	for _, rec := range recs {
		out = append(out, tokenView(rec))
	}
	writeJSON(w, map[string]any{"tokens": out})
}

func (p *portalAPI) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := p.identifyCookie(w, r)
	if !ok {
		return
	}
	var b struct {
		Label string `json:"label"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	plain, rec, err := p.opt.Tokens.Mint(id, b.Label)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]any{"id": rec.ID, "token": plain, "label": rec.Label, "created": rec.Created})
}

func (p *portalAPI) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := p.identifyCookie(w, r)
	if !ok {
		return
	}
	if !p.opt.Tokens.Revoke(id.Email, r.PathValue("id")) {
		http.Error(w, "token not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleState serves the whole team overlay — open read, like the site.
func (p *portalAPI) handleState(w http.ResponseWriter, r *http.Request) {
	if p.opt.Live != nil {
		st := p.opt.Live.Status()
		etag := `"` + st.EffectiveRevision + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeJSON(w, p.opt.Live.TeamStateJSON())
		return
	}
	writeJSON(w, p.opt.Store.Ext())
}

// handleSnapshot gives external tools the same read model the portal builds in
// the browser: published items and rocks plus the live multi-writer overlay.
// When the live projection is wired, both published data and team state are
// read on every request; otherwise the embedded published fallback is reused.
func (p *portalAPI) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if p.opt.Live != nil {
		out := map[string]any{"team": p.opt.Live.TeamStateJSON()}
		for key, path := range map[string]string{"backlog": "/data/backlog.json", "goals": "/data/goals.json", "people": "/data/people.json", "meta": "/data/meta.json"} {
			if b, _, err := p.opt.Live.File(path); err == nil {
				var value any
				if json.Unmarshal(b, &value) == nil {
					out[key] = value
				}
			}
		}
		writeJSON(w, out)
		return
	}
	out := make(map[string]any, len(p.published)+1)
	for key, value := range p.published {
		out[key] = value
	}
	out["team"] = p.opt.Store.Ext()
	writeJSON(w, out)
}

func portalPublishedState(sub fs.FS) map[string]any {
	out := make(map[string]any, 4)
	for key, path := range map[string]string{
		"backlog": "data/backlog.json",
		"goals":   "data/goals.json",
		"people":  "data/people.json",
		"meta":    "data/meta.json",
	} {
		b, err := fs.ReadFile(sub, path)
		if err != nil {
			continue
		}
		var value any
		if json.Unmarshal(b, &value) == nil {
			out[key] = value
		}
	}
	return out
}

func (p *portalAPI) handleComment(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct {
		Item, Text string
		Mentions   []string
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, exists := p.ownerOf(b.Item); !exists {
		http.Error(w, "unknown item", http.StatusNotFound)
		return
	}
	c, err := p.opt.Store.AddCommentFull(id, b.Item, b.Text, nil, b.Mentions, time.Now())
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	// the dialog hook (kairos plan Phase C): AFTER the store write, so the
	// relay's thread tail includes this comment. Synchronous, mirroring the
	// dashboard's handleTaskThreadPost; a missed relay is retried by the sweep.
	if p.opt.OnComment != nil {
		p.opt.OnComment(b.Item, b.Mentions, b.Text)
	}
	writeJSON(w, c)
}

// --- the agent loop on the portal (kairos plan Phases C/D) -------------------

func (p *portalAPI) handleAgents(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.identify(w, r); !ok {
		return
	}
	writeJSON(w, map[string]any{"agents": p.opt.Agents()})
}

func (p *portalAPI) handlePanel(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.identify(w, r); !ok {
		return
	}
	itemID := strings.TrimSpace(r.URL.Query().Get("id"))
	if _, exists := p.ownerOf(itemID); !exists {
		http.Error(w, "unknown item", http.StatusNotFound)
		return
	}
	writeJSON(w, p.opt.Panel(itemID))
}

// handleAssign — TEAM-WIDE (owner decision 2026-08-16, Buzz heuristic: scope
// by membership): any signed-in member may put the team agent on an item.
// Token validation (roster check, bare-token rule) lives in the bridge.
func (p *portalAPI) handleAssign(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct{ Item, Owner string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, exists := p.ownerOf(b.Item); !exists {
		http.Error(w, "unknown item", http.StatusNotFound)
		return
	}
	if err := p.opt.Assign(b.Item, b.Owner, id.Email, id.Name); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, p.opt.Panel(b.Item))
}

// handleFire — TEAM-WIDE: any member executes an agent-held plan; the fire is
// attributed to them, the owner gets a FEED notice, and the result posts back
// into this item's thread (the closed loop).
func (p *portalAPI) handleFire(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct{ Item string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if _, exists := p.ownerOf(b.Item); !exists {
		http.Error(w, "unknown item", http.StatusNotFound)
		return
	}
	switch err := p.opt.Fire(b.Item, id.Email, id.Name); {
	case err == nil:
		writeJSON(w, map[string]any{"ok": true, "queued": true})
	case errors.Is(err, spirits.ErrAlreadyActive):
		http.Error(w, "the agent is already running — try again when it finishes", http.StatusConflict)
	default:
		httpError(w, errBadRequest(err.Error()))
	}
}

// handleActivity — one item's slice of the team activity trail (v2 stream's
// "changed" rows). Any signed-in member may read it (same visibility as the
// state dump).
func (p *portalAPI) handleActivity(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.identify(w, r); !ok {
		return
	}
	itemID := strings.TrimSpace(r.URL.Query().Get("item"))
	if itemID == "" {
		httpError(w, errBadRequest("item is required"))
		return
	}
	writeJSON(w, map[string]any{"activity": p.opt.Activity(itemID)})
}

// handlePlanWrite — the v2 plan editor's section write. Gate: the item's
// ASSIGNEE (email→initials mapping) or the portal admin; everyone else reads.
func (p *portalAPI) handlePlanWrite(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct{ Item, Section, Text string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	owner, exists := p.ownerOf(b.Item)
	if !exists {
		http.Error(w, "unknown item", http.StatusNotFound)
		return
	}
	if !p.isAdmin(id.Email) && !strings.EqualFold(owner, p.ownerToken(id.Email)) {
		http.Error(w, "the plan record is assignee-only — "+orDash(owner)+" holds it", http.StatusForbidden)
		return
	}
	if err := p.opt.PlanWrite(b.Item, b.Section, b.Text); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, p.opt.Panel(b.Item))
}

// handleFileBlob serves a comment-attachment blob by hash.
func (p *portalAPI) handleFileBlob(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.identify(w, r); !ok {
		return
	}
	path := p.opt.FileBlob(r.PathValue("hash"))
	if path == "" {
		http.Error(w, "no such file", http.StatusNotFound)
		return
	}
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition", "attachment")
	}
	http.ServeFile(w, r, path)
}

// --- native chat with kairos (chat-kairos handoff) --------------------------

func (p *portalAPI) handleChatThreads(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.identify(w, r); !ok {
		return
	}
	writeJSON(w, p.opt.ChatThreads())
}

func (p *portalAPI) handleChatThread(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct{ Op, ID, Title, Rock string }
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	out, err := p.opt.ChatThread(b.Op, b.ID, b.Title, b.Rock, id.Email, id.Name)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, out)
}

func (p *portalAPI) handleChatAsk(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct {
		Thread, Text, Ritual string
		Context              []string
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	switch err := p.opt.ChatAsk(b.Thread, b.Text, b.Ritual, b.Context, id.Email, id.Name); {
	case err == nil:
		writeJSON(w, map[string]any{"ok": true, "queued": true})
	case errors.Is(err, spirits.ErrAlreadyActive):
		http.Error(w, "kairos is running — this queues behind the active run", http.StatusConflict)
	default:
		httpError(w, errBadRequest(err.Error()))
	}
}

func (p *portalAPI) handleChatEngine(w http.ResponseWriter, r *http.Request) {
	if _, ok := p.identify(w, r); !ok {
		return
	}
	writeJSON(w, p.opt.ChatEngine())
}

func (p *portalAPI) handleChatProposal(w http.ResponseWriter, r *http.Request) {
	id, ok := p.identify(w, r)
	if !ok {
		return
	}
	var b struct {
		Thread, Msg string
		Index       int
		Apply       bool
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := p.opt.ChatProposal(b.Thread, b.Msg, b.Index, b.Apply, id.Email, id.Name, p.ownerToken(id.Email), p.isAdmin(id.Email)); err != nil {
		if strings.Contains(err.Error(), "only") { // gate refusal → 403
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, p.opt.ChatThreads())
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
	// The target arm compares IDENTITY, not just the address string: one person
	// may hold two addresses that resolve to the same initials (an OODA partner
	// with both an ooda.group and a personal address), and without this they
	// would get a 403 on their own proposal when signed in as the other one.
	// A widening for AION in principle — no such second address exists there —
	// taken deliberately rather than branched on an unrelated policy field.
	if !p.isAdmin(id.Email) && !strings.EqualFold(id.Email, target) &&
		!strings.EqualFold(p.ownerToken(id.Email), p.ownerToken(target)) {
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

// PortalLiveStatus is the projection-status shape a portal's revision route
// serves. An ALIAS, not a new type: AionLiveStatus keeps its name for the
// cockpit and the FEED signal, so nothing outside this seam moves.
type PortalLiveStatus = AionLiveStatus

// PortalLive is the READ side of a portal (ooda-portal plan, Stage A). These
// five methods are EXACTLY what this file calls on opt.Live — nothing more, so
// a second portal can supply a completely different projection (real estate
// rather than AION) while every WRITE route above stays shared and unforked.
//
// If a sixth call appears here, TestPortalLiveIsExactlyFiveMethods stops
// compiling. That is the point: the seam should be widened deliberately.
type PortalLive interface {
	File(urlPath string) ([]byte, string, error)
	Status() PortalLiveStatus
	TeamStateJSON() any
	People() []portalPerson
	OwnerOf(itemID string) (string, bool)
}

var _ PortalLive = (*AionLive)(nil)

// aionReadRoutes is the AION portal's read surface — the contract data files
// and the revision poll. Lifted verbatim out of PortalHandler when the read
// side became per-portal (ooda-portal plan, Stage A step 6).
func aionReadRoutes(mux *http.ServeMux, live PortalLive) {
	for _, path := range []string{
		"/data/finances.json", "/data/vto.json", "/data/goals.json", "/data/backlog.json",
		"/data/heuristics.json", "/data/people.json", "/data/meta.json",
		"/content/hiring.md", "/content/references.md",
	} {
		mux.HandleFunc("GET "+path, handleAionLiveFile(live, path))
	}
	mux.HandleFunc("GET /api/live/revision", handleAionLiveRevision(live))
}

// liveOrNil collapses a typed-nil PortalLive to a true nil.
func liveOrNil(l PortalLive) PortalLive {
	if l == nil {
		return nil
	}
	if v := reflect.ValueOf(l); v.Kind() == reflect.Ptr && v.IsNil() {
		return nil
	}
	return l
}
