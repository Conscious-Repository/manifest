package calendar

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
	"regexp"
	"runtime"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gcal "google.golang.org/api/calendar/v3"
)

// ErrNotConfigured means no Google credentials/account are present; the app runs
// fine without calendar.
var ErrNotConfigured = errors.New("calendar not configured")

// Secrets live OUTSIDE the vault and repo, under ~/.config/manifest/
// (overridable via MANIFEST_CONFIG_DIR for tests). The OAuth client credentials
// are shared across accounts; each connected Google account has its own token
// file under tokens/.
func configDir() string {
	if d := os.Getenv("MANIFEST_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "manifest")
}

func credPath() string    { return filepath.Join(configDir(), "google_credentials.json") }
func tokensDir() string   { return filepath.Join(configDir(), "tokens") }
func legacyToken() string { return filepath.Join(configDir(), "google_token.json") }

func oauthConfig() (*oauth2.Config, error) {
	b, err := os.ReadFile(credPath())
	if err != nil {
		return nil, ErrNotConfigured
	}
	return google.ConfigFromJSON(b, gcal.CalendarReadonlyScope)
}

// ----- per-account token storage -----

// accountToken is the on-disk wrapper: the email is authoritative (the filename
// is only a sanitized, filesystem-safe handle).
type accountToken struct {
	Email string        `json:"email"`
	Token *oauth2.Token `json:"token"`
}

var emailUnsafeRe = regexp.MustCompile(`[^a-z0-9._-]`)

func sanitizeEmail(email string) string {
	return emailUnsafeRe.ReplaceAllString(strings.ToLower(email), "_")
}

func accountTokenPath(email string) string {
	return filepath.Join(tokensDir(), sanitizeEmail(email)+".json")
}

func saveAccountToken(email string, tok *oauth2.Token) error {
	if err := os.MkdirAll(tokensDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(accountToken{Email: email, Token: tok}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(accountTokenPath(email), b, 0o600) // owner-only
}

func removeAccountToken(email string) error {
	err := os.Remove(accountTokenPath(email))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// loadAllAccounts reads every tokens/*.json (tolerant: a missing dir or a single
// unparseable file never disables calendar). If no per-account tokens exist but a
// legacy single google_token.json does, it is surfaced as one empty-email entry
// (its email is resolved + migrated lazily on first use).
func loadAllAccounts() []accountToken {
	var out []accountToken
	seen := map[string]bool{}
	entries, _ := os.ReadDir(tokensDir())
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(tokensDir(), e.Name()))
		if err != nil {
			continue
		}
		var a accountToken
		if err := json.Unmarshal(b, &a); err != nil || a.Token == nil {
			continue
		}
		if a.Email != "" {
			if seen[a.Email] {
				continue
			}
			seen[a.Email] = true
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		if b, err := os.ReadFile(legacyToken()); err == nil {
			var t oauth2.Token
			if json.Unmarshal(b, &t) == nil && t.AccessToken != "" {
				out = append(out, accountToken{Email: "", Token: &t})
			}
		}
	}
	return out
}

// savingTokenSource persists refreshed tokens back to a SPECIFIC account's file
// (the oauth2 library refreshes in memory but won't re-save).
type savingTokenSource struct {
	email string
	src   oauth2.TokenSource
	last  *oauth2.Token
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	t, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	if s.email != "" && (s.last == nil || t.AccessToken != s.last.AccessToken) {
		_ = saveAccountToken(s.email, t)
		s.last = t
	}
	return t, nil
}

// Authorize runs the installed-app loopback OAuth flow and RETURNS the token (the
// caller resolves the account email and saves it). `select_account` makes Google
// show the account chooser so each connect can pick a DIFFERENT account;
// `consent` guarantees a refresh token is issued. NB: oauth2.ApprovalForce is
// itself prompt=consent, so we must pass a single combined prompt param.
func Authorize(ctx context.Context) (*oauth2.Token, error) {
	cfg, err := oauthConfig()
	if err != nil {
		return nil, err
	}
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
		fmt.Fprintln(w, "Manifest is connected to this Google account. You can close this tab.")
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
