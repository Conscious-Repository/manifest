package agentchat

import (
	"errors"
	"testing"
)

func TestShareFenceSurvivesRestartAndRetainsReceiptRecovery(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	id, _ := s.Create("kairos-private", "kairos-private", "Private", "")
	a, e := s.Accept("kairos-private", id, "source-message-001", "hello")
	if e != nil {
		t.Fatal(e)
	}
	sess, body, _, _ := s.Get("kairos-private", id)
	if _, _, e = s.BeginShare("kairos-private", id, "share-request-001", ShareRevision(sess, body), "kairos", "shared-one"); !errors.Is(e, ErrShareBusy) {
		t.Fatal("queued source shared", e)
	}
	d, _, _ := s.Claim("kairos-private", id)
	if e = s.Finish("kairos-private", id, d.ID, "kairos-private", "reply", DeliveryCompleted, "", 0); e != nil {
		t.Fatal(e)
	}
	sess, body, _, _ = s.Get("kairos-private", id)
	revision := ShareRevision(sess, body)
	if _, _, e = s.BeginShare("kairos-private", id, "share-request-001", revision, "zeck", "shared-one"); e == nil {
		t.Fatal("cross-team share accepted")
	}
	if _, _, e = s.BeginShare("kairos-private", id, "share-request-001", "stale", "kairos", "shared-one"); !errors.Is(e, ErrShareChanged) {
		t.Fatal(e)
	}
	if _, _, e = s.BeginShare("kairos-private", id, "share-request-001", revision, "kairos", "shared-one"); e != nil {
		t.Fatal(e)
	}
	s = New(root)
	if _, e = s.Accept("kairos-private", id, "after-share-001", "do not accept"); !errors.Is(e, ErrShared) {
		t.Fatal("new input accepted", e)
	}
	if _, e = s.AppendTurn("kairos-private", id, "user", "bypass", 0); !errors.Is(e, ErrShared) {
		t.Fatal("direct append accepted", e)
	}
	if e = s.Delete("kairos-private", id); !errors.Is(e, ErrShared) {
		t.Fatal("share fence deleted", e)
	}
	retry, e := s.Accept("kairos-private", id, a.Delivery.ID, "hello")
	if e != nil || retry.New || retry.Delivery.State != DeliveryCompleted {
		t.Fatal("receipt recovery failed", retry, e)
	}
	if _, _, e = s.BeginShare("kairos-private", id, "share-request-001", revision, "kairos", "shared-one"); e != nil {
		t.Fatal("share recovery failed", e)
	}
	if _, e = s.CompleteShare("kairos-private", id, "share-request-001", revision); e != nil {
		t.Fatal(e)
	}
	if _, e = New(root).CompleteShare("kairos-private", id, "share-request-001", revision); e != nil {
		t.Fatal(e)
	}
	_, after, _, _ := s.Get("kairos-private", id)
	if after != body {
		t.Fatal("source history changed")
	}
}

func TestShareAndNewMessageCannotBothWin(t *testing.T) {
	for n := 0; n < 25; n++ {
		s := New(t.TempDir())
		id, _ := s.Create("zeck-private", "zeck-private", "Private", "")
		sess, body, _, _ := s.Get("zeck-private", id)
		revision := ShareRevision(sess, body)
		start := make(chan struct{})
		share := make(chan error, 1)
		message := make(chan error, 1)
		go func() {
			<-start
			_, _, err := s.BeginShare("zeck-private", id, "share-race-request", revision, "zeck", "target")
			share <- err
		}()
		go func() {
			<-start
			_, err := s.Accept("zeck-private", id, "message-race-request", "new instruction")
			message <- err
		}()
		close(start)
		se, me := <-share, <-message
		if se == nil && me == nil {
			t.Fatal("sharing and new input both committed")
		}
		if se != nil && me != nil {
			t.Fatal("neither operation progressed", se, me)
		}
	}
}
