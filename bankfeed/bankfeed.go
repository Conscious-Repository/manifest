// Package bankfeed pulls bank transactions into the statement workbench —
// the SimpleFIN feed lane (bank-accounts-integration plan, audited
// 2026-08-19). It mirrors the portals shape: a provider interface over one
// HTTP client, a 0600 secrets file + disposable cache under dataDir, and a
// Start(ctx) poller that runs in the APP SERVER (poller lane, ARCHITECTURE
// §7) — on metis, where dataDir lives and never syncs.
//
// The owner-facing account↔entity binding is the VAULT entity record
// (EntityAccount rows); this package holds only what must not enter the
// vault: the revocable read-only access URL, the SimpleFIN-account-id ↔
// (entity, label) linkage, cursors, and seen-transaction ids.
package bankfeed

import (
	"context"
	"fmt"
	"time"
)

// Account is one bridge-side bank account.
type Account struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Org     string `json:"org"`
	Balance string `json:"balance,omitempty"`
}

// Txn is one bank transaction. Amount keeps the SimpleFIN sign convention:
// negative = money OUT (an expense), positive = money IN (a deposit).
type Txn struct {
	ID          string
	Posted      time.Time
	Amount      float64
	Description string
	Payee       string
}

// Provider is the pluggable bridge client (SimpleFIN today; Teller could
// slot in behind the same shape — the portals poller idiom).
type Provider interface {
	// Claim exchanges a one-time setup token for the revocable access URL.
	Claim(ctx context.Context, setupToken string) (accessURL string, err error)
	// Accounts lists the accounts the access URL can read.
	Accounts(ctx context.Context, accessURL string) ([]Account, error)
	// Transactions reads one account's transactions since a cursor.
	Transactions(ctx context.Context, accessURL, accountID string, since time.Time) ([]Txn, error)
}

// Link binds one bridge account to an EntityAccount row on a vault entity
// record. entitySlug+accountLabel name the row; the record stays the
// owner-facing truth — this is the machine half.
type Link struct {
	SimplefinID     string `json:"simplefinId"`
	EntitySlug      string `json:"entitySlug"`
	AccountLabel    string `json:"accountLabel"`
	DefaultProperty string `json:"defaultProperty,omitempty"` // prefill only — never auto-applies by itself
	Enabled         bool   `json:"enabled"`
	OrgName         string `json:"orgName,omitempty"`
	AccountName     string `json:"accountName,omitempty"`
	LastSync        string `json:"lastSync,omitempty"`  // RFC3339
	LastError       string `json:"lastError,omitempty"` // "" = healthy; non-empty → needs-reauth overlay
}

// Digest is one FEED card entry (auto-apply receipts + sync notices).
type Digest struct {
	ID        string `json:"id"` // "bank:" prefix — the feed dismiss router keys on it
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	Date      string `json:"date"` // RFC3339
	Dismissed bool   `json:"dismissed,omitempty"`
}

// NewTxns is one sync's haul for one link.
type NewTxns struct {
	Link Link
	Txns []Txn
}

// Service owns the store, cache, and provider.
type Service struct {
	store    *Store
	provider Provider
}

func New(dataDir string, p Provider) *Service {
	return &Service{store: NewStore(dataDir), provider: p}
}

func (s *Service) Store() *Store { return s.store }

// Claimed reports whether an access URL is on file (the feature self-enables
// once a token is claimed — no config key).
func (s *Service) Claimed() bool { return s.store.AccessURL() != "" }

// Claim exchanges the setup token and persists the access URL (0600).
func (s *Service) Claim(ctx context.Context, setupToken string) error {
	url, err := s.provider.Claim(ctx, setupToken)
	if err != nil {
		return err
	}
	return s.store.SetAccessURL(url)
}

// Accounts lists the bridge accounts (live call).
func (s *Service) Accounts(ctx context.Context) ([]Account, error) {
	return s.provider.Accounts(ctx, s.store.AccessURL())
}

// FetchAll pulls ONE link's entire history from the bridge (since epoch —
// the bridge serves whatever the institution gave it), unseen rows only, and
// marks them seen so the daily poll never re-hauls them. The backfill lane:
// bulk historic import for hand categorization, never auto-applied.
func (s *Service) FetchAll(ctx context.Context, simplefinID string, now time.Time) (NewTxns, error) {
	link, ok := s.store.LinkFor(simplefinID)
	if !ok {
		return NewTxns{}, fmt.Errorf("account %s is not linked", simplefinID)
	}
	txns, err := s.provider.Transactions(ctx, s.store.AccessURL(), simplefinID, time.Unix(86400, 0))
	if err != nil {
		s.store.SetLinkHealth(simplefinID, "", err.Error())
		return NewTxns{}, err
	}
	var fresh []Txn
	var newest time.Time
	ids := make([]string, 0, len(txns))
	for _, t := range txns {
		if t.Posted.After(newest) {
			newest = t.Posted
		}
		if s.store.Seen(simplefinID, t.ID) {
			continue
		}
		ids = append(ids, t.ID)
		fresh = append(fresh, t)
	}
	s.store.MarkSeen(simplefinID, ids, newest)
	s.store.SetLinkHealth(simplefinID, now.Format(time.RFC3339), "")
	return NewTxns{Link: link, Txns: fresh}, nil
}

// FetchNew pulls every enabled link's unseen transactions. The cursor backs
// up three days on every poll (banks repost/settle late); the seen-id set
// makes the overlap free. Per-link errors land on the link (needs-reauth
// overlay) without stopping the others.
func (s *Service) FetchNew(ctx context.Context, now time.Time) []NewTxns {
	var out []NewTxns
	for _, link := range s.store.Links() {
		if !link.Enabled {
			continue
		}
		since := s.store.Cursor(link.SimplefinID)
		if since.IsZero() {
			since = now.AddDate(0, 0, -90) // the bridge's backfill window
		} else {
			since = since.AddDate(0, 0, -3)
		}
		txns, err := s.provider.Transactions(ctx, s.store.AccessURL(), link.SimplefinID, since)
		if err != nil {
			s.store.SetLinkHealth(link.SimplefinID, "", err.Error())
			continue
		}
		var fresh []Txn
		var newest time.Time
		for _, t := range txns {
			if t.Posted.After(newest) {
				newest = t.Posted
			}
			if s.store.Seen(link.SimplefinID, t.ID) {
				continue
			}
			fresh = append(fresh, t)
		}
		ids := make([]string, 0, len(fresh))
		for _, t := range fresh {
			ids = append(ids, t.ID)
		}
		s.store.MarkSeen(link.SimplefinID, ids, newest)
		s.store.SetLinkHealth(link.SimplefinID, now.Format(time.RFC3339), "")
		if len(fresh) > 0 {
			out = append(out, NewTxns{Link: link, Txns: fresh})
		}
	}
	return out
}
