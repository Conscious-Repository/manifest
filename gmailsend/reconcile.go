package gmailsend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/mail"
	"time"
)

// SentProof must come from a provider reader that checks a unique SENT result
// in the requested mailbox. It is never accepted directly from a browser or an
// agent. Envelope validation below is additional to that reader's checks.
type SentProof struct {
	Mailbox string
	Ref     Ref
	Raw     []byte
}

type DeliveryEvidence struct {
	Mailbox   string    `json:"mailbox"`
	RawSHA256 string    `json:"rawSHA256"`
	CheckedAt time.Time `json:"checkedAt"`
}

// ReconcileApproved records an existing send using provider evidence. Like
// SendApproved, it consumes an independently authorized immutable envelope hash.
// Prepared deliveries cannot be reconciled; absent evidence never permits retry.
func (s DeliveryStore) ReconcileApproved(ctx context.Context, id, approvedHash string, lookup func(context.Context, string, string) (SentProof, error)) (Delivery, error) {
	unlock, err := s.lock(id)
	if err != nil {
		return Delivery{}, err
	}
	defer unlock()
	d, err := s.read(id)
	if err != nil {
		return d, err
	}
	if d.Hash != approvedHash {
		return d, fmt.Errorf("approved mail envelope changed")
	}
	if err := ctx.Err(); err != nil {
		return d, err
	}
	if d.Status == "sent" {
		return d, nil
	}
	if d.Status != "uncertain" || lookup == nil {
		return d, ErrDeliveryUncertain
	}
	sender, err := mail.ParseAddress(d.Message.From)
	if err != nil {
		return d, ErrEnvelopeEvidence
	}
	proof, err := lookup(ctx, sender.Address, d.Message.MessageID)
	if ctx.Err() != nil {
		return d, ctx.Err()
	}
	if err != nil {
		return d, fmt.Errorf("%w: provider evidence unavailable", ErrDeliveryUncertain)
	}
	if proof.Mailbox != sender.Address || proof.Ref.ID == "" || proof.Ref.ThreadID == "" || MatchSentEnvelope(d.Message, proof.Raw) != nil {
		return d, ErrEnvelopeEvidence
	}
	hash := sha256.Sum256(proof.Raw)
	if err := ctx.Err(); err != nil {
		return d, err
	}
	d.Status, d.Ref = "sent", proof.Ref
	d.Evidence = &DeliveryEvidence{Mailbox: proof.Mailbox, RawSHA256: hex.EncodeToString(hash[:]), CheckedAt: time.Now().UTC()}
	if err := s.write(d); err != nil {
		d.Status, d.Ref, d.Evidence = "uncertain", Ref{}, nil
		return d, fmt.Errorf("%w: evidence receipt could not be saved", ErrDeliveryUncertain)
	}
	return d, nil
}
