package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"manifest/gmailsend"
	"manifest/recruiting"
)

// mailSendersFixture: the recruiting client (ben@aion.bio, legacy token
// path) plus ben@ooda.group under ooda.group, wired the way main.go does it
// (registry first, then the recruiting client joins it under its domain).
func mailSendersFixture(t *testing.T) (*Server, *gmailsend.Client) {
	t.Helper()
	t.Setenv("GMAIL_SEND_TOKEN", "")
	t.Setenv("GMAIL_OAUTH_CLIENT", filepath.Join(t.TempDir(), "absent.json"))
	dir := t.TempDir()
	s := New(nil, nil, nil)
	reg := gmailsend.NewRegistry()
	if err := reg.Register("ooda.group", gmailsend.New("ben@ooda.group", gmailsend.SenderTokenPath(dir, "ben@ooda.group"))); err != nil {
		t.Fatal(err)
	}
	s.UseMailSenders(reg)
	rec := gmailsend.New("", gmailsend.TokenPath(dir))
	s.UseGmailSend(rec)
	return s, rec
}

// Settings › Connections shows one row per sender account, and the
// connect/disconnect routes address the account named by ?domain= — an
// unmapped domain is a 404, never the recruiting client.
func TestMailSendersSettingsRowsAndDomainRouting(t *testing.T) {
	s, rec := mailSendersFixture(t)
	t.Setenv("MANIFEST_CONFIG_DIR", t.TempDir())

	w := httptest.NewRecorder()
	s.handleSettingsConnections(w, httptest.NewRequest(http.MethodGet, "/api/settings/connections", nil))
	var out struct {
		Rows []panelRow `json:"rows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	byID := map[string]panelRow{}
	for _, r := range out.Rows {
		byID[r.ID] = r
	}
	rr, ok := byID["gmail-send"]
	if !ok || rr.Masked != "ben@aion.bio" || rr.Extra["domain"] != nil {
		t.Fatalf("recruiting row: %+v", rr)
	}
	or, ok := byID["gmail-send-ooda.group"]
	if !ok || or.Masked != "ben@ooda.group" || or.Extra["domain"] != "ooda.group" || or.Kind != "gmailsend" {
		t.Fatalf("ooda row: %+v", or)
	}
	if b := w.Body.String(); strings.Contains(b, "access_token") || strings.Contains(b, "refresh_token") || strings.Contains(b, "token.json") {
		t.Fatalf("connections body carries token material or paths: %s", b)
	}

	// disconnect for ooda.group answers the ooda row
	w = httptest.NewRecorder()
	s.handleSettingsGmailSendDisconnect(w, httptest.NewRequest(http.MethodPost, "/api/settings/gmail-send/disconnect?domain=OODA.group", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"gmail-send-ooda.group"`) {
		t.Fatalf("ooda disconnect: %d %s", w.Code, w.Body.String())
	}
	// no domain → the recruiting row (unchanged behaviour)
	w = httptest.NewRecorder()
	s.handleSettingsGmailSendDisconnect(w, httptest.NewRequest(http.MethodPost, "/api/settings/gmail-send/disconnect", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"gmail-send"`) {
		t.Fatalf("recruiting disconnect: %d %s", w.Code, w.Body.String())
	}
	// the recruiting domain resolves to the recruiting client, answered as the legacy row
	w = httptest.NewRecorder()
	s.handleSettingsGmailSendDisconnect(w, httptest.NewRequest(http.MethodPost, "/api/settings/gmail-send/disconnect?domain=aion.bio", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"gmail-send"`) || strings.Contains(w.Body.String(), "gmail-send-aion") {
		t.Fatalf("aion.bio disconnect: %d %s", w.Code, w.Body.String())
	}
	// an unmapped domain is refused — it does NOT fall through to the recruiting client
	for _, route := range []func(http.ResponseWriter, *http.Request){
		s.handleSettingsGmailSendStart, s.handleSettingsGmailSendFinish, s.handleSettingsGmailSendDisconnect,
	} {
		w = httptest.NewRecorder()
		route(w, httptest.NewRequest(http.MethodPost, "/api/settings/gmail-send/x?domain=benjaminbanderson.com", strings.NewReader(`{"redirect":"http://127.0.0.1:8123/oauth/callback?code=x&state=y"}`)))
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "benjaminbanderson.com") {
			t.Fatalf("unmapped domain: %d %s", w.Code, w.Body.String())
		}
	}
	if c, err := s.mailSender("aion.bio"); err != nil || c != rec {
		t.Fatalf("aion.bio should resolve to the recruiting client: %v", err)
	}
}

// The outreach adapter is pinned to the recruiting domain, and a send for a
// domain nobody registered is gmailsend.ErrNoSender → 409, nothing sent.
func TestOutreachSenderPinnedAndUnmappedDomainRefused(t *testing.T) {
	s, _ := mailSendersFixture(t)
	snd := s.outreachSender()
	if snd == nil || snd.Sender() != "ben@aion.bio" || snd.SendCapable() {
		t.Fatalf("outreach sender: %v", snd)
	}
	// the recruiting adapter never sends when its own account is not connected
	if _, err := snd.Send(context.Background(), recruiting.OutreachMessage{To: []string{"d@x.test"}, Subject: "s", Body: "b"}); !errors.Is(err, gmailsend.ErrUnconfigured) {
		t.Fatalf("unconnected recruiting send: %v", err)
	}
	// a personal-domain send with nothing registered for it fails loud
	_, err := s.mailSenders.Send(context.Background(), "benjaminbanderson.com", gmailsend.Message{To: []string{"d@x.test"}, Subject: "s", Body: "b"})
	if !errors.Is(err, gmailsend.ErrNoSender) {
		t.Fatalf("unmapped: %v", err)
	}
	w := httptest.NewRecorder()
	outreachError(w, err, recruiting.OutreachReadiness{})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "never sent from another domain") {
		t.Fatalf("ErrNoSender should be a 409 with the reason: %d %s", w.Code, w.Body.String())
	}
	// no registry wired at all: the outreach posture is "unconfigured", not "send as ben@aion.bio"
	bare := New(nil, nil, nil)
	if bare.outreachSender() != nil {
		t.Fatal("a server without a client has a sender")
	}
	if _, err := bare.mailSender("aion.bio"); !errors.Is(err, gmailsend.ErrNoSender) {
		t.Fatalf("bare mailSender: %v", err)
	}
}
