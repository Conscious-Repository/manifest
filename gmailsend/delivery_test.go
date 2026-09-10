package gmailsend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func deliveryFixture(t *testing.T) (DeliveryStore, Delivery, Message) {
	t.Helper()
	s := DeliveryStore{Dir: t.TempDir()}
	m := Message{From: "owner@example.test", To: []string{"contractor@example.test"}, Subject: "Bid request", Body: "Please review attached plans.", Attachments: []Attachment{{Name: "plans.pdf", Data: []byte("approved bytes")}}}
	d, err := s.Prepare("test-message-001", m)
	if err != nil {
		t.Fatal(err)
	}
	return s, d, m
}

func TestMailDeliverySnapshotAndApprovalBinding(t *testing.T) {
	s, d, m := deliveryFixture(t)
	again, err := s.Prepare(d.ID, m)
	if err != nil || again.Hash != d.Hash {
		t.Fatal("prepare replay changed envelope", err)
	}
	m.Attachments[0].Data[0] = 'X'
	if _, err := s.Prepare(d.ID, m); err == nil {
		t.Fatal("edited attachment reused approval identity")
	}
	var calls int
	send := func(context.Context, Message) (Ref, error) { calls++; return Ref{ID: "provider-001"}, nil }
	if _, err := s.SendApproved(context.Background(), d.ID, "wrong", send); err == nil || calls != 0 {
		t.Fatal("stale approval sent")
	}
	got, err := s.Get(d.ID)
	if err != nil || string(got.Message.Attachments[0].Data) != "approved bytes" {
		t.Fatal("caller edit mutated snapshot", err)
	}
}

func TestMailDeliveryConcurrentReplayUsesOneNetworkAttempt(t *testing.T) {
	s, d, _ := deliveryFixture(t)
	var calls atomic.Int32
	send := func(_ context.Context, m Message) (Ref, error) {
		calls.Add(1)
		before, err := s.read(d.ID)
		if err != nil || before.Status != "uncertain" {
			t.Error("intent not durable before network", err)
		}
		if m.MessageID != d.Message.MessageID {
			t.Error("wire identity changed")
		}
		return Ref{ID: "gmail-001", ThreadID: "thread-001"}, nil
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := DeliveryStore{Dir: s.Dir}
			got, err := other.SendApproved(context.Background(), d.ID, d.Hash, send)
			if err != nil || got.Status != "sent" || got.Ref.ID != "gmail-001" {
				t.Error(got.Status, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate dispatch", calls.Load())
	}
}

func TestMailDeliveryUncertainSurvivesReopen(t *testing.T) {
	s, d, _ := deliveryFixture(t)
	calls := 0
	send := func(context.Context, Message) (Ref, error) {
		calls++
		return Ref{}, errors.New("provider may have accepted before connection dropped")
	}
	if _, err := s.SendApproved(context.Background(), d.ID, d.Hash, send); !errors.Is(err, ErrDeliveryUncertain) {
		t.Fatal(err)
	}
	reopened := DeliveryStore{Dir: s.Dir}
	if _, err := reopened.SendApproved(context.Background(), d.ID, d.Hash, send); !errors.Is(err, ErrDeliveryUncertain) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("uncertain delivery replayed")
	}
}

func TestMailDeliveryCorruptionAndCancellationNeverSend(t *testing.T) {
	s, d, _ := deliveryFixture(t)
	calls := 0
	send := func(context.Context, Message) (Ref, error) { calls++; return Ref{}, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.SendApproved(ctx, d.ID, d.Hash, send); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, d.ID+".json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendApproved(context.Background(), d.ID, d.Hash, send); err == nil || calls != 0 {
		t.Fatal("invalid receipt sent")
	}
}

func TestMailDeliveryReceiptWriteFailureDoesNotResend(t *testing.T) {
	s, d, _ := deliveryFixture(t)
	backup := s.Dir + "-offline"
	calls := 0
	send := func(context.Context, Message) (Ref, error) {
		calls++
		if err := os.Rename(s.Dir, backup); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.Dir, []byte("storage unavailable"), 0600); err != nil {
			t.Fatal(err)
		}
		return Ref{ID: "accepted-by-provider"}, nil
	}
	got, err := s.SendApproved(context.Background(), d.ID, d.Hash, send)
	if !errors.Is(err, ErrDeliveryUncertain) || got.Status != "uncertain" {
		t.Fatal(got.Status, err)
	}
	if err := os.Remove(s.Dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, s.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendApproved(context.Background(), d.ID, d.Hash, send); !errors.Is(err, ErrDeliveryUncertain) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("provider success with lost local receipt resent")
	}
}
