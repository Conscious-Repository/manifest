package teamportal

import (
	"os"
	"path/filepath"
	"strings"
)

// Policy scopes ONE portal instance's identity rules (ooda-portal plan, Stage
// A). Until 2026-08-20 this package was single-tenant: the domain gate, the
// cookie names, the credential filenames, and the token prefix were all
// package constants, so a second portal could not coexist with AION's.
//
// The ZERO VALUE is the AION portal exactly as it shipped — every accessor
// falls back to the constant it replaced. An Auth, TokenStore, or Store built
// without a Policy behaves byte-identically to the pre-refactor code, which is
// what makes this refactor provable against the existing test suite.
type Policy struct {
	Domain       string // Workspace domain gate           ("" → Domain const)
	CookiePrefix string // session/state cookie namespace   ("" → "aion_portal")
	ClientFile   string // basename under <dataDir>/portals ("" → aion-portal-oauth.json)
	ClientEnv    string // env var overriding that path     ("" → AION_PORTAL_OAUTH_CLIENT)
	KeyFile      string //   ""                             ("" → aion-portal-session.key)
	TokenFile    string //   ""                             ("" → aion-portal-tokens.json)
	TokenPrefix  string // plaintext token prefix           ("" → tokenPrefix const)

	// ExtraScopes are OAuth scopes requested at sign-in BEYOND identity
	// (openid/email/profile). Empty = identity-only, byte-identical to the
	// pre-refactor AION flow. The OODA portal bundles gmail.readonly here
	// (owner decision 2026-08-21): signing in IS connecting your mailbox —
	// the login flow then requests offline access and hands the minted token
	// to the Auth's token sink. Google shows the extra consent checkbox; a
	// member who unticks it still signs in, just with no mailbox connected.
	ExtraScopes []string

	// AllowExtra admits a NON-domain address. Nil → the domain gate is the
	// whole rule (AION's wildcard-by-design, decided 2026-08-13).
	//
	// The OODA portal supplies a closure over its store's emails.json, so ONE
	// file is both the allow-list and the email→initials map. That coupling is
	// an INVARIANT, not a convenience: ownerToken() falls back to the email's
	// local part when a member is unmapped, so an address admitted here but
	// missing from the map would file work under a garbage owner
	// ("me@olgasobkiv.com" → owner "me" instead of "OS").
	//
	// The closure is called on the request path (Identify runs twice per
	// request), and EmailOverrides() re-reads the file every call. That is the
	// price of allow-list edits taking effect on the next request with no
	// restart — a property the OODA runbook depends on. The file is a handful
	// of lines; if it ever matters, cache inside Store (never inside Policy,
	// which would break the live-edit property).
	AllowExtra func(email string) bool
}

// Allows is THE identity gate: an address under the policy's Workspace domain,
// or one the allow-list names.
func (p Policy) Allows(email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return false
	}
	if strings.HasSuffix(e, "@"+strings.ToLower(p.DomainName())) {
		return true
	}
	return p.AllowExtra != nil && p.AllowExtra(e)
}

// DomainName is the Workspace domain this policy gates on. (Named apart from
// the Domain field so both can exist on the same value.)
func (p Policy) DomainName() string {
	if d := strings.TrimSpace(p.Domain); d != "" {
		return d
	}
	return Domain
}

// Cookie names fall back to the package consts rather than re-spelling
// "aion_portal" here — one source of truth for the AION names.
func (p Policy) sessionCookieName() string {
	if s := strings.TrimSpace(p.CookiePrefix); s != "" {
		return s + "_session"
	}
	return sessionCookie
}

func (p Policy) stateCookieName() string {
	if s := strings.TrimSpace(p.CookiePrefix); s != "" {
		return s + "_state"
	}
	return stateCookie
}

// clientPath resolves the OAuth client file, honoring this policy's env
// override (mirrors gmailauth's GMAIL_OAUTH_CLIENT).
func (p Policy) clientPath(dataDir string) string {
	env := strings.TrimSpace(p.ClientEnv)
	if env == "" {
		env = "AION_PORTAL_OAUTH_CLIENT"
	}
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return v
	}
	return filepath.Join(dataDir, "portals", orFile(p.ClientFile, "aion-portal-oauth.json"))
}

func (p Policy) keyPath(dataDir string) string {
	return filepath.Join(dataDir, "portals", orFile(p.KeyFile, "aion-portal-session.key"))
}

func (p Policy) tokenPath(dataDir string) string {
	return filepath.Join(dataDir, "portals", orFile(p.TokenFile, "aion-portal-tokens.json"))
}

func (p Policy) tokenPfx() string {
	if s := strings.TrimSpace(p.TokenPrefix); s != "" {
		return s
	}
	return tokenPrefix
}

func orFile(v, fallback string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return fallback
}
