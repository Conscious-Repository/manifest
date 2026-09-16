package gmailauth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrimaryBindingNoFallbackOrCredentialMutation(t *testing.T) {
	d := t.TempDir()
	t.Setenv("GMAIL_TOKEN", filepath.Join(d, "primary"))
	t.Setenv("GMAIL_ACCOUNTS_DIR", filepath.Join(d, "accounts"))
	t.Setenv("EMAIL_ACCOUNTS_FILE", filepath.Join(d, "settings"))
	raw := []byte(`{"email":"owner@example.com","token":{"access_token":"fixture"}}`)
	os.WriteFile(tokenPath(), raw, 0600)
	path, on, extract, workspace, err := PrimaryBinding("owner@example.com")
	if err != nil || path != tokenPath() || !on || !extract || workspace != "aion" {
		t.Fatal(path, on, extract, workspace, err)
	}
	if _, _, _, _, err = PrimaryBinding("other@example.com"); err == nil {
		t.Fatal("account mismatch accepted")
	}
	os.MkdirAll(accountsDir(), 0700)
	os.WriteFile(extraTokenPath("extra@example.com"), []byte(`{"email":"extra@example.com","token":{"access_token":"fixture"}}`), 0600)
	bindings, err := ExtraBindings()
	if err != nil || len(bindings) != 1 || bindings[0].Account != "extra@example.com" || !bindings[0].Sync {
		t.Fatal(bindings, err)
	}
	after, _ := os.ReadFile(tokenPath())
	if string(after) != string(raw) {
		t.Fatal("credential changed")
	}
}

func TestConfiguredAccountsWithoutTokens(t *testing.T) {
	d := t.TempDir()
	t.Setenv("EMAIL_ACCOUNTS_FILE", filepath.Join(d, "settings"))
	t.Setenv("GMAIL_ACCOUNTS_DIR", filepath.Join(d, "absent"))
	for _, raw := range []string{`{"accounts":{"extra@example.com":{"sync":false}}}`, `{"accounts":{}}`, `broken`, `null`, `{"accounts":{"extra@example.com":null}}`, `{"accounts":{"extra@example.com":{},"extra@example.com":{}}}`, `{"accounts":{},"accounts":{}}`, `{"accounts":{}} {}`, `{"accounts":{"UNKNOWN":{}}}`} {
		if err := os.WriteFile(settingsPath(), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		accounts, err := ConfiguredAccounts()
		switch raw {
		case `{"accounts":{"extra@example.com":{"sync":false}}}`:
			if err != nil || len(accounts) != 1 || accounts[0] != "extra@example.com" {
				t.Fatal(accounts, err)
			}
			bindings, err := ExtraBindings()
			if err != nil || len(bindings) != 0 {
				t.Fatal(bindings, err)
			}
		case `{"accounts":{}}`:
			if err != nil || len(accounts) != 0 {
				t.Fatal(accounts, err)
			}
		default:
			if err == nil {
				t.Fatal("invalid settings accepted", raw)
			}
		}
		after, _ := os.ReadFile(settingsPath())
		if string(after) != raw {
			t.Fatal("settings rewritten")
		}
	}
}
