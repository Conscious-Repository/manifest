package gmailsend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

// DeliveryStore holds immutable envelopes and network-boundary receipts. It is
// not an approval system or a scheduler. The existing decision executor must
// authorize the exact envelope hash before calling SendApproved.
type DeliveryStore struct{ Dir string }

type Delivery struct {
	ID      string  `json:"id"`
	Hash    string  `json:"hash"`
	Message Message `json:"message"`
	Status  string  `json:"status"` // prepared, uncertain, sent
	Ref     Ref     `json:"ref"`
}

var deliveryKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{7,95}$`)
var ErrDeliveryUncertain = errors.New("mail delivery is uncertain; inspect the existing attempt before any new send")

func (s DeliveryStore) lock(id string) (func(), error) {
	if !deliveryKey.MatchString(id) {
		return nil, errors.New("invalid mail delivery ID")
	}
	if s.Dir == "" {
		return nil, errors.New("mail delivery directory is required")
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, id+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func envelopeHash(m Message) (string, error) {
	raw, err := Build(m)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}

func (s DeliveryStore) read(id string) (Delivery, error) {
	var d Delivery
	b, err := os.ReadFile(filepath.Join(s.Dir, id+".json"))
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return d, err
	}
	if d.ID != id || d.Message.Date.IsZero() || d.Message.MessageID == "" {
		return d, errors.New("invalid mail envelope identity")
	}
	h, err := envelopeHash(d.Message)
	if err != nil || h != d.Hash {
		return d, errors.New("mail envelope hash mismatch")
	}
	if d.Status != "prepared" && d.Status != "uncertain" && d.Status != "sent" {
		return d, errors.New("invalid mail receipt status")
	}
	if d.Status == "sent" && d.Ref.ID == "" {
		return d, errors.New("sent mail receipt lacks provider identity")
	}
	return d, nil
}

func (s DeliveryStore) write(d Delivery) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".mail-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(s.Dir, d.ID+".json")); err != nil {
		return err
	}
	dir, err := os.Open(s.Dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (s DeliveryStore) Get(id string) (Delivery, error) {
	unlock, err := s.lock(id)
	if err != nil {
		return Delivery{}, err
	}
	defer unlock()
	return s.read(id)
}

// Prepare snapshots all bytes, including attachments, before approval. Reusing
// an ID with edited content fails; the edited draft needs a new approval ID.
func (s DeliveryStore) Prepare(id string, m Message) (Delivery, error) {
	unlock, err := s.lock(id)
	if err != nil {
		return Delivery{}, err
	}
	defer unlock()
	old, err := s.read(id)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Delivery{}, err
	}
	if m.Date.IsZero() {
		m.Date = old.Message.Date
		if m.Date.IsZero() {
			m.Date = time.Now().UTC().Truncate(time.Second)
		}
	}
	if m.MessageID == "" {
		m.MessageID = old.Message.MessageID
		if m.MessageID == "" {
			m.MessageID = NewMessageID(m.From)
		}
	}
	h, err := envelopeHash(m)
	if err != nil {
		return Delivery{}, err
	}
	if old.ID != "" {
		if old.Hash != h {
			return Delivery{}, errors.New("mail delivery ID already belongs to another envelope")
		}
		return old, nil
	}
	d := Delivery{ID: id, Hash: h, Message: m, Status: "prepared"}
	if err := s.write(d); err != nil {
		return Delivery{}, err
	}
	return s.read(id)
}

// SendApproved consumes an already-authorized envelope hash, not a mutable draft.
// Any network error remains uncertain. Reopening or repeating an attempt never
// retries that network boundary. No provider exactly-once guarantee is implied.
func (s DeliveryStore) SendApproved(ctx context.Context, id, approvedHash string, send func(context.Context, Message) (Ref, error)) (Delivery, error) {
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
		return d, errors.New("approved mail envelope changed")
	}
	if d.Status == "sent" {
		return d, nil
	}
	if d.Status == "uncertain" {
		return d, ErrDeliveryUncertain
	}
	if send == nil {
		return d, errors.New("mail sender is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return d, err
	}
	d.Status = "uncertain"
	if err := s.write(d); err != nil {
		return d, err
	}
	ref, err := send(ctx, d.Message)
	if err != nil {
		return d, fmt.Errorf("%w: sender did not confirm success", ErrDeliveryUncertain)
	}
	if ref.ID == "" {
		return d, ErrDeliveryUncertain
	}
	d.Status, d.Ref = "sent", ref
	if err := s.write(d); err != nil {
		d.Status = "uncertain"
		return d, fmt.Errorf("%w: provider accepted but local receipt could not be saved", ErrDeliveryUncertain)
	}
	return d, nil
}
