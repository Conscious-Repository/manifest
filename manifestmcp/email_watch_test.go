package manifestmcp

import (
	"context"
	"errors"
	"manifest/gmailsend"
	"manifest/gmailsync"
	"strings"
	"testing"
	"time"
)

func TestEmailWatchReadsOnlyConfirmedExactMailboxAndNeverResends(t *testing.T) {
	a, _, _ := fixture(t)
	q := EmailInput{Domain: "ooda", To: []string{"contractor@example.com"}, Subject: "Bid", Body: "Please quote", IdempotencyKey: "watch-fixture", MonitorReplies: true}
	p, err := a.PrepareEmail(q)
	if err != nil {
		t.Fatal(err)
	}
	id := p["operationId"].(string)
	calls := 0
	a.mailSend = func(context.Context, gmailsend.Message) (gmailsend.Ref, error) {
		calls++
		return gmailsend.Ref{ID: "sent", ThreadID: "thread"}, nil
	}
	readCalls := 0
	now := time.Now().UTC()
	read := func(_ context.Context, sender, thread string) ([]gmailsync.Msg, error) {
		readCalls++
		if sender != "ben@ooda.group" || thread != "thread" {
			t.Fatalf("wrong mailbox/thread: %s %s", sender, thread)
		}
		return []gmailsync.Msg{{ID: "old", From: "contractor@example.com", Body: "OLDER PRIVATE MESSAGE", Internal: now.Add(-time.Hour)}, {ID: "sent", From: sender, Internal: now}, {ID: "reply", From: "Contractor <contractor@example.com>", Body: "Here is the bid", Internal: now.Add(time.Minute)}, {ID: "own", From: sender, Body: "My own reply", Internal: now.Add(2 * time.Minute)}, {ID: "alias", From: "alias@example.com", Sent: true, Body: "Alias reply", Internal: now.Add(3 * time.Minute)}}, nil
	}
	if err = a.PollEmailReplies(context.Background(), now, read); err != nil || readCalls != 0 {
		t.Fatal(err, readCalls)
	}
	approve(t, a, id)
	execute(t, a, id, "succeeded")
	if err = a.PollEmailReplies(context.Background(), now, read); err != nil {
		t.Fatal(err)
	}
	o, _ := a.loadOperation(id)
	if o.EmailWatch == nil || o.EmailWatch.Total != 1 || o.EmailWatch.Replies[0].Body != "Here is the bid" {
		t.Fatal(o.EmailWatch)
	}
	if err = a.PollEmailReplies(context.Background(), now.Add(time.Minute), read); err != nil || readCalls != 1 {
		t.Fatal(err, readCalls)
	}
	restarted, err := New(a.Vault, a.Data, a.System)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.PollEmailReplies(context.Background(), now.Add(6*time.Minute), read); err != nil {
		t.Fatal(err)
	}
	o, _ = restarted.loadOperation(id)
	if len(o.EmailWatch.Replies) != 1 || calls != 1 {
		t.Fatal("duplicated reply or sent email", calls)
	}
	if _, err = restarted.SetEmailWatch(id, false); err != nil {
		t.Fatal(err)
	}
	if err = restarted.PollEmailReplies(context.Background(), now.Add(time.Hour), read); err != nil || readCalls != 2 {
		t.Fatal(err, readCalls)
	}
	if _, err = restarted.SetEmailWatch(id, true); err != nil {
		t.Fatal(err)
	}
	err = restarted.PollEmailReplies(context.Background(), now.Add(2*time.Hour), func(context.Context, string, string) ([]gmailsync.Msg, error) {
		return nil, errors.New("secret provider error")
	})
	o, _ = restarted.loadOperation(id)
	if err != nil || o.EmailWatch.Error == "" || strings.Contains(o.EmailWatch.Error, "secret") || len(o.EmailWatch.Replies) != 1 {
		t.Fatal(err, o.EmailWatch)
	}
}
func TestEmailWatchRejectsMissingSentAnchor(t *testing.T) {
	if _, _, err := emailReplies([]gmailsync.Msg{{ID: "other", From: "x@example.com", Internal: time.Now()}}, "missing", "ben@aion.bio"); err == nil {
		t.Fatal("accepted unanchored thread")
	}
}

// A schedule timestamp is not a read identity: two polls may claim the same
// tick after stop/restart, including across independent adapter instances.
func TestEmailWatchRestartRejectsOlderInFlightRead(t *testing.T) {
	for _, staleError := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale-replies", true: "stale-error"}[staleError], func(t *testing.T) {
			a, _, _ := fixture(t)
			p, err := a.PrepareEmail(EmailInput{Domain: "aion", To: []string{"candidate@example.test"}, Subject: "Question", Body: "Reply please", IdempotencyKey: "watch-restart-race", MonitorReplies: true})
			if err != nil {
				t.Fatal(err)
			}
			id := p["operationId"].(string)
			a.mailSend = func(context.Context, gmailsend.Message) (gmailsend.Ref, error) {
				return gmailsend.Ref{ID: "sent", ThreadID: "thread"}, nil
			}
			approve(t, a, id)
			execute(t, a, id, "succeeded")
			restarted, err := New(a.Vault, a.Data, a.System)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			started := make(chan struct{})
			release := make(chan struct{})
			done := make(chan error, 1)
			messages := func(body string) []gmailsync.Msg {
				return []gmailsync.Msg{{ID: "sent", From: "ben@aion.bio", Internal: now.Add(-time.Hour)}, {ID: "reply", From: "candidate@example.test", Body: body, Internal: now.Add(-time.Minute)}}
			}
			go func() {
				done <- a.PollEmailReplies(context.Background(), now, func(context.Context, string, string) ([]gmailsync.Msg, error) {
					close(started)
					<-release
					if staleError {
						return nil, errors.New("old provider failure")
					}
					return messages("STALE READ"), nil
				})
			}()
			<-started
			// Always release the blocked provider fixture, including on an assertion error.
			released := false
			defer func() {
				if !released {
					close(release)
					<-done
				}
			}()
			if _, err = restarted.SetEmailWatch(id, false); err != nil {
				t.Fatal(err)
			}
			if _, err = restarted.SetEmailWatch(id, true); err != nil {
				t.Fatal(err)
			}
			if err = restarted.PollEmailReplies(context.Background(), now, func(context.Context, string, string) ([]gmailsync.Msg, error) { return messages("CURRENT READ"), nil }); err != nil {
				t.Fatal(err)
			}
			current, err := restarted.loadOperation(id)
			if err != nil || current.EmailWatch.Claim == "" {
				t.Fatal(current, err)
			}
			claim := current.EmailWatch.Claim
			close(release)
			released = true
			if err = <-done; err != nil {
				t.Fatal(err)
			}
			final, err := restarted.loadOperation(id)
			if err != nil || final.EmailWatch.Claim != claim || final.EmailWatch.Error != "" || len(final.EmailWatch.Replies) != 1 || final.EmailWatch.Replies[0].Body != "CURRENT READ" {
				t.Fatal("old read overwrote restarted watch", final, err)
			}
			if _, err = restarted.SetEmailWatch(id, false); err != nil {
				t.Fatal(err)
			}
			disabled, _ := restarted.loadOperation(id)
			if disabled.EmailWatch.Claim != "" || disabled.EmailWatch.Enabled {
				t.Fatal("stop retained read authority", disabled.EmailWatch)
			}

		})
	}
}
