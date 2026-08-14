package calendar

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Manifest runs headless (metis), so connecting an account can't use the
// loopback listener in Authorize — its redirect would land on metis's own
// 127.0.0.1, not the owner's browser. StartConnect/FinishConnect implement the
// same PASTE-BACK flow gmailauth uses: StartConnect returns the consent URL
// (redirecting to a localhost port nothing listens on); the owner approves in
// their browser, the redirect fails to load locally, and they paste the
// resulting address back — FinishConnect exchanges the code it carries.

// pasteRedirectURI is the registered-loopback shape Google accepts for
// installed-app clients; nothing ever listens on it — the code rides the pasted
// URL.
const pasteRedirectURI = "http://127.0.0.1:8123/oauth/callback"

// calRevalidateEvery bounds how often AccountStatuses triggers a network check.
const calRevalidateEvery = 10 * time.Minute

// StartConnect begins the paste-back flow: it returns the consent URL and
// remembers the state token (pruning stale attempts). The URL redirects to a
// localhost port nothing listens on — the owner pastes the resulting address
// back to FinishConnect.
func (c *Client) StartConnect() (string, error) {
	cfg, err := oauthConfig()
	if err != nil {
		return "", err
	}
	cfg.RedirectURL = pasteRedirectURI
	state := randState()
	c.authMu.Lock()
	if c.pending == nil {
		c.pending = map[string]time.Time{}
	}
	for k, t := range c.pending { // prune stale attempts
		if time.Since(t) > 15*time.Minute {
			delete(c.pending, k)
		}
	}
	c.pending[state] = time.Now()
	c.authMu.Unlock()
	return cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "select_account consent")), nil
}

// FinishConnect completes the paste-back flow from the redirect URL (or query
// string) the owner pasted: it exchanges the code, resolves the account email,
// saves its token, and reloads. Reconnecting an already-listed account simply
// overwrites its token (saveAccountToken keys on email), clearing the reauth
// flag.
func (c *Client) FinishConnect(ctx context.Context, pasted string) (string, error) {
	code, state, err := parsePasted(pasted)
	if err != nil {
		return "", err
	}
	c.authMu.Lock()
	_, ok := c.pending[state]
	if ok {
		delete(c.pending, state)
	}
	c.authMu.Unlock()
	if !ok {
		return "", errors.New("this sign-in attempt is stale — press reconnect again and use the fresh link")
	}
	cfg, err := oauthConfig()
	if err != nil {
		return "", err
	}
	cfg.RedirectURL = pasteRedirectURI
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return "", fmt.Errorf("code exchange failed: %w", err)
	}
	if tok.RefreshToken == "" {
		return "", errors.New("Google returned no refresh token — revoke this app at myaccount.google.com/permissions and retry")
	}
	svc, err := gcal.NewService(ctx, option.WithTokenSource(cfg.TokenSource(ctx, tok)))
	if err != nil {
		return "", err
	}
	email, err := primaryEmail(ctx, svc)
	if err != nil {
		return "", err
	}
	if email == "" {
		return "", errors.New("could not resolve the Google account email after sign-in")
	}
	if err := saveAccountToken(email, tok); err != nil {
		return "", err
	}
	c.clearAcctCheck(email)
	c.reload()
	return email, nil
}

// AccountStatus is the per-account auth verdict the calendar UI renders.
type AccountStatus struct {
	Email       string `json:"email"`
	NeedsReauth bool   `json:"needsReauth"`
	Detail      string `json:"detail,omitempty"`
}

// AccountStatuses returns each connected account's cached auth verdict
// immediately and, if the cache is stale, kicks a background revalidation. It
// never blocks on the network: a revoked refresh token surfaces as NeedsReauth
// so the UI can prompt a reconnect instead of rendering a silently empty month.
// Before the first check completes every account reads as healthy (no false
// nag on startup).
func (c *Client) AccountStatuses(now time.Time) []AccountStatus {
	emails := c.Accounts() // locks mu; taken before authMu to avoid nesting

	c.authMu.Lock()
	cache := c.valCache
	stale := c.valAt.IsZero() || now.Sub(c.valAt) > calRevalidateEvery
	if stale && !c.valBusy {
		c.valBusy = true
		go c.revalidate()
	}
	c.authMu.Unlock()

	out := make([]AccountStatus, 0, len(emails))
	for _, e := range emails {
		st := AccountStatus{Email: e}
		if ch, ok := cache[e]; ok {
			st.NeedsReauth = ch.needsReauth
			st.Detail = ch.detail
		}
		out = append(out, st)
	}
	return out
}

// revalidate probes every connected account's refresh token and rebuilds the
// cache. A dead refresh token (invalid_grant) → needsReauth; a transient
// network error preserves that account's prior verdict, so a brief outage never
// produces a false reconnect prompt.
func (c *Client) revalidate() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c.mu.Lock()
	accts := append([]*account(nil), c.accounts...)
	c.mu.Unlock()

	cfg, cfgErr := oauthConfig()
	fresh := map[string]acctCheck{}
	for _, a := range accts {
		if a.email == "" {
			continue // legacy un-resolved token; resolved on first Events use
		}
		if cfgErr != nil {
			fresh[a.email] = acctCheck{detail: cfgErr.Error()}
			continue
		}
		// TokenSource refreshes the access token from the refresh token; a
		// revoked/expired refresh token surfaces here as an *oauth2.RetrieveError
		// carrying invalid_grant.
		if _, err := cfg.TokenSource(ctx, a.token).Token(); err == nil {
			fresh[a.email] = acctCheck{}
			continue
		} else {
			var re *oauth2.RetrieveError
			if errors.As(err, &re) {
				fresh[a.email] = acctCheck{needsReauth: true, detail: "sign-in expired (" + reauthReason(re) + ")"}
				continue
			}
			// transient (network/DNS) — keep the last known verdict, don't flap
			c.authMu.Lock()
			prev, had := c.valCache[a.email]
			c.authMu.Unlock()
			if had {
				fresh[a.email] = prev
			} else {
				fresh[a.email] = acctCheck{detail: "couldn't verify (offline?)"}
			}
		}
	}

	c.authMu.Lock()
	c.valCache = fresh
	c.valAt = time.Now()
	c.valBusy = false
	c.authMu.Unlock()
}

// clearAcctCheck drops one account's cached verdict and forces the next
// AccountStatuses call to revalidate, so a reconnect (or removal) reflects
// immediately instead of waiting out the throttle.
func (c *Client) clearAcctCheck(email string) {
	c.authMu.Lock()
	delete(c.valCache, email)
	c.valAt = time.Time{}
	c.authMu.Unlock()
}

func reauthReason(re *oauth2.RetrieveError) string {
	if re.ErrorCode != "" {
		return re.ErrorCode
	}
	return "invalid_grant"
}

// parsePasted digs code+state out of whatever the owner pasted: the full
// redirect URL, or just its query string.
func parsePasted(pasted string) (code, state string, err error) {
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return "", "", errors.New("paste the address-bar URL from the sign-in tab")
	}
	q := pasted
	if i := strings.Index(pasted, "?"); i >= 0 {
		q = pasted[i+1:]
	}
	vals, perr := url.ParseQuery(strings.TrimPrefix(q, "?"))
	if perr != nil {
		return "", "", errors.New("that doesn't look like the redirect URL — paste the whole address")
	}
	if e := vals.Get("error"); e != "" {
		return "", "", fmt.Errorf("Google returned %q — the sign-in was denied", e)
	}
	code, state = vals.Get("code"), vals.Get("state")
	if code == "" || state == "" {
		return "", "", errors.New("no authorization code in that URL — copy the FULL address the sign-in tab landed on")
	}
	return code, state, nil
}
