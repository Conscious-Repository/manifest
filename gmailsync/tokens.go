// Package gmailsync syncs OODA portal members' OWN mailboxes into the shared
// email-candidate store (ooda-portal email plan, 2026-08-21). It is the
// manifest-side sibling of the engine's email-sync pipeline: the same
// deterministic thread→note conversion and confirm-first state machine, but
// per PORTAL MEMBER (tokens minted at portal sign-in via Policy.ExtraScopes)
// and landing in a shared store instead of the owner's vault. No LLM runs
// here — extraction happens later, when a confirmed note is spooled to the
// extractor with its text inline.
package gmailsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// storedAccount is one member's Gmail grant: a superset of gmailauth's
// storedToken ({email, token}) so the file stays readable by anything that
// speaks that shape, plus the reauth state the sync loop maintains.
type storedAccount struct {
	Email string        `json:"email"`
	Token *oauth2.Token `json:"token"`
	// NeedsReauth is set when a refresh fails (revoked grant / invalid_grant).
	// The sync loop skips the account; the portal FEED shows "reconnect".
	// Signing in again overwrites the whole record and clears it.
	NeedsReauth bool   `json:"needsReauth,omitempty"`
	Error       string `json:"error,omitempty"`     // last refresh failure, human-readable
	CheckedAt   string `json:"checkedAt,omitempty"` // RFC3339 of the last successful use
}

// Account is the read-side view (no token material).
type Account struct {
	Email       string `json:"email"`
	NeedsReauth bool   `json:"needsReauth"`
	Error       string `json:"error,omitempty"`
	CheckedAt   string `json:"checkedAt,omitempty"`
}

// Tokens is the per-member token store: one 0600 JSON file per mailbox under
// a 0700 dir (<dataDir>/portals/ooda-gmail). Files are the store — no index
// to drift; the slug convention matches gmailauth so an operator reading the
// dir sees familiar names.
type Tokens struct {
	dir string
	mu  sync.Mutex
}

// NewTokens roots the store, creating the directory 0700.
func NewTokens(dir string) (*Tokens, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Tokens{dir: dir}, nil
}

// accountSlug mirrors gmailauth.accountSlug byte-for-byte: lowercased email,
// every non-alphanumeric run collapsed to one dash, trimmed.
func accountSlug(email string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(email)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (t *Tokens) path(email string) string {
	return filepath.Join(t.dir, accountSlug(email)+".json")
}

// Put stores (or replaces) a member's grant — the sign-in token sink. A fresh
// sign-in always clears any needs-reauth state: the new refresh token IS the
// re-authorization.
func (t *Tokens) Put(email string, tok *oauth2.Token) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || tok == nil || tok.RefreshToken == "" {
		return nil // nothing storable — never write a record that cannot refresh
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.write(storedAccount{Email: email, Token: tok})
}

func (t *Tokens) write(st storedAccount) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := t.path(st.Email) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, t.path(st.Email))
}

func (t *Tokens) read(email string) (storedAccount, bool) {
	var st storedAccount
	b, err := os.ReadFile(t.path(email))
	if err != nil || json.Unmarshal(b, &st) != nil || st.Email == "" {
		return storedAccount{}, false
	}
	return st, true
}

// List returns every connected account (no token material), sorted by email.
func (t *Tokens) List() []Account {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries, _ := os.ReadDir(t.dir)
	var out []Account
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var st storedAccount
		b, err := os.ReadFile(filepath.Join(t.dir, e.Name()))
		if err != nil || json.Unmarshal(b, &st) != nil || st.Email == "" {
			continue
		}
		out = append(out, Account{Email: st.Email, NeedsReauth: st.NeedsReauth, Error: st.Error, CheckedAt: st.CheckedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out
}

// Status reports one member's connection state. Not connected at all →
// (Account{}, false): the FEED renders "connect Gmail" (which for this portal
// just means signing in again — the scope rides the login).
func (t *Tokens) Status(email string) (Account, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.read(strings.ToLower(strings.TrimSpace(email)))
	if !ok {
		return Account{}, false
	}
	return Account{Email: st.Email, NeedsReauth: st.NeedsReauth, Error: st.Error, CheckedAt: st.CheckedAt}, true
}

// MarkNeedsReauth records a refresh failure; the account sits out sync until
// the member signs in again.
func (t *Tokens) MarkNeedsReauth(email string, cause error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.read(email)
	if !ok {
		return
	}
	st.NeedsReauth = true
	if cause != nil {
		st.Error = cause.Error()
	}
	_ = t.write(st)
}

// touch advances CheckedAt after a successful use, persisting any refreshed
// token so the next process start does not burn a refresh round trip.
func (t *Tokens) touch(email string, tok *oauth2.Token, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.read(email)
	if !ok {
		return
	}
	if tok != nil && tok.RefreshToken != "" {
		st.Token = tok
	} else if tok != nil && st.Token != nil {
		// Google often omits the refresh token on refresh responses — keep ours.
		keep := st.Token.RefreshToken
		st.Token = tok
		st.Token.RefreshToken = keep
	}
	st.NeedsReauth, st.Error = false, ""
	st.CheckedAt = now.UTC().Format(time.RFC3339)
	_ = t.write(st)
}

// Source returns an oauth2.TokenSource for the account that persists refreshed
// tokens and flips needs-reauth on failure. ok=false when the account is not
// connected or is sitting out pending re-auth.
func (t *Tokens) Source(email string, cfg *oauth2.Config) (oauth2.TokenSource, bool) {
	t.mu.Lock()
	st, ok := t.read(strings.ToLower(strings.TrimSpace(email)))
	t.mu.Unlock()
	if !ok || st.NeedsReauth || st.Token == nil {
		return nil, false
	}
	base := cfg.TokenSource(oauth2.NoContext, st.Token)
	return &persistingSource{tokens: t, email: st.Email, base: base}, true
}

// persistingSource wraps a refresh-capable source with the store's bookkeeping.
type persistingSource struct {
	tokens *Tokens
	email  string
	base   oauth2.TokenSource
}

func (p *persistingSource) Token() (*oauth2.Token, error) {
	tok, err := p.base.Token()
	if err != nil {
		// A refresh failure here is almost always a revoked grant
		// (invalid_grant); transient network errors also land the account in
		// needs-reauth, which a fresh sign-in clears — fail safe, not silent.
		p.tokens.MarkNeedsReauth(p.email, err)
		return nil, err
	}
	p.tokens.touch(p.email, tok, time.Now())
	return tok, nil
}
