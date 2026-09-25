package server

// Durable recovery helpers for the herdr adapter. Nothing here writes a
// record or sends anything: the input receipt stays the one durable artifact
// of a submission, and these helpers only read it against the provider's own
// transcript and this process's live dispatch table.

import "time"

// markInflight records that this process is dispatching request on terminal
// id right now. The caller holds the terminal's input mutex; the entry is
// cleared by the returned func when the handler returns, whatever the outcome.
func (c *termCfg) markInflight(id, request string) func() {
	if request == "" {
		return func() {}
	}
	key := id + "/" + request
	c.inflightMu.Lock()
	if c.inflight == nil {
		c.inflight = map[string]time.Time{}
	}
	c.inflight[key] = time.Now().UTC()
	c.inflightMu.Unlock()
	return func() {
		c.inflightMu.Lock()
		delete(c.inflight, key)
		c.inflightMu.Unlock()
	}
}

// inflightSince reports whether this process is inside handleTermInput for
// the request, and since when. A restart returns false for everything: a
// send that outlived its process is uncertain, never "still in progress".
func (c *termCfg) inflightSince(id, request string) (time.Time, bool) {
	if c == nil || request == "" {
		return time.Time{}, false
	}
	c.inflightMu.Lock()
	defer c.inflightMu.Unlock()
	at, ok := c.inflight[id+"/"+request]
	return at, ok
}

// receiptConfirmedByTranscript finds the provider's own record of an input
// whose receipt never left `unconfirmed` (the process died between writing
// the receipt and the daemon's reply). The match is the exact submitted bytes
// (SubmittedHash), the same rule projectContinuationTurns uses to attribute
// a user turn to its receipt; nothing is inferred from timing or similarity.
// The receipt file is not rewritten: the transcript is the proof, and it is
// re-read on every projection.
func receiptConfirmedByTranscript(r terminalInputReceipt, tr termTranscript) (termTurn, bool) {
	if r.State != "unconfirmed" || len(r.SubmittedHash) != 64 {
		return termTurn{}, false
	}
	for _, t := range tr.Turns {
		if t.Who != "user" || t.Text == "" {
			continue
		}
		if hashTerminalText(t.Text) == r.SubmittedHash || hashTerminalText(t.Text+"\n") == r.SubmittedHash {
			return t, true
		}
	}
	return termTurn{}, false
}
