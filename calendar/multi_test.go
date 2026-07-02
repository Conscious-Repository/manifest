package calendar

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func tok(access string) *oauth2.Token {
	return &oauth2.Token{AccessToken: access, RefreshToken: "r-" + access}
}

const credFixture = `{"installed":{"client_id":"x.apps.googleusercontent.com","client_secret":"s","redirect_uris":["http://localhost"],"auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token"}}`

func writeCreds(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "google_credentials.json"), []byte(credFixture), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAccountTokenRoundTrip(t *testing.T) {
	t.Setenv("MANIFEST_CONFIG_DIR", t.TempDir())
	if err := saveAccountToken("Me@Gmail.com", tok("a1")); err != nil {
		t.Fatal(err)
	}
	if err := saveAccountToken("work@corp.com", tok("a2")); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range loadAllAccounts() {
		got[a.Email] = a.Token.AccessToken
	}
	if len(got) != 2 || got["Me@Gmail.com"] != "a1" || got["work@corp.com"] != "a2" {
		t.Fatalf("round-trip mismatch: %v", got)
	}
	if fi, _ := os.Stat(accountTokenPath("Me@Gmail.com")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file perms: %v", fi.Mode().Perm())
	}
	if di, _ := os.Stat(tokensDir()); di.Mode().Perm() != 0o700 {
		t.Fatalf("tokens dir perms: %v", di.Mode().Perm())
	}
}

func TestSanitizeEmailDistinct(t *testing.T) {
	if sanitizeEmail("a@b.com") == sanitizeEmail("a@c.com") {
		t.Fatal("distinct emails must map to distinct files")
	}
}

func TestRemoveAccountTokenIdempotent(t *testing.T) {
	t.Setenv("MANIFEST_CONFIG_DIR", t.TempDir())
	_ = saveAccountToken("x@y.com", tok("a"))
	if err := removeAccountToken("x@y.com"); err != nil {
		t.Fatal(err)
	}
	if err := removeAccountToken("x@y.com"); err != nil {
		t.Fatalf("second remove should be nil: %v", err)
	}
	if len(loadAllAccounts()) != 0 {
		t.Fatal("expected no accounts after remove")
	}
}

func TestLoadAllAccountsTolerant(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MANIFEST_CONFIG_DIR", dir)
	if len(loadAllAccounts()) != 0 {
		t.Fatal("missing tokens dir should yield no accounts")
	}
	_ = saveAccountToken("good@x.com", tok("a"))
	_ = os.WriteFile(filepath.Join(tokensDir(), "corrupt.json"), []byte("{not json"), 0o600)
	accts := loadAllAccounts()
	if len(accts) != 1 || accts[0].Email != "good@x.com" {
		t.Fatalf("a corrupt file must be skipped, not fatal: %v", accts)
	}
}

func TestLegacyTokenMigrationShape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MANIFEST_CONFIG_DIR", dir)
	b, _ := json.Marshal(tok("legacy"))
	_ = os.WriteFile(filepath.Join(dir, "google_token.json"), b, 0o600)
	accts := loadAllAccounts()
	if len(accts) != 1 || accts[0].Email != "" || accts[0].Token.AccessToken != "legacy" {
		t.Fatalf("legacy token should load as one empty-email entry: %v", accts)
	}
}

func TestClientDisabledNoCreds(t *testing.T) {
	t.Setenv("MANIFEST_CONFIG_DIR", t.TempDir())
	c := NewClient(context.Background(), "")
	if c.HasCreds() || c.Enabled() || c.NeedsAuth() {
		t.Fatalf("no creds -> all false; got hasCreds=%v enabled=%v needsAuth=%v", c.HasCreds(), c.Enabled(), c.NeedsAuth())
	}
	d := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	if _, err := c.Events(context.Background(), d, d.Add(24*time.Hour)); err == nil {
		t.Fatal("Events should error when not configured")
	}
}

func TestClientNeedsAuthWithCredsNoToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MANIFEST_CONFIG_DIR", dir)
	writeCreds(t, dir)
	c := NewClient(context.Background(), "")
	if !c.HasCreds() || c.Enabled() || !c.NeedsAuth() {
		t.Fatalf("creds but no token -> hasCreds && needsAuth && !enabled; got %v %v %v", c.HasCreds(), c.Enabled(), c.NeedsAuth())
	}
}

func TestClientEnabledWithAccount(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MANIFEST_CONFIG_DIR", dir)
	writeCreds(t, dir)
	_ = saveAccountToken("me@x.com", tok("a"))
	c := NewClient(context.Background(), "")
	if !c.Enabled() || c.NeedsAuth() {
		t.Fatalf("an account -> enabled && !needsAuth; got enabled=%v needsAuth=%v", c.Enabled(), c.NeedsAuth())
	}
	if accts := c.Accounts(); len(accts) != 1 || accts[0] != "me@x.com" {
		t.Fatalf("accounts: %v", accts)
	}
}

// Events merged across accounts must still resolve slot collisions by earliest
// start time, regardless of the order accounts/goroutines contribute them.
func TestEventsToSlotsMergedMultiAccount(t *testing.T) {
	day := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	d := func(h, m int) time.Time { return time.Date(2026, 6, 30, h, m, 0, 0, time.UTC) }
	merged := []Event{
		{ID: "work-1", Title: "Work standup", Start: d(9, 0), End: d(9, 30)},
		{ID: "personal-1", Title: "Gym", Start: d(8, 30), End: d(9, 0)},
		{ID: "work-2", Title: "1:1", Start: d(9, 0), End: d(9, 30)}, // same slot, later in list
	}
	byTok := map[string]Slot{}
	for _, s := range EventsToSlots(merged, day, time.UTC) {
		byTok[s.Token] = s
	}
	if byTok["8:30A"].Title != "Gym" {
		t.Fatalf("8:30A should be Gym: %+v", byTok["8:30A"])
	}
	if byTok["9:00A"].EventID != "work-1" {
		t.Fatalf("9:00A should go to the earliest-start event work-1: %+v", byTok["9:00A"])
	}
}
