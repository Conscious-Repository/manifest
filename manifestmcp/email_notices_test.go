package manifestmcp

import (
	"context"
	"manifest/gmailsend"
	"manifest/gmailsync"
	"testing"
	"time"
)

func TestEmailReplyNoticeLifecycle(t *testing.T) {
	a, _, _ := fixture(t)
	p, err := a.PrepareEmail(EmailInput{Domain: "aion", To: []string{"candidate@example.test"}, Subject: "Private opportunity", Body: "Invitation", MonitorReplies: true, IdempotencyKey: "notice-test"})
	if err != nil {
		t.Fatal(err)
	}
	id := p["operationId"].(string)
	sends := 0
	a.mailSend = func(context.Context, gmailsend.Message) (gmailsend.Ref, error) {
		sends++
		return gmailsend.Ref{ID: "sent", ThreadID: "thread"}, nil
	}
	approve(t, a, id)
	execute(t, a, id, "succeeded")
	now := time.Now().UTC()
	anchor := gmailsync.Msg{ID: "sent", From: "ben@aion.bio", Internal: now.Add(-time.Hour)}
	first := gmailsync.Msg{ID: "first", From: "Candidate <candidate@example.test>", Internal: now.Add(-time.Minute), Body: "PRIVATE REPLY BODY"}
	poll := func(at time.Time, reply gmailsync.Msg) {
		t.Helper()
		if err := a.PollEmailReplies(context.Background(), at, func(context.Context, string, string) ([]gmailsync.Msg, error) {
			return []gmailsync.Msg{anchor, reply}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	notices := func(at time.Time) []EmailNotice {
		t.Helper()
		n, err := a.EmailNotices(at)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	poll(now, first)
	n := notices(now)
	if len(n) != 1 || n[0].OperationID != id || n[0].Subject != "Private opportunity" || n[0].From != first.From {
		t.Fatal(n)
	}
	oldID := n[0].ID
	if err := a.DismissEmailNotice(oldID); err != nil {
		t.Fatal(err)
	}
	if err := a.DismissEmailNotice(oldID); err != nil {
		t.Fatal("dismiss retry", err)
	}
	a, err = New(a.Vault, a.Data, a.System)
	if err != nil {
		t.Fatal(err)
	}
	poll(now.Add(5*time.Minute), first)
	if len(notices(now)) != 0 {
		t.Fatal("dismissed reply resurfaced after restart")
	}
	second := first
	second.ID = "second"
	second.Internal = now.Add(time.Minute)
	poll(now.Add(10*time.Minute), second)
	n = notices(now)
	if len(n) != 1 || n[0].ID == oldID {
		t.Fatal("new reply did not rearm", n)
	}
	if err := a.DismissEmailNotice(oldID); err == nil {
		t.Fatal("stale dismissal hid newer reply")
	}
	poll(now.Add(15*time.Minute), first)
	if got := notices(now); len(got) != 1 || got[0].ID != n[0].ID {
		t.Fatal("older provider result regressed notice", got)
	}
	if len(notices(second.Internal.Add(14*24*time.Hour))) != 0 {
		t.Fatal("notice did not expire")
	}
	if _, err := a.SetEmailWatch(id, false); err != nil {
		t.Fatal(err)
	}
	if len(notices(now)) != 1 {
		t.Fatal("manual stop erased already received notice")
	}
	if sends != 1 {
		t.Fatal("notice lifecycle sent mail", sends)
	}
}
