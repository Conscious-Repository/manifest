package manifestmcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// One durable notice per sent thread. Keeping the latest reply watermark after
// dismissal/expiry prevents repeated provider reads from resurfacing old mail.
type EmailReplyNotice struct {
	Reply     *EmailReply `json:"reply,omitempty"` // bounded preview behind this notice, retained across later reads
	ReplyID   string      `json:"replyId"`
	From      string      `json:"from"`
	At        time.Time   `json:"at"`
	Dismissed bool        `json:"dismissed,omitempty"`
}

type EmailNotice struct {
	ID          string
	OperationID string
	Subject     string
	Sender      string
	From        string
	At          time.Time
}

func emailNoticeID(operationID, replyID string) string {
	sum := sha256.Sum256([]byte(replyID))
	return "email-reply:" + strings.TrimPrefix(operationID, "sha256:") + ":" + hex.EncodeToString(sum[:])
}

func updateEmailNotice(w *EmailWatch, replies []EmailReply) {
	for _, r := range replies {
		previous := w.Notice
		if previous == nil || r.At.After(previous.At) || r.At.Equal(previous.At) && r.ID > previous.ReplyID {
			copy := r
			w.Notice = &EmailReplyNotice{ReplyID: r.ID, From: r.From, At: r.At, Reply: &copy}
		} else if previous.Reply == nil && r.ID == previous.ReplyID && r.At.Equal(previous.At) {
			// Hydrate older notices only from the same verified reply, without
			// rearming a dismissal or moving the latest-reply watermark.
			copy := r
			previous.Reply = &copy
		}
	}
}

// EmailNotices is a read-only projection: it never observes, reconciles or
// executes an operation and requires no provider connection.
func (a *Adapter) EmailNotices(now time.Time) ([]EmailNotice, error) {
	entries, err := os.ReadDir(filepath.Join(a.Data, "operations"))
	if os.IsNotExist(err) {
		return []EmailNotice{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []EmailNotice{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		o, err := a.loadOperation("sha256:" + strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		if o.Tool != "email.prepare" || o.Status != "succeeded" || o.EmailWatch == nil || o.EmailWatch.Notice == nil {
			continue
		}
		n := o.EmailWatch.Notice
		if n.Dismissed || n.At.IsZero() || !n.At.Add(14*24*time.Hour).After(now) {
			continue
		}
		var p struct {
			Email struct {
				Subject string `json:"subject"`
				From    string `json:"from"`
			} `json:"email"`
		}
		if err = json.Unmarshal(o.Arguments, &p); err != nil {
			return nil, err
		}
		out = append(out, EmailNotice{ID: emailNoticeID(o.ID, n.ReplyID), OperationID: o.ID, Subject: p.Email.Subject, Sender: p.Email.From, From: n.From, At: n.At})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out, nil
}

func (a *Adapter) DismissEmailNotice(id string) error {
	parts := strings.Split(id, ":")
	if len(parts) != 3 || parts[0] != "email-reply" || len(parts[1]) != 64 || len(parts[2]) != 64 {
		return errors.New("invalid reply notice")
	}
	unlock, err := a.lockOperations()
	if err != nil {
		return err
	}
	defer unlock()
	o, err := a.loadOperation("sha256:" + parts[1])
	if err != nil {
		return err
	}
	if o.Tool != "email.prepare" || o.Status != "succeeded" || o.EmailWatch == nil || o.EmailWatch.Notice == nil {
		return errors.New("reply notice unavailable")
	}
	n := o.EmailWatch.Notice
	if id != emailNoticeID(o.ID, n.ReplyID) {
		return errors.New("a newer reply arrived; refresh before dismissing")
	}
	n.Dismissed = true
	return a.saveOperation(o)
}
