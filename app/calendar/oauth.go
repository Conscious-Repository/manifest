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
	"runtime"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gcal "google.golang.org/api/calendar/v3"
)

// ErrNotConfigured means no Google credentials/token are present; the app runs
// fine without calendar.
var ErrNotConfigured = errors.New("calendar not configured")

// Secrets live OUTSIDE the vault and repo, under ~/.config/manifest/
// (overridable via MANIFEST_CONFIG_DIR for tests).
func configDir() string {
	if d := os.Getenv("MANIFEST_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "manifest")
}

func credPath() string  { return filepath.Join(configDir(), "google_credentials.json") }
func tokenPath() string { return filepath.Join(configDir(), "google_token.json") }

func oauthConfig() (*oauth2.Config, error) {
	b, err := os.ReadFile(credPath())
	if err != nil {
		return nil, ErrNotConfigured
	}
	return google.ConfigFromJSON(b, gcal.CalendarReadonlyScope)
}

func loadToken() (*oauth2.Token, error) {
	b, err := os.ReadFile(tokenPath())
	if err != nil {
		return nil, err
	}
	var t oauth2.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func saveToken(t *oauth2.Token) error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tokenPath(), b, 0o600) // token file is owner-only
}

// savingTokenSource persists refreshed tokens back to disk (the oauth2 library
// refreshes in memory but won't re-save).
type savingTokenSource struct {
	src  oauth2.TokenSource
	last *oauth2.Token
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	t, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	if s.last == nil || t.AccessToken != s.last.AccessToken {
		_ = saveToken(t)
		s.last = t
	}
	return t, nil
}

// Authorize runs the installed-app loopback OAuth flow: it opens the browser,
// captures the code on a transient 127.0.0.1 listener, exchanges it, and caches
// the token (with a refresh token).
func Authorize(ctx context.Context) error {
	cfg, err := oauthConfig()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
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
		fmt.Fprintln(w, "Manifest is connected to Google Calendar. You can close this tab.")
		codeCh <- r.URL.Query().Get("code")
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())

	openBrowser(cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce))

	select {
	case code := <-codeCh:
		tok, err := cfg.Exchange(ctx, code)
		if err != nil {
			return err
		}
		return saveToken(tok)
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Disconnect removes the cached token (credentials are left in place).
func Disconnect() error {
	err := os.Remove(tokenPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
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
