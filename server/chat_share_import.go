package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"manifest/agentchat"
	"manifest/artifacts"
	"manifest/chatthreads"
	"strings"
	"time"
)

// Convert the exact review, not mutable source files. The caller must stage
// all Files in the approved audience before atomically importing the result.
// Operation/proposal IDs remain in the reviewed envelope and per-turn source
// records; this conversion never creates duplicate decisions.
func buildChatShareImport(review chatShareReview, threadID string) (chatthreads.Thread, []chatthreads.Message, error) {
	fail := func() (chatthreads.Thread, []chatthreads.Message, error) {
		return chatthreads.Thread{}, nil, errors.New("sharing review is incomplete or changed")
	}
	stamp, err := time.Parse(time.RFC3339Nano, review.Session.Created)
	if err != nil || stamp.IsZero() || threadID == "" || review.OwnerEmail == "" || !review.FutureMessages || len(review.Blockers) != 0 || review.Session.Sharing != nil {
		return fail()
	}
	if !((review.Session.Agent == "kairos-private" && review.TargetAgent == "kairos") || (review.Session.Agent == "zeck-private" && review.TargetAgent == "zeck")) {
		return fail()
	}
	revision := review.Revision
	review.Revision = ""
	b, err := json.Marshal(review)
	if err != nil || artifacts.Hash(b) != revision || agentchat.ShareRevision(review.Session, review.Body) != review.SourceRevision {
		return fail()
	}
	review.Revision = revision
	key := sessionConversation(review.Session).Key
	thread := chatthreads.Thread{ID: threadID, Title: review.Session.Title, Created: stamp, By: review.OwnerEmail,
		SharedSource: &chatthreads.SharedSource{Agent: review.Session.Agent, ID: review.Session.ID}, ImportSource: key, ImportRevision: revision}
	msgs := []chatthreads.Message{}
	usedFiles := map[int]bool{}
	seenMessages := map[string]bool{}
	add := func(turn, kind, author, name, text, at string, record any, references []string) error {
		id := "shared-" + artifacts.Hash([]byte(threadID + "\x00" + key + "\x00" + turn))[:24]
		if seenMessages[id] {
			return errors.New("duplicate source turn in sharing review")
		}
		seenMessages[id] = true
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		ts, timeErr := time.Parse(time.RFC3339Nano, at)
		known := timeErr == nil && !ts.IsZero()
		if !known {
			ts = stamp
		}
		m := chatthreads.Message{ID: id, Thread: threadID, Kind: kind, Author: author, AuthName: name, Text: text, At: ts,
			Source: &chatthreads.MessageSource{Conversation: key, Turn: turn, TimestampKnown: known, Record: raw}}
		for i, file := range review.Files {
			matched := false
			for _, ref := range references {
				for _, origin := range file.References {
					if ref == origin {
						matched = true
					}
				}
			}
			if !matched {
				continue
			}
			m.Files = append(m.Files, chatthreads.FileRef{Hash: file.Hash, Name: file.Name, Size: file.Size})
			usedFiles[i] = true
		}
		msgs = append(msgs, m)
		return nil
	}
	if o := review.Session.Origin; o != nil && (o.Context != "" || o.Prompt != "" || len(o.Artifacts) > 0) {
		text := o.Context
		if o.Prompt != "" {
			if text != "" {
				text += "\n\n"
			}
			text += o.Prompt
		}
		if text == "" {
			text = "Files attached when this conversation began."
		}
		if err := add("origin", "system", "system", "Conversation context", text, "", o, []string{"origin"}); err != nil {
			return fail()
		}
	}
	for _, turn := range review.Timeline {
		label := fmt.Sprint(turn.N)
		refs := []string{"turn:" + label}
		if turn.Delivery != nil {
			refs = append(refs, "delivery:"+turn.Delivery.ID)
		}
		if turn.Native != nil && turn.Submission != nil {
			for hash, receipt := range review.NativeReceipts[turn.Native.ID] {
				if receipt.ID == turn.Submission.ID {
					refs = append(refs, "terminal:"+turn.Native.ID+":"+hash)
				}
			}
		}
		kind, author, name := "agent", "agent:"+strings.TrimPrefix(turn.Who, "agent:"), strings.TrimPrefix(turn.Who, "agent:")
		text := turn.Text
		switch turn.Who {
		case "user":
			kind, author, name = "ask", review.OwnerEmail, review.OwnerName
			if turn.Submission != nil && turn.Submission.ActorEmail != "" {
				author, name = turn.Submission.ActorEmail, turn.Submission.ActorName
			}
		case "system":
			kind, author, name = "system", "system", "System"
		default:
			if turn.Native == nil {
				text = agentchat.SayBody(text)
			}
			if text == "" {
				parts := []string{}
				for _, block := range turn.Blocks {
					if block.T == "say" {
						parts = append(parts, block.Text)
					}
				}
				text = strings.Join(parts, "\n\n")
			}
			if text == "" {
				text = "Tool activity"
			}
		}
		// File references become clickable attachments, not duplicated tokens.
		text = strings.TrimSpace(fileTokenRe.ReplaceAllString(text, ""))
		if err := add("turn:"+label, kind, author, name, text, turn.TS, turn, refs); err != nil {
			return fail()
		}
	}
	for _, result := range review.CodingResults {
		if err := add("result:"+result.Agent+":"+result.ID, "agent", "agent:"+result.Agent, result.Agent, result.Body, result.Started, result, nil); err != nil {
			return fail()
		}
		msgs[len(msgs)-1].Outcome = result.Outcome
	}
	// Keep every approved file discoverable, even if an old receipt cannot be
	// associated with a visible turn. Never quietly drop such an attachment.
	remaining := []chatthreads.FileRef{}
	for i, file := range review.Files {
		if !usedFiles[i] {
			remaining = append(remaining, chatthreads.FileRef{Hash: file.Hash, Name: file.Name, Size: file.Size})
		}
	}
	if len(remaining) > 0 {
		if err := add("files", "system", "system", "Files", "Files shared with this conversation.", "", review.Files, nil); err != nil {
			return fail()
		}
		msgs[len(msgs)-1].Files = remaining
	}
	return thread, msgs, nil
}
