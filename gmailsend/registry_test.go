package gmailsend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// senderFixture seeds one send-capable client for `from` at its own token
// file under dir (dir already holds the shared OAuth client file) and binds
// it to its own httptest endpoint, so a test can tell which account a
// message left through.
func senderFixture(t *testing.T, dir, from string) (*Client, *sendFixture) {
	t.Helper()
	c := New(from, SenderTokenPath(dir, from))
	if err := c.SaveToken(from, &oauth2.Token{
		AccessToken: testAccess + "-" + from, RefreshToken: testRefresh, TokenType: "Bearer",
		Expiry: time.Now().Add(time.Hour),
	}, []string{SendScope}); err != nil {
		t.Fatal(err)
	}
	fx := &sendFixture{}
	srv := httptest.NewServer(http.HandlerFunc(fx.serve))
	t.Cleanup(srv.Close)
	c.UseEndpoint(srv.URL+"/gmail/v1/users/me/messages/send", srv.Client())
	return c, fx
}

// registryFixture: the recruiting sender (ben@aion.bio, the legacy token.json
// path, exactly as main.go builds it) plus ben@ooda.group under ooda.group.
func registryFixture(t *testing.T) (*Registry, *sendFixture, *sendFixture) {
	t.Helper()
	aion, _ := testClient(t, []string{SendScope}, nil) // seeds creds + GMAIL_SEND_TOKEN=""
	dir := filepath.Dir(filepath.Dir(aion.tokenPath))
	afx := &sendFixture{}
	asrv := httptest.NewServer(http.HandlerFunc(afx.serve))
	t.Cleanup(asrv.Close)
	aion.UseEndpoint(asrv.URL+"/gmail/v1/users/me/messages/send", asrv.Client())
	ooda, ofx := senderFixture(t, dir, "ben@ooda.group")
	r := NewRegistry()
	if err := r.Register("", aion); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("ooda.group", ooda); err != nil {
		t.Fatal(err)
	}
	return r, afx, ofx
}

func wireFrom(t *testing.T, fx *sendFixture) string {
	t.Helper()
	raw, err := DecodeRaw(fx.raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(string(raw), "\r\n") {
		if strings.HasPrefix(l, "From: ") {
			return strings.TrimPrefix(l, "From: ")
		}
	}
	return ""
}

func TestParseSenders(t *testing.T) {
	got, err := ParseSenders(" ooda.group=Ben@OODA.group, me@example.com\n@personal.test=me@gmail.com ")
	if err != nil {
		t.Fatal(err)
	}
	want := []SenderSpec{
		{Domain: "ooda.group", From: "ben@ooda.group"},
		{Domain: "example.com", From: "me@example.com"},
		{Domain: "personal.test", From: "me@gmail.com"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d: got %+v want %+v", i, got[i], want[i])
		}
	}
	if got, err := ParseSenders(""); err != nil || len(got) != 0 {
		t.Fatalf("empty: %+v %v", got, err)
	}
	for _, bad := range []string{"ooda.group=ben", "ooda.group=", "Ben Anderson <ben@ooda.group>", "a@b=ben@ooda.group"} {
		if _, err := ParseSenders(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestSenderTokenPathIsPerSenderAndDistinct(t *testing.T) {
	t.Setenv("GMAIL_SEND_TOKEN", "")
	a := SenderTokenPath("/data", "Ben@OODA.group")
	if a != filepath.Join("/data", "gmail-send", "ben@ooda.group.json") {
		t.Fatalf("path: %s", a)
	}
	if a == TokenPath("/data") {
		t.Fatal("a registry sender's token would overwrite the recruiting token")
	}
	if got := SenderTokenPath("/data", "x/y@evil..test"); strings.Contains(filepath.Base(got), "/") {
		t.Fatalf("unsanitized: %s", got)
	}
}

// aion.bio → ben@aion.bio (unchanged) and ooda.group → ben@ooda.group: each
// send leaves through its own account's token and endpoint, From stamped.
func TestRegistrySendsByDomain(t *testing.T) {
	r, afx, ofx := registryFixture(t)
	if d := r.Domains(); strings.Join(d, ",") != "aion.bio,ooda.group" {
		t.Fatalf("domains: %v", d)
	}
	msg := Message{To: []string{"dana@example.test"}, Subject: "s", Body: "b"}
	if _, err := r.Send(context.Background(), "aion.bio", msg); err != nil {
		t.Fatal(err)
	}
	if wireFrom(t, afx) != "ben@aion.bio" || ofx.path != "" {
		t.Fatalf("aion send went wrong: aion From %q, ooda path %q", wireFrom(t, afx), ofx.path)
	}
	if afx.auth != "Bearer "+testAccess {
		t.Fatalf("aion auth: %q", afx.auth)
	}
	if _, err := r.Send(context.Background(), "OODA.group", msg); err != nil {
		t.Fatal(err)
	}
	if wireFrom(t, ofx) != "ben@ooda.group" {
		t.Fatalf("ooda From: %q", wireFrom(t, ofx))
	}
	if ofx.auth != "Bearer "+testAccess+"-ben@ooda.group" {
		t.Fatalf("ooda send used another account's token: %q", ofx.auth)
	}
	// explicit From resolves the same account
	if _, err := r.Send(context.Background(), "", Message{From: "ben@ooda.group", To: msg.To, Subject: "s", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	// the probe names both, never token material
	sts := r.Statuses()
	if len(sts) != 2 || sts[0].Domain != "aion.bio" || sts[0].Sender != "ben@aion.bio" || !sts[1].SendCapable {
		t.Fatalf("statuses: %+v", sts)
	}
}

// The owner's rule: an unmapped domain FAILS LOUD and nothing leaves — not
// from ben@aion.bio, not from anyone.
func TestRegistryUnmappedDomainFailsLoud(t *testing.T) {
	r, afx, ofx := registryFixture(t)
	msg := Message{To: []string{"dana@example.test"}, Subject: "s", Body: "b"}
	cases := map[string]struct {
		domain string
		msg    Message
	}{
		"unmapped domain":        {"benjaminbanderson.com", msg},
		"no domain, no From":     {"", msg},
		"unregistered From":      {"", Message{From: "me@benjaminbanderson.com", To: msg.To, Subject: "s", Body: "b"}},
		"same-domain other From": {"aion.bio", Message{From: "other@aion.bio", To: msg.To, Subject: "s", Body: "b"}},
	}
	for name, tc := range cases {
		_, err := r.Send(context.Background(), tc.domain, tc.msg)
		if !errors.Is(err, ErrNoSender) {
			t.Errorf("%s: err = %v, want ErrNoSender", name, err)
		}
		if err != nil && !strings.Contains(err.Error(), "never sent from another domain") {
			t.Errorf("%s: error does not say why: %v", name, err)
		}
	}
	if afx.path != "" || ofx.path != "" {
		t.Fatalf("an unmapped send reached the network (aion %q, ooda %q)", afx.path, ofx.path)
	}
	if _, err := r.ForDomain("nope.test"); !errors.Is(err, ErrNoSender) {
		t.Fatalf("ForDomain: %v", err)
	}
}

// An explicit From for one domain cannot be sent "for" another domain.
func TestRegistryCrossDomainFromRefused(t *testing.T) {
	r, afx, ofx := registryFixture(t)
	_, err := r.Send(context.Background(), "ooda.group", Message{From: "ben@aion.bio", To: []string{"d@x.test"}, Subject: "s", Body: "b"})
	if !errors.Is(err, ErrSenderMismatch) {
		t.Fatalf("err: %v", err)
	}
	if afx.path != "" || ofx.path != "" {
		t.Fatal("a cross-domain send reached the network")
	}
}

// Register refuses two accounts for one domain and one account for two
// domains; the same client re-registered is a no-op (UseGmailSend does this).
func TestRegistryRegisterRules(t *testing.T) {
	r := NewRegistry()
	a := New("ben@aion.bio", filepath.Join(t.TempDir(), "a.json"))
	if err := r.Register("", a); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("aion.bio", a); err != nil {
		t.Fatalf("re-register same client: %v", err)
	}
	if err := r.Register("aion.bio", New("other@aion.bio", filepath.Join(t.TempDir(), "b.json"))); err == nil {
		t.Fatal("two accounts took one domain")
	}
	if err := r.Register("personal.test", New("ben@aion.bio", filepath.Join(t.TempDir(), "c.json"))); err == nil {
		t.Fatal("one account took two domains")
	}
	if err := r.Register("", nil); err == nil {
		t.Fatal("nil client registered")
	}
	if d := r.Domains(); len(d) != 1 || d[0] != "aion.bio" {
		t.Fatalf("domains: %v", d)
	}
}

// A registry sender that is not yet connected is a state (its Status), and a
// send through it refuses as the single-sender client does — never rerouted.
func TestRegistryUnconnectedSenderRefuses(t *testing.T) {
	r, afx, _ := registryFixture(t)
	dir := t.TempDir()
	if err := r.Register("personal.test", New("me@personal.test", SenderTokenPath(dir, "me@personal.test"))); err != nil {
		t.Fatal(err)
	}
	_, err := r.Send(context.Background(), "personal.test", Message{To: []string{"d@x.test"}, Subject: "s", Body: "b"})
	if !errors.Is(err, ErrUnconfigured) {
		t.Fatalf("err: %v", err)
	}
	if afx.path != "" {
		t.Fatal("an unconnected sender's mail left through the recruiting account")
	}
	if _, err := os.Stat(SenderTokenPath(dir, "me@personal.test")); !os.IsNotExist(err) {
		t.Fatal("a token file appeared without a connect")
	}
}
