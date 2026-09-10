package chatthreads

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"time"
)

var ErrImportConflict = errors.New("team thread already exists with different imported history")

// ImportSharedThread publishes the entire prepared history in one state write.
// The caller has already obtained the owner's approval and fenced the source.
// All attachment references must already resolve in the destination domain.
// Exact retries recover even if writing activity.log failed after state commit.
func (s *Store) ImportSharedThread(thread Thread, messages []Message, now time.Time) (Thread, error) {
	fingerprint, err := sharedImportFingerprint(thread, messages)
	if err != nil {
		return Thread{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := state{Messages: map[string][]Message{}}
	stored, readErr := os.ReadFile(s.statePath())
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return Thread{}, readErr
	}
	if readErr == nil {
		if err := json.Unmarshal(stored, &st); err != nil {
			return Thread{}, errors.New("existing team history is unreadable; sharing stopped")
		}
		if st.Messages == nil {
			st.Messages = map[string][]Message{}
		}
	}
	if i := threadIndex(st, thread.ID); i >= 0 {
		existing := st.Threads[i]
		if existing.ImportSource == thread.ImportSource && existing.ImportRevision == thread.ImportRevision && existing.ImportFingerprint == fingerprint {
			return existing, nil
		}
		return Thread{}, ErrImportConflict
	}
	thread.ImportFingerprint = fingerprint
	st.Threads = append(st.Threads, thread)
	st.Messages[thread.ID] = append([]Message(nil), messages...)
	err = s.write(st, Entry{TS: now.UTC(), Actor: thread.By, Action: "thread-share", Payload: map[string]any{"thread": thread.ID, "source": thread.ImportSource, "revision": thread.ImportRevision, "messages": len(messages)}})
	return thread, err
}

// ValidateSharedImport checks the exact import representation without writing.
// Publication callers use this before fencing the private source.
func ValidateSharedImport(thread Thread, messages []Message) error {
	_, err := sharedImportFingerprint(thread, messages)
	return err
}
func sharedImportFingerprint(thread Thread, messages []Message) (string, error) {
	if thread.ID == "" || thread.ImportSource == "" || thread.ImportRevision == "" || thread.ImportFingerprint != "" || thread.Created.IsZero() {
		return "", errors.New("invalid shared conversation import")
	}
	seen := map[string]bool{}
	for _, m := range messages {
		if m.ID == "" || m.Thread != thread.ID || m.Author == "" || m.At.IsZero() || seen[m.ID] {
			return "", errors.New("invalid imported message identity")
		}
		seen[m.ID] = true
	}
	bytes, err := json.Marshal(struct {
		Thread   Thread
		Messages []Message
	}{thread, messages})
	if err != nil {
		return "", err
	}
	if len(bytes) > 8*1024*1024 {
		return "", errors.New("conversation exceeds the sharing import limit")
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}
