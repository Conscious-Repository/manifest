package server

// Construction Intelligence access boundary (plan §7, §12.2).
//
// Construction is a local host-trust feature, not an authenticated one.
// Manifest has no verified owner authentication, so the routes answer a
// request only when its TCP peer (RemoteAddr, which net/http records from the
// accepted connection) is a loopback address, its Host names loopback, and it
// carries no proxy forwarding header. Whoever reaches the routes that way is
// treated as the owner. That is a property of the connection, not proof of a
// person: every local process qualifies, and so does any remote client whose
// traffic an operator-created TCP forward or tunnel delivers onto loopback
// (ssh -L/-R, socat, `tailscale serve --tcp`, a proxy that strips its
// headers and rewrites Host). At the application layer such a raw forward is
// indistinguishable from a local browser, and this code cannot detect it.
// Remote use through any relay, proxy or tunnel is therefore unsupported. It
// needs a verified, authenticated owner gateway, which does not exist;
// adding one means changing this guard, with its own review.
//
// What the checks do refuse, with 403 kind "remote-disabled":
//
//   - a non-loopback peer: direct tailnet or LAN connections;
//   - a non-loopback Host: DNS-rebinding pages, and proxies that keep the
//     public name;
//   - proxy forwarding headers: HTTP reverse proxies and `tailscale serve`.
//
// Host, Origin, Sec-Fetch-Site, the nonce and tailnet identity headers are
// written by the caller and are never treated as authentication. As
// defences inside local trust, not as authentication:
//
//   - routes exist only on Server.Handler, never on the portal, deal-share or
//     public curation listeners;
//   - cross-site requests are refused by Origin and Sec-Fetch-Site;
//   - every mutation carries a per-process nonce that the same-origin page
//     reads from /session (CSRF defence);
//   - the actor is derived here, never read from a request body or header.

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

// constructionPeer is everything a principal resolver is given: transport
// facts net/http recorded from the accepted connection. It deliberately
// carries no headers, cookies, URL or body, so a resolver cannot derive
// identity from anything a caller writes into the request.
type constructionPeer struct {
	RemoteAddr string
}

// constructionPrincipal resolves the actor for a request that already passed
// the loopback and same-origin checks. nil (production) means the owner. An
// injected resolver (tests) can only narrow access — deny, or present an
// actor the browser routes refuse. It is never consulted for a remote
// request, so it cannot open remote access.
type constructionPrincipal func(p constructionPeer) (construction.Actor, error)

func newConstructionNonce() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// constructionRemoteMessage explains the refusal; the UI shows it as is. It
// says why this request was refused without claiming that every remote path
// is: a raw TCP forward onto loopback is not detectable.
const constructionRemoteMessage = "Construction Intelligence is a local feature for this computer's own browser. " +
	"This request did not arrive as a loopback connection to a loopback address without proxy headers, so it was refused. " +
	"Remote use through the tailnet, the LAN or any proxy, relay or tunnel is unsupported: " +
	"it needs a verified, authenticated owner gateway, which does not exist."

func constructionRemoteDisabled(reason string) error {
	return &construction.Error{Status: http.StatusForbidden, Kind: "remote-disabled", Message: constructionRemoteMessage, Problems: []string{reason}}
}

// constructionProxyHeaders mark a request relayed by a proxy (reverse proxy,
// CDN, `tailscale serve`, whose tailnet identity headers start "Tailscale-").
// They are only ever grounds to refuse, never to admit.
var constructionProxyHeaders = map[string]bool{
	"forwarded": true, "via": true, "x-forwarded-for": true, "x-forwarded-host": true, "x-forwarded-proto": true,
	"x-forwarded-port": true, "x-forwarded-server": true, "x-forwarded-prefix": true, "x-original-forwarded-for": true,
	"x-real-ip": true, "x-client-ip": true, "true-client-ip": true, "cf-connecting-ip": true, "fastly-client-ip": true,
	"x-cluster-client-ip": true,
}

func constructionProxied(h http.Header) bool {
	for k := range h {
		k = strings.ToLower(k)
		if constructionProxyHeaders[k] || strings.HasPrefix(k, "tailscale-") {
			return true
		}
	}
	return false
}

// loopbackPeer reports whether the TCP peer address is loopback: a local
// process, or a local relay carrying someone else's connection; the two
// cannot be told apart here. No handler in this server rewrites RemoteAddr
// (TestConstructionRemoteAddrNeverRewritten).
func loopbackPeer(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackHost reports whether the Host header names loopback: a guard
// against DNS rebinding and proxies that keep the public name, not
// authentication (a caller can write any Host).
func loopbackHost(hostport string) bool {
	host := strings.TrimSpace(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
	// loopback peer, loopback Host, no proxy headers: before anything else
	// in the request is used
	var remote string
	switch {
	case constructionProxied(r.Header):
		remote = "the request came through a proxy (forwarding headers present)"
	case !loopbackPeer(r.RemoteAddr):
		remote = "the TCP peer is not a loopback address"
	case !loopbackHost(r.Host):
		remote = "the Host is not a loopback name"
	}
	if remote != "" {
		constructionError(w, constructionRemoteDisabled(remote))
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
	actor := construction.OwnerActor()
	if c.principal != nil {
		a, err := c.principal(constructionPeer{RemoteAddr: r.RemoteAddr})
		if err != nil {
			constructionError(w, err)
			return construction.Actor{}, false
		}
		actor = a
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
