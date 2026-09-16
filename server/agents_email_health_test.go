package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"manifest/gmailsync"
)

func TestAgentsEmailHealthCountsWithoutContent(t *testing.T) {
	tokens, err := gmailsync.NewTokens(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = tokens.Put("owner@example.com", &oauth2.Token{AccessToken: "fixture", RefreshToken: "fixture", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	candidates, err := gmailsync.NewCandidates(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		err = candidates.Upsert(gmailsync.Candidate{ID: id, Account: id + "@example.com", ThreadID: id, Fingerprint: "same-conversation", Subject: "private subject", Note: "private body"})
		if err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{oodaEmail: candidates, oodaGmail: tokens}
	rows := s.agentEmailHealth(nil)
	b, _ := json.Marshal(rows)
	if len(rows) != 1 || !strings.Contains(rows[0].Detail, "1 conversations") {
		t.Fatalf("%s", b)
	}
	for _, private := range []string{"private subject", "private body", "owner@example.com", "fixture"} {
		if strings.Contains(string(b), private) {
			t.Fatal("leaked", private)
		}
	}
}
