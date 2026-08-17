// Package teamportal is the AION team portal's write layer (portal move,
// Phase 2–4): Google OAuth sign-in for @aion.bio accounts, signed session
// cookies, the derived team-state store on /shared (activity.log JSONL +
// items.ext.json), and the bridge that surfaces team writes as FEED notices.
//
// Doctrine fit (ARCHITECTURE §1 amendment 2026-08-14, §2, §6): the portal is
// login-to-view (Google @aion.bio only, gated whole in server/portal.go) and
// login-to-write; team members are AUTHORIZED writers of portal team state
// ONLY — never the vault, never the owner's records. All team
// state is derived state outside the vault; credentials live in the secrets
// tier (<dataDir>/portals/, 0600) following the Calendar/Gmail OAuth pattern
// (gmailauth). The OAuth client here is a "web" client (fixed redirect URIs:
// https://portal.aion.bio/oauth2/callback + http://localhost:8080/oauth2/callback)
// — a separate client from the installed-app Calendar/Gmail one.
package teamportal

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// Domain is the authorized Google Workspace domain — ANY account under it may
// write (wildcard by design; no manual allow-list, decided 2026-08-13).
const Domain = "aion.bio"

// Cookie names. The session cookie is httpOnly + SameSite=Lax (Lax also keeps
// cross-site POSTs cookie-less, so no separate CSRF token is needed) and
// Secure whenever the request arrived over https (Cloudflare Tunnel sets
// X-Forwarded-Proto).
const (
	sessionCookie = "aion_portal_session"
	stateCookie   = "aion_portal_state"
)

const (
	sessionTTL = 30 * 24 * time.Hour
	stateTTL   = 10 * time.Minute
)

// Identity is a verified signed-in team member.
type Identity struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Auth owns the portal's OAuth client + session signing key. Both live in the
// secrets tier: <dataDir>/portals/aion-portal-oauth.json (the web client,
// never committed — see docs/aion-portal-oauth.example.json) and
// <dataDir>/portals/aion-portal-session.key (auto-generated, 0600).
type Auth struct {
	clientPath        string
	keyPath           string
	sessionName       string
	stateName         string
	hostedDomain      string
	authorize         func(string) bool
	accessDescription string

	mu     sync.Mutex
	key    []byte // cached session HMAC key
	tokens *TokenStore
}

// NewAuth resolves the credential paths under dataDir. AION_PORTAL_OAUTH_CLIENT
// overrides the client file path (mirrors gmailauth's GMAIL_OAUTH_CLIENT).
func NewAuth(dataDir string) *Auth {
	cp := filepath.Join(dataDir, "portals", "aion-portal-oauth.json")
	if p := strings.TrimSpace(os.Getenv("AION_PORTAL_OAUTH_CLIENT")); p != "" {
		cp = p
	}
	auth := newScopedAuth(dataDir, cp, "aion_portal", Domain, Authorized, "@"+Domain+" accounts")
	auth.keyPath = filepath.Join(dataDir, "portals", "aion-portal-session.key") // preserve deployed sessions
	return auth
}

// NewScopedAuth creates an isolated Google OAuth/session realm backed by a
// caller-supplied authorization check. It is used by fundraising.aion.bio so
// external invitees never enter the @aion.bio team realm.
func NewScopedAuth(dataDir, clientPath, namespace string, authorize func(string) bool, accessDescription string) *Auth {
	if strings.TrimSpace(clientPath) == "" {
		clientPath = filepath.Join(dataDir, "portals", namespace+"-oauth.json")
	}
	return newScopedAuth(dataDir, clientPath, namespace, "", authorize, accessDescription)
}

func newScopedAuth(dataDir, clientPath, namespace, hostedDomain string, authorize func(string) bool, accessDescription string) *Auth {
	return &Auth{
		clientPath: clientPath, keyPath: filepath.Join(dataDir, "portals", namespace+"-session.key"),
		sessionName: namespace + "_session", stateName: namespace + "_state", hostedDomain: hostedDomain,
		authorize: authorize, accessDescription: accessDescription,
	}
}

func (a *Auth) authorized(email string) bool {
	return a != nil && a.authorize != nil && a.authorize(strings.ToLower(strings.TrimSpace(email)))
}

// WithTokens adds the non-browser authentication path used by scripts and MCP
// clients. It returns the receiver for fluent setup in main.
func (a *Auth) WithTokens(tokens *TokenStore) *Auth {
	a.mu.Lock()
	a.tokens = tokens
	a.mu.Unlock()
	return a
}

// Enabled reports whether the OAuth client file is present (sign-in possible).
func (a *Auth) Enabled() bool {
	_, err := os.Stat(a.clientPath)
	return err == nil
}

// Authorized is THE domain gate: any @aion.bio account, nothing else.
func Authorized(email string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), "@"+Domain)
}

// webClient is the Google "web application" client JSON shape (the file
// downloaded from Google Cloud console; redacted example committed alongside).
type webClient struct {
	Web struct {
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		AuthURI      string   `json:"auth_uri"`
		TokenURI     string   `json:"token_uri"`
		RedirectURIs []string `json:"redirect_uris"`
	} `json:"web"`
}

func (a *Auth) client() (webClient, error) {
	var wc webClient
	b, err := os.ReadFile(a.clientPath)
	if err != nil {
		return wc, fmt.Errorf("portal OAuth client not found (%s)", a.clientPath)
	}
	if err := json.Unmarshal(b, &wc); err != nil || wc.Web.ClientID == "" {
		return wc, fmt.Errorf("portal OAuth client file is not a web-client JSON (%s)", a.clientPath)
	}
	if wc.Web.AuthURI == "" {
		wc.Web.AuthURI = "https://accounts.google.com/o/oauth2/auth"
	}
	if wc.Web.TokenURI == "" {
		wc.Web.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return wc, nil
}

// oauthConfig builds the authorization-code config for the request host,
// picking the registered redirect URI whose host matches (portal.aion.bio in
// production, localhost:8080 for local smoke tests).
func (a *Auth) oauthConfig(host string) (*oauth2.Config, error) {
	wc, err := a.client()
	if err != nil {
		return nil, err
	}
	if len(wc.Web.RedirectURIs) == 0 {
		return nil, errors.New("portal OAuth client has no redirect_uris")
	}
	redirect := wc.Web.RedirectURIs[0]
	for _, u := range wc.Web.RedirectURIs {
		if p, err := url.Parse(u); err == nil && strings.EqualFold(p.Host, host) {
			redirect = u
			break
		}
	}
	return &oauth2.Config{
		ClientID:     wc.Web.ClientID,
		ClientSecret: wc.Web.ClientSecret,
		RedirectURL:  redirect,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     oauth2.Endpoint{AuthURL: wc.Web.AuthURI, TokenURL: wc.Web.TokenURI},
	}, nil
}

// sessionKey loads (or mints once, 0600) the HMAC key sessions are signed with.
func (a *Auth) sessionKey() ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.key) > 0 {
		return a.key, nil
	}
	if b, err := os.ReadFile(a.keyPath); err == nil && len(b) >= 32 {
		a.key = b
		return a.key, nil
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(a.keyPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(a.keyPath, k, 0o600); err != nil {
		return nil, err
	}
	a.key = k
	return a.key, nil
}

// sessionPayload is what the cookie carries — signed, never encrypted; it
// holds no secret, only the asserted identity + expiry.
type sessionPayload struct {
	Email string `json:"e"`
	Name  string `json:"n"`
	Exp   int64  `json:"x"`
}

func (a *Auth) sign(b []byte) (string, error) {
	key, err := a.sessionKey()
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *Auth) verify(v string) ([]byte, bool) {
	dot := strings.LastIndexByte(v, '.')
	if dot <= 0 {
		return nil, false
	}
	body, err := base64.RawURLEncoding.DecodeString(v[:dot])
	if err != nil {
		return nil, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(v[dot+1:])
	if err != nil {
		return nil, false
	}
	key, err := a.sessionKey()
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return nil, false
	}
	return body, true
}

// SessionCookie mints the signed session cookie for a verified @aion.bio
// identity. It refuses any other domain — defense in depth on top of the
// callback's own check. Exported for the server-side tests that exercise the
// write endpoints without a live Google round-trip.
func (a *Auth) SessionCookie(email, name string, secure bool, now time.Time) (*http.Cookie, error) {
	if !a.authorized(email) {
		return nil, fmt.Errorf("account is not authorized: %s", email)
	}
	b, _ := json.Marshal(sessionPayload{Email: email, Name: name, Exp: now.Add(sessionTTL).Unix()})
	v, err := a.sign(b)
	if err != nil {
		return nil, err
	}
	return &http.Cookie{
		Name: a.sessionName, Value: v, Path: "/",
		Expires: now.Add(sessionTTL), MaxAge: int(sessionTTL / time.Second),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	}, nil
}

// Identify returns the verified identity on r, if any. An expired, tampered,
// or non-@aion.bio session is simply anonymous.
func (a *Auth) Identify(r *http.Request) (Identity, bool) {
	c, err := r.Cookie(a.sessionName)
	if err != nil || c.Value == "" {
		return Identity{}, false
	}
	body, ok := a.verify(c.Value)
	if !ok {
		return Identity{}, false
	}
	var p sessionPayload
	if json.Unmarshal(body, &p) != nil || p.Exp < time.Now().Unix() || !a.authorized(p.Email) {
		return Identity{}, false
	}
	return Identity{Email: p.Email, Name: p.Name}, true
}

// IdentifyRequest preserves cookie sessions as the first choice, then accepts
// an Authorization: Bearer token. OAuth callbacks and token-management routes
// deliberately keep using Identify so a token cannot mint another token.
func (a *Auth) IdentifyRequest(r *http.Request) (Identity, bool) {
	if id, ok := a.Identify(r); ok {
		return id, true
	}
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return Identity{}, false
	}
	a.mu.Lock()
	tokens := a.tokens
	a.mu.Unlock()
	if tokens == nil {
		return Identity{}, false
	}
	return tokens.Resolve(parts[1])
}

// isSecure reports whether the request arrived over https — directly or via a
// terminating proxy (Cloudflare Tunnel / Funnel set X-Forwarded-Proto).
func isSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// HandleLogin (GET /oauth2/login) sends the browser to Google's account
// chooser, pinning a signed state cookie against CSRF.
func (a *Auth) HandleLogin(w http.ResponseWriter, r *http.Request) {
	cfg, err := a.oauthConfig(r.Host)
	if err != nil {
		http.Error(w, "sign-in unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	state := hex.EncodeToString(nonce)
	b, _ := json.Marshal(sessionPayload{Email: state, Exp: time.Now().Add(stateTTL).Unix()})
	v, err := a.sign(b)
	if err != nil {
		http.Error(w, "sign-in unavailable", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: a.stateName, Value: v, Path: "/", MaxAge: int(stateTTL / time.Second),
		HttpOnly: true, Secure: isSecure(r), SameSite: http.SameSiteLaxMode,
	})
	// hd biases Google's chooser to the workspace domain (advisory only — the
	// callback still verifies the email itself).
	opts := []oauth2.AuthCodeOption{}
	if a.hostedDomain != "" {
		opts = append(opts, oauth2.SetAuthURLParam("hd", a.hostedDomain))
	}
	// After an explicit sign-out (?switch=1) force the account chooser, else
	// Google silently re-authenticates the still-live session and the user never
	// leaves — "sign out" would look like it did nothing.
	if r.URL.Query().Get("switch") != "" {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "select_account"))
	}
	http.Redirect(w, r, cfg.AuthCodeURL(state, opts...), http.StatusFound)
}

// HandleCallback (GET /oauth2/callback) verifies state, exchanges the code,
// reads the identity from Google's id_token, enforces the @aion.bio gate, and
// sets the session cookie.
func (a *Auth) HandleCallback(w http.ResponseWriter, r *http.Request) {
	sc, err := r.Cookie(a.stateName)
	if err != nil {
		http.Error(w, "sign-in expired — start again at /oauth2/login", http.StatusBadRequest)
		return
	}
	body, ok := a.verify(sc.Value)
	var p sessionPayload
	if !ok || json.Unmarshal(body, &p) != nil || p.Exp < time.Now().Unix() ||
		p.Email == "" || p.Email != r.URL.Query().Get("state") {
		http.Error(w, "state mismatch — start again at /oauth2/login", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: a.stateName, Value: "", Path: "/", MaxAge: -1})
	cfg, err := a.oauthConfig(r.Host)
	if err != nil {
		http.Error(w, "sign-in unavailable", http.StatusServiceUnavailable)
		return
	}
	tok, err := cfg.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "Google sign-in failed — please try again", http.StatusBadGateway)
		return
	}
	email, name, verified, err := parseIDToken(tok)
	if err != nil || !verified {
		http.Error(w, "Google returned no verified email — please try again", http.StatusBadGateway)
		return
	}
	if !a.authorized(email) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, "This portal is limited to %s.\nYou signed in as %s — switch Google accounts and try again.\n", a.accessDescription, email)
		return
	}
	c, err := a.SessionCookie(email, name, isSecure(r), time.Now())
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, c)
	http.Redirect(w, r, "/", http.StatusFound)
}

// HandleLogout (POST /oauth2/logout) clears the session.
func (a *Auth) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: a.sessionName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: isSecure(r), SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// parseIDToken pulls the identity claims from the id_token minted alongside
// the access token. No signature check is needed: the token arrived directly
// from Google's token endpoint over TLS in the same exchange.
func parseIDToken(tok *oauth2.Token) (email, name string, verified bool, err error) {
	raw, _ := tok.Extra("id_token").(string)
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", "", false, errors.New("no id_token in exchange")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", false, err
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := json.Unmarshal(b, &claims); err != nil {
		return "", "", false, err
	}
	return claims.Email, claims.Name, claims.EmailVerified, nil
}
