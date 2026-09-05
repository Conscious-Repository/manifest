package gmailauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccountSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Ben@ooda.group":           "ben-ooda-group",
		"me@benjaminbanderson.com": "me-benjaminbanderson-com",
		"  A.B+c@X.io ":            "a-b-c-x-io",
	} {
		if got := accountSlug(in); got != want {
			t.Errorf("accountSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEffectiveSettings pins the defaults rule the ENGINE mirrors
// (engine casts.emailSettingsFor — the two must agree).
func TestEffectiveSettings(t *testing.T) {
	on, off := true, false
	// primary default: sync on, extract on, aion
	if s, x, w := EffectiveSettings(AccountSettings{}, true); !s || !x || w != "aion" {
		t.Fatalf("primary default = %v %v %q", s, x, w)
	}
	// extra default: sync on, extract off, untagged
	if s, x, w := EffectiveSettings(AccountSettings{}, false); !s || x || w != "" {
		t.Fatalf("extra default = %v %v %q", s, x, w)
	}
	// explicit entry owns all three (incl. clearing the primary's aion default)
	if _, x, w := EffectiveSettings(AccountSettings{Sync: &on, Extract: &off, Workspace: ""}, true); x || w != "" {
		t.Fatalf("cleared primary = %v %q", x, w)
	}
	if s, x, w := EffectiveSettings(AccountSettings{Sync: &on, Extract: &on, Workspace: "real-estate"}, false); !s || !x || w != "real-estate" {
		t.Fatalf("re extra = %v %v %q", s, x, w)
	}
	// junk workspace never routes
	if _, _, w := EffectiveSettings(AccountSettings{Extract: &on, Workspace: "junk"}, false); w != "" {
		t.Fatalf("junk workspace = %q", w)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EMAIL_ACCOUNTS_FILE", filepath.Join(dir, "email_accounts.json"))
	c := New()
	if err := c.SetAccount("ben@ooda.group", true, true, "real-estate"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetAccount("x@y.com", false, false, ""); err != nil {
		t.Fatal(err)
	}
	if err := c.SetAccount("bad@y.com", true, true, "nope"); err == nil {
		t.Fatal("junk workspace must refuse")
	}
	sf := loadSettings()
	s, x, w := EffectiveSettings(sf.Accounts["ben@ooda.group"], false)
	if !s || !x || w != "real-estate" {
		t.Fatalf("round-trip = %v %v %q", s, x, w)
	}
	if s, _, _ := EffectiveSettings(sf.Accounts["x@y.com"], false); s {
		t.Fatal("x@y.com should be sync off")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "email_accounts.json")); len(b) == 0 {
		t.Fatal("settings file not written")
	}
}

func TestParsePasted(t *testing.T) {
	code, state, err := parsePasted("http://127.0.0.1:8123/oauth/callback?state=abc&code=4%2Fxyz&scope=gmail")
	if err != nil || code != "4/xyz" || state != "abc" {
		t.Fatalf("full URL: %q %q %v", code, state, err)
	}
	if _, _, err := parsePasted("state=abc&code=4xyz"); err != nil {
		t.Fatalf("bare query: %v", err)
	}
	if _, _, err := parsePasted("http://127.0.0.1:8123/oauth/callback?error=access_denied"); err == nil {
		t.Fatal("denied sign-in must error")
	}
	if _, _, err := parsePasted("https://google.com"); err == nil {
		t.Fatal("code-less URL must error")
	}
	if _, _, err := parsePasted(""); err == nil {
		t.Fatal("empty paste must error")
	}
}
