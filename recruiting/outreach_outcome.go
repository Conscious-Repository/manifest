package recruiting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"
)

func OutreachRevision(entry OutreachEntry) string {
	raw, _ := json.Marshal(entry)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

type OutreachOutcome struct {
	OperationID, Revision, Sender, Subject, Body, MessageID, ThreadID string
	To                                                                []string
	ConfirmedAt                                                       time.Time
}

// RecordOutreachOutcome materializes an already confirmed receipt. There is no
// sender dependency. Retry repairs a failed candidate write after the log append.
func (s *Store) RecordOutreachOutcome(id string, outcome OutreachOutcome) (OutreachEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty OutreachEntry
	if !strings.HasPrefix(outcome.OperationID, "sha256:") || len(outcome.OperationID) != 71 || outcome.MessageID == "" || outcome.ConfirmedAt.IsZero() {
		return empty, fmt.Errorf("confirmed outreach outcome required")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(outcome.OperationID, "sha256:")); err != nil {
		return empty, fmt.Errorf("invalid operation identity")
	}
	slug, _, err := s.resolve(id)
	if err != nil {
		return empty, err
	}
	candidateRaw, err := os.ReadFile(s.Path("candidates/" + slug + ".md"))
	if err != nil {
		return empty, err
	}
	doc := ParseCandidate(string(candidateRaw))
	if doc.Get("id") != id {
		return empty, fmt.Errorf("candidate identity changed")
	}
	raw, err := os.ReadFile(s.Path(OutreachLogName(slug)))
	if err != nil {
		return empty, err
	}
	log := ParseOutreach(string(raw))
	if log.Get("candidate") != id {
		return empty, fmt.Errorf("outreach log identity changed")
	}
	var source, recorded OutreachEntry
	for _, entry := range log.Entries() {
		if entry.OperationID == outcome.OperationID {
			if recorded.Seq != 0 {
				return empty, fmt.Errorf("duplicate operation rows")
			}
			recorded = entry
		}
		if (entry.Status == OutreachStatusDraft || entry.Status == OutreachStatusReady) && OutreachRevision(entry) == outcome.Revision {
			source = entry
		}
	}
	if source.Seq == 0 || source.Sender != outcome.Sender || source.Subject != outcome.Subject || source.Body != outcome.Body || !reflect.DeepEqual(source.To, outcome.To) {
		return empty, fmt.Errorf("reviewed source draft no longer matches confirmed email")
	}
	if recorded.Seq != 0 {
		if recorded.Status != OutreachStatusSent || recorded.DraftSeq != source.Seq || recorded.MessageID != outcome.MessageID || recorded.ThreadID != outcome.ThreadID || recorded.Body != source.Body || recorded.Subject != source.Subject || recorded.Sender != source.Sender || recorded.SentAt != outcome.ConfirmedAt.UTC().Format(time.RFC3339Nano) || !reflect.DeepEqual(recorded.To, source.To) {
			return empty, fmt.Errorf("recorded outcome conflicts with canonical receipt")
		}
	} else {
		entry := source
		entry.OperationID = outcome.OperationID
		entry.DraftSeq = source.Seq
		entry.Status = OutreachStatusSent
		entry.At = outcome.ConfirmedAt.UTC().Format("2006-01-02")
		entry.SentAt = outcome.ConfirmedAt.UTC().Format(time.RFC3339Nano)
		entry.Actor = "owner:local"
		entry.MessageID = outcome.MessageID
		entry.ThreadID = outcome.ThreadID
		recorded, err = log.Append(entry)
		if err != nil {
			return empty, err
		}
		if err = s.saveOutreach(slug, log); err != nil {
			return empty, err
		}
	}
	for _, pointer := range doc.Outreach() {
		for _, applied := range pointer.Operations {
			if applied == outcome.OperationID {
				return recorded, nil
			}
		}
	}
	// Choose the latest confirmed send, even when receipts are recorded out of order.
	var latest OutreachEntry
	for _, entry := range log.Entries() {
		if entry.Status == OutreachStatusSent && (latest.Seq == 0 || outreachOutcomeTime(entry).After(outreachOutcomeTime(latest))) {
			latest = entry
		}
	}
	pointer := OutreachRef{Operations: []string{outcome.OperationID}, Log: OutreachLogName(slug), Last: latest.At, Status: OutreachStatusSent, MessageID: latest.MessageID, ThreadID: latest.ThreadID}
	if draft, ok := log.CurrentDraft(); ok {
		pointer.Last = draft.At
		pointer.Status = OutreachStatusDraft
	}
	for _, old := range doc.Outreach() {
		if old.Log == pointer.Log && old.MessageID == pointer.MessageID && old.Status == OutreachStatusReplied && pointer.Status != OutreachStatusDraft {
			pointer.Status = OutreachStatusReplied
		}
	}
	doc.SetOutreach(pointer)
	if stageBefore(doc.Get("stage"), StageOutreach) {
		doc.Set("stage", StageOutreach)
	}
	if next := SerializeCandidate(doc); next != string(candidateRaw) {
		if err = s.SaveCandidate(slug, doc); err != nil {
			return recorded, err
		}
	}
	return recorded, nil
}

func outreachOutcomeTime(entry OutreachEntry) time.Time {
	if at, err := time.Parse(time.RFC3339Nano, entry.SentAt); err == nil {
		return at
	}
	at, _ := time.Parse("2006-01-02", entry.At)
	return at
}
