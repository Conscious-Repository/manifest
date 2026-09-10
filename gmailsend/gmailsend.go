// Package gmailsend is Manifest's ONE outbound-mail capability: a send-only
// Gmail client for exactly one From address (recruiting outreach, Phase 5 of
// the AION scout plan, §4.8). It is deliberately separate from gmailauth —
// whose token is minted at gmail.readonly and only ever at gmail.readonly —
// so the send grant is its own consent, its own token file, and revocable
// on its own.
//
// Scope: https://www.googleapis.com/auth/gmail.send — the narrowest scope
// that sends and cannot read. Not gmail.modify, not mail.google.com.
//
// Token: <dataDir>/gmail-send/token.json (dir 0700, file 0600), never the
// vault, never config.json. GMAIL_SEND_TOKEN overrides the path; the OAuth
// client is the shared one gmailauth reads (GMAIL_OAUTH_CLIENT override).
// The engine's GMAIL_TOKEN is NOT shared: writing a send-scope token into the
// read-only engine's file would change what that token can do.
//
// Doctrine (D5): nothing in this package decides to send. Send is a method a
// route calls after the owner approved exactly these bytes; there is no
// queue, no retry loop, no goroutine. The client cannot list mailboxes, read
// mail, or choose another sender. The token is never logged or echoed, and
// every error string this package returns has been through the redactor.
package gmailsend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"manifest/gmailauth"
)

// SendScope is the ONLY scope this package requests.
const SendScope = "https://www.googleapis.com/auth/gmail.send"

// SendURL is the Gmail messages.send endpoint.
const SendURL = "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"

// DefaultSender is the one From address when GMAIL_SEND_FROM is unset.
const DefaultSender = "ben@aion.bio"

// ErrUnconfigured: no token has been minted (or the OAuth client is absent).
var ErrUnconfigured = errors.New("gmailsend: sending is not connected — connect the sender first")

// ErrNoSendScope: a token exists but was not granted gmail.send.
var ErrNoSendScope = errors.New("gmailsend: the connected token lacks the gmail.send scope — reconnect")

// ErrSenderMismatch: the token belongs to a different account than the one
// allowed sender. Raised before any network call.
var ErrSenderMismatch = errors.New("gmailsend: the connected account is not the allowed sender")

// TokenPath resolves the send token file: GMAIL_SEND_TOKEN, else
// <dataDir>/gmail-send/token.json.
func TokenPath(dataDir string) string {
	if p := strings.TrimSpace(os.Getenv("GMAIL_SEND_TOKEN")); p != "" {
		return p
	}
	return filepath.Join(dataDir, "gmail-send", "token.json")
}

// storedToken is the on-disk shape: gmailauth's {email, token} plus the
// scopes Google actually granted, so a token minted without gmail.send is
// detectable offline.
type storedToken struct {
	Email  string        `json:"email"`
	Token  *oauth2.Token `json:"token"`
	Scopes []string      `json:"scopes"`
}

// State is the probe surface. It never carries token material.
type State struct {
	// Configured: the OAuth client file exists and a token has been minted.
	Configured bool `json:"configured"`
	// SendCapable: Configured AND the token carries gmail.send AND the
	// connected account is the allowed sender.
	SendCapable bool `json:"sendCapable"`
	// Sender is the one allowed From address.
	Sender string `json:"sender"`
	// Email is the connected account when known (may be empty until a
	// profile read succeeds; gmail.send alone cannot read the profile).
	Email    string   `json:"email,omitempty"`
	Scopes   []string `json:"scopes"`
	HasCreds bool     `json:"hasCreds"`
	Detail   string   `json:"detail,omitempty"`
}

// Client is the send-only client for one From address.
type Client struct {
	from      string
	tokenPath string
	endpoint  string
	hc        *http.Client
	mu        sync.Mutex
	pending   map[string]time.Time
}

// New builds the client. from is the ONE allowed sender (empty → DefaultSender).
func New(from, tokenPath string) *Client {
	from = strings.ToLower(strings.TrimSpace(from))
	if from == "" {
		from = DefaultSender
	}
	return &Client{from: from, tokenPath: tokenPath, endpoint: SendURL}
}

// UseEndpoint swaps the send endpoint and transport (tests bind httptest).
// The oauth2 transport wraps hc, so the Authorization header still rides
// every request.
func (c *Client) UseEndpoint(url string, hc *http.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if url != "" {
		c.endpoint = url
	}
	c.hc = hc
}

// Sender returns the one allowed From address.
func (c *Client) Sender() string { return c.from }

func (c *Client) readToken() (*storedToken, error) {
	b, err := os.ReadFile(c.tokenPath)
	if err != nil {
		return nil, err
	}
	var st storedToken
	if err := json.Unmarshal(b, &st); err != nil || st.Token == nil {
		return nil, errors.New("bad token file")
	}
	return &st, nil
}

// SaveToken writes the token file 0600 under a 0700 dir. Exported so a test
// can seed a fixture token; production only reaches it through FinishConnect.
func (c *Client) SaveToken(email string, tok *oauth2.Token, scopes []string) error {
	if err := os.MkdirAll(filepath.Dir(c.tokenPath), 0o700); err != nil {
		return err
	}
	if scopes == nil {
		scopes = []string{}
	}
	b, err := json.MarshalIndent(storedToken{Email: strings.ToLower(strings.TrimSpace(email)), Token: tok, Scopes: scopes}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.tokenPath, b, 0o600); err != nil {
		return err
	}
	return os.Chmod(c.tokenPath, 0o600)
}

// Disconnect removes the send token.
func (c *Client) Disconnect() error {
	err := os.Remove(c.tokenPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if strings.EqualFold(strings.TrimSpace(s), want) {
			return true
		}
	}
	return false
}

// Status is the offline probe: file facts only, no network. An absent token
// is a state the UI paints (sendCapable:false), never an error.
func (c *Client) Status() State {
	st := State{Sender: c.from, Scopes: []string{}}
	if _, err := os.Stat(gmailauth.CredPath()); err == nil {
		st.HasCreds = true
	} else {
		st.Detail = "OAuth client credentials missing"
	}
	tok, err := c.readToken()
	if err != nil {
		if st.Detail == "" {
			st.Detail = "sender not connected"
		}
		return st
	}
	st.Configured = st.HasCreds
	st.Email = tok.Email
	st.Scopes = append(st.Scopes, tok.Scopes...)
	switch {
	case !st.HasCreds:
	case !hasScope(tok.Scopes, SendScope):
		st.Detail = "token lacks gmail.send — reconnect"
	case tok.Email != "" && !strings.EqualFold(tok.Email, c.from):
		st.Detail = "connected as " + tok.Email + ", not " + c.from
	default:
		st.SendCapable = true
	}
	return st
}

// SendCapable is Status().SendCapable.
func (c *Client) SendCapable() bool { return c.Status().SendCapable }

// StartConnect begins the paste-back flow at SendScope (mirrors
// gmailauth.Client.StartConnect; separate pending map, separate scope).
func (c *Client) StartConnect() (string, error) {
	cfg, err := gmailauth.ClientConfig(SendScope)
	if err != nil {
		return "", err
	}
	cfg.RedirectURL = gmailauth.PasteRedirectURI
	state := gmailauth.RandState()
	c.mu.Lock()
	if c.pending == nil {
		c.pending = map[string]time.Time{}
	}
	for k, t := range c.pending {
		if time.Since(t) > 15*time.Minute {
			delete(c.pending, k)
		}
	}
	c.pending[state] = time.Now()
	c.mu.Unlock()
	return cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("login_hint", c.from),
		oauth2.SetAuthURLParam("prompt", "select_account consent")), nil
}

// FinishConnect exchanges the pasted redirect URL for a token and stores it.
// The granted scopes come off the exchange response; a grant without
// gmail.send is stored (so the probe can say why) but never send-capable.
// When the profile is readable and names a different account than the
// allowed sender, the token is refused and not stored.
func (c *Client) FinishConnect(ctx context.Context, pasted string) (State, error) {
	code, state, err := gmailauth.ParsePasted(pasted)
	if err != nil {
		return c.Status(), err
	}
	c.mu.Lock()
	_, ok := c.pending[state]
	if ok {
		delete(c.pending, state)
	}
	c.mu.Unlock()
	if !ok {
		return c.Status(), errors.New("this sign-in attempt is stale — press connect again and use the fresh link")
	}
	cfg, err := gmailauth.ClientConfig(SendScope)
	if err != nil {
		return c.Status(), err
	}
	cfg.RedirectURL = gmailauth.PasteRedirectURI
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return c.Status(), fmt.Errorf("code exchange failed: %s", redactAny(err.Error(), tok))
	}
	if tok.RefreshToken == "" {
		return c.Status(), errors.New("Google returned no refresh token — revoke this app at myaccount.google.com/permissions and retry")
	}
	scopes := strings.Fields(fmt.Sprint(tok.Extra("scope")))
	email := c.from
	// gmail.send alone cannot read users/me/profile; when it can (a broader
	// grant on the same client), use it to refuse a wrong-account token.
	if got, err := fetchProfileEmail(ctx, cfg.Client(ctx, tok)); err == nil && got != "" {
		if !strings.EqualFold(got, c.from) {
			return c.Status(), fmt.Errorf("%w: signed in as %s, expected %s", ErrSenderMismatch, got, c.from)
		}
		email = strings.ToLower(got)
	}
	if err := c.SaveToken(email, tok, scopes); err != nil {
		return c.Status(), err
	}
	return c.Status(), nil
}

func fetchProfileEmail(ctx context.Context, hc *http.Client) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://gmail.googleapis.com/gmail/v1/users/me/profile", nil)
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		EmailAddress string `json:"emailAddress"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body.EmailAddress, nil
}

// Ref is what a successful send returns: Gmail's message and thread ids.
type Ref struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

// Send builds the RFC 5322 message, base64url-encodes it, and POSTs it to
// users/me/messages/send. It refuses before any network call when the
// sender is not connected, the token lacks gmail.send, or the message's
// From is not the one allowed sender.
func (c *Client) Send(ctx context.Context, msg Message) (Ref, error) {
	st := c.Status()
	tok, err := c.readToken()
	if err != nil || !st.Configured {
		return Ref{}, ErrUnconfigured
	}
	if !hasScope(tok.Scopes, SendScope) {
		return Ref{}, ErrNoSendScope
	}
	if !st.SendCapable {
		return Ref{}, fmt.Errorf("%w: %s", ErrSenderMismatch, st.Detail)
	}
	if msg.From == "" {
		msg.From = c.from
	}
	if !strings.EqualFold(strings.TrimSpace(msg.From), c.from) {
		return Ref{}, fmt.Errorf("%w: From %q is not %s", ErrSenderMismatch, msg.From, c.from)
	}
	raw, err := Build(msg)
	if err != nil {
		return Ref{}, err
	}
	body, err := json.Marshal(map[string]string{"raw": EncodeRaw(raw)})
	if err != nil {
		return Ref{}, err
	}

	hc, src, err := c.httpClient(ctx, tok.Token)
	if err != nil {
		return Ref{}, c.redactErr(err, tok.Token)
	}
	c.mu.Lock()
	endpoint := c.endpoint
	c.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Ref{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		// the "gmail send:" prefix is what the outreach route maps to a 502:
		// a transport failure is Gmail's, not the owner's request
		return Ref{}, c.redactErr(fmt.Errorf("gmail send: %w", err), tok.Token)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	// persist a refreshed access token so the next send does not refresh again
	if fresh, ferr := src.Token(); ferr == nil && fresh != nil && fresh.AccessToken != tok.Token.AccessToken {
		_ = c.SaveToken(tok.Email, fresh, tok.Scopes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Ref{}, c.redactErr(fmt.Errorf("gmail send: HTTP %d: %s", resp.StatusCode, gmailErrorText(out)), tok.Token)
	}
	var ref Ref
	if err := json.Unmarshal(out, &ref); err != nil || ref.ID == "" {
		return Ref{}, errors.New("gmail send: response carried no message id")
	}
	return ref, nil
}

// httpClient wraps the configured transport in the oauth2 transport, so the
// access token refreshes from the refresh token when it has expired.
func (c *Client) httpClient(ctx context.Context, tok *oauth2.Token) (*http.Client, oauth2.TokenSource, error) {
	cfg, err := gmailauth.ClientConfig(SendScope)
	if err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	base := c.hc
	c.mu.Unlock()
	if base != nil {
		ctx = context.WithValue(ctx, oauth2.HTTPClient, base)
	}
	src := oauth2.ReuseTokenSource(tok, cfg.TokenSource(ctx, tok))
	return oauth2.NewClient(ctx, src), src, nil
}

// gmailErrorText reduces an error body to Gmail's message when it has one.
func gmailErrorText(b []byte) string {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &env) == nil && env.Error.Message != "" {
		return env.Error.Message
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// redact scrubs the access and refresh tokens from any text. Every error
// this client returns after the token is read goes through it.
func redactAny(s string, tok *oauth2.Token) string {
	if tok == nil {
		return s
	}
	for _, secret := range []string{tok.AccessToken, tok.RefreshToken} {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

func (c *Client) redactErr(err error, tok *oauth2.Token) error {
	if err == nil {
		return nil
	}
	return errors.New(redactAny(err.Error(), tok))
}
