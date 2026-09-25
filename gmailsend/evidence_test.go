package gmailsend

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"mime"
	"net/mail"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestEnvelopeEvidenceMIMEAndHeaders(t *testing.T) {
	_, d, _ := deliveryFixture(t)
	for _, attachments := range []bool{false, true} {
		m := d.Message
		if !attachments {
			m.Attachments = nil
		}
		raw, err := Build(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := MatchSentEnvelope(m, raw); err != nil {
			t.Fatal(attachments, err)
		}
		transported := []byte("Received: by provider\r\nReceived: from provider\r\nX-Google-Smtp-Source: transport\r\n" + string(raw))
		if err := MatchSentEnvelope(m, transported); err != nil {
			t.Fatal(err)
		}
		changed := m
		changed.Body += " changed"
		different, _ := Build(changed)
		if MatchSentEnvelope(m, different) == nil {
			t.Fatal("changed body accepted")
		}
		for _, pair := range [][2]string{{"contractor@example.test", "intruder@example.test"}, {"Bid request", "Different subject"}, {"Message-ID:", "Other-ID:"}, {"From:", "From: extra@example.test\r\nFrom:"}, {"MIME-Version: 1.0", "Bcc: hidden@example.test\r\nMIME-Version: 1.0"}} {
			if MatchSentEnvelope(m, []byte(strings.Replace(string(raw), pair[0], pair[1], 1))) == nil {
				t.Fatal("changed header accepted", pair)
			}
		}
		if attachments {
			changed = m
			changed.Attachments = []Attachment{{Name: "other.pdf", Data: m.Attachments[0].Data}}
			different, _ = Build(changed)
			if MatchSentEnvelope(m, different) == nil {
				t.Fatal("renamed attachment accepted")
			}
			changed.Attachments = []Attachment{{Name: m.Attachments[0].Name, Data: []byte("different")}}
			different, _ = Build(changed)
			if MatchSentEnvelope(m, different) == nil {
				t.Fatal("changed attachment accepted")
			}
		} else {
			header, body, _ := strings.Cut(string(raw), "\r\n\r\n")
			encoded := strings.Replace(header, "Content-Transfer-Encoding: 8bit", "Content-Transfer-Encoding: base64", 1) + "\r\n\r\n" + base64.StdEncoding.EncodeToString([]byte(body))
			if MatchSentEnvelope(m, []byte(encoded)) != nil {
				t.Fatal("equivalent transfer encoding rejected")
			}
		}
	}
}
func uncertainFixture(t *testing.T) (DeliveryStore, Delivery) {
	t.Helper()
	s, d, _ := deliveryFixture(t)
	_, err := s.SendApproved(context.Background(), d.ID, d.Hash, func(context.Context, Message) (Ref, error) { return Ref{}, errors.New("lost ack") })
	if !errors.Is(err, ErrDeliveryUncertain) {
		t.Fatal(err)
	}
	return s, d
}
func TestReconcileLostAckConcurrentAndReopen(t *testing.T) {
	s, d := uncertainFixture(t)
	raw, _ := Build(d.Message)
	var calls atomic.Int32
	lookup := func(_ context.Context, mailbox, id string) (SentProof, error) {
		calls.Add(1)
		if mailbox != d.Message.From || id != d.Message.MessageID {
			t.Error("wrong lookup")
		}
		return SentProof{mailbox, Ref{ID: "sent-1", ThreadID: "thread-1"}, raw}, nil
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := DeliveryStore{Dir: s.Dir}
			got, err := other.ReconcileApproved(context.Background(), d.ID, d.Hash, lookup)
			if err != nil || got.Status != "sent" || got.Evidence == nil || len(got.Evidence.RawSHA256) != 64 {
				t.Error(got.Status, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("repeated lookup", calls.Load())
	}
	got, err := s.SendApproved(context.Background(), d.ID, d.Hash, func(context.Context, Message) (Ref, error) { t.Error("resent recovered delivery"); return Ref{}, nil })
	if err != nil || got.Ref.ID != "sent-1" {
		t.Fatal(got, err)
	}
}
func TestReconcileRefusalsNeverReleaseUncertain(t *testing.T) {
	for _, kind := range []string{"missing", "wrong-mailbox", "wrong-content", "no-thread", "canceled", "wrong-approval"} {
		t.Run(kind, func(t *testing.T) {
			s, d := uncertainFixture(t)
			raw, _ := Build(d.Message)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hash := d.Hash
			if kind == "wrong-approval" {
				hash = "wrong"
			}
			_, err := s.ReconcileApproved(ctx, d.ID, hash, func(context.Context, string, string) (SentProof, error) {
				p := SentProof{d.Message.From, Ref{ID: "sent-1", ThreadID: "thread-1"}, raw}
				switch kind {
				case "missing":
					return p, errors.New("PRIVATE ERROR")
				case "wrong-mailbox":
					p.Mailbox = "other@example.test"
				case "wrong-content":
					p.Raw = []byte("different")
				case "no-thread":
					p.Ref.ThreadID = ""
				case "canceled":
					cancel()
				case "wrong-approval":
					t.Error("read on wrong approval")
				}
				return p, nil
			})
			if err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal(err)
			}
			got, err := s.Get(d.ID)
			if err != nil || got.Status != "uncertain" || got.Evidence != nil || got.Ref.ID != "" {
				t.Fatal(got, err)
			}
			_, err = s.SendApproved(context.Background(), d.ID, d.Hash, func(context.Context, Message) (Ref, error) { t.Error("resent"); return Ref{}, nil })
			if !errors.Is(err, ErrDeliveryUncertain) {
				t.Fatal(err)
			}
		})
	}
	s, d, _ := deliveryFixture(t)
	_, err := s.ReconcileApproved(context.Background(), d.ID, d.Hash, func(context.Context, string, string) (SentProof, error) {
		t.Error("read unsent message")
		return SentProof{}, nil
	})
	if !errors.Is(err, ErrDeliveryUncertain) {
		t.Fatal(err)
	}
}

func TestEnvelopeEvidenceMultipartBoundaryAndEncodings(t *testing.T) {
	_, d, _ := deliveryFixture(t)
	raw, _ := Build(d.Message)
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.ReplaceAll(string(raw), params["boundary"], "provider-reencoded-boundary")
	if MatchSentEnvelope(d.Message, []byte(changed)) != nil {
		t.Fatal("equivalent multipart boundary rejected")
	}
	changed = strings.Replace(changed, base64.StdEncoding.EncodeToString(d.Message.Attachments[0].Data), "%%%", 1)
	if MatchSentEnvelope(d.Message, []byte(changed)) == nil {
		t.Fatal("invalid attachment encoding accepted")
	}
	for _, header := range []string{"Reply-To: different@example.test", "Content-Type: text/html", "Content-Transfer-Encoding: unknown"} {
		if MatchSentEnvelope(d.Message, append([]byte(header+"\r\n"), raw...)) == nil {
			t.Fatal("unexpected or duplicate header", header)
		}
	}
}
func TestReconcileReceiptWriteFailureCanRecoverWithoutSend(t *testing.T) {
	s, d := uncertainFixture(t)
	raw, _ := Build(d.Message)
	backup := s.Dir + "-offline"
	proof := SentProof{d.Message.From, Ref{ID: "accepted", ThreadID: "thread"}, raw}
	got, err := s.ReconcileApproved(context.Background(), d.ID, d.Hash, func(context.Context, string, string) (SentProof, error) {
		if err := os.Rename(s.Dir, backup); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.Dir, []byte("unavailable"), 0600); err != nil {
			t.Fatal(err)
		}
		return proof, nil
	})
	if !errors.Is(err, ErrDeliveryUncertain) || got.Status != "uncertain" || got.Evidence != nil {
		t.Fatal(got, err)
	}
	if err := os.Remove(s.Dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, s.Dir); err != nil {
		t.Fatal(err)
	}
	got, err = s.ReconcileApproved(context.Background(), d.ID, d.Hash, func(context.Context, string, string) (SentProof, error) { return proof, nil })
	if err != nil || got.Status != "sent" || got.Ref.ID != "accepted" {
		t.Fatal(got, err)
	}
}
