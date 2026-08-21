package gmailsync

// The sync loop: every tick, for every connected member, list threads with
// activity since the account's watermark (1h overlap for clock skew, the
// engine's rule), fetch the qualifying ones, and upsert pending candidates.
// A thread qualifies only if a participant OTHER THAN the member resolves in
// the OODA roster — the portal twin of the engine's known-contacts filter:
// personal mail never surfaces, because nobody in it is on the roster.

import (
	"context"
	"log"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// Fetcher is the mailbox surface the loop needs — *Client satisfies it; tests
// substitute a fake.
type Fetcher interface {
	ThreadIDsSince(ctx context.Context, after time.Time, max int) ([]string, error)
	ThreadFull(ctx context.Context, id string) (string, []Msg, error)
}

// Loop owns one portal's mailbox sync.
type Loop struct {
	Tokens     *Tokens
	Candidates *Candidates
	// OAuthConfig supplies the client config tokens refresh against (the
	// portal's web client). Nil disables sync (tokens can't refresh).
	OAuthConfig func() (*oauth2.Config, error)
	// Roster resolves an address to a display name — the relevance filter AND
	// the sender naming. Rebuilt per pass so people.md edits are live.
	Roster func() Resolver
	// OnConfirmRetry re-attempts the extractor spool for a confirmed candidate
	// whose first spool was refused (engine busy). Set by the server.
	OnConfirmRetry func(cand Candidate) bool
	// NewFetcher builds the mailbox client for one member; tests override.
	NewFetcher func(src oauth2.TokenSource) Fetcher

	// FirstRunLookback bounds the initial backfill for a newly connected
	// mailbox (the engine defaults to 30 days).
	FirstRunLookback time.Duration
}

const maxThreadPage = 100

// Start runs the loop until ctx ends. A tick that fails for one account
// degrades to a log line and moves on — one revoked grant must not stall the
// other mailboxes.
func (l *Loop) Start(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 10 * time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.Pass(ctx)
		}
	}
}

// Pass runs one sweep over every connected account.
func (l *Loop) Pass(ctx context.Context) {
	if l.Tokens == nil || l.Candidates == nil || l.OAuthConfig == nil || l.Roster == nil {
		return
	}
	cfg, err := l.OAuthConfig()
	if err != nil {
		log.Printf("gmailsync: oauth client unavailable: %v", err)
		return
	}
	res := l.Roster()
	for _, acc := range l.Tokens.List() {
		if acc.NeedsReauth {
			continue
		}
		if err := l.syncAccount(ctx, cfg, acc.Email, res); err != nil {
			log.Printf("gmailsync: %s: %v", acc.Email, err)
		}
	}
	// retry extractor spools the engine refused earlier
	if l.OnConfirmRetry != nil {
		for _, cand := range l.Candidates.SpoolRetries() {
			if l.OnConfirmRetry(cand) {
				l.Candidates.ClearSpoolPending(cand.ID)
			}
		}
	}
}

func (l *Loop) syncAccount(ctx context.Context, cfg *oauth2.Config, email string, res Resolver) error {
	src, ok := l.Tokens.Source(email, cfg)
	if !ok {
		return nil
	}
	newFetcher := l.NewFetcher
	if newFetcher == nil {
		newFetcher = func(s oauth2.TokenSource) Fetcher { return NewClient(s) }
	}
	fc := newFetcher(src)

	watermark := l.Candidates.Watermark(email)
	if watermark.IsZero() {
		lookback := l.FirstRunLookback
		if lookback <= 0 {
			lookback = 30 * 24 * time.Hour
		}
		watermark = time.Now().Add(-lookback)
	}
	ids, err := fc.ThreadIDsSince(ctx, watermark.Add(-time.Hour), maxThreadPage)
	if err != nil {
		return err
	}
	var maxSeen time.Time
	for _, id := range ids {
		prior, known := l.Candidates.ThreadState(email, id)
		if known && prior.Status == StatusDismissed {
			continue // muted forever
		}
		subject, msgs, err := fc.ThreadFull(ctx, id)
		if err != nil || len(msgs) == 0 {
			continue // one bad thread never aborts the account
		}
		if last := msgs[len(msgs)-1].Internal; last.After(maxSeen) {
			maxSeen = last
		}
		if IsCalendarThread(msgs) {
			continue
		}
		if !l.qualifies(msgs, email, res) {
			continue
		}
		switch {
		case !known:
			l.upsertFrom(email, id, subject, msgs, res, 1)
		case prior.Status == StatusPending:
			// re-render in place — same id, note grows until decided
			l.upsertFrom(email, id, subject, msgs, res, prior.Seq)
		case prior.Status == StatusConfirmed:
			// growth beyond the confirmed watermark → NEW candidate carrying
			// only the fresh messages (the append lane)
			fresh := messagesAfter(msgs, prior.LastMsgID, prior.LastMsgAt)
			if len(fresh) > 0 {
				l.upsertFrom(email, id, subject, fresh, res, prior.Seq+1)
			}
		}
	}
	l.Candidates.AdvanceWatermark(email, maxSeen)
	return nil
}

// qualifies is the relevance filter: at least one participant address, other
// than the mailbox owner, resolves in the roster.
func (l *Loop) qualifies(msgs []Msg, ownEmail string, res Resolver) bool {
	if res == nil {
		return false
	}
	for _, m := range msgs {
		for _, header := range []string{m.From, m.To, m.Cc} {
			for _, addr := range headerAddresses(header) {
				lower := strings.ToLower(addr)
				if strings.EqualFold(lower, ownEmail) {
					continue
				}
				if _, ok := res.PersonByEmail(lower); ok {
					return true
				}
			}
		}
	}
	return false
}

func (l *Loop) upsertFrom(email, threadID, subject string, msgs []Msg, res Resolver, seq int) {
	cand := Candidate{
		ID:         CandidateID(email, threadID, seq),
		Account:    strings.ToLower(email),
		ThreadID:   threadID,
		Subject:    subject,
		FirstMsgAt: msgs[0].Internal,
		LastMsgAt:  msgs[len(msgs)-1].Internal,
		LastMsgID:  msgs[len(msgs)-1].ID,
		Note:       ThreadNote(msgs, res, email),
		Filename:   NoteFilename(msgs, subject),
		Seq:        seq,
	}
	if line := ParticipantsLine(msgs, res, email); line != "" {
		cand.Participants = strings.Split(line, " · ")
	}
	if err := l.Candidates.Upsert(cand); err != nil {
		log.Printf("gmailsync: upsert %s: %v", cand.ID, err)
	}
}

// messagesAfter returns the messages strictly newer than the confirmed
// watermark (by id first — exact — falling back to time for safety).
func messagesAfter(msgs []Msg, lastID string, lastAt time.Time) []Msg {
	if lastID != "" {
		for i, m := range msgs {
			if m.ID == lastID {
				return msgs[i+1:]
			}
		}
	}
	var out []Msg
	for _, m := range msgs {
		if m.Internal.After(lastAt) {
			out = append(out, m)
		}
	}
	return out
}
