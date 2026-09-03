package gmailauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Multi-account Gmail (email-sync workspaces): the PRIMARY account keeps the
// engine's original token path (~/.config/excalibur/gmail_token); every
// additional account gets its own token file under gmail_accounts/. Alongside
// the tokens, email_accounts.json carries the per-account sync/extract/
// workspace settings the engine's email.sync_check reads — manifest owns the
// writes, the engine only reads.
//
// Because manifest runs headless (metis), connecting an account is a
// PASTE-BACK flow, not the loopback listener: StartConnect returns the Google
// consent URL (redirect to a localhost port nothing listens on); the owner
// approves in their own browser, the redirect 404s locally, and they paste
// the resulting URL back — FinishConnect exchanges the code it carries.

// Workspaces the extraction routing accepts (the two live extractor rituals).
var validWorkspaces = map[string]bool{"": true, "aion": true, "real-estate": true}

// pasteRedirectURI is the registered-loopback shape Google accepts for
// installed-app clients. Nothing ever listens on it — the code rides the
// pasted URL.
const pasteRedirectURI = "http://127.0.0.1:8123/oauth/callback"

// accountsDir holds one token file per ADDITIONAL account.
func accountsDir() string {
	if p := strings.TrimSpace(os.Getenv("GMAIL_ACCOUNTS_DIR")); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "excalibur", "gmail_accounts")
}

// settingsPath is the per-account sync/extract/workspace file (no secrets).
func settingsPath() string {
	if p := strings.TrimSpace(os.Getenv("EMAIL_ACCOUNTS_FILE")); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "excalibur", "email_accounts.json")
}

// accountSlug names an extra account's token file: lowercased email with every
// non-alphanumeric run collapsed to "-". Mirrors the engine's slug exactly.
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

func extraTokenPath(email string) string {
	return filepath.Join(accountsDir(), accountSlug(email)+".json")
}

// AccountSettings is one account's routing config (email_accounts.json entry).
type AccountSettings struct {
	Sync      *bool  `json:"sync,omitempty"`
	Extract   *bool  `json:"extract,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type settingsFile struct {
	Accounts map[string]AccountSettings `json:"accounts"`
}

func loadSettings() settingsFile {
	sf := settingsFile{Accounts: map[string]AccountSettings{}}
	if b, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(b, &sf)
		if sf.Accounts == nil {
			sf.Accounts = map[string]AccountSettings{}
		}
	}
	return sf
}

func saveSettings(sf settingsFile) error {
	if err := os.MkdirAll(filepath.Dir(settingsPath()), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), append(b, '\n'), 0o644)
}

// EffectiveSettings resolves an account's settings with the defaults the
// ENGINE also applies (the rule must agree on both sides): sync defaults on;
// extraction+workspace default to AION for the primary account (owner
// decision 2026-08-13) and off/untagged for extras. An explicit entry
// (SetAccount always writes all three fields) owns all three verbatim.
func EffectiveSettings(s AccountSettings, primary bool) (sync, extract bool, workspace string) {
	sync = s.Sync == nil || *s.Sync
	if s.Extract != nil {
		// explicit entry: trust it fully, including an empty workspace
		extract, workspace = *s.Extract, s.Workspace
	} else if primary {
		extract, workspace = true, "aion"
	}
	if !validWorkspaces[workspace] {
		workspace = ""
	}
	return sync, extract, workspace
}

// AccountRow is what the Portals accounts panel renders.
type AccountRow struct {
	Email       string `json:"email"`
	Primary     bool   `json:"primary"`
	Connected   bool   `json:"connected"`
	NeedsReauth bool   `json:"needsReauth"`
	Detail      string `json:"detail,omitempty"`
	Sync        bool   `json:"sync"`
	Extract     bool   `json:"extract"`
	Workspace   string `json:"workspace"`
}

// Accounts lists every connected account (primary first) with its effective
// settings. Only the primary is network-validated (Status's throttled check);
// extras report token presence — the engine's run summaries surface a broken
// extra loudly enough.
func (c *Client) Accounts(now time.Time) []AccountRow {
	sf := loadSettings()
	var rows []AccountRow
	st := c.Status(now)
	if st.HasToken {
		sync, extract, ws := EffectiveSettings(sf.Accounts[st.Email], true)
		rows = append(rows, AccountRow{
			Email: st.Email, Primary: true,
			Connected:   st.Connected || (st.HasToken && !st.NeedsReauth),
			NeedsReauth: st.NeedsReauth, Detail: st.Detail,
			Sync: sync, Extract: extract, Workspace: ws,
		})
	}
	entries, _ := os.ReadDir(accountsDir())
	var extras []AccountRow
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(accountsDir(), e.Name()))
		if err != nil {
			continue
		}
		var tok storedToken
		if json.Unmarshal(b, &tok) != nil || tok.Token == nil || tok.Email == "" {
			continue
		}
		sync, extract, ws := EffectiveSettings(sf.Accounts[tok.Email], false)
		extras = append(extras, AccountRow{
			Email: tok.Email, Connected: true,
			Sync: sync, Extract: extract, Workspace: ws,
		})
	}
	sort.Slice(extras, func(i, j int) bool { return extras[i].Email < extras[j].Email })
	return append(rows, extras...)
}

// SetAccount persists one account's sync/extract/workspace settings.
func (c *Client) SetAccount(email string, sync, extract bool, workspace string) error {
	workspace = strings.TrimSpace(workspace)
	if !validWorkspaces[workspace] {
		return fmt.Errorf("workspace must be aion, real-estate, or empty (got %q)", workspace)
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return errors.New("email is required")
	}
	sf := loadSettings()
	s, x := sync, extract
	sf.Accounts[email] = AccountSettings{Sync: &s, Extract: &x, Workspace: workspace}
	return saveSettings(sf)
}

// StartConnect begins the paste-back flow: it returns the consent URL and
// remembers the state token. The URL redirects to a localhost port nothing
// listens on — the owner pastes the resulting address back to FinishConnect.
func (c *Client) StartConnect() (string, error) {
	cfg, err := oauthConfig()
	if err != nil {
		return "", err
	}
	cfg.RedirectURL = pasteRedirectURI
	state := randState()
	c.mu.Lock()
	if c.pending == nil {
		c.pending = map[string]time.Time{}
	}
	for k, t := range c.pending { // prune stale attempts
		if time.Since(t) > 15*time.Minute {
			delete(c.pending, k)
		}
	}
	c.pending[state] = time.Now()
	c.mu.Unlock()
	return cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "select_account consent")), nil
}

// FinishConnect completes the paste-back flow from the URL (or query string)
// the owner pasted. The authorized account routes itself: the primary slot if
// empty or same-email (a reconnect), a gmail_accounts/ file otherwise. New
// extra accounts get a default settings entry (sync on, extraction off) so
// the panel shows them immediately.
func (c *Client) FinishConnect(ctx context.Context, pasted string) (string, bool, error) {
	code, state, err := parsePasted(pasted)
	if err != nil {
		return "", false, err
	}
	c.mu.Lock()
	_, ok := c.pending[state]
	if ok {
		delete(c.pending, state)
	}
	c.mu.Unlock()
	if !ok {
		return "", false, errors.New("this sign-in attempt is stale — press connect again and use the fresh link")
	}
	cfg, err := oauthConfig()
	if err != nil {
		return "", false, err
	}
	cfg.RedirectURL = pasteRedirectURI
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return "", false, fmt.Errorf("code exchange failed: %w", err)
	}
	if tok.RefreshToken == "" {
		return "", false, errors.New("Google returned no refresh token — revoke this app at myaccount.google.com/permissions and retry")
	}
	email, err := fetchProfileEmail(ctx, cfg.Client(ctx, tok))
	if err != nil || email == "" {
		return "", false, errors.New("couldn't read the account's address after sign-in")
	}
	email = strings.ToLower(email)

	primaryTok, primaryErr := readToken()
	isPrimary := primaryErr != nil || strings.EqualFold(primaryTok.Email, email)
	if isPrimary {
		if err := writeToken(email, tok); err != nil {
			return "", false, err
		}
		c.mu.Lock()
		c.cached = State{HasCreds: true, HasToken: true, Connected: true, Email: email, CheckedAt: time.Now()}
		c.checkedAt = time.Now()
		c.mu.Unlock()
	} else {
		if err := os.MkdirAll(accountsDir(), 0o700); err != nil {
			return "", false, err
		}
		b, err := json.MarshalIndent(storedToken{Email: email, Token: tok}, "", "  ")
		if err != nil {
			return "", false, err
		}
		if err := os.WriteFile(extraTokenPath(email), b, 0o600); err != nil {
			return "", false, err
		}
		sf := loadSettings()
		if _, have := sf.Accounts[email]; !have {
			on, off := true, false
			sf.Accounts[email] = AccountSettings{Sync: &on, Extract: &off}
			_ = saveSettings(sf)
		}
	}
	return email, isPrimary, nil
}

// DisconnectAccount removes an EXTRA account's token and settings entry; the
// primary routes through Disconnect (its settings entry is kept — a reconnect
// of the same address picks the routing back up).
func (c *Client) DisconnectAccount(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if tok, err := readToken(); err == nil && strings.EqualFold(tok.Email, email) {
		return c.Disconnect()
	}
	if err := os.Remove(extraTokenPath(email)); err != nil && !os.IsNotExist(err) {
		return err
	}
	sf := loadSettings()
	delete(sf.Accounts, email)
	return saveSettings(sf)
}

// parsePasted digs code+state out of whatever the owner pasted: the full
// redirect URL, or just its query string.
func parsePasted(pasted string) (code, state string, err error) {
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return "", "", errors.New("paste the address bar URL from the sign-in tab")
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

// ---- shared with gmailsend (Phase 5) ----
//
// The send-only client reuses this package's OAuth client file and paste-back
// mechanics rather than forking them. Nothing below changes what THIS package
// requests: the read-only engine token is still minted at gmail.readonly and
// only ever at gmail.readonly. gmailsend holds its own token file, its own
// scope, and its own pending-state map.

// CredPath is the shared OAuth client file (GMAIL_OAUTH_CLIENT override).
func CredPath() string { return credPath() }

// PasteRedirectURI is the registered loopback shape the paste-back flow uses.
const PasteRedirectURI = pasteRedirectURI

// ClientConfig reads the shared OAuth client at the given scopes. It is the
// only way another package gets an *oauth2.Config from the client file, and
// it takes the scopes explicitly so the caller's consent is visible at the
// call site.
func ClientConfig(scopes ...string) (*oauth2.Config, error) {
	b, err := os.ReadFile(credPath())
	if err != nil {
		return nil, fmt.Errorf("gmail: OAuth client not found (%s)", credPath())
	}
	return google.ConfigFromJSON(b, scopes...)
}

// ParsePasted digs code+state out of a pasted redirect URL (or query string).
func ParsePasted(pasted string) (code, state string, err error) { return parsePasted(pasted) }

// RandState mints an OAuth state token.
func RandState() string { return randState() }
