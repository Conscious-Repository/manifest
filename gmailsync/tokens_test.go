package gmailsync

import (
	"errors"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestTokensLifecycle(t *testing.T) {
	st, err := NewTokens(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// no refresh token → nothing stored (an unticked Gmail checkbox is not a
	// connection)
	if err := st.Put("a@b.com", &oauth2.Token{AccessToken: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Status("a@b.com"); ok {
		t.Fatal("refresh-token-less grant must not be stored")
	}
	tok := &oauth2.Token{AccessToken: "x", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}
	if err := st.Put("A@B.com", tok); err != nil {
		t.Fatal(err)
	}
	acc, ok := st.Status("a@b.com")
	if !ok || acc.Email != "a@b.com" || acc.NeedsReauth {
		t.Fatalf("status after put: %+v ok=%v", acc, ok)
	}
	st.MarkNeedsReauth("a@b.com", errors.New("invalid_grant"))
	if acc, _ := st.Status("a@b.com"); !acc.NeedsReauth || acc.Error == "" {
		t.Fatalf("needs-reauth not recorded: %+v", acc)
	}
	// a fresh sign-in clears the state
	if err := st.Put("a@b.com", tok); err != nil {
		t.Fatal(err)
	}
	if acc, _ := st.Status("a@b.com"); acc.NeedsReauth {
		t.Fatal("fresh sign-in must clear needs-reauth")
	}
	if got := len(st.List()); got != 1 {
		t.Fatalf("list = %d accounts, want 1", got)
	}
	// a needs-reauth account yields no source
	st.MarkNeedsReauth("a@b.com", nil)
	if _, ok := st.Source("a@b.com", &oauth2.Config{}); ok {
		t.Fatal("needs-reauth account must not yield a token source")
	}
}
