package server

// SETTINGS — the app-wide tab (spirits-settings-agents plan §4.2, Phase 1).
// Every endpoint here is a PROJECTION of files and process state that already
// exist: config.json as loaded (in-memory), env-var PRESENCE, the portal rows,
// the bank-feed store, the gmail-send token, the fundraising sheet block, and
// the owner's ~/.hermes tree. Nothing is cached or stored, and no secret value
// is ever returned — env rows are {name, set}, keys are the masked previews
// the portal rows already carry (D3: Hosts & paths is read-only).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HostsInfo is the read-only projection main.go builds from the loaded Config
// (the server package cannot import package main's Config). Paths only — no
// tokens, no cookies, no key contents; TerminalDevice.Identity is a key PATH.
type HostsInfo struct {
	Vault struct {
		VaultPath     string `json:"vaultPath"`
		SystemRoot    string `json:"systemRoot"`
		ExtrinsicRoot string `json:"extrinsicRoot"`
		NewDailyDir   string `json:"newDailyDir"`
	} `json:"vault"`
	Data struct {
		DataDir string `json:"dataDir"`
	} `json:"data"`
	Harnesses []HostsHarness `json:"harnesses"`
	Files     struct {
		Roots  []string     `json:"roots"`
		Agents []HostsAgent `json:"agents"`
	} `json:"files"`
	TerminalDevices []HostsDevice `json:"terminalDevices"`
	Listeners       struct {
		Port              int `json:"port"`
		PortalPort        int `json:"portalPort"`
		OodaPort          int `json:"oodaPort"`
		ConsumePublicPort int `json:"consumePublicPort"` // 0 = off
	} `json:"listeners"`
	Consume struct {
		RSSIntervalMinutes int    `json:"rssIntervalMinutes"`
		XIntervalMinutes   int    `json:"xIntervalMinutes"` // a spending decision
		RSSHubBase         string `json:"rsshubBase"`
	} `json:"consume"`
	Hermes struct {
		Enabled        bool   `json:"enabled"`
		Bin            string `json:"bin"`
		TimeoutSeconds int    `json:"timeoutSeconds"`
	} `json:"hermes"`
	Fundraising struct {
		Enabled             bool   `json:"enabled"`
		SpreadsheetID       string `json:"spreadsheetId"`
		CredentialsPath     string `json:"credentialsPath"`
		SyncIntervalMinutes int    `json:"syncIntervalMinutes"`
	} `json:"fundraising"`
}

type HostsHarness struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Surface string `json:"surface,omitempty"`
}

type HostsAgent struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type HostsDevice struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	User     string `json:"user"`
	Port     int    `json:"port,omitempty"`
	Identity string `json:"identity,omitempty"` // a key PATH, never contents
	Agent    string `json:"agent,omitempty"`
}

// UseHosts wires the config projection (main.go). Optional — without it the
// hosts endpoint answers with empty groups and env presence only.
func (s *Server) UseHosts(h HostsInfo) { s.hosts = &h }

// envPresence is one {name, set} row. Value is filled ONLY for path-class
// variables (a directory, never a credential).
type envPresence struct {
	Name  string `json:"name"`
	Set   bool   `json:"set"`
	Value string `json:"value,omitempty"`
}

// settingsEnvVars is the fixed list the Hosts group reports. Secrets get
// presence only; the two path variables also show their value.
var settingsEnvVars = []struct {
	name string
	path bool
}{
	{"MANIFEST_CONFIG_DIR", true},
	{"HERMES_HOME", true},
	{"ASHBY_API_KEY", false},
	{"ASHBY_WEBHOOK_SECRET", false},
	{"GMAIL_SEND_FROM", false},
	{"POCKET_API_KEY", false},
	{xTokenEnv, false},
	{"LAB_MODEL_URL", false},
}

func settingsEnv() []envPresence {
	out := make([]envPresence, 0, len(settingsEnvVars))
	for _, v := range settingsEnvVars {
		val := strings.TrimSpace(os.Getenv(v.name))
		row := envPresence{Name: v.name, Set: val != ""}
		if v.path && row.Set {
			row.Value = val
		}
		out = append(out, row)
	}
	return out
}

// hermesHome is the owner's Hermes state root: $HERMES_HOME, else ~/.hermes.
func hermesHome() string {
	if v := strings.TrimSpace(os.Getenv("HERMES_HOME")); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hermes")
}

// GET /api/settings/hosts → the config projection + env presence.
func (s *Server) handleSettingsHosts(w http.ResponseWriter, _ *http.Request) {
	info := HostsInfo{}
	if s.hosts != nil {
		info = *s.hosts
	}
	if info.Harnesses == nil {
		info.Harnesses = []HostsHarness{}
	}
	if info.Files.Roots == nil {
		info.Files.Roots = []string{}
	}
	if info.Files.Agents == nil {
		info.Files.Agents = []HostsAgent{}
	}
	if info.TerminalDevices == nil {
		info.TerminalDevices = []HostsDevice{}
	}
	credsPresent := false
	if p := info.Fundraising.CredentialsPath; p != "" {
		if _, err := os.Stat(p); err == nil {
			credsPresent = true
		}
	}
	writeJSON(w, map[string]any{
		"config":                 info,
		"hermesHome":             hermesHome(),
		"fundraisingCredsPresent": credsPresent,
		"env":                    settingsEnv(),
		"file":                   "config.json (read-only — edit on metis and restart)",
	})
}

// ---- Connections: the composed row list ----

// GET /api/settings/connections → every service row of §4.2: the portal rows
// (minus the engine's LLM conduits, which belong to the Agents card) plus
// bank feed, Gmail send, the fundraising sheet, and the Ashby env rows.
// Ordering (problems first, then alphabetical) is the client's — a repaired
// row re-sorts in place without a refetch.
func (s *Server) handleSettingsConnections(w http.ResponseWriter, _ *http.Request) {
	rows := []panelRow{}
	for _, r := range s.portalRows() {
		if r.Kind == "llm" || r.ID == deepseekID {
			continue
		}
		rows = append(rows, r)
	}
	rows = append(rows, s.bankfeedConnectionRow())
	rows = append(rows, s.gmailSendConnectionRow())
	rows = append(rows, s.fundraisingConnectionRow())
	rows = append(rows, envConnectionRow("ashby-api", "Ashby (API key)", "ASHBY_API_KEY",
		"the private recruiting client — pushes candidates, syncs applicants"))
	rows = append(rows, envConnectionRow("ashby-webhook", "Ashby (webhook)", "ASHBY_WEBHOOK_SECRET",
		"signs inbound applicant deliveries; missing = the receiver answers 503"))
	writeJSON(w, map[string]any{"rows": rows})
}

// envConnectionRow is a read-only row whose only source of truth is an
// environment variable: open when set, sealed ("missing") otherwise. The key
// column shows the variable NAME, never its value.
func envConnectionRow(id, name, env, note string) panelRow {
	row := panelRow{ID: id, Name: name, Kind: "env", Env: env, Masked: "env: " + env, Note: note}
	if strings.TrimSpace(os.Getenv(env)) != "" {
		row.State = "open"
	} else {
		row.State = "sealed"
		row.Err = "missing — set " + env + " in the environment on metis"
	}
	return row
}

// bankfeedConnectionRow projects the SimpleFIN bridge: sealed until the
// one-time setup token is claimed, degraded when any linked account's last
// sync failed. Extra carries the counts the row line reads.
func (s *Server) bankfeedConnectionRow() panelRow {
	row := panelRow{ID: "bankfeed", Name: "SimpleFIN bank feed", Kind: "bankfeed", Masked: "access URL (0600, dataDir)"}
	if s.bankFeed == nil {
		row.State, row.Note = "sealed", "not available — real-estate + vault stores required"
		row.Extra = map[string]any{"available": false, "claimed": false}
		return row
	}
	if !s.bankFeed.Claimed() {
		row.State, row.Note = "sealed", "claim a SimpleFIN setup token to link bank accounts"
		row.Extra = map[string]any{"available": true, "claimed": false}
		return row
	}
	row.State = "open"
	links := s.bankFeed.Store().Links()
	linked, paused := 0, 0
	for _, l := range links {
		if l.EntitySlug == "" {
			continue
		}
		linked++
		if !l.Enabled {
			paused++
		}
		if l.LastSync > row.LastCrossing {
			row.LastCrossing = l.LastSync
		}
		if l.LastError != "" && row.Err == "" {
			row.State, row.Err = "degraded", l.AccountLabel+": "+l.LastError
		}
	}
	row.Note = plural(linked, "1 account linked", " accounts linked") + " · daily sync → the $ tab"
	if paused > 0 {
		row.Note += " · " + plural(paused, "1 paused", " paused")
	}
	row.Extra = map[string]any{"available": true, "claimed": true, "linked": linked, "paused": paused}
	return row
}

// gmailSendConnectionRow projects the recruiting sender: open when the token
// carries gmail.send for the allowed From address, degraded when a token
// exists but cannot send (wrong scope / wrong account), sealed otherwise.
func (s *Server) gmailSendConnectionRow() panelRow {
	row := panelRow{ID: "gmail-send", Name: "Gmail (send, recruiting)", Kind: "gmailsend", Masked: "oauth · gmail.send"}
	if s.gmailSend == nil {
		row.State, row.Note = "sealed", "not wired"
		return row
	}
	st := s.gmailSend.Status()
	row.Masked = st.Sender
	if st.Email != "" {
		row.Accounts = []string{st.Email}
	}
	switch {
	case st.SendCapable:
		row.State, row.Note = "open", "outreach sends as "+st.Sender+" · every send is approved by hand"
	case st.Configured:
		row.State, row.Err = "degraded", st.Detail
	case !st.HasCreds:
		row.State, row.Note = "sealed", "add ~/.config/manifest/google_credentials.json, then connect"
	default:
		row.State, row.Note = "sealed", "connect the sender (gmail.send only) — drafts work without it; a send refuses"
	}
	row.Extra = map[string]any{"sender": st.Sender, "sendCapable": st.SendCapable, "hasCreds": st.HasCreds}
	return row
}

// fundraisingConnectionRow is read-only (§4.2): the config block + whether the
// service-account file is present. Conflict resolution stays in AION ›
// Fundraising; the sync toggle is config.
func (s *Server) fundraisingConnectionRow() panelRow {
	row := panelRow{ID: "fundraising-sheet", Name: "Fundraising Sheet", Kind: "info", Masked: "—"}
	if s.hosts == nil {
		row.State, row.Note = "sealed", "config not projected"
		return row
	}
	f := s.hosts.Fundraising
	credsPresent := false
	if f.CredentialsPath != "" {
		if _, err := os.Stat(f.CredentialsPath); err == nil {
			credsPresent = true
		}
	}
	if id := f.SpreadsheetID; len(id) > 8 {
		row.Masked = id[:4] + "…" + id[len(id)-4:]
	} else if id != "" {
		row.Masked = id
	}
	interval := f.SyncIntervalMinutes
	if interval <= 0 {
		interval = 60
	}
	switch {
	case !f.Enabled:
		row.State, row.Note = "sealed", "disabled in config.json (fundraisingSheets.enabled)"
	case !credsPresent:
		row.State, row.Err = "degraded", "enabled, but the service-account file is missing at "+f.CredentialsPath
	default:
		row.State = "open"
		row.Note = "sync every " + plural(interval, "1 minute", " minutes") + " · conflicts resolve in AION › Fundraising"
	}
	row.Extra = map[string]any{
		"enabled": f.Enabled, "credentialsPresent": credsPresent,
		"credentialsPath": f.CredentialsPath, "syncIntervalMinutes": interval,
	}
	if s.fundraisingSync != nil {
		row.Extra["status"] = s.fundraisingSync.Status()
	}
	return row
}

// ---- Gmail send: connect / disconnect from Settings ----
// The same paste-back flow calendar and gmail-read take, on the send-only
// client, mounted independently of the recruiting block so Settings works
// whether or not the recruiting store is wired.

func (s *Server) gmailSendReady(w http.ResponseWriter) bool {
	if s.gmailSend == nil {
		http.Error(w, "sending unavailable — no Gmail send client wired", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *Server) handleSettingsGmailSendStart(w http.ResponseWriter, _ *http.Request) {
	if !s.gmailSendReady(w) {
		return
	}
	u, err := s.gmailSend.StartConnect()
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, map[string]any{"authUrl": u, "sender": s.gmailSend.Sender()})
}

func (s *Server) handleSettingsGmailSendFinish(w http.ResponseWriter, r *http.Request) {
	if !s.gmailSendReady(w) {
		return
	}
	var b struct {
		Redirect string `json:"redirect"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Redirect) == "" {
		httpError(w, errBadRequest("paste the address the sign-in tab landed on"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if _, err := s.gmailSend.FinishConnect(ctx, b.Redirect); err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, s.gmailSendConnectionRow())
}

func (s *Server) handleSettingsGmailSendDisconnect(w http.ResponseWriter, _ *http.Request) {
	if !s.gmailSendReady(w) {
		return
	}
	if err := s.gmailSend.Disconnect(); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.gmailSendConnectionRow())
}

// ---- Agents: the Alfred (Hermes) card ----
// GET /api/agents/hermes reads the owner's ~/.hermes tree directly (D4) —
// gateway_state.json, the cron ticker heartbeat, jobs.json — and shells out
// to `hermes profile list` (read-only). Every piece degrades on its own: a
// missing or reshaped file yields a nil/zero field, never an error.

type hermesProfile struct {
	Name         string `json:"name"`
	Active       bool   `json:"active"`
	Model        string `json:"model"`
	Gateway      string `json:"gateway"`
	Alias        string `json:"alias"`
	Distribution string `json:"distribution"`
}

func (s *Server) handleAgentsHermes(w http.ResponseWriter, r *http.Request) {
	home := hermesHome()
	out := map[string]any{
		"home":   home,
		"runner": map[string]any{"enabled": s.hermes != nil},
	}
	if s.hosts != nil {
		out["runner"] = map[string]any{
			"enabled": s.hosts.Hermes.Enabled && s.hermes != nil,
			"bin":     s.hosts.Hermes.Bin, "timeoutSeconds": s.hosts.Hermes.TimeoutSeconds,
		}
	}
	// gateway
	if b, err := os.ReadFile(filepath.Join(home, "gateway_state.json")); err == nil {
		var gs struct {
			State        string                    `json:"gateway_state"`
			ActiveAgents int                       `json:"active_agents"`
			UpdatedAt    string                    `json:"updated_at"`
			Platforms    map[string]map[string]any `json:"platforms"`
		}
		if json.Unmarshal(b, &gs) == nil {
			platforms := []map[string]any{}
			names := make([]string, 0, len(gs.Platforms))
			for n := range gs.Platforms {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				p := gs.Platforms[n]
				platforms = append(platforms, map[string]any{"name": n, "state": p["state"], "error": p["error_message"]})
			}
			out["gateway"] = map[string]any{"state": gs.State, "activeAgents": gs.ActiveAgents, "updatedAt": gs.UpdatedAt, "platforms": platforms}
		}
	}
	// cron ticker heartbeat (mtime) + jobs
	cron := map[string]any{}
	if st, err := os.Stat(filepath.Join(home, "cron", "ticker_heartbeat")); err == nil {
		cron["heartbeat"] = st.ModTime().UTC().Format(time.RFC3339)
	}
	if b, err := os.ReadFile(filepath.Join(home, "cron", "jobs.json")); err == nil {
		var jf struct {
			Jobs []struct {
				Name    string `json:"name"`
				Enabled *bool  `json:"enabled"`
			} `json:"jobs"`
		}
		if json.Unmarshal(b, &jf) == nil {
			enabled := 0
			for _, j := range jf.Jobs {
				if j.Enabled == nil || *j.Enabled {
					enabled++
				}
			}
			cron["jobs"] = len(jf.Jobs)
			cron["enabled"] = enabled
		}
	}
	out["cron"] = cron
	// profiles (shell, read-only, bounded)
	bin := "hermes"
	if s.hosts != nil && s.hosts.Hermes.Bin != "" {
		bin = s.hosts.Hermes.Bin
	}
	if profiles, err := hermesProfiles(r.Context(), bin); err == nil {
		out["profiles"] = profiles
	} else {
		out["profiles"] = []hermesProfile{}
		out["profilesErr"] = err.Error()
	}
	writeJSON(w, out)
}

// hermesProfiles parses `hermes profile list` (a column table: Profile ·
// Model · Gateway · Alias · Distribution; the active row is marked ◆).
func hermesProfiles(ctx context.Context, bin string) ([]hermesProfile, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "profile", "list")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return parseHermesProfiles(buf.String()), nil
}

func parseHermesProfiles(text string) []hermesProfile {
	out := []hermesProfile{}
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "Profile") || strings.HasPrefix(t, "─") {
			continue
		}
		cols := splitColumns(t)
		if len(cols) == 0 {
			continue
		}
		p := hermesProfile{}
		name := cols[0]
		if strings.HasPrefix(name, "◆") {
			p.Active = true
			name = strings.TrimSpace(strings.TrimPrefix(name, "◆"))
		}
		p.Name = name
		get := func(i int) string {
			if i < len(cols) && cols[i] != "—" {
				return cols[i]
			}
			return ""
		}
		p.Model, p.Gateway, p.Alias, p.Distribution = get(1), get(2), get(3), get(4)
		out = append(out, p)
	}
	return out
}

// splitColumns splits a table row on runs of two or more spaces.
func splitColumns(s string) []string {
	var cols []string
	for _, part := range strings.Split(s, "  ") {
		part = strings.TrimSpace(part)
		if part != "" {
			cols = append(cols, part)
		}
	}
	return cols
}
