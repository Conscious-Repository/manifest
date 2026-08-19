package bankfeed

import (
	"context"
	"encoding/base64"
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

	accounts, err := svc.Accounts(context.Background())
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
