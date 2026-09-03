package gmailsend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

const testAccess = "ya29.SECRET_ACCESS_TOKEN_xyz"
const testRefresh = "1//SECRET_REFRESH_TOKEN_abc"

// testClient seeds a fake OAuth client file and a send token with the given
// scopes, and binds the client to an httptest send endpoint.
func testClient(t *testing.T, scopes []string, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	cred := filepath.Join(dir, "client.json")
	if err := os.WriteFile(cred, []byte(`{"installed":{"client_id":"cid","client_secret":"csecret","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","redirect_uris":["http://localhost"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GMAIL_OAUTH_CLIENT", cred)
	t.Setenv("GMAIL_SEND_TOKEN", "")
	c := New("ben@aion.bio", TokenPath(dir))
	if scopes != nil {
		if err := c.SaveToken("ben@aion.bio", &oauth2.Token{
			AccessToken: testAccess, RefreshToken: testRefresh, TokenType: "Bearer",
			Expiry: time.Now().Add(time.Hour),
		}, scopes); err != nil {
			t.Fatal(err)
		}
	}
	var srv *httptest.Server
	if handler != nil {
		srv = httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		c.UseEndpoint(srv.URL+"/gmail/v1/users/me/messages/send", srv.Client())
	}
	return c, srv
}

func TestTokenPathOverride(t *testing.T) {
	t.Setenv("GMAIL_SEND_TOKEN", "")
	if got := TokenPath("/data"); got != filepath.Join("/data", "gmail-send", "token.json") {
		t.Fatalf("default token path: %s", got)
	}
	t.Setenv("GMAIL_SEND_TOKEN", "/elsewhere/tok.json")
	if got := TokenPath("/data"); got != "/elsewhere/tok.json" {
		t.Fatalf("override ignored: %s", got)
	}
}

// Build → deterministic RFC 5322 bytes; EncodeRaw is base64url, unpadded.
func TestBuildAndEncode(t *testing.T) {
	m := Message{
		From: "ben@aion.bio", FromName: "Ben Anderson", To: []string{"dana@example.test"}, Cc: []string{"rj@aion.bio"},
		Subject: "AION — MRI Engineer", Body: "Hi Dana,\n\nline two\n",
		Date: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC), MessageID: "<fixed@aion.bio>",
		InReplyTo: "<prev@aion.bio>",
	}
	a, err := Build(m)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Build(m)
	if string(a) != string(b) {
		t.Fatal("Build is not deterministic for a fixed Date/Message-ID")
	}
	s := string(a)
	for _, want := range []string{
		"From: \"Ben Anderson\" <ben@aion.bio>\r\n",
		"To: dana@example.test\r\n",
		"Cc: rj@aion.bio\r\n",
		"Subject: =?utf-8?q?AION_=E2=80=94_MRI_Engineer?=\r\n",
		"Date: Thu, 03 Sep 2026 12:00:00 +0000\r\n",
		"Message-ID: <fixed@aion.bio>\r\n",
		"In-Reply-To: <prev@aion.bio>\r\n",
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n",
		"\r\n\r\nHi Dana,\r\n\r\nline two\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	enc := EncodeRaw(a)
	if strings.ContainsAny(enc, "+/=") {
		t.Fatalf("raw is not unpadded base64url: %s", enc)
	}
	back, err := DecodeRaw(enc)
	if err != nil || string(back) != s {
		t.Fatal("EncodeRaw does not round-trip")
	}
}

func TestBuildRefusesBadInput(t *testing.T) {
	base := Message{From: "ben@aion.bio", To: []string{"dana@example.test"}, Subject: "s", Body: "b"}
	cases := map[string]func(m *Message){
		"no recipient":  func(m *Message) { m.To = nil },
		"bad recipient": func(m *Message) { m.To = []string{"not an address"} },
		"no subject":    func(m *Message) { m.Subject = " " },
		"no body":       func(m *Message) { m.Body = "" },
		"bad from":      func(m *Message) { m.From = "ben" },
	}
	for name, mut := range cases {
		m := base
		m.To = append([]string{}, base.To...)
		mut(&m)
		if _, err := Build(m); err == nil {
			t.Errorf("%s: Build accepted it", name)
		}
	}
}

type sendFixture struct {
	mu      sync.Mutex
	auth    string
	path    string
	raw     string
	status  int
	payload string
}

func (f *sendFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = r.Header.Get("Authorization")
	f.path = r.URL.Path
	var body struct {
		Raw string `json:"raw"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.raw = body.Raw
	w.Header().Set("Content-Type", "application/json")
	if f.status != 0 {
		w.WriteHeader(f.status)
	}
	if f.payload != "" {
		_, _ = w.Write([]byte(f.payload))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"id": "msg_1", "threadId": "thr_1"})
}

// The send path: users/me/messages/send, Bearer auth from the token file,
// the raw body decodes back to the RFC 5322 message, ids come back.
func TestSendHitsGmailWithAuth(t *testing.T) {
	fx := &sendFixture{}
	c, _ := testClient(t, []string{SendScope}, fx.serve)
	if st := c.Status(); !st.SendCapable || !st.Configured || st.Sender != "ben@aion.bio" {
		t.Fatalf("status: %+v", st)
	}
	ref, err := c.Send(context.Background(), Message{
		To: []string{"dana@example.test"}, Subject: "hello", Body: "body text",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "msg_1" || ref.ThreadID != "thr_1" {
		t.Fatalf("ref: %+v", ref)
	}
	if fx.path != "/gmail/v1/users/me/messages/send" {
		t.Fatalf("path: %s", fx.path)
	}
	if fx.auth != "Bearer "+testAccess {
		t.Fatalf("auth header: %q", fx.auth)
	}
	raw, err := DecodeRaw(fx.raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "From: ben@aion.bio\r\n") || !strings.Contains(string(raw), "Subject: hello\r\n") {
		t.Fatalf("wire message:\n%s", raw)
	}
}

// A failing send never echoes the token, even when Gmail's error body does.
func TestSendRedactsToken(t *testing.T) {
	fx := &sendFixture{status: http.StatusUnauthorized,
		payload: `{"error":{"message":"invalid credentials ` + testAccess + ` / ` + testRefresh + `"}}`}
	c, _ := testClient(t, []string{SendScope}, fx.serve)
	_, err := c.Send(context.Background(), Message{To: []string{"dana@example.test"}, Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), testAccess) || strings.Contains(err.Error(), testRefresh) {
		t.Fatalf("token leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("error: %v", err)
	}
}

// No token → ErrUnconfigured before any network call; a token without
// gmail.send → ErrNoSendScope; the probe paints both as states.
func TestSendRefusesWhenNotCapable(t *testing.T) {
	fx := &sendFixture{}
	c, _ := testClient(t, nil, fx.serve)
	st := c.Status()
	if st.Configured || st.SendCapable || st.Sender != "ben@aion.bio" {
		t.Fatalf("status: %+v", st)
	}
	if _, err := c.Send(context.Background(), Message{To: []string{"d@x.test"}, Subject: "s", Body: "b"}); !errors.Is(err, ErrUnconfigured) {
		t.Fatalf("err: %v", err)
	}
	if err := c.SaveToken("ben@aion.bio", &oauth2.Token{AccessToken: testAccess, Expiry: time.Now().Add(time.Hour)},
		[]string{"https://www.googleapis.com/auth/gmail.readonly"}); err != nil {
		t.Fatal(err)
	}
	st = c.Status()
	if !st.Configured || st.SendCapable || !strings.Contains(st.Detail, "gmail.send") {
		t.Fatalf("status: %+v", st)
	}
	if _, err := c.Send(context.Background(), Message{To: []string{"d@x.test"}, Subject: "s", Body: "b"}); !errors.Is(err, ErrNoSendScope) {
		t.Fatalf("err: %v", err)
	}
	if fx.path != "" {
		t.Fatal("an incapable client reached the network")
	}
}

// The From lock: a message from anyone but the one allowed sender is
// refused before any network call; a token for another account is not
// send-capable.
func TestSenderLock(t *testing.T) {
	fx := &sendFixture{}
	c, _ := testClient(t, []string{SendScope}, fx.serve)
	_, err := c.Send(context.Background(), Message{From: "someone@else.test", To: []string{"d@x.test"}, Subject: "s", Body: "b"})
	if !errors.Is(err, ErrSenderMismatch) {
		t.Fatalf("err: %v", err)
	}
	if err := c.SaveToken("other@aion.bio", &oauth2.Token{AccessToken: testAccess, Expiry: time.Now().Add(time.Hour)}, []string{SendScope}); err != nil {
		t.Fatal(err)
	}
	if st := c.Status(); st.SendCapable || !strings.Contains(st.Detail, "other@aion.bio") {
		t.Fatalf("status: %+v", st)
	}
	if fx.path != "" {
		t.Fatal("a locked send reached the network")
	}
}

// The token file is 0600 under a 0700 dir and the probe never carries it.
func TestTokenFileModeAndProbeShape(t *testing.T) {
	c, _ := testClient(t, []string{SendScope}, nil)
	fi, err := os.Stat(c.tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("token mode %o", fi.Mode().Perm())
	}
	if di, _ := os.Stat(filepath.Dir(c.tokenPath)); di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", di.Mode().Perm())
	}
	b, _ := json.Marshal(c.Status())
	if strings.Contains(string(b), testAccess) || strings.Contains(string(b), testRefresh) {
		t.Fatalf("probe carries token material: %s", b)
	}
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if c.Status().Configured {
		t.Fatal("still configured after disconnect")
	}
}

// StartConnect requests exactly gmail.send at the paste-back redirect.
func TestStartConnectScope(t *testing.T) {
	c, _ := testClient(t, nil, nil)
	u, err := c.StartConnect()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "scope=https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fgmail.send") {
		t.Fatalf("consent url lacks gmail.send: %s", u)
	}
	if strings.Contains(u, "gmail.readonly") || strings.Contains(u, "gmail.modify") {
		t.Fatalf("consent url requests a read scope: %s", u)
	}
	if !strings.Contains(u, "redirect_uri=http%3A%2F%2F127.0.0.1%3A8123%2Foauth%2Fcallback") {
		t.Fatalf("consent url redirect: %s", u)
	}
	if _, err := c.FinishConnect(context.Background(), "http://127.0.0.1:8123/oauth/callback?code=x&state=stale"); err == nil {
		t.Fatal("a stale state finished")
	}
}
