package bankfeed

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeBridge is an httptest SimpleFIN: POST /simplefin/claim/<hex> mints the
// access URL; GET /accounts serves a canned account set.
func fakeBridge(t *testing.T, accountsJSON string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/simplefin/claim/"):
			if r.Method != http.MethodPost {
				http.Error(w, "claim is POST", http.StatusMethodNotAllowed)
				return
			}
			w.Write([]byte(srv.URL + "/access"))
		case r.URL.Path == "/access/accounts":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(accountsJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const twoAccounts = `{"errors":[],"accounts":[
  {"id":"act-1","name":"Checking ····4821","balance":"1200.00","org":{"name":"Midwest Bank"},
   "transactions":[
     {"id":"t1","posted":1755300000,"amount":"-5500.00","description":"CHECK 1041","payee":"Olga Sobkiv"},
     {"id":"t2","posted":1755386400,"amount":"2000.00","description":"ZELLE DEPOSIT","payee":"Tenant A"}]},
  {"id":"act-2","name":"Card ····9010","balance":"-40.00","org":{"name":"Midwest Bank"},
   "transactions":[
     {"id":"t3","posted":1755386400,"amount":"-123.45","description":"HOME DEPOT #55","payee":"Home Depot"}]}
]}`

func TestClaimBothTokenForms(t *testing.T) {
	bridge := fakeBridge(t, twoAccounts)
	sf := NewSimpleFIN()
	sf.ClaimBase = bridge.URL + "/simplefin/claim/"

	hexToken := "FACF826E18832AC7A80DEB8553F11FDE295AF417B95730D1B6CB99C13BA0DBF3"
	url, err := sf.Claim(context.Background(), hexToken)
	if err != nil || url != bridge.URL+"/access" {
		t.Fatalf("hex claim: %q %v", url, err)
	}
	b64 := base64.StdEncoding.EncodeToString([]byte(bridge.URL + "/simplefin/claim/" + hexToken))
	url, err = sf.Claim(context.Background(), b64)
	if err != nil || url != bridge.URL+"/access" {
		t.Fatalf("base64 claim: %q %v", url, err)
	}
	if _, err := sf.Claim(context.Background(), "not a token"); err == nil {
		t.Fatal("garbage token must fail before any request")
	}
}

func TestFetchNewDedupesAndFlagsErrors(t *testing.T) {
	bridge := fakeBridge(t, twoAccounts)
	sf := NewSimpleFIN()
	sf.ClaimBase = bridge.URL + "/simplefin/claim/"
	dataDir := t.TempDir()
	svc := New(dataDir, sf)

	if svc.Claimed() {
		t.Fatal("claimed before any claim")
	}
	if err := svc.Claim(context.Background(), "FACF826E18832AC7A80DEB8553F11FDE295AF417B95730D1B6CB99C13BA0DBF3"); err != nil {
		t.Fatal(err)
	}
	// the secrets file is 0600 (portals creds class)
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(dataDir, "bankfeeds", "feed.json"))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("feed.json mode = %v, want 0600", fi.Mode().Perm())
		}
	}

	accounts, _, err := svc.Accounts(context.Background(), time.Now(), false)
	if err != nil || len(accounts) != 2 {
		t.Fatalf("accounts: %v %v", accounts, err)
	}

	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(svc.Store().Upsert(Link{SimplefinID: "act-1", EntitySlug: "garden-spe", AccountLabel: "checking", Enabled: true}))
	must(svc.Store().Upsert(Link{SimplefinID: "act-2", EntitySlug: "garden-spe", AccountLabel: "card", Enabled: false}))

	now := time.Unix(1755500000, 0)
	hauls := svc.FetchNew(context.Background(), now)
	if len(hauls) != 1 || hauls[0].Link.SimplefinID != "act-1" {
		t.Fatalf("hauls = %+v, want only the enabled account", hauls)
	}
	if len(hauls[0].Txns) != 2 {
		t.Fatalf("txns = %+v, want 2", hauls[0].Txns)
	}
	// the sign convention rides through untouched: negative = money out
	if hauls[0].Txns[0].Amount != -5500 || hauls[0].Txns[1].Amount != 2000 {
		t.Fatalf("amounts = %v / %v", hauls[0].Txns[0].Amount, hauls[0].Txns[1].Amount)
	}
	// re-sync is free: same txns, all seen
	if again := svc.FetchNew(context.Background(), now); len(again) != 0 {
		t.Fatalf("re-sync hauled %+v, want nothing", again)
	}
	links := svc.Store().Links()
	if links[0].LastSync == "" || links[0].LastError != "" {
		t.Fatalf("link health after clean sync: %+v", links[0])
	}

	// a dead access URL flips the link into needs-reauth territory
	bridge.Close()
	svc.FetchNew(context.Background(), now)
	if l, _ := svc.Store().LinkFor("act-1"); l.LastError == "" {
		t.Fatal("a failing sync must land on the link as lastError")
	}
}

// countingProvider counts Accounts requests — the SimpleFIN budget is
// requests/day (notice 2026-09-10), so the cache is measured in calls.
type countingProvider struct {
	calls int
	list  []Account
}

func (c *countingProvider) Claim(_ context.Context, _ string) (string, error) { return "stub://a", nil }
func (c *countingProvider) Accounts(_ context.Context, _ string) ([]Account, error) {
	c.calls++
	return c.list, nil
}
func (c *countingProvider) Transactions(_ context.Context, _, _ string, _, _ time.Time) ([]Txn, []string, error) {
	return nil, nil, nil
}

// Accounts serves the cached listing inside the TTL (fetchedAt stays the
// real fetch time), refetches once stale, honors force, survives a process
// restart through bankfeed-cache, and forgets the listing on a new claim.
func TestAccountsCachedWithinTTL(t *testing.T) {
	prov := &countingProvider{list: []Account{{ID: "act-1", Name: "Checking", Balance: "10.00"}}}
	dataDir := t.TempDir()
	svc := New(dataDir, prov)
	svc.AccountsTTL = time.Hour
	if err := svc.Store().SetAccessURL("stub://a"); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)

	got, at, err := svc.Accounts(context.Background(), t0, false)
	if err != nil || len(got) != 1 || !at.Equal(t0) || prov.calls != 1 {
		t.Fatalf("first call: %v at=%v err=%v calls=%d", got, at, err, prov.calls)
	}
	// 30 min later: cached, and the stamp is still the real fetch time
	got, at, err = svc.Accounts(context.Background(), t0.Add(30*time.Minute), false)
	if err != nil || len(got) != 1 || !at.Equal(t0) || prov.calls != 1 {
		t.Fatalf("inside TTL: at=%v err=%v calls=%d (want cached, at=t0)", at, err, prov.calls)
	}
	// a restart reads the same cache — no fetch
	svc2 := New(dataDir, prov)
	svc2.AccountsTTL = time.Hour
	if _, at, err := svc2.Accounts(context.Background(), t0.Add(45*time.Minute), false); err != nil || !at.Equal(t0) || prov.calls != 1 {
		t.Fatalf("after restart: at=%v err=%v calls=%d (want cached)", at, err, prov.calls)
	}
	// stale → refetch
	prov.list[0].Balance = "12.00"
	got, at, err = svc.Accounts(context.Background(), t0.Add(2*time.Hour), false)
	if err != nil || got[0].Balance != "12.00" || !at.Equal(t0.Add(2*time.Hour)) || prov.calls != 2 {
		t.Fatalf("stale: %v at=%v err=%v calls=%d", got, at, err, prov.calls)
	}
	// force → refetch even though fresh
	if _, _, err := svc.Accounts(context.Background(), t0.Add(2*time.Hour+time.Second), true); err != nil || prov.calls != 3 {
		t.Fatalf("force: err=%v calls=%d", err, prov.calls)
	}
	// a new claim drops the cached listing
	if err := svc.Store().SetAccessURL("stub://b"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Accounts(context.Background(), t0.Add(2*time.Hour+2*time.Second), false); err != nil || prov.calls != 4 {
		t.Fatalf("after re-claim: err=%v calls=%d (want a live fetch)", err, prov.calls)
	}
	// a poll stamps SyncAt only when a link actually went to the bridge
	if !svc.Store().LastSyncAt().IsZero() {
		t.Fatal("SyncAt stamped without any enabled link")
	}
	if err := svc.Store().Upsert(Link{SimplefinID: "act-1", EntitySlug: "e", AccountLabel: "l", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	svc.FetchNew(context.Background(), t0.Add(3*time.Hour))
	if got := svc.Store().LastSyncAt(); !got.Equal(t0.Add(3 * time.Hour)) {
		t.Fatalf("SyncAt = %v, want the poll time", got)
	}
}

// cappedProvider truncates every response to its NEWEST 50 rows — the beta
// bridge's behavior that silently loses history on a single wide pull.
type cappedProvider struct {
	txns []Txn
}

func (c *cappedProvider) Claim(_ context.Context, _ string) (string, error) { return "stub://a", nil }
func (c *cappedProvider) Accounts(_ context.Context, _ string) ([]Account, error) {
	return []Account{{ID: "act-1", Name: "Checking"}}, nil
}
func (c *cappedProvider) Transactions(_ context.Context, _, _ string, start, end time.Time) ([]Txn, []string, error) {
	var in []Txn
	for _, t := range c.txns {
		if !t.Posted.Before(start) && (end.IsZero() || t.Posted.Before(end)) {
			in = append(in, t)
		}
	}
	// newest 50 only, like the bridge
	if len(in) > 50 {
		newest := append([]Txn(nil), in...)
		for i := range newest { // sort newest-first by posted (insertion is fine at this size)
			for j := i + 1; j < len(newest); j++ {
				if newest[j].Posted.After(newest[i].Posted) {
					newest[i], newest[j] = newest[j], newest[i]
				}
			}
		}
		in = newest[:50]
	}
	return in, nil, nil
}

// 180 txns over 60 days (3/day) — one uncapped pull would lose 130 of them.
// The windowed walk must recover every single one.
func TestFetchAllDefeatsResponseCap(t *testing.T) {
	now := time.Unix(1755500000, 0)
	var txns []Txn
	for d := 0; d < 60; d++ {
		for k := 0; k < 3; k++ {
			txns = append(txns, Txn{
				ID:     fmt.Sprintf("t-%d-%d", d, k),
				Posted: now.AddDate(0, 0, -d).Add(time.Duration(k) * time.Hour),
				Amount: -1, Description: "x", Payee: "y",
			})
		}
	}
	svc := New(t.TempDir(), &cappedProvider{txns: txns})
	if err := svc.Store().SetAccessURL("stub://a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store().Upsert(Link{SimplefinID: "act-1", EntitySlug: "e", AccountLabel: "l", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	haul, err := svc.FetchAll(context.Background(), "act-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(haul.Txns) != len(txns) {
		t.Fatalf("windowed fetch got %d of %d txns — the cap ate history", len(haul.Txns), len(txns))
	}
	// FetchNew after FetchAll: everything seen, nothing re-hauled
	if again := svc.FetchNew(context.Background(), now); len(again) != 0 {
		t.Fatalf("re-sync hauled %+v", again)
	}
}
