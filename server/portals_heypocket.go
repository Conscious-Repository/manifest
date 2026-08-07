package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/portals"
)

// HeyPocket is an ENGINE-managed portal: the excalibur pocket-sync ritual (9a/6p)
// polls it and files call transcripts into the vault as create-vault-note
// approvals — manifest never polls it. But its API key lives at
// ~/.config/excalibur/pocket_key (the engine client's location), so the PORTALS
// panel surfaces it here: connection state + last sync, and set/replace the key
// (written to the engine's key file, 0600). This mirrors how calendar/LLM/aside
// rows are server-assembled for realms managed outside the portals package.

const heypocketID = "heypocket"

// pocketKeyPath is where the excalibur pocket client reads the key (its default;
// POCKET_API_KEY env still wins for the engine at runtime).
func pocketKeyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "excalibur", "pocket_key")
}

// pocketKey returns the effective key (env override, else the file), trimmed.
func pocketKey() string {
	if v := strings.TrimSpace(os.Getenv("POCKET_API_KEY")); v != "" {
		return v
	}
	b, _ := os.ReadFile(pocketKeyPath())
	return strings.TrimSpace(string(b))
}

func maskKey(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 4 {
		if s == "" {
			return ""
		}
		return "····" + s
	}
	return "····" + s[len(s)-4:]
}

// heypocketPortalRow assembles the HeyPocket row from the engine key file + the
// pocket sync watermark.
func (s *Server) heypocketPortalRow() panelRow {
	row := panelRow{
		ID: heypocketID, Name: "HeyPocket", Kind: "apikey", Engine: true,
		Fields: []portals.CredField{{
			Key: "apiKey", Label: "API key", Secret: true,
			Hint: "pk_…  (HeyPocket app → Settings → API)",
		}},
	}
	key := pocketKey()
	if key == "" {
		row.State = "sealed"
		row.Note = "add your HeyPocket key — the engine's pocket-sync ritual then files call transcripts into the vault (9a/6p)"
		return row
	}
	row.State = "open"
	row.Masked = maskKey(key)
	if strings.TrimSpace(os.Getenv("POCKET_API_KEY")) != "" {
		row.Note = "engine-synced (9a/6p) · key from POCKET_API_KEY env"
	} else {
		row.Note = "engine-synced (9a/6p) · key at ~/.config/excalibur/pocket_key"
	}
	// last sync = the watermark the pocket-sync ritual advances
	if s.spirits != nil {
		wm := filepath.Join(s.spirits.Root(), "vessel", "state", "pocket", "watermark")
		if b, err := os.ReadFile(wm); err == nil {
			if t := strings.TrimSpace(string(b)); t != "" {
				row.LastCrossing = t
			}
		}
	}
	return row
}

// handleHeypocketKey writes/replaces the HeyPocket API key at the engine's key
// path (0600) and auto-tests it. Env-override case is refused (the file wouldn't
// take effect). Called from handlePortalKey when id == heypocket.
func (s *Server) handleHeypocketKey(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(os.Getenv("POCKET_API_KEY")) != "" {
		httpError(w, errBadRequest("POCKET_API_KEY is set in the environment — it overrides the file; unset it to manage the key here"))
		return
	}
	var b struct {
		Fields map[string]string `json:"fields"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	key := strings.TrimSpace(b.Fields["apiKey"])
	if key == "" {
		httpError(w, errBadRequest("apiKey is required"))
		return
	}
	if !strings.HasPrefix(key, "pk_") {
		httpError(w, errBadRequest("that doesn't look like a HeyPocket key (expected pk_…)"))
		return
	}
	if err := os.MkdirAll(filepath.Dir(pocketKeyPath()), 0o700); err != nil {
		httpError(w, err)
		return
	}
	if err := os.WriteFile(pocketKeyPath(), []byte(key+"\n"), 0o600); err != nil {
		httpError(w, err)
		return
	}
	// auto-test the freshly saved key
	row := s.heypocketPortalRow()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := pocketPing(ctx, key); err != nil {
		row.State, row.Err = "degraded", "key saved but the test call failed: "+err.Error()
	}
	writeJSON(w, row)
}

// handleHeypocketTest hits the HeyPocket API with the current key.
func (s *Server) handleHeypocketTest(w http.ResponseWriter, r *http.Request) {
	row := s.heypocketPortalRow()
	if row.State == "sealed" {
		writeJSON(w, row)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := pocketPing(ctx, pocketKey()); err != nil {
		row.State, row.Err = "degraded", err.Error()
	}
	writeJSON(w, row)
}

// handleHeypocketDisconnect removes the engine key file (env-managed keys can't
// be removed here).
func (s *Server) handleHeypocketDisconnect(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(os.Getenv("POCKET_API_KEY")) != "" {
		httpError(w, errBadRequest("POCKET_API_KEY is set in the environment — unset it there; nothing to remove here"))
		return
	}
	if err := os.Remove(pocketKeyPath()); err != nil && !os.IsNotExist(err) {
		httpError(w, err)
		return
	}
	writeJSON(w, s.heypocketPortalRow())
}

// pocketPing validates a HeyPocket key with a cheap list call (limit=1).
func pocketPing(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://public.heypocketai.com/api/v1/public/recordings?limit=1", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("key rejected (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d — %s", resp.StatusCode, strings.TrimSpace(string(body[:min(len(body), 120)])))
	}
	return nil
}
