package server

import (
	"context"
	"encoding/json"
	"fmt"
	"manifest/chatthreads"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Reviewed turns already live in the team import. Only newer native turns are
// projected alongside them; reads never append another durable copy. Missing
// histories remain visible as unavailable runtimes, not empty successes.
func (s *Server) sharedNativeViews(ctx context.Context, ag *chatAgent, thread string, review chatShareReview) ([]codingContinuationView, error) {
	views := []codingContinuationView{}
	for _, included := range review.Continuations {
		se, err := s.sharedTerminal(ag, thread, included.ID)
		if err != nil {
			return nil, err
		}
		v := s.projectCodingContinuation(ctx, se, sessionConversation(review.Session).Key)
		seen := map[string]bool{}
		for _, turn := range included.Turns {
			seen[turn.ID] = true
		}
		fresh := []termTurn{}
		for _, turn := range v.Turns {
			if !seen[turn.ID] {
				fresh = append(fresh, turn)
			}
		}
		v.Turns = fresh
		// A portal needs neither private workspace paths nor owner-only routes.
		v.Cwd = ""
		v.Conversation = agentConversation("portal", ag.Name, thread, "team:"+ag.Domain, "")
		views = append(views, v)
	}
	if s.terminal != nil {
		rows, err := s.terminal.loadChecked()
		if err != nil {
			return nil, err
		}
		for _, se := range rows {
			if !directSharedTerminal(se, ag, thread) {
				continue
			}
			if _, err := s.sharedTerminal(ag, thread, se.ID); err != nil {
				return nil, err
			}
			v := s.projectCodingContinuation(ctx, se, agentConversation("portal", ag.Name, thread, "team:"+ag.Domain, "").Key)
			v.Cwd = ""
			v.Conversation = agentConversation("portal", ag.Name, thread, "team:"+ag.Domain, "")
			views = append(views, v)
		}
	}
	return views, nil
}

func sharedHistoryMessages(review chatShareReview, thread string, stored []chatthreads.Message, views []codingContinuationView) []chatthreads.Message {
	msgs := append([]chatthreads.Message{}, stored...)
	for _, v := range views {
		for _, turn := range v.Turns {
			key := "terminal:" + v.ID + ":" + turn.ID
			ts, err := time.Parse(time.RFC3339Nano, turn.TS)
			known := err == nil && !ts.IsZero()
			if !known {
				ts, _ = time.Parse(time.RFC3339Nano, v.Created)
			}
			kind, author, name, text := "agent", "agent:"+v.Agent, v.Agent, turn.Text
			if turn.Who == "user" {
				kind, author, name = "ask", review.OwnerEmail, review.OwnerName
				if receipt, ok := v.Submissions[turn.ID]; ok && receipt.ActorEmail != "" {
					author, name = receipt.ActorEmail, receipt.ActorName
				}
			} else if turn.Who == "system" {
				kind, author, name = "system", "system", "System"
			}
			if text == "" {
				parts := []string{}
				for _, b := range turn.Blocks {
					if b.T == "say" {
						parts = append(parts, b.Text)
					}
				}
				text = strings.Join(parts, "\n\n")
			}
			if text == "" {
				text = "Tool activity"
			}
			raw, _ := json.Marshal(turn)
			m := chatthreads.Message{ID: "native-" + hashTerminalText(thread + "\x00" + key)[:24], Thread: thread, Kind: kind, Author: author, AuthName: name, Text: text, At: ts, Source: &chatthreads.MessageSource{Conversation: sessionConversation(review.Session).Key, Turn: key, TimestampKnown: known, Record: raw}}
			if receipt, ok := v.Submissions[turn.ID]; ok {
				m.Files = append(m.Files, receipt.Files...)
				for _, ref := range receipt.Artifacts {
					for _, file := range review.Files {
						if ref.ID == file.ArtifactID && ref.Revision == file.Hash {
							m.Files = append(m.Files, chatthreads.FileRef{Hash: file.Hash, Name: file.Name, Size: file.Size})
							break
						}
					}
				}
			}
			msgs = append(msgs, m)
		}
	}
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].At.Before(msgs[j].At) })
	return msgs
}

func (s *Server) AionChatConversation(w http.ResponseWriter, r *http.Request) {
	s.sharedConversationHistory(s.kairosAgent(), w, r)
}
func (s *Server) OodaChatConversation(w http.ResponseWriter, r *http.Request) {
	s.sharedConversationHistory(s.zeckAgent(), w, r)
}

func (s *Server) sharedConversationHistory(ag *chatAgent, w http.ResponseWriter, r *http.Request) {
	thread := r.PathValue("thread")
	review, err := s.sharedConversationReview(ag, thread)
	if err != nil {
		http.Error(w, errSharedConversationAccess.Error(), http.StatusForbidden)
		return
	}
	views, err := s.sharedNativeViews(r.Context(), ag, thread, review)
	if err != nil {
		http.Error(w, errSharedConversationAccess.Error(), http.StatusForbidden)
		return
	}
	warnings := []string{}
	for _, v := range views {
		if !v.HistoryAvailable {
			warnings = append(warnings, fmt.Sprintf("%s history is temporarily unavailable.", v.Agent))
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"thread": thread, "messages": sharedHistoryMessages(review, thread, ag.Store.Messages(thread), views), "terminals": views, "warnings": warnings, "files": s.sharedConversationFiles(ag, thread, review)})
}
