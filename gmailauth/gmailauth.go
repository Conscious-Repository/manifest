// Package gmailauth lets the manifest dashboard mint and validate the Gmail
// read-only OAuth token that the excalibur engine's `gmail.threads` cast reads.
//
// The engine's gmail cast is headless (it runs under launchd and can't drive a
// browser), so the interactive reconnect belongs here — manifest is the
// user-facing process, and it already owns the shared Google OAuth client
// (~/.config/manifest/google_credentials.json) used for Calendar. This package
// runs the same loopback flow at the gmail.readonly scope and writes the token
// to the engine's path (~/.config/excalibur/gmail_token) in the exact
// {email, token} shape the engine unmarshals. No engine changes required.
//
// Validation is non-blocking: Status() returns the last cached result
// immediately and refreshes in the background on a throttle, so the FEED
// emitter and the Portals row never stall a request on a network call.
package gmailauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// gmailReadonlyScope mirrors the engine — the ONLY scope ever requested.
const gmailReadonlyScope = "https://www.googleapis.com/auth/gmail.readonly"

// revalidateEvery bounds how often Status triggers a network check.
const revalidateEvery = 10 * time.Minute

// credPath is the shared OAuth client (same file the Calendar flow uses). The
// engine reads the same client via GMAIL_OAUTH_CLIENT; we honor that override so
// both sides resolve to one client.
func credPath() string {
	if p := strings.TrimSpace(os.Getenv("GMAIL_OAUTH_CLIENT")); p != "" {
		return p
	}
	if d := os.Getenv("MANIFEST_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "google_credentials.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "manifest", "google_credentials.json")
}

// tokenPath is the engine's gmail token location (GMAIL_TOKEN override matches
// the engine so both agree).
func tokenPath() string {
	if p := strings.TrimSpace(os.Getenv("GMAIL_TOKEN")); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "excalibur", "gmail_token")
}

// storedToken is byte-compatible with the engine's storedGmailToken.
type storedToken struct {
	Email string        `json:"email"`
	Token *oauth2.Token `json:"token"`
}

// State is the reconnect surface both the Portals row and the FEED emitter read.
type State struct {
	HasCreds    bool   // the shared OAuth client is present
	HasToken    bool   // a token file exists (gmail was connected at some point)
	Connected   bool   // token exists AND validates right now
	NeedsReauth bool   // token exists but the refresh token is dead (invalid_grant)
	Email       string // the connected account, when known
	Detail      string // human note (last error, etc.)
	CheckedAt   time.Time
}

// Client holds the throttled validation cache. One instance is shared by the
// server (Portals row + endpoints) and the signals emitter.
type Client struct {
	mu        sync.Mutex
	cached    State
	checkedAt time.Time
	inflight  bool
}

func New() *Client { return &Client{} }

func hasCreds() bool {
	_, err := os.Stat(credPath())
	return err == nil
}

func oauthConfig() (*oauth2.Config, error) {
	b, err := os.ReadFile(credPath())
	if err != nil {
		return nil, fmt.Errorf("gmail: OAuth client not found (%s)", credPath())
	}
	return google.ConfigFromJSON(b, gmailReadonlyScope)
}

func readToken() (*storedToken, error) {
	b, err := os.ReadFile(tokenPath())
	if err != nil {
		return nil, err
	}
	var st storedToken
	if err := json.Unmarshal(b, &st); err != nil || st.Token == nil {
		return nil, fmt.Errorf("bad token file")
	}
	return &st, nil
}

// Status returns the cached auth state immediately and, if the cache is stale,
// kicks a background revalidation. It never blocks on the network. The cheap,
// offline-safe facts (creds present, token file present, last known email) are
// filled synchronously so the row renders sensibly even before the first check.
func (c *Client) Status(now time.Time) State {
	c.mu.Lock()
	st := c.cached
	stale := c.checkedAt.IsZero() || now.Sub(c.checkedAt) > revalidateEvery
	busy := c.inflight
	c.mu.Unlock()

	// synchronous, offline facts (may differ from the cached network verdict)
	st.HasCreds = hasCreds()
	if tok, err := readToken(); err == nil {
		st.HasToken = true
		if st.Email == "" {
			st.Email = tok.Email
		}
	} else {
		st.HasToken = false
	}

	if stale && !busy {
		c.mu.Lock()
		c.inflight = true
		c.mu.Unlock()
		go c.refresh()
	}
	return st
}

// AuthState is the signals.GmailAuthChecker surface: it returns whether the
// engine's Gmail sign-in has genuinely expired (token present, refresh dead),
// and triggers a background revalidation. It never errors and never reports
// reauth before the first successful check, so no false nag on startup.
func (c *Client) AuthState(now time.Time) (needsReauth bool, email, detail string, err error) {
	st := c.Status(now)
	return st.NeedsReauth, st.Email, st.Detail, nil
}

// refresh runs the actual network validation and updates the cache.
func (c *Client) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	st := c.validate(ctx)
	c.mu.Lock()
	c.cached = st
	c.checkedAt = time.Now()
	c.inflight = false
	c.mu.Unlock()
}

// validate reads the token and confirms it can still reach Gmail. A dead refresh
// token (invalid_grant) → NeedsReauth. A transient network error leaves the prior
// verdict untouched by returning it with a note (never a false "needs reauth").
func (c *Client) validate(ctx context.Context) State {
	st := State{HasCreds: hasCreds(), CheckedAt: time.Now()}
	if !st.HasCreds {
		st.Detail = "OAuth client credentials missing"
		return st
	}
	tok, err := readToken()
	if err != nil {
		st.Detail = "not connected"
		return st // HasToken=false, NeedsReauth=false: setup state, not a failure
	}
	st.HasToken = true
	st.Email = tok.Email
	cfg, err := oauthConfig()
	if err != nil {
		st.Detail = err.Error()
		return st
	}
	// TokenSource refreshes the access token from the refresh token; a revoked
	// refresh token surfaces here as an *oauth2.RetrieveError (invalid_grant).
	fresh, err := cfg.TokenSource(ctx, tok.Token).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			st.NeedsReauth = true
			st.Detail = "sign-in expired (" + reauthReason(re) + ")"
			return st
		}
		// transient (network/DNS) — keep whatever we last knew; don't flap
		c.mu.Lock()
		prev := c.cached
		c.mu.Unlock()
		prev.Detail = "couldn't verify (offline?)"
		prev.CheckedAt = st.CheckedAt
		prev.HasCreds, prev.HasToken, prev.Email = st.HasCreds, st.HasToken, st.Email
		return prev
	}
	// confirm the token actually works with a cheap profile read
	if email, err := fetchProfileEmail(ctx, cfg.Client(ctx, fresh)); err == nil {
		st.Connected = true
		if email != "" {
			st.Email = email
		}
		st.Detail = ""
	} else {
		// token refreshed but the API rejected it — treat as reauth needed
		st.NeedsReauth = true
		st.Detail = "sign-in rejected by Gmail"
	}
	return st
}

func reauthReason(re *oauth2.RetrieveError) string {
	if re.ErrorCode != "" {
		return re.ErrorCode
	}
	return "invalid_grant"
}

// fetchProfileEmail hits users/me/profile — the cheapest authenticated Gmail call.
func fetchProfileEmail(ctx context.Context, hc *http.Client) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://gmail.googleapis.com/gmail/v1/users/me/profile", nil)
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		EmailAddress string `json:"emailAddress"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body.EmailAddress, nil
}

// Connect runs the loopback OAuth flow (opens the browser, shows the account
// chooser), writes the engine's token file, and returns the account email. The
// cache is set to Connected synchronously so the UI reflects it immediately.
func (c *Client) Connect(ctx context.Context) (string, error) {
	cfg, err := oauthConfig()
	if err != nil {
		return "", err
	}
	tok, err := authorize(ctx, cfg)
	if err != nil {
		return "", err
	}
	if tok.RefreshToken == "" {
		return "", errors.New("Google returned no refresh token — revoke this app at myaccount.google.com/permissions and retry")
	}
	email, _ := fetchProfileEmail(ctx, cfg.Client(ctx, tok))
	if err := writeToken(email, tok); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.cached = State{HasCreds: true, HasToken: true, Connected: true, Email: email, CheckedAt: time.Now()}
	c.checkedAt = time.Now()
	c.mu.Unlock()
	return email, nil
}

// Disconnect removes the engine's token file.
func (c *Client) Disconnect() error {
	err := os.Remove(tokenPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	c.mu.Lock()
	c.cached = State{HasCreds: hasCreds(), CheckedAt: time.Now()}
	c.checkedAt = time.Now()
	c.mu.Unlock()
	return nil
}

func writeToken(email string, tok *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(tokenPath()), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(storedToken{Email: email, Token: tok}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tokenPath(), b, 0o600)
}

// authorize is the installed-app loopback flow (mirrors calendar/oauth.go).
func authorize(ctx context.Context, cfg *oauth2.Config) (*oauth2.Token, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	cfg.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", ln.Addr().(*net.TCPAddr).Port)

	state := randState()
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- errors.New("oauth state mismatch")
			return
		}
		fmt.Fprintln(w, "Manifest reconnected Gmail for the executive-assistant digest. You can close this tab.")
		codeCh <- r.URL.Query().Get("code")
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	openBrowser(cfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "select_account consent")))

	select {
	case code := <-codeCh:
		return cfg.Exchange(ctx, code)
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func randState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	_ = exec.Command(cmd, append(args, url)...).Start()
}
