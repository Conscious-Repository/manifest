package calendar

import (
	"context"
	"testing"
	"time"
)

func TestParsePasted(t *testing.T) {
	cases := []struct {
		name              string
		in                string
		wantCode, wantErr bool
	}{
		{"full url", "http://127.0.0.1:8123/oauth/callback?state=abc&code=xyz", true, false},
		{"query only", "state=abc&code=xyz", true, false},
		{"leading question mark", "?state=abc&code=xyz", true, false},
		{"empty", "", false, true},
		{"denied", "http://127.0.0.1:8123/oauth/callback?error=access_denied&state=abc", false, true},
		{"missing code", "http://127.0.0.1:8123/oauth/callback?state=abc", false, true},
		{"missing state", "http://127.0.0.1:8123/oauth/callback?code=xyz", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, state, err := parsePasted(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got code=%q state=%q", code, state)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.wantCode && (code == "" || state == "") {
				t.Fatalf("expected code+state, got code=%q state=%q", code, state)
			}
		})
	}
}

// A pasted URL whose state was never issued by StartConnect must be rejected as
// stale — this is the CSRF guard on the paste-back flow.
func TestFinishConnectRejectsUnknownState(t *testing.T) {
	t.Setenv("MANIFEST_CONFIG_DIR", t.TempDir()) // no creds -> oauthConfig fails first anyway
	c := &Client{}
	_, err := c.FinishConnect(context.Background(),
		"http://127.0.0.1:8123/oauth/callback?state=never-issued&code=xyz")
	if err == nil {
		t.Fatal("expected FinishConnect to reject an unissued state")
	}
}

// AccountStatuses with no connected accounts returns an empty slice and never
// blocks (no creds, no network).
func TestAccountStatusesEmpty(t *testing.T) {
	t.Setenv("MANIFEST_CONFIG_DIR", t.TempDir())
	c := &Client{}
	got := c.AccountStatuses(time.Now())
	if len(got) != 0 {
		t.Fatalf("expected no statuses, got %d", len(got))
	}
}
