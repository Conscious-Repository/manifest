package agentchat

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestReviewedSharingRecoversExactEnvelope(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	id, err := s.Create("kairos-private", "kairos-private", "Reviewed", "")
	if err != nil {
		t.Fatal(err)
	}
	sess, body, _, _ := s.Get("kairos-private", id)
	revision := ShareRevision(sess, body)
	envelope := []byte(`{"revision":"approved-review","history":["old plan","native turn"],"files":["exact-revision"]}`)
	prepared, _, err := s.BeginReviewedShare("kairos-private", id, "approved-share-01", revision, "kairos", "team-one", envelope)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Sharing.EnvelopeHash != fingerprint(string(envelope)) {
		t.Fatal("envelope not bound to fence")
	}
	s = New(root)
	got, saved, err := s.ReviewedShare("kairos-private", id)
	if err != nil || !bytes.Equal(saved, envelope) || got.Sharing.State != "prepared" {
		t.Fatal(got, string(saved), err)
	}
	if _, _, err := s.BeginReviewedShare("kairos-private", id, "approved-share-01", revision, "kairos", "team-one", envelope); err != nil {
		t.Fatal("exact recovery", err)
	}
	if _, _, err := s.BeginReviewedShare("kairos-private", id, "approved-share-01", revision, "kairos", "team-one", []byte(`{"history":"newer unapproved contents"}`)); !errors.Is(err, ErrRequestConflict) {
		t.Fatal("changed approval accepted", err)
	}
	if _, _, err := s.BeginShare("kairos-private", id, "approved-share-01", revision, "kairos", "team-one"); !errors.Is(err, ErrRequestConflict) {
		t.Fatal("review bypass accepted", err)
	}
	if _, err := s.CompleteShare("kairos-private", id, "approved-share-01", revision); err != nil {
		t.Fatal(err)
	}
	if _, saved, err := New(root).ReviewedShare("kairos-private", id); err != nil || !bytes.Equal(saved, envelope) {
		t.Fatal("completed recovery", err)
	}
	if err := os.WriteFile(s.shareEnvelopePath(prepared.Sharing.EnvelopeHash), []byte(`{"tampered":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReviewedShare("kairos-private", id); err == nil {
		t.Fatal("corrupted approval recovered")
	}
	if _, _, err := s.BeginReviewedShare("kairos-private", id, "approved-share-01", revision, "kairos", "team-one", envelope); err == nil {
		t.Fatal("corruption silently repaired")
	}
	if _, err := s.Accept("kairos-private", id, "later-input-01", "cannot resume private writes"); !errors.Is(err, ErrShared) {
		t.Fatal("corruption reopened source", err)
	}
}

func TestReviewedShareRefusesInvalidAndStaleEnvelopes(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("zeck-private", "zeck-private", "Private", "")
	sess, body, _, _ := s.Get("zeck-private", id)
	revision := ShareRevision(sess, body)
	for _, invalid := range []string{"", " null ", "[]", "42", "{", `{"large":"` + strings.Repeat("x", maxShareEnvelope) + `"}`} {
		if _, _, err := s.BeginReviewedShare("zeck-private", id, "share-request-01", revision, "zeck", "team-one", []byte(invalid)); err == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
	if _, err := s.AppendTurn("zeck-private", id, "user", "new history", 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.BeginReviewedShare("zeck-private", id, "share-request-01", revision, "zeck", "team-one", []byte(`{"review":"stale"}`)); !errors.Is(err, ErrShareChanged) {
		t.Fatal("stale source fenced", err)
	}
	sess, _, _, _ = s.Get("zeck-private", id)
	if sess.Sharing != nil {
		t.Fatal("invalid review fenced source")
	}
}

func TestConcurrentReviewedSharesChooseOneEnvelope(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("zeck-private", "zeck-private", "Private", "")
	sess, body, _, _ := s.Get("zeck-private", id)
	revision := ShareRevision(sess, body)
	start := make(chan struct{})
	errs := make([]error, 2)
	envelopes := [][]byte{[]byte(`{"approved":"first"}`), []byte(`{"approved":"second"}`)}
	var wg sync.WaitGroup
	for i := range envelopes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, _, errs[i] = s.BeginReviewedShare("zeck-private", id, "same-request-01", revision, "zeck", "team-one", envelopes[i])
		}(i)
	}
	close(start)
	wg.Wait()
	winner := 0
	if errs[0] != nil {
		winner = 1
	}
	if errs[winner] != nil || !errors.Is(errs[1-winner], ErrRequestConflict) {
		t.Fatal(errs)
	}
	_, saved, err := New(s.Root()).ReviewedShare("zeck-private", id)
	if err != nil || !bytes.Equal(saved, envelopes[winner]) {
		t.Fatal("fence and envelope disagree", string(saved), err)
	}
}
