package consume

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// THE GUARD's tests. Every one of them runs without touching a network: the
// rejections happen before a socket is opened, which is the point.

func TestCurateURLRejectsNonHTTPSchemes(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd",
		"gopher://example.com/1",
		"data:text/html,<h1>hi</h1>",
		"javascript:alert(1)",
		"ftp://example.com/x",
		"",
		"   ",
	} {
		if got, err := guardURL(raw, false); err == nil {
			t.Errorf("guardURL(%q) accepted it as %q", raw, got)
		}
	}
}

func TestCurateURLRejectsPrivateAddresses(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/x",
		"http://127.0.0.1:1200/twitter/user/melissa", // RSSHub — reachable by the LANE, never by a paste
		"http://10.0.0.1/x",
		"http://192.168.1.1/admin",
		"http://172.16.0.5/x",
		"http://169.254.169.254/latest/meta-data/", // the cloud metadata endpoint
		"http://[::1]/x",
		"http://[fc00::1]/x",
		"http://0.0.0.0/x",
		"http://100.100.1.1/x", // CGNAT — where a tailnet lives
		"http://localhost/x",
		"http://api.localhost/x",
	} {
		if _, err := guardURL(raw, false); err == nil {
			t.Errorf("guardURL(%q) accepted a private address", raw)
		}
	}
}

func TestCurateURLRejectsNonStandardPorts(t *testing.T) {
	for _, raw := range []string{
		"http://example.com:9200/_search",
		"http://example.com:1200/feed",
		"http://example.com:22/",
	} {
		if _, err := guardURL(raw, false); err == nil {
			t.Errorf("guardURL(%q) accepted a non-web port", raw)
		}
	}
	for _, raw := range []string{
		"https://example.com/p/essay",
		"http://example.com:80/p/essay",
		"https://example.com:443/p/essay",
	} {
		if _, err := guardURL(raw, false); err != nil {
			t.Errorf("guardURL(%q) rejected the open web: %v", raw, err)
		}
	}
}

// A public host answering 302 → 127.0.0.1 is the whole bypass if hops are not
// re-checked. CheckRedirect is exercised directly: the rejection has to happen
// before the dialer is asked for a connection.
func TestGuardFollowsRedirectsAndRechecks(t *testing.T) {
	c := guardedClient(false)
	hop := func(target string, hops int) error {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		via := make([]*http.Request, hops)
		return c.CheckRedirect(req, via)
	}
	if err := hop("http://127.0.0.1:1200/", 1); err == nil {
		t.Error("a redirect to loopback was followed")
	}
	if err := hop("http://169.254.169.254/latest/meta-data/", 1); err == nil {
		t.Error("a redirect to the metadata endpoint was followed")
	}
	if err := hop("https://example.com/next", 1); err != nil {
		t.Errorf("an ordinary redirect was blocked: %v", err)
	}
	if err := hop("https://example.com/next", guardMaxHops); err == nil {
		t.Error("the hop cap did not stop a redirect loop")
	}
}

// The dial-time control is what closes DNS rebinding: a NAME that resolves to
// loopback passes guardURL and must still fail before bytes move.
func TestGuardBlocksAtDialTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><title>inside</title></html>"))
	}))
	defer srv.Close()

	s := New(t.TempDir(), newVault(t).io(), Config{})
	if _, ok := s.fetchPage(context.Background(), srv.URL); ok {
		t.Fatal("the guarded client reached a loopback server")
	}
	// …and the seam that lets the rest of these tests run at all.
	open := New(t.TempDir(), newVault(t).io(), Config{AllowPrivateCurateFetch: true})
	if _, ok := open.fetchPage(context.Background(), srv.URL); !ok {
		t.Fatal("AllowPrivateCurateFetch did not reach the test server")
	}
}

// ⚠ THE TRIPWIRE. defaultRSSHubBase is http://127.0.0.1:1200 and every
// @handle subscription rides it. If a future tightening moves the guard onto
// s.hc, @melissa stops polling and nothing else in the suite notices.
func TestRSSHubLoopbackStillReachable(t *testing.T) {
	rsshub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/twitter/user/melissa") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel>` +
			`<title>@melissa</title><item><title>a post</title>` +
			`<link>https://x.com/melissa/status/1</link><guid>1</guid></item></channel></rss>`))
	}))
	defer rsshub.Close()

	v := newVault(t)
	s := New(t.TempDir(), v.io(), Config{RSSHubBase: rsshub.URL})
	sub, err := s.Subscribe(context.Background(), "@melissa", "", "essays", MirrorFull)
	if err != nil {
		t.Fatalf("the RSSHub bridge is unreachable — every @handle subscription is broken: %v", err)
	}
	if !strings.Contains(sub.URL, "/twitter/user/melissa") {
		t.Errorf("subscription did not route through RSSHub: %s", sub.URL)
	}
}
