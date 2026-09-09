package agentchat

// Delivery receipts and waiting messages live in the same atomic record as
// the transcript. No second scheduler, volatile queue, or dual transcript writer.
import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	DeliveryQueued      = "queued"
	DeliveryRunning     = "running"
	DeliveryCompleted   = "completed"
	DeliveryFailed      = "failed"
	DeliveryInterrupted = "interrupted"
)

var ErrRequestConflict = errors.New("request ID was already used for different content")
var requestRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

func ValidRequestID(id string) bool { return requestRE.MatchString(id) }
func NewRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

type Delivery struct {
	ID          string          `json:"id"`
	Text        string          `json:"text,omitempty"`
	Fingerprint string          `json:"fingerprint"`
	State       string          `json:"state"`
	Accepted    string          `json:"accepted"`
	Updated     string          `json:"updated"`
	UserTurn    int             `json:"userTurn,omitempty"`
	ReplyTurn   int             `json:"replyTurn,omitempty"`
	Error       string          `json:"error,omitempty"`
	Context     *MessageContext `json:"context,omitempty"`
}

type ArtifactReference struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

type MessageContext struct {
	Conversation string              `json:"conversation"`
	Task         string              `json:"task,omitempty"`
	Agent        string              `json:"agent"`
	Artifacts    []ArtifactReference `json:"artifacts,omitempty"`
}

func deliveryFingerprint(text string, context *MessageContext) string {
	if context == nil {
		return fingerprint(text)
	}
	b, _ := json.Marshal(struct {
		Text    string
		Context *MessageContext
	}{text, context})
	return fingerprint(string(b))
}

type Acceptance struct {
	Delivery Delivery `json:"delivery"`
	New      bool     `json:"new"`
}

func fingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
func deliveryJSON(ds []Delivery) string {
	if len(ds) == 0 {
		return ""
	}
	b, _ := json.Marshal(ds)
	return string(b)
}

// Accept persists the instruction BEFORE acknowledging it. Matching retries
// recover the existing receipt regardless of its current lifecycle state.
func (s *Store) Accept(agent, id, requestID, text string, contexts ...*MessageContext) (Acceptance, error) {
	var ctx *MessageContext
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	if requestID == "" {
		requestID = NewRequestID()
	}
	if !ValidRequestID(requestID) {
		return Acceptance{}, errors.New("invalid request ID")
	}
	if strings.TrimSpace(text) == "" {
		return Acceptance{}, errors.New("empty message")
	}
	var out Acceptance
	_, err := s.update(agent, id, func(sess *Session, _ *string) error {
		for _, d := range sess.Deliveries {
			if d.ID == requestID {
				if d.Fingerprint != deliveryFingerprint(text, ctx) {
					return ErrRequestConflict
				}
				out.Delivery = d
				return nil
			}
		}
		d := Delivery{ID: requestID, Text: text, Fingerprint: deliveryFingerprint(text, ctx), Context: ctx, State: DeliveryQueued, Accepted: now(), Updated: now()}
		sess.Deliveries = append(sess.Deliveries, d)
		sess.Status = StatusThinking
		out = Acceptance{Delivery: d, New: true}
		return nil
	})
	return out, err
}

// Claim appends the user turn and records running in ONE atomic write before
// any external invocation. Concurrent callers can claim at most one delivery.
func (s *Store) Claim(agent, id string) (Delivery, bool, error) {
	var out Delivery
	claimed := false
	_, err := s.update(agent, id, func(sess *Session, body *string) error {
		for _, d := range sess.Deliveries {
			if d.State == DeliveryRunning {
				return nil
			}
		}
		for i := range sess.Deliveries {
			d := &sess.Deliveries[i]
			if d.State != DeliveryQueued {
				continue
			}
			appendTurn(sess, body, "user", d.Text, 0)
			d.UserTurn = sess.Turns
			d.Text = "" // transcript is now the only copy
			d.State = DeliveryRunning
			d.Updated = now()
			sess.Status = StatusThinking
			out = *d
			claimed = true
			return nil
		}
		sess.Status = StatusIdle
		return nil
	})
	return out, claimed, err
}

// Finish atomically saves the reply and receipt. A retry cannot append another
// reply. If the process dies before Finish, recovery reports uncertainty rather
// than invoking the provider a second time.
func (s *Store) Finish(agent, id, requestID, who, text, state, detail string, usd float64, nativeSession ...string) error {
	if state != DeliveryCompleted && state != DeliveryFailed {
		return errors.New("invalid completion state")
	}
	_, err := s.update(agent, id, func(sess *Session, body *string) error {
		for i := range sess.Deliveries {
			d := &sess.Deliveries[i]
			if d.ID != requestID {
				continue
			}
			if d.State == state {
				return nil
			}
			if d.State != DeliveryRunning {
				return errors.New("delivery is not running")
			}
			appendTurn(sess, body, who, text, usd)
			if len(nativeSession) > 0 && strings.TrimSpace(nativeSession[0]) != "" {
				sess.HermesSession = strings.TrimSpace(nativeSession[0])
			}
			d.State = state
			d.ReplyTurn = sess.Turns
			d.Error = detail
			d.Updated = now()
			sess.Status = StatusIdle
			for _, q := range sess.Deliveries {
				if q.State == DeliveryQueued {
					sess.Status = StatusThinking
				}
			}
			return nil
		}
		return errors.New("delivery not found")
	})
	return err
}
func appendTurn(sess *Session, body *string, who, text string, usd float64) {
	sess.Turns++
	head := fmt.Sprintf("## Turn %d — %s · %s", sess.Turns, who, now())
	if usd > 0 {
		head += fmt.Sprintf(" · $%.4f", usd)
		sess.SpentUSD += usd
	}
	if *body != "" {
		*body += "\n\n"
	}
	*body += head + "\n\n" + strings.TrimSpace(text)
}
func (s *Store) Receipt(agent, id, requestID string) (Delivery, bool) {
	if !ValidAgent(agent) || !ValidID(id) || !ValidRequestID(requestID) {
		return Delivery{}, false
	}
	sess, _, err := s.read(agent, id)
	if err != nil {
		return Delivery{}, false
	}
	for _, d := range sess.Deliveries {
		if d.ID == requestID {
			return d, true
		}
	}
	return Delivery{}, false
}

// CreateOnce deduplicates lost responses to New chat, not just later sends.
// The original signature is retained even if the conversation is later renamed.
func (s *Store) CreateOnce(agent, profile, title, model, requestID string) (string, error) {
	if requestID == "" {
		return s.create(agent, profile, title, model, "", "")
	}
	if !ValidRequestID(requestID) {
		return "", errors.New("invalid request ID")
	}
	s.createMu.Lock()
	defer s.createMu.Unlock()
	signature := fingerprint(profile + "\x00" + title + "\x00" + model)
	for _, sess := range s.List(agent) {
		if sess.CreateRequest == requestID {
			if sess.CreateSignature != signature {
				return "", ErrRequestConflict
			}
			return sess.ID, nil
		}
	}
	return s.create(agent, profile, title, model, requestID, signature)
}
