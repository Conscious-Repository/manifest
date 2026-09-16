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
