package server

// Construction Intelligence access boundary (plan §7, §12.2).
//
// The private listener has no general owner-authentication middleware; it is
// bound to 127.0.0.1 and reached through the owner's trusted private host or
// tailnet. Construction adopts that single-owner, trusted-local-host posture
// explicitly and adds feature-specific checks on top of it:
//
//   - routes exist only on Server.Handler — never on the portal, deal-share or
//     public curation listeners;
//   - the Host header must be loopback or a configured trusted host, which also
//     defeats DNS-rebinding pages that would otherwise look same-origin;
//   - cross-site requests are refused by Origin and Sec-Fetch-Site;
//   - every mutation must also carry a per-process nonce that only a
//     same-origin page (or a trusted local process) can read from /session;
//   - the actor is derived here, never read from a request body or header.
//
// A hostile process running as the owner's OS user is outside this boundary.
// If Manifest is ever bound beyond the trusted host/tailnet, an authenticated
// owner gateway must be added before these routes are exposed.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"manifest/construction"
)

// constructionPrincipal resolves the actor for a request that already passed
// the transport checks. The default is the trusted private owner; tests (and
// a future gateway) inject their own.
type constructionPrincipal func(r *http.Request) (construction.Actor, error)

func newConstructionNonce() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// trustedConstructionHost reports whether the Host header names this machine
// (loopback) or a host the owner configured as their private entry point.
func (c *constructionCfg) trustedHost(hostport string) bool {
	host := strings.ToLower(strings.TrimSpace(hostport))
	if host == "" {
		return false
	}
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.Trim(name, "[]")
	switch name {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return c.trustedHosts[host] || c.trustedHosts[name]
}

// sameOriginURL reports whether an Origin/Referer URL names the request host.
func sameOriginURL(raw, host string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, host)
}

// constructionGuard runs the transport checks and returns the actor, or
// writes the refusal and returns ok=false.
func (s *Server) constructionGuard(w http.ResponseWriter, r *http.Request, mutation bool) (construction.Actor, bool) {
	c := s.construction
	if c == nil || c.store == nil {
		constructionError(w, construction.Unavailable("construction is not enabled on this server"))
		return construction.Actor{}, false
	}
	if !c.trustedHost(r.Host) {
		constructionError(w, construction.Forbidden("untrusted host for private construction routes"))
		return construction.Actor{}, false
	}
	site := strings.ToLower(r.Header.Get("Sec-Fetch-Site"))
	if site != "" && site != "same-origin" && site != "none" {
		constructionError(w, construction.Forbidden("cross-site request refused"))
		return construction.Actor{}, false
	}
	if origin := r.Header.Get("Origin"); origin != "" && !sameOriginURL(origin, r.Host) {
		constructionError(w, construction.Forbidden("cross-origin request refused"))
		return construction.Actor{}, false
	}
	if mutation {
		if r.Header.Get("Origin") == "" || site == "none" {
			constructionError(w, construction.Forbidden("mutations must come from the Manifest page (same-origin)"))
			return construction.Actor{}, false
		}
		got := r.Header.Get("X-Construction-Nonce")
		if len(got) != len(c.nonce) || subtle.ConstantTimeCompare([]byte(got), []byte(c.nonce)) != 1 {
			constructionError(w, &construction.Error{Status: http.StatusForbidden, Kind: "nonce", Message: "missing or stale construction nonce; reload the session"})
			return construction.Actor{}, false
		}
	}
	resolve := c.principal
	if resolve == nil {
		resolve = func(*http.Request) (construction.Actor, error) { return construction.OwnerActor(), nil }
	}
	actor, err := resolve(r)
	if err != nil {
		constructionError(w, err)
		return construction.Actor{}, false
	}
	if actor.Kind != construction.ActorOwner {
		// browser routes act for the owner only; agents act through their
		// bound in-process capability (construction_agent_tools.go)
		constructionError(w, construction.Forbidden("this route acts for the owner only"))
		return construction.Actor{}, false
	}
	return actor, true
}

// constructionError writes a domain error as JSON with its status.
func constructionError(w http.ResponseWriter, err error) {
	var e *construction.Error
	if !errors.As(err, &e) {
		e = &construction.Error{Status: http.StatusInternalServerError, Kind: "internal", Message: err.Error()}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e)
}

func constructionJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = json.NewEncoder(w).Encode(v)
}
