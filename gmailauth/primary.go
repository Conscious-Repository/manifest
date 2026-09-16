package gmailauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// ConfiguredAccounts inventories settings even when their tokens are absent.
// Missing settings are empty; unreadable or malformed settings fail closed.
func ConfiguredAccounts() ([]string, error) {
	sf, err := readBindingSettings()
	if err != nil {
		return nil, err
	}
	var accounts []string
	for account := range sf.Accounts {
		if account == "" || account != strings.ToLower(strings.TrimSpace(account)) || !strings.Contains(account, "@") {
			return nil, fmt.Errorf("ambiguous mailbox settings identity")
		}
		accounts = append(accounts, account)
	}
	sort.Strings(accounts)
	return accounts, nil
}

func readBindingSettings() (settingsFile, error) {
	var sf settingsFile
	b, err := os.ReadFile(settingsPath())
	if os.IsNotExist(err) {
		return sf, nil
	}
	if err != nil {
		return sf, fmt.Errorf("mailbox settings unavailable")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	invalid := func() (settingsFile, error) { return settingsFile{}, fmt.Errorf("mailbox settings invalid") }
	// Read identity keys explicitly: duplicate or case-folded keys must not hide
	// configured accounts through encoding/json's last-wins behavior.
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		return invalid()
	}
	if tok, err := d.Token(); err != nil || tok != "accounts" {
		return invalid()
	}
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		return invalid()
	}
	sf.Accounts = map[string]AccountSettings{}
	for d.More() {
		tok, err := d.Token()
		account, ok := tok.(string)
		if err != nil || !ok {
			return invalid()
		}
		if _, exists := sf.Accounts[account]; exists {
			return invalid()
		}
		var settings *AccountSettings
		if d.Decode(&settings) != nil || settings == nil {
			return invalid()
		}
		sf.Accounts[account] = *settings
	}
	if tok, err := d.Token(); err != nil || tok != json.Delim('}') {
		return invalid()
	}
	if tok, err := d.Token(); err != nil || tok != json.Delim('}') {
		return invalid()
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return invalid()
	}
	return sf, nil
}

func ExtraBindings() ([]MailboxBinding, error) {
	sf, err := readBindingSettings()
	if err != nil {
		return nil, err
	}
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
