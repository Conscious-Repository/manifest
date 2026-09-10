package gmailsend

import (
	"strings"
	"testing"
)

func TestDomainSenderIsolation(t *testing.T) {
	r := NewRegistry(t.TempDir())
	for _, tc := range []struct {
		domain string
		to     []string
		want   string
	}{{"aion", []string{"contractor@example.com"}, "ben@aion.bio"}, {"real-estate", []string{"contractor@example.com"}, "ben@ooda.group"}, {"", []string{"team@ooda.group"}, "ben@ooda.group"}} {
		c, e := r.Resolve(tc.domain, tc.to)
		if e != nil || c.Sender() != tc.want {
			t.Fatalf("%+v: %v %v", tc, c, e)
		}
		if !strings.Contains(c.endpoint, tc.want) {
			t.Fatal("provider mailbox is not pinned to sender")
		}
	}
	for _, tc := range []struct {
		domain string
		to     []string
	}{{"personal", nil}, {"missing", nil}, {"", []string{"x@example.com"}}, {"", []string{"x@ooda.group", "x@aion.bio"}}} {
		if _, e := r.Resolve(tc.domain, tc.to); e == nil {
			t.Fatalf("fallback: %+v", tc)
		}
	}
	if r.Aion.tokenPath == r.Ooda.tokenPath {
		t.Fatal("accounts share token file")
	}
}
func TestRejectHeaderInjection(t *testing.T) {
	m := Message{From: "ben@ooda.group", To: []string{"x@example.com"}, Subject: "hello", Body: "body", InReplyTo: "<ok>\r\nBcc: x@bad.example"}
	if _, e := Build(m); e == nil {
		t.Fatal("accepted injected header")
	}
}
