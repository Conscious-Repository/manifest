package agentchat

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var ErrShared = errors.New("conversation sharing has started; recover or open its team conversation")
var ErrShareChanged = errors.New("conversation changed since sharing review")
var ErrShareBusy = errors.New("wait for all conversation work to finish before sharing")

// ShareState is a durable source-side fence. A prepared share is never silently
// canceled: the target may already have committed even if its response was lost.
type ShareState struct {
	EnvelopeHash string `json:"envelopeHash,omitempty"`
	RequestID    string `json:"requestId"`
	Revision     string `json:"revision"`
	Agent        string `json:"agent"`
	Thread       string `json:"thread"`
	State        string `json:"state"` // prepared | shared
}

func shareJSON(s *ShareState) string {
	if s == nil {
		return ""
	}
	b, _ := json.Marshal(s)
	return string(b)
}

// ShareRevision binds the reviewed history and its recorded context, not a
// current-path file lookup. Callers must separately stage referenced bytes.
func ShareRevision(s Session, body string) string {
	s.Sharing = nil
	b, _ := json.Marshal(struct {
		Session Session
		Body    string
	}{s, body})
	return fingerprint(string(b))
}

func (s *Store) BeginShare(agent, id, request, revision, target, thread string) (Session, string, error) {
	return s.beginShare(agent, id, request, revision, target, thread, "")
}

// BeginReviewedShare persists the exact approved envelope before fencing the
// source. Its content address is part of the fence, so recovery never rebuilds
// an approval from changed plans, attachments or external conversation state.
// The caller must validate the review and fence other included writers before
// publication. This method only fences this source session.
func (s *Store) BeginReviewedShare(agent, id, request, revision, target, thread string, envelope []byte) (Session, string, error) {
	if !ValidAgent(agent) || !ValidID(id) || !ValidRequestID(request) || revision == "" || thread == "" || !shareAudienceMatches(agent, target) {
		return Session{}, "", errors.New("invalid sharing request")
	}
	var payload map[string]json.RawMessage
	if len(envelope) == 0 || len(envelope) > maxShareEnvelope || json.Unmarshal(envelope, &payload) != nil || payload == nil {
		return Session{}, "", errors.New("invalid or oversized sharing envelope")
	}
	hash := fingerprint(string(envelope))
	if err := s.saveShareEnvelope(hash, envelope); err != nil {
		return Session{}, "", err
	}
	return s.beginShare(agent, id, request, revision, target, thread, hash)
}

const maxShareEnvelope = 8 * 1024 * 1024

func shareAudienceMatches(agent, target string) bool {
	return (agent == "kairos-private" && target == "kairos") || (agent == "zeck-private" && target == "zeck")
}

func (s *Store) beginShare(agent, id, request, revision, target, thread, envelopeHash string) (Session, string, error) {
	if !ValidAgent(agent) || !ValidID(id) || !ValidRequestID(request) || revision == "" || thread == "" {
		return Session{}, "", errors.New("invalid sharing request")
	}
	if !shareAudienceMatches(agent, target) {
		return Session{}, "", errors.New("sharing audience does not match this agent")
	}
	m := s.lock(agent, id)
	m.Lock()
	defer m.Unlock()
	sess, body, err := s.read(agent, id)
	if err != nil {
		return Session{}, "", err
	}
	if p := sess.Sharing; p != nil {
		if (p.State == "prepared" || p.State == "shared") && p.RequestID == request && p.Revision == revision && p.Agent == target && p.Thread == thread && p.EnvelopeHash == envelopeHash {
			return sess, body, nil
		}
		return Session{}, "", ErrRequestConflict
	}
	if sess.Status != StatusIdle {
		return Session{}, "", ErrShareBusy
	}
	for _, d := range sess.Deliveries {
		if d.State == DeliveryQueued || d.State == DeliveryRunning || d.State == DeliveryInterrupted {
			return Session{}, "", ErrShareBusy
		}
	}
	if ShareRevision(sess, body) != revision {
		return Session{}, "", ErrShareChanged
	}
	sess.Sharing = &ShareState{RequestID: request, Revision: revision, Agent: target, Thread: thread, State: "prepared", EnvelopeHash: envelopeHash}
	if err = s.write(agent, id, sess, body); err != nil {
		return Session{}, "", err
	}
	return sess, body, nil
}

func validShareHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	for _, c := range hash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *Store) shareEnvelopePath(hash string) string {
	return filepath.Join(s.root, ".shares", hash+".json")
}

func (s *Store) readShareEnvelope(hash string) ([]byte, error) {
	if !validShareHash(hash) {
		return nil, errors.New("sharing envelope reference is invalid")
	}
	f, err := os.Open(s.shareEnvelopePath(hash))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxShareEnvelope+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxShareEnvelope || fingerprint(string(b)) != hash || !json.Valid(b) {
		return nil, errors.New("sharing envelope is unreadable or changed; recovery stopped")
	}
	return b, nil
}

func (s *Store) saveShareEnvelope(hash string, b []byte) error {
	path := s.shareEnvelopePath(hash)
	if _, err := os.Stat(path); err == nil {
		_, err = s.readShareEnvelope(hash)
		return err // Do not silently repair a corrupted recovery record.
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".share-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Publish without replacing an existing content address, including a
	// competing writer which arrived after the initial existence check.
	if err := os.Link(f.Name(), path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		_, err = s.readShareEnvelope(hash)
		return err
	}
	return nil
}

// ReviewedShare recovers only the envelope referenced by this source's durable
// fence. Missing/corrupt payloads fail closed; callers must never regenerate
// them from whatever happens to be visible now.
func (s *Store) ReviewedShare(agent, id string) (Session, []byte, error) {
	if !ValidAgent(agent) || !ValidID(id) {
		return Session{}, nil, errors.New("invalid sharing source")
	}
	m := s.lock(agent, id)
	m.Lock()
	defer m.Unlock()
	sess, _, err := s.read(agent, id)
	if err != nil {
		return Session{}, nil, err
	}
	if sess.Sharing == nil || (sess.Sharing.State != "prepared" && sess.Sharing.State != "shared") {
		return Session{}, nil, errors.New("no recoverable reviewed share")
	}
	b, err := s.readShareEnvelope(sess.Sharing.EnvelopeHash)
	return sess, b, err
}

// CompleteShare is called only after the target's idempotent import is verified.
func (s *Store) CompleteShare(agent, id, request, revision string) (Session, error) {
	if !ValidAgent(agent) || !ValidID(id) {
		return Session{}, errors.New("invalid sharing source")
	}
	m := s.lock(agent, id)
	m.Lock()
	defer m.Unlock()
	sess, body, err := s.read(agent, id)
	if err != nil {
		return Session{}, err
	}
	if sess.Sharing == nil || (sess.Sharing.State != "prepared" && sess.Sharing.State != "shared") || sess.Sharing.RequestID != request || sess.Sharing.Revision != revision {
		return Session{}, ErrRequestConflict
	}
	if sess.Sharing.State == "shared" {
		return sess, nil
	}
	sess.Sharing.State = "shared"
	return sess, s.write(agent, id, sess, body)
}
