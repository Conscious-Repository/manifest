package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The hosts projection reports env PRESENCE only: a secret set in the
// environment must never appear in the body, while its name does with
// set:true; the two path variables carry their (non-secret) value.
func TestSettingsHostsNeverLeaksSecrets(t *testing.T) {
	const secret = "sk-live-THIS-MUST-NOT-LEAK-7q2"
	t.Setenv("ASHBY_API_KEY", secret)
	t.Setenv("POCKET_API_KEY", "pk_"+secret)
	t.Setenv("MANIFEST_CONFIG_DIR", "/tmp/manifest-cfg")

	s := New(nil, nil, nil)
	var info HostsInfo
	info.Vault.VaultPath = "/vault"
	info.Fundraising.Enabled = true
	info.Fundraising.SpreadsheetID = "1abcdefghijklmnop9"
	s.UseHosts(info)

	w := httptest.NewRecorder()
	s.handleSettingsHosts(w, httptest.NewRequest(http.MethodGet, "/api/settings/hosts", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, secret) {
		t.Fatalf("secret value leaked into /api/settings/hosts: %s", body)
	}
	var out struct {
		Env []envPresence `json:"env"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	seen := map[string]envPresence{}
	for _, e := range out.Env {
		seen[e.Name] = e
	}
	if !seen["ASHBY_API_KEY"].Set || seen["ASHBY_API_KEY"].Value != "" {
		t.Fatalf("ASHBY_API_KEY should be set:true with no value: %+v", seen["ASHBY_API_KEY"])
	}
	if seen["MANIFEST_CONFIG_DIR"].Value != "/tmp/manifest-cfg" {
		t.Fatalf("path var should carry its value: %+v", seen["MANIFEST_CONFIG_DIR"])
	}
	if !strings.Contains(body, `"vaultPath":"/vault"`) {
		t.Fatalf("config projection missing: %s", body)
	}
}

// Connections composes the portal rows with the bank feed, Gmail send,
// fundraising and Ashby env rows; the engine's LLM conduits stay out (they
// belong to the Agents card) and DocuSign is gone for good.
func TestSettingsConnectionsComposition(t *testing.T) {
	t.Setenv("ASHBY_API_KEY", "set-but-never-shown")
	t.Setenv("ASHBY_WEBHOOK_SECRET", "")
	t.Setenv("MANIFEST_CONFIG_DIR", t.TempDir())

	s := New(nil, nil, nil)
	var info HostsInfo
	info.Fundraising.Enabled = true
	info.Fundraising.SpreadsheetID = "1abcdefghijklmnop9"
	info.Fundraising.CredentialsPath = "/nonexistent/creds.json"
	s.UseHosts(info)

	w := httptest.NewRecorder()
	s.handleSettingsConnections(w, httptest.NewRequest(http.MethodGet, "/api/settings/connections", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "set-but-never-shown") {
		t.Fatalf("env value leaked: %s", body)
	}
	if strings.Contains(strings.ToLower(body), "docusign") {
		t.Fatalf("docusign row still present: %s", body)
	}
	var out struct {
		Rows []panelRow `json:"rows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	byID := map[string]panelRow{}
	for _, r := range out.Rows {
		if r.Kind == "llm" {
			t.Fatalf("llm conduit %s leaked into Connections", r.ID)
		}
		byID[r.ID] = r
	}
	for _, id := range []string{"google-calendar", "gmail", "gmail-send", "bankfeed", "fundraising-sheet", "ashby-api", "ashby-webhook", "aside"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("row %s missing from Connections: %v", id, rowIDs(byID))
		}
	}
	if r := byID["ashby-api"]; r.State != "open" || r.Env != "ASHBY_API_KEY" || r.Kind != "env" {
		t.Fatalf("ashby-api row wrong: %+v", r)
	}
	if r := byID["ashby-webhook"]; r.State != "sealed" || !strings.Contains(r.Err, "missing") {
		t.Fatalf("ashby-webhook row should be sealed/missing: %+v", r)
	}
	if r := byID["fundraising-sheet"]; r.State != "degraded" || r.Masked != "1abc…nop9" {
		t.Fatalf("fundraising row should be degraded (creds missing) with a masked id: %+v", r)
	}
	if r := byID["bankfeed"]; r.State != "sealed" {
		t.Fatalf("bankfeed row without a service should be sealed: %+v", r)
	}
}

func rowIDs(m map[string]panelRow) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestParseHermesProfiles(t *testing.T) {
	text := ` Profile          Model                        Gateway      Alias        Distribution
 ───────────────    ───────────────────────────    ───────────    ───────────    ────────────────────
 ◆default         deepseek-v4-flash-vision-e   running      —            —
 scratch          claude-opus-5                stopped      alf          —
`
	got := parseHermesProfiles(text)
	if len(got) != 2 {
		t.Fatalf("want 2 profiles, got %d: %+v", len(got), got)
	}
	if !got[0].Active || got[0].Name != "default" || got[0].Model != "deepseek-v4-flash-vision-e" || got[0].Gateway != "running" || got[0].Alias != "" {
		t.Fatalf("default row wrong: %+v", got[0])
	}
	if got[1].Active || got[1].Name != "scratch" || got[1].Alias != "alf" || got[1].Gateway != "stopped" {
		t.Fatalf("scratch row wrong: %+v", got[1])
	}
}
