package gmailauth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
)

// PrimaryBinding reads the existing primary token metadata and settings only.
// Credentials never leave this package and are never copied or refreshed here.
func PrimaryBinding(account string) (path string, sync, extract bool, workspace string, err error) {
	st, e := readToken()
	if e != nil || st == nil || st.Token == nil || !strings.EqualFold(st.Email, account) {
		err = fmt.Errorf("primary mailbox binding mismatch")
		return
	}
	sf := settingsFile{Accounts: map[string]AccountSettings{}}
	b, e := os.ReadFile(settingsPath())
	if e != nil && !os.IsNotExist(e) {
		err = fmt.Errorf("mailbox settings unavailable")
		return
	}
	if e == nil && json.Unmarshal(b, &sf) != nil {
		err = fmt.Errorf("mailbox settings invalid")
		return
	}
	path = tokenPath()
	sync, extract, workspace = EffectiveSettings(sf.Accounts[strings.ToLower(account)], true)
	return
}
func (c *Client) PrimaryReadSource(ctx context.Context, account string) (oauth2.TokenSource, error) {
	if _, _, _, _, err := PrimaryBinding(account); err != nil {
		return nil, err
	}
	st, err := readToken()
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(st.Email, account) {
		return nil, fmt.Errorf("primary mailbox changed")
	}
	cfg, err := oauthConfig()
	if err != nil {
		return nil, fmt.Errorf("Gmail read-only connection unavailable")
	}
	return cfg.TokenSource(ctx, st.Token), nil
}

// MailboxBinding exposes metadata only, never credential contents.
type MailboxBinding struct {
	Account, Path string
	Sync, Extract bool
	Workspace     string
}

func ExtraBindings() ([]MailboxBinding, error) {
	sf := loadSettings()
	entries, err := os.ReadDir(accountsDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("extra mailbox inventory unavailable")
	}
	var out []MailboxBinding
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(accountsDir(), entry.Name())
		b, err := os.ReadFile(path)
		var st storedToken
		if err != nil || json.Unmarshal(b, &st) != nil || st.Email == "" || st.Token == nil {
			return nil, fmt.Errorf("extra mailbox inventory invalid")
		}
		if path != extraTokenPath(st.Email) {
			return nil, fmt.Errorf("extra mailbox path binding mismatch")
		}
		sync, extract, ws := EffectiveSettings(sf.Accounts[st.Email], false)
		out = append(out, MailboxBinding{strings.ToLower(st.Email), path, sync, extract, ws})
	}
	return out, nil
}
func ExtraStateFilename(account string) string { return "state-" + accountSlug(account) + ".json" }
