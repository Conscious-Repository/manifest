package gmailauth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadSourceNeverFallsBackToPrimary(t *testing.T) {
	d := t.TempDir()
	t.Setenv("GMAIL_TOKEN", filepath.Join(d, "primary"))
	t.Setenv("GMAIL_ACCOUNTS_DIR", filepath.Join(d, "accounts"))
	t.Setenv("GMAIL_OAUTH_CLIENT", filepath.Join(d, "client"))
	os.WriteFile(tokenPath(), []byte(`{"email":"ben@aion.bio","token":{"access_token":"fixture"}}`), 0600)
	c := New()
	if _, err := c.ReadSource(context.Background(), "ben@ooda.group"); err == nil {
		t.Fatal("fell back to primary")
	}
	os.MkdirAll(accountsDir(), 0700)
	os.WriteFile(extraTokenPath("ben@ooda.group"), []byte(`{"email":"other@example.com","token":{"access_token":"fixture"}}`), 0600)
	if _, err := c.ReadSource(context.Background(), "ben@ooda.group"); err == nil {
		t.Fatal("trusted filename over mailbox identity")
	}
	os.WriteFile(extraTokenPath("ben@ooda.group"), []byte(`{"email":"ben@ooda.group","token":{"access_token":"fixture"}}`), 0600)
	os.WriteFile(credPath(), []byte(`{"installed":{"client_id":"fixture","client_secret":"fixture","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","redirect_uris":["http://localhost"]}}`), 0600)
	if _, err := c.ReadSource(context.Background(), "ben@ooda.group"); err != nil {
		t.Fatal(err)
	}
}
