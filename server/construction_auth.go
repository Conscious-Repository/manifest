package server

// Construction Intelligence access boundary (plan §7, §12.2).
//
// Construction uses Manifest's own trust model (plan §12.2): the private
// listener is bound to loopback, and the owner reaches it from this computer
// or across the tailnet through `tailscale serve`. Whoever reaches the
// private handler that way is treated as the owner, exactly as for every
// other owner surface. A request is answered when:
//
//   - its TCP peer is loopback (the private listener; nothing in the handler
//     chain rewrites RemoteAddr);
//   - it carries no public-relay header (Cloudflare, Fastly, Akamai…): a
//     public tunnel in front of this listener is refused, not trusted;
//   - its Host is loopback, a tailnet (*.ts.net) name, or listed in
//     construction.trustedHosts — so a DNS-rebinding page cannot pose as the
//     owner's own origin.
//
// Inside that boundary, as browser defences (not authentication):
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

// constructionRemoteMessage explains a refusal; the UI shows it as is.
const constructionRemoteMessage = "Construction answers this computer and your tailnet (the way you reach the rest of Manifest). " +
	"This request came from somewhere else, so it was refused."

func constructionRemoteDisabled(reason string) error {
	return &construction.Error{Status: http.StatusForbidden, Kind: "remote-disabled", Message: constructionRemoteMessage, Problems: []string{reason}}
}

// constructionPublicRelayHeaders mark a request carried in from the public
// internet by a CDN or tunnel (Cloudflare, Fastly, Akamai). `tailscale
// serve`'s forwarding and Tailscale-* identity headers are expected: that is
// how the owner reaches Manifest from the tailnet. Only ever grounds to
// refuse, never to admit.
var constructionPublicRelayHeaders = map[string]bool{
	"cf-connecting-ip": true, "cf-ray": true, "cf-ipcountry": true, "cf-visitor": true, "cdn-loop": true,
	"true-client-ip": true, "fastly-client-ip": true, "akamai-origin-hop": true,
}

func constructionPublicRelay(h http.Header) bool {
	for k := range h {
		if constructionPublicRelayHeaders[strings.ToLower(k)] {
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

// constructionHostAllowed: loopback, a tailnet MagicDNS name, or a host the
// owner listed. A guard against DNS rebinding, not authentication.
func constructionHostAllowed(hostport string, trusted []string) bool {
	if loopbackHost(hostport) {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(hostport))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	if strings.HasSuffix(host, ".ts.net") {
		return true
	}
	for _, t := range trusted {
		if strings.EqualFold(strings.TrimSpace(t), host) {
			return true
		}
	}
	return false
}

// loopbackHost reports whether the Host header names loopback.
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
	// the private listener, no public relay, a known Host: before anything
	// else in the request is used
	var remote string
	switch {
	case constructionPublicRelay(r.Header):
		remote = "the request came in through a public CDN or tunnel"
	case !loopbackPeer(r.RemoteAddr):
		remote = "the TCP peer is not the private listener"
	case !constructionHostAllowed(r.Host, c.opts.TrustedHosts):
		remote = "the Host is not this computer, a tailnet name or a trusted host"
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
