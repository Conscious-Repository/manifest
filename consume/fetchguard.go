package consume

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// THE GUARD — the one place a URL the OWNER PASTED becomes an outbound
// request.
//
// Everything else this package fetches was named by a subscription the owner
// added deliberately, or by a feed that subscription served. A pasted link is
// different in kind: the address is arbitrary at the moment of the request,
// and the server making it sits inside the tailnet with the vault, RSSHub and
// whatever else answers on loopback. That is the server-side request forgery
// shape, and it wants a real answer rather than a hopeful one.
//
// Four rules, and the third is the one that matters:
//
//   - scheme is http or https (externalURL already says so — reused, not
//     restated);
//   - port is the web's: 80, 443, or unstated;
//   - the RESOLVED address is public, checked in the dialer's Control hook.
//     Checking the resolved address at dial time rather than the hostname
//     beforehand is what closes DNS rebinding — a name that answers 93.x on
//     the first lookup and 127.0.0.1 on the second never reaches Dial;
//   - every redirect hop is re-checked, capped at five. A public host
//     answering 302 → http://127.0.0.1:1200/ is otherwise the whole bypass.
//
// ⚠ This guard belongs to the curate-url client ONLY, never to s.hc.
// defaultRSSHubBase is http://127.0.0.1:1200 and every @handle subscription
// depends on reaching it; a package-wide private-address deny would kill them
// all silently. TestRSSHubLoopbackStillReachable is the tripwire for that.

// errBlockedAddress is what a dial to a private address fails with. It is
// deliberately vague about WHICH address answered — the message reaches the
// owner's browser, and "127.0.0.1 answered" is a fact about his network that a
// pasted link should not be able to ask for.
var errBlockedAddress = errors.New("that link resolves to a private address")

const (
	guardMaxHops = 5
	guardTimeout = 20 * time.Second
)

// guardURL validates a pasted link and returns it trimmed, or says why not.
// The errors are written to be read by a person in a toast.
//
// allowPrivate is the test seam (Config.AllowPrivateCurateFetch): an httptest
// server binds to loopback, so without it no test could exercise a successful
// curate at all. main.go never sets it.
func guardURL(raw string, allowPrivate bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("paste a link to curate")
	}
	if externalURL(raw) == "" {
		return "", errors.New("only http and https links can be curated")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("that does not look like a link")
	}
	if allowPrivate {
		return raw, nil
	}
	if port := u.Port(); port != "" && port != "80" && port != "443" {
		return "", fmt.Errorf("only the web's ports can be curated, not :%s", port)
	}
	host := strings.ToLower(strings.Trim(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "", errBlockedAddress
	}
	// A literal address is answerable here, before any lookup. A NAME is not —
	// that answer belongs to the dialer, where it cannot go stale.
	if ip := net.ParseIP(host); ip != nil && blockedIP(ip) {
		return "", errBlockedAddress
	}
	return raw, nil
}

// blockedIP reports whether an address is one the open web has no business
// sending us to: this box, this network, the link-local range that holds cloud
// metadata endpoints, and the carrier-grade range tailnets live in.
func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// 0.0.0.0/8 ("this network") and 100.64.0.0/10 (CGNAT — where a
		// tailnet's own addresses live).
		return v4[0] == 0 || (v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127)
	}
	// fc00::/7 — IPv6 unique-local, the RFC1918 of v6.
	return len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc
}

// guardedClient is the HTTP client for pasted links. It is a second client
// beside s.hc rather than a setting on it — see the ⚠ above.
func guardedClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !allowPrivate {
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			switch network {
			case "tcp", "tcp4", "tcp6":
			default:
				return errBlockedAddress
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return errBlockedAddress
			}
			if blockedIP(net.ParseIP(host)) {
				return errBlockedAddress
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: guardTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			ExpectContinueTimeout: time.Second,
			MaxIdleConns:          4,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= guardMaxHops {
				return errors.New("that link redirects too many times")
			}
			// A pasted link carries no credential of ours and must not pick
			// one up on the way.
			req.Header.Del("Cookie")
			_, err := guardURL(req.URL.String(), allowPrivate)
			return err
		},
	}
}

// curateClient is the guarded client, built once per service.
func (s *Service) curateClient() *http.Client {
	if s.curateHC != nil {
		return s.curateHC
	}
	return s.hc
}

// guardPasted is the guard as the lane calls it.
func (s *Service) guardPasted(raw string) (string, error) {
	return guardURL(raw, s.cfg.AllowPrivateCurateFetch)
}

// getGuarded issues one GET through the guarded client. ctx bounds it; the
// caller reads the body and closes it.
func (s *Service) getGuarded(ctx context.Context, target, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	return s.curateClient().Do(req)
}
