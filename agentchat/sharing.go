package agentchat

import (
	"encoding/json"
	"errors"
)

var ErrShared = errors.New("conversation sharing has started; recover or open its team conversation")
var ErrShareChanged = errors.New("conversation changed since sharing review")
var ErrShareBusy = errors.New("wait for all conversation work to finish before sharing")

// ShareState is a durable source-side fence. A prepared share is never silently
// canceled: the target may already have committed even if its response was lost.
type ShareState struct {
	RequestID string `json:"requestId"`
	Revision  string `json:"revision"`
	Agent     string `json:"agent"`
	Thread    string `json:"thread"`
	State     string `json:"state"` // prepared | shared
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
	if !ValidAgent(agent) || !ValidID(id) || !ValidRequestID(request) || revision == "" || thread == "" {
		return Session{}, "", errors.New("invalid sharing request")
	}
	if !((agent == "kairos-private" && target == "kairos") || (agent == "zeck-private" && target == "zeck")) {
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
		if (p.State == "prepared" || p.State == "shared") && p.RequestID == request && p.Revision == revision && p.Agent == target && p.Thread == thread {
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
	sess.Sharing = &ShareState{RequestID: request, Revision: revision, Agent: target, Thread: thread, State: "prepared"}
	if err = s.write(agent, id, sess, body); err != nil {
		return Session{}, "", err
	}
	return sess, body, nil
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
