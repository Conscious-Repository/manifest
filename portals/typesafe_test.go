package portals

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A credential-only portal: sealed until a key is saved, verified by one Jev
// call, open or degraded by that answer, never polled, readable by consumers.
func TestTypeSafeCredentialPortal(t *testing.T) {
	var auth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/systemone" || r.Method != http.MethodPost {
			w.WriteHeader(404)
			return
		}
		if auth != "Bearer apikey_good" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"answers":{"ping":{"type":"noul","value":0.9}}}`))
	}))
	defer ts.Close()
	svc := New(t.TempDir(), time.UTC)
	svc.tsBase = ts.URL
	var row Row
	for _, r := range svc.Rows() {
		if r.ID == "typesafe" {
			row = r
		}
	}
	if row.ID == "" || row.State != StateSealed || !row.Credential || row.Polled {
		t.Fatalf("registry row: %+v", row)
	}
	bad, err := svc.SetCreds(context.Background(), "typesafe", map[string]string{"apiKey": "apikey_bad"})
	if err != nil || bad.State != StateDegraded || !strings.Contains(bad.Err, "401") {
		t.Fatalf("bad key: %+v %v", bad, err)
	}
	good, err := svc.SetCreds(context.Background(), "typesafe", map[string]string{"apiKey": "apikey_good"})
	if err != nil || good.State != StateOpen || good.Masked != "····good" || good.LastCrossing == "" {
		t.Fatalf("good key: %+v %v", good, err)
	}
	if svc.Credential("typesafe", "apiKey") != "apikey_good" {
		t.Fatal("consumer read")
	}
	if _, err := svc.PollNow(context.Background(), "typesafe"); err == nil {
		t.Fatal("a credential-only portal must not poll")
	}
	if len(svc.Cards()) != 0 {
		t.Fatal("credential portal produced feed cards")
	}
	if _, err := svc.Disconnect("typesafe"); err != nil || svc.Credential("typesafe", "apiKey") != "" {
		t.Fatal("disconnect")
	}
}
