package bankfeed

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SimpleFIN implements Provider against the SimpleFIN bridge protocol
// (https://www.simplefin.org/protocol.html).
type SimpleFIN struct {
	HTTP *http.Client
	// ClaimBase turns a raw-hex setup token into a claim URL. Default is the
	// public bridge; tests point it at httptest.
	ClaimBase string
}

func NewSimpleFIN() *SimpleFIN {
	return &SimpleFIN{
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		ClaimBase: "https://beta-bridge.simplefin.org/simplefin/claim/",
	}
}

var hexTokenRe = regexp.MustCompile(`^[0-9A-Fa-f]{32,}$`)

// Claim exchanges a one-time setup token for the access URL. Tokens come in
// two dashboard forms: base64 of the full claim URL (canonical), or the raw
// hex claim id. Either way the claim is a POST whose body is the access URL.
func (s *SimpleFIN) Claim(ctx context.Context, setupToken string) (string, error) {
	token := strings.TrimSpace(setupToken)
	claimURL := ""
	if decoded, err := base64.StdEncoding.DecodeString(token); err == nil && strings.HasPrefix(string(decoded), "http") {
		claimURL = strings.TrimSpace(string(decoded))
	} else if hexTokenRe.MatchString(token) {
		claimURL = s.ClaimBase + token
	} else {
		return "", fmt.Errorf("setup token is neither base64-of-URL nor hex")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claimURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Length", "0")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claim: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	accessURL := strings.TrimSpace(string(body))
	if !strings.HasPrefix(accessURL, "http") {
		return "", fmt.Errorf("claim returned a non-URL body")
	}
	return accessURL, nil
}

// sfAccountSet is the bridge's /accounts response.
type sfAccountSet struct {
	Errors   []string    `json:"errors"`
	Accounts []sfAccount `json:"accounts"`
}

type sfAccount struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Balance      string  `json:"balance"`
	Org          sfOrg   `json:"org"`
	Transactions []sfTxn `json:"transactions"`
}

type sfOrg struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

type sfTxn struct {
	ID          string      `json:"id"`
	Posted      int64       `json:"posted"`
	Amount      json.Number `json:"amount"`
	Description string      `json:"description"`
	Payee       string      `json:"payee"`
}

// fetch GETs <accessURL>/accounts with the given query. The access URL
// carries basic-auth credentials in its userinfo — http.Client honors them.
func (s *SimpleFIN) fetch(ctx context.Context, accessURL, query string) (*sfAccountSet, error) {
	url := strings.TrimSuffix(accessURL, "/") + "/accounts" + query
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("access revoked (%s) — re-claim needed", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return nil, fmt.Errorf("accounts: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var set sfAccountSet
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, fmt.Errorf("accounts: bad JSON: %w", err)
	}
	return &set, nil
}

// Accounts lists accounts without transaction history (start-date now).
func (s *SimpleFIN) Accounts(ctx context.Context, accessURL string) ([]Account, error) {
	set, err := s.fetch(ctx, accessURL, "?balances-only=1")
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(set.Accounts))
	for _, a := range set.Accounts {
		out = append(out, Account{ID: a.ID, Name: a.Name, Org: a.Org.Name, Balance: a.Balance})
	}
	return out, nil
}

// Transactions reads one account's transactions in [start, end) — zero end
// means "through now" (no end-date param).
func (s *SimpleFIN) Transactions(ctx context.Context, accessURL, accountID string, start, end time.Time) ([]Txn, error) {
	query := "?start-date=" + strconv.FormatInt(start.Unix(), 10) + "&account=" + urlQueryEscape(accountID)
	if !end.IsZero() {
		query += "&end-date=" + strconv.FormatInt(end.Unix(), 10)
	}
	set, err := s.fetch(ctx, accessURL, query)
	if err != nil {
		return nil, err
	}
	var out []Txn
	for _, a := range set.Accounts {
		if a.ID != accountID {
			continue
		}
		for _, t := range a.Transactions {
			amt, err := t.Amount.Float64()
			if err != nil {
				continue
			}
			out = append(out, Txn{
				ID:          t.ID,
				Posted:      time.Unix(t.Posted, 0),
				Amount:      amt,
				Description: t.Description,
				Payee:       t.Payee,
			})
		}
	}
	return out, nil
}

// urlQueryEscape covers the account-id charset without importing net/url's
// full QueryEscape semantics for spaces (bridge ids are ASCII "ACT-…" forms,
// but be safe).
func urlQueryEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}
