package server

// A dev harness, not a test: OODA_SMOKE=1 go test -run TestServeOodaSmoke
// serves the fully seeded portal fixture on a real port and prints session
// cookies for the admin and a partner, so the frontend can be driven by a
// real browser (CDP) against real routes. Env-gated so the suite never runs
// it; it blocks until killed.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestServeOodaSmoke(t *testing.T) {
	if os.Getenv("OODA_SMOKE") == "" {
		t.Skip("dev harness — set OODA_SMOKE=1 to serve the seeded portal")
	}
	f, ap := oodaFeedFixture(t)

	// seed the surfaces: two pending email candidates, one confirmed artifact,
	// one contract proposal (in-entity for Olga), one task proposal for OS
	seedCandidate(t, f, "me@olgasobkiv.com", "t-olga", "751 permit sets")
	seedCandidate(t, f, "brian@ooda.group", "t-brian", "vine removal quote")
	conf := seedCandidate(t, f, "me@olgasobkiv.com", "t-done", "roof scope agreed")
	rec := oodaDoAs(t, f, "me@olgasobkiv.com", "Olga", "POST", "/api/ooda/email/"+conf.ID, `{"action":"confirm"}`)
	if rec.Code != 200 {
		t.Fatalf("seed confirm: %d %s", rec.Code, rec.Body)
	}
	reContractProposal(t, ap, "olga-permit-smoke", "748-n-euclid", 5500)

	admin, _ := f.auth.SessionCookie("ben@ooda.group", "Benjamin", false, time.Now())
	partner, _ := f.auth.SessionCookie("me@olgasobkiv.com", "Olga", false, time.Now())
	addr := "127.0.0.1:7797"
	info := fmt.Sprintf("SMOKE-COOKIE-ADMIN %s=%s\nSMOKE-COOKIE-PARTNER %s=%s\nSMOKE-URL http://%s\n",
		admin.Name, admin.Value, partner.Name, partner.Value, addr)
	if out := os.Getenv("OODA_SMOKE_OUT"); out != "" {
		_ = os.WriteFile(out, []byte(info), 0o600) // go test buffers stdout — the file is readable NOW
	}
	fmt.Print(info)
	_ = http.ListenAndServe(addr, f.h)
}

func oodaDoAs(t *testing.T, f *oodaPortalHandles, email, name, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	c, err := f.auth.SessionCookie(email, name, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return oodaDo(t, f.h, c, method, path, body)
}
