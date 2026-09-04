package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"manifest/portals"
)

// The X row in the PORTALS panel.
//
// §10 splits this cleanly: the API token is a CONDUIT and belongs to the system
// level (here), while WHICH accounts to follow is domain data and is managed in
// the CONSUME view's subscription panel. Neither knows about the other beyond
// this row's nudge.
//
// The token lives in the secrets tier — <dataDir>/consume/x-creds.json, 0600,
// never in the vault, never in the repo, never logged, masked in the UI —
// with the same env-override convention as portals/store.go so a key can be
// injected without touching disk.

const consumeXID = "x"

const xTokenEnv = "MANIFEST_PORTAL_X_TOKEN"

func (s *Server) xTokenPath() string { return s.consumeXTokenPath }

// consumeXToken returns the effective bearer token: env wins, then the file.
func (s *Server) consumeXToken() string {
	if v := strings.TrimSpace(os.Getenv(xTokenEnv)); v != "" {
		return v
	}
	b, _ := os.ReadFile(s.xTokenPath())
	return strings.TrimSpace(string(b))
}

func (s *Server) consumeXReady() bool { return s.consumeXToken() != "" }

// consumeXPortalRow assembles the row: sealed without a token, and degraded
// from whatever the X subscriptions' last polls actually reported — so a dead
// token or an empty credit balance shows up here rather than as a lane that
// quietly stopped filling.
func (s *Server) consumeXPortalRow() panelRow {
	row := panelRow{
		ID: consumeXID, Name: "X (reading)", Kind: "apikey",
		Fields: []portals.CredField{{
			Key: "token", Label: "API bearer token", Secret: true,
			Hint: "X developer console → Keys and tokens → Bearer Token",
		}},
	}
	token := s.consumeXToken()
	if token == "" {
		row.State = "sealed"
		row.Note = "add a bearer token to follow X accounts in CONSUME · pay-per-use, ~$0.005 per post read"
		return row
	}
	row.State = "open"
	row.Masked = maskKey(token)
	if strings.TrimSpace(os.Getenv(xTokenEnv)) != "" {
		row.Note = "token from " + xTokenEnv
		row.Env = xTokenEnv
	}

	if s.consume == nil {
		return row
	}
	// The X subscriptions' own status IS this portal's health.
	accounts, degraded := 0, ""
	for _, st := range s.consume.Statuses() {
		if st.Kind != "x" {
			continue
		}
		accounts++
		if st.LastErr != "" && degraded == "" {
			degraded = st.LastErr
		}
		if st.LastOK != "" && st.LastOK > row.LastCrossing {
			row.LastCrossing = st.LastOK
		}
	}
	switch {
	case degraded != "":
		row.State, row.Err = "degraded", degraded
	case accounts == 0:
		row.Note = strings.TrimSpace(row.Note + " · no X accounts followed yet — add one in CONSUME")
	}
	return row
}

// handleConsumeXKey writes/replaces the token (0600). It is deliberately NOT
// validated with a live call: every X read costs money, and spending it to
// check a paste is the wrong default — the next poll reports the truth.
func (s *Server) handleConsumeXKey(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(os.Getenv(xTokenEnv)) != "" {
		httpError(w, errBadRequest(xTokenEnv+" is set in the environment — it overrides the file; unset it to manage the token here"))
		return
	}
	var b struct {
		Fields map[string]string `json:"fields"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	token := strings.TrimSpace(b.Fields["token"])
	if token == "" {
		httpError(w, errBadRequest("token is required"))
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.xTokenPath()), 0o700); err != nil {
		httpError(w, err)
		return
	}
	if err := os.WriteFile(s.xTokenPath(), []byte(token+"\n"), 0o600); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.consumeXPortalRow())
}

// handleConsumeXDisconnect removes the token file.
func (s *Server) handleConsumeXDisconnect(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(os.Getenv(xTokenEnv)) != "" {
		httpError(w, errBadRequest(xTokenEnv+" is set in the environment — unset it there; nothing to remove here"))
		return
	}
	if err := os.Remove(s.xTokenPath()); err != nil && !os.IsNotExist(err) {
		httpError(w, err)
		return
	}
	writeJSON(w, s.consumeXPortalRow())
}

// handleConsumeXTest reports the row without spending a read. "Test" for a
// metered portal means "show me what the last real poll found".
func (s *Server) handleConsumeXTest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.consumeXPortalRow())
}
