package manifestmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"manifest/gmailsync"
)

type EmailReply struct {
	ID      string    `json:"id"`
	From    string    `json:"from"`
	At      time.Time `json:"at"`
	Body    string    `json:"body"`
	Clipped bool      `json:"clipped,omitempty"`
}
type EmailWatch struct {
	Enabled   bool         `json:"enabled"`
	CheckedAt time.Time    `json:"checkedAt,omitempty"`
	NextCheck time.Time    `json:"nextCheck,omitempty"`
	Error     string       `json:"error,omitempty"`
	Replies   []EmailReply `json:"replies"`
	Total     int          `json:"total"`
}
type EmailThreadReader func(context.Context, string, string) ([]gmailsync.Msg, error)

func (a *Adapter) SetEmailWatch(id string, enabled bool) (Object, error) {
	unlock, err := a.lockOperations()
	if err != nil {
		return nil, err
	}
	defer unlock()
	o, err := a.loadOperation(id)
	if err != nil {
		return nil, err
	}
	if o.Tool != "email.prepare" || o.Status != "succeeded" || !hasEmailThread(o) {
		return nil, errors.New("only a confirmed sent email can be tracked")
	}
	if o.EmailWatch == nil {
		o.EmailWatch = &EmailWatch{Replies: []EmailReply{}}
	}
	o.EmailWatch.Enabled = enabled
	o.EmailWatch.NextCheck = time.Time{}
	if err = a.saveOperation(o); err != nil {
		return nil, err
	}
	return receipt(o), nil
}

// PollEmailReplies is driven by the existing server ticker. Claims are persisted
// under the operation lock, but network reads never hold it or replay a send.
func (a *Adapter) PollEmailReplies(ctx context.Context, now time.Time, read EmailThreadReader) error {
	if read == nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(a.Data, "operations"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	type dueWatch struct {
		id   string
		next time.Time
	}
	due := []dueWatch{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := "sha256:" + strings.TrimSuffix(entry.Name(), ".json")
		o, e := a.loadOperation(id)
		if e == nil && o.Tool == "email.prepare" && o.Status == "succeeded" && o.EmailWatch != nil && o.EmailWatch.Enabled && !o.EmailWatch.NextCheck.After(now) {
			due = append(due, dueWatch{id, o.EmailWatch.NextCheck})
		}
	}
	// Least recently observed first; large watch lists do not starve old entries.
	sort.SliceStable(due, func(i, j int) bool { return due[i].next.Before(due[j].next) })
	count := 0
	for _, item := range due {
		id := item.id
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if count >= 20 {
			break
		}
		unlock, err := a.lockOperations()
		if err != nil {
			return err
		}
		o, err := a.loadOperation(id)
		if err != nil || o.Tool != "email.prepare" || o.Status != "succeeded" || o.EmailWatch == nil || !o.EmailWatch.Enabled || o.EmailWatch.NextCheck.After(now) {
			unlock()
			continue
		}
		sender, _ := o.Result["sender"].(string)
		thread, _ := o.Result["threadId"].(string)
		sent, _ := o.Result["messageId"].(string)
		if (sender != "ben@aion.bio" && sender != "ben@ooda.group") || thread == "" || sent == "" {
			unlock()
			continue
		}
		o.EmailWatch.NextCheck = now.Add(5 * time.Minute)
		if err = a.saveOperation(o); err != nil {
			unlock()
			return err
		}
		unlock()
		count++
		messages, readErr := read(ctx, sender, thread)
		replies, total := []EmailReply{}, 0
		if readErr == nil {
			replies, total, readErr = emailReplies(messages, sent, sender)
		}
		unlock, err = a.lockOperations()
		if err != nil {
			return err
		}
		current, err := a.loadOperation(id)
		if err == nil && current.EmailWatch != nil && current.EmailWatch.Enabled && current.EmailWatch.NextCheck.Equal(now.Add(5*time.Minute)) {
			current.EmailWatch.CheckedAt = now
			if readErr != nil {
				current.EmailWatch.Error = "Could not check replies. Verify the sender's read-only Gmail connection."
			} else {
				current.EmailWatch.Error = ""
				current.EmailWatch.Replies = replies
				current.EmailWatch.Total = total
			}
			err = a.saveOperation(current)
		}
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
func emailReplies(messages []gmailsync.Msg, sent, sender string) ([]EmailReply, int, error) {
	var sentAt time.Time
	for _, m := range messages {
		if m.ID == sent {
			sentAt = m.Internal
			break
		}
	}
	if sentAt.IsZero() {
		return nil, 0, errors.New("sent message missing from provider thread")
	}
	out := []EmailReply{}
	seen := map[string]bool{}
	for _, m := range messages {
		from, err := mail.ParseAddress(m.From)
		if err != nil || m.Sent || strings.EqualFold(from.Address, sender) || !m.Internal.After(sentAt) || m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		body := []rune(m.Body)
		clipped := len(body) > 4000
		if clipped {
			body = body[:4000]
		}
		out = append(out, EmailReply{m.ID, m.From, m.Internal, string(body), clipped})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	total := len(out)
	if total > 50 {
		out = out[total-50:]
	}
	return out, total, nil
}

// Keep imports and schema decoding near the preparation contract.
func emailWatchRequested(arguments json.RawMessage) bool {
	var p struct {
		MonitorReplies bool `json:"monitorReplies"`
	}
	_ = json.Unmarshal(arguments, &p)
	return p.MonitorReplies
}

func hasEmailThread(o *OperationRecord) bool {
	thread, _ := o.Result["threadId"].(string)
	return thread != ""
}
