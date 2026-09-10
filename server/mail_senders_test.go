package server

import (
	"manifest/gmailsend"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestMailSenderDomainsNeverFallback(t *testing.T) {
	s := New(nil, nil, nil)
	a := gmailsend.New("ben@aion.bio", filepath.Join(t.TempDir(), "aion.json"))
	o := gmailsend.New("ben@ooda.group", filepath.Join(t.TempDir(), "ooda.json"))
	s.UseGmailSend(a)
	s.UseOodaMailSend(o)
	for _, domain := range []string{"recruiting", "aion", "aion.bio"} {
		c, e := s.mailSender(domain)
		if e != nil || c != a {
			t.Fatal(domain, c, e)
		}
	}
	for _, domain := range []string{"ooda", "ooda.group"} {
		c, e := s.mailSender(domain)
		if e != nil || c != o {
			t.Fatal(domain, c, e)
		}
	}
	for _, domain := range []string{"", "personal", "outlook.com", "unknown", "recipient@gmail.com"} {
		if c, e := s.mailSender(domain); e == nil || c != nil {
			t.Fatal("fell back", domain, c)
		}
	}
	s.UseOodaMailSend(a)
	if _, e := s.mailSender("ooda"); e == nil {
		t.Fatal("wrong domain account accepted")
	}
	s.UseGmailSend(o)
	if s.outreachSender() != nil {
		t.Fatal("recruiting sent from OODA")
	}
}
func TestDomainMailSettingsAndPortalBoundary(t *testing.T) {
	s := New(nil, nil, nil)
	s.UseOodaMailSend(gmailsend.New("ben@ooda.group", filepath.Join(t.TempDir(), "ooda.json")))
	h := s.Handler()
	for path, want := range map[string]int{"/api/settings/gmail-send/ooda": 200, "/api/settings/gmail-send/personal": 400} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	p, e := PortalHandler(PortalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/settings/gmail-send/ooda", "/api/settings/gmail-send/ooda/connect/start", "/api/settings/gmail-send/ooda/disconnect"} {
		for _, method := range []string{"GET", "POST"} {
			w := httptest.NewRecorder()
			p.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code == 200 {
				t.Fatal("portal exposed sender configuration", path)
			}
		}
	}
}
