package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"manifest/agentchat"
	"manifest/threads"
)

// Idempotency for POST /api/tasks/thread. A comment there can spend an agent
// turn, so a lost acknowledgment followed by an explicit retry must not post
// twice or dispatch twice. The rule is the one the chat delivery journal and
// terminal input receipts use: a client request ID plus a SHA-256 fingerprint
// of the payload; the same ID with the same payload recovers the recorded
// outcome, the same ID with a different payload is a conflict.
//
// The receipt is two private markers in the task's own thread store (hidden
// from every thread view, never team-visible, no text — only the fingerprint):
//
//	request-open    written before anything is posted
//	request-closed  the comment ID it produced, or the refusal that left nothing
//
// An open with no close and no live claim in this process is reconciled
// against the thread itself: a matching owner comment after the open proves
// the post landed (its dispatch is then reported uncertain and is NOT re-run);
// no such comment proves nothing was posted, so the request may proceed.
const (
	actRequestOpen   = "request-open"
	actRequestClosed = "request-closed"
)

type taskThreadPost struct {
	ID, Text  string
	Context   []artifactContextRef
	Mentions  []string
	Files     []threads.FileRef
	Mode      string
	Agent     string
	RequestID string `json:"requestId"`
}

func (b taskThreadPost) fingerprint(id string) string {
	b.ID = id
	b.RequestID = ""
	b.Text = strings.TrimSpace(b.Text)
	b.Mode = strings.ToLower(strings.TrimSpace(b.Mode))
	b.Agent = strings.TrimSpace(b.Agent)
	raw, _ := json.Marshal(b)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// taskThreadReplay is a recovered outcome: the recorded comment and whether
// its dispatch is known (recorded) or uncertain (the close was lost).
type taskThreadReplay struct {
	Comment  threads.Comment
	Dispatch string
}

type taskThreadClaim struct {
	s                          *Server
	task, request, fingerprint string
}

func (s *Server) claimTaskThreadRequest(taskID string, b taskThreadPost) (*taskThreadClaim, *taskThreadReplay, error) {
	if !agentchat.ValidRequestID(b.RequestID) {
		return nil, nil, errBadRequest("invalid request ID")
	}
	if s.threads == nil || s.threads.private == nil {
		return nil, nil, &taskThreadStatusErr{code: http.StatusServiceUnavailable, msg: "request receipts need the private thread store; nothing posted"}
	}
	fp := b.fingerprint(taskID)
	key := taskID + "#" + b.RequestID
	s.taskThreadReqMu.Lock()
	defer s.taskThreadReqMu.Unlock()
	var open, closed *threads.Comment
	for _, c := range s.threads.private.Thread(taskID) {
		if metaString(c.Meta["requestId"]) != b.RequestID {
			continue
		}
		c := c
		switch c.Action {
		case actRequestOpen:
			open, closed = &c, nil
		case actRequestClosed:
			closed = &c
		}
	}
	if open != nil && metaString(open.Meta["fingerprint"]) != fp {
		return nil, nil, &taskThreadStatusErr{code: http.StatusConflict, msg: "request ID already used for a different comment; nothing posted"}
	}
	claim := &taskThreadClaim{s: s, task: taskID, request: b.RequestID, fingerprint: fp}
	switch {
	case closed != nil && metaString(closed.Meta["comment"]) != "":
		c, ok := s.threadComment(taskID, metaString(closed.Meta["comment"]))
		if !ok {
			return nil, nil, &taskThreadStatusErr{code: http.StatusConflict, msg: "request already recorded as comment " + metaString(closed.Meta["comment"]) + ", which is no longer on the thread; nothing re-posted"}
		}
		dispatch := "recorded"
		if closed.Meta["reconciled"] == true {
			dispatch = "uncertain"
		}
		return nil, &taskThreadReplay{Comment: c, Dispatch: dispatch}, nil
	case open != nil && closed == nil:
		if s.taskThreadInflight[key] {
			return nil, nil, &taskThreadStatusErr{code: http.StatusConflict, msg: "this request is still being recorded; retry to recover its outcome"}
		}
		if c, ok := s.ownerCommentSince(taskID, strings.TrimSpace(b.Text), open.At); ok {
			s.markerAddMeta(taskID, actRequestClosed, "", map[string]any{"requestId": b.RequestID, "fingerprint": fp, "comment": c.ID, "reconciled": true})
			return nil, &taskThreadReplay{Comment: c, Dispatch: "uncertain"}, nil
		}
		// No comment after the open: nothing was posted, so nothing can repeat.
	}
	if s.taskThreadInflight == nil {
		s.taskThreadInflight = map[string]bool{}
	}
	s.taskThreadInflight[key] = true
	s.markerAddMeta(taskID, actRequestOpen, "", map[string]any{"requestId": b.RequestID, "fingerprint": fp})
	return claim, nil, nil
}

func (c *taskThreadClaim) finish(meta map[string]any) {
	if c == nil {
		return
	}
	c.s.taskThreadReqMu.Lock()
	defer c.s.taskThreadReqMu.Unlock()
	meta["requestId"], meta["fingerprint"] = c.request, c.fingerprint
	c.s.markerAddMeta(c.task, actRequestClosed, "", meta)
	delete(c.s.taskThreadInflight, c.task+"#"+c.request)
}

func (c *taskThreadClaim) recorded(comment threads.Comment) {
	c.finish(map[string]any{"comment": comment.ID})
}

// refused closes a claim that left nothing on the thread; the same request
// may be retried (it proceeds as new, since no effect exists to repeat).
func (c *taskThreadClaim) refused(err error) {
	c.finish(map[string]any{"refused": err.Error()})
}

func (s *Server) threadComment(taskID, commentID string) (threads.Comment, bool) {
	for _, c := range s.listThread(taskID) {
		if c.ID == commentID {
			return c, true
		}
	}
	return threads.Comment{}, false
}

func (s *Server) ownerCommentSince(taskID, text string, since time.Time) (threads.Comment, bool) {
	owner := s.ownerIdentity()
	authors := map[string]bool{owner.ID: true, owner.Name: true}
	if s.threads != nil && s.threads.admin.Email != "" {
		authors[s.threads.admin.Email] = true // aion comments post as the portal owner
	}
	for _, c := range s.listThread(taskID) {
		if c.Action == threads.ActComment && !c.At.Before(since) && strings.TrimSpace(c.Text) == text && authors[c.Author] {
			return c, true
		}
	}
	return threads.Comment{}, false
}

type taskThreadStatusErr struct {
	code int
	msg  string
}

func (e *taskThreadStatusErr) Error() string { return e.msg }

func writeTaskThreadError(w http.ResponseWriter, err error) {
	var st *taskThreadStatusErr
	if errors.As(err, &st) {
		http.Error(w, st.msg, st.code)
		return
	}
	httpError(w, err)
}
