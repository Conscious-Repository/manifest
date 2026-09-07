package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"manifest/hermes"
	"manifest/vaultindex"
	"manifest/vaultwriter"
	"manifest/writing"
	"net/http"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

func (s *Server) writingReadRecover(path string) (writing.Document, error) {
	s.writingAskMu.Lock()
	defer s.writingAskMu.Unlock()
	d, err := s.writing.Read(path)
	if err != nil {
		return d, err
	}
	for _, turn := range d.Turns {
		if turn.State == "running" && !s.writingAsks[path+"\x00"+turn.ID] {
			turn.State = "failed"
			turn.Error = "The request was interrupted. Try again."
			d, err = s.writing.Append(path, "", writing.Event{Type: "ask_result", ID: turn.ID + "-result", Turn: &turn}, true)
			if err != nil {
				return d, err
			}
		}
	}
	return d, nil
}

func writingClip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (s *Server) writingPacket(path string, t writing.Thread, question string) (any, error) {
	full, err := vaultwriter.SafePath(s.vault.VaultRoot(), path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(full)
	if err != nil || fi.Size() > 4<<20 {
		return nil, errors.New("note unavailable")
	}
	raw, err := os.ReadFile(full)
	if err != nil || !utf8.Valid(raw) {
		return nil, errors.New("note unavailable")
	}
	text := string(raw)
	pattern := t.Anchor.Prefix + t.Anchor.Quote + t.Anchor.Suffix
	start := strings.Index(text, pattern)
	if start >= 0 && strings.Contains(text[start+1:], pattern) {
		start = -1
	}
	if start >= 0 {
		start += len(t.Anchor.Prefix)
	}
	// An exact original revision can resolve a repeated passage without guessing.
	if t.Anchor.Revision == vaultwriter.Revision(raw) && t.Anchor.Start >= 0 && t.Anchor.End <= len(raw) && t.Anchor.End > t.Anchor.Start && string(raw[t.Anchor.Start:t.Anchor.End]) == t.Anchor.Quote {
		start = t.Anchor.Start
	}
	if start < 0 && t.Anchor.Quote != "" {
		pos := strings.Index(text, t.Anchor.Quote)
		if pos >= 0 && !strings.Contains(text[pos+1:], t.Anchor.Quote) {
			start = pos
		}
	}
	surrounding := ""
	if start >= 0 {
		lo := start - 3000
		if lo < 0 {
			lo = 0
		}
		for lo > 0 && !utf8.RuneStart(raw[lo]) {
			lo--
		}
		surrounding = writingClip(text[lo:], len(t.Anchor.Quote)+6000)
	}
	history := t.Replies
	if len(history) > 12 {
		history = history[len(history)-12:]
	}
	// Bound total context even when old replies are long.
	bounded := make([]writing.Reply, 0, len(history))
	for _, r := range history {
		r.Body = writingClip(r.Body, 2400)
		bounded = append(bounded, r)
	}
	packet := map[string]any{"document": path, "selectedPassage": t.Anchor.Quote, "surroundingText": surrounding, "question": question, "conversation": bounded}
	if s.index != nil {
		refs, e := s.index.Passages(writingClip(t.Anchor.Quote+" "+question, 12000), path, []string{""}, s.writing.Excluded)
		if e != nil {
			return nil, e
		}
		linked := []vaultindex.Passage{}
		seen := map[string]bool{}
		for _, match := range regexp.MustCompile(`\[\[([^\]]+)\]\]`).FindAllStringSubmatch(t.Anchor.Quote+" "+question, 4) {
			ref, e := s.index.LinkedPassage(match[1], path, s.writing.Excluded)
			if e != nil {
				return nil, e
			}
			if ref != nil && !seen[ref.Path] {
				linked = append(linked, *ref)
				seen[ref.Path] = true
			}
		}
		for _, ref := range refs {
			if !seen[ref.Path] {
				linked = append(linked, ref)
			}
		}
		packet["vaultExcerpts"] = linked
	}
	b, _ := json.Marshal(packet)
	if len(b) > 100000 {
		return nil, errors.New("writing context too large")
	}
	return packet, nil
}

func (s *Server) handleWritingAsk(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10000)
	if s.writing == nil || s.writingComplete == nil {
		http.Error(w, "Ask is unavailable on this host.", 503)
		return
	}
	var b struct {
		Path     string `json:"path"`
		Thread   string `json:"thread"`
		Question string `json:"question"`
		ID       string `json:"id"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if len(b.ID) < 8 || len(b.ID) > 80 {
		http.Error(w, "invalid request", 400)
		return
	}
	if _, err := vaultwriter.SafePath(s.vault.VaultRoot(), b.Path); err != nil {
		http.Error(w, "invalid note path", 400)
		return
	}
	s.writingAskMu.Lock()
	defer s.writingAskMu.Unlock()
	doc, err := s.writing.Read(b.Path)
	if err != nil {
		http.Error(w, "Could not load the conversation.", 409)
		return
	}
	for _, turn := range doc.Turns {
		if turn.ID == b.ID {
			if turn.Thread != b.Thread || turn.Question != b.Question {
				http.Error(w, "request ID collision", 409)
				return
			}
			writeJSON(w, doc)
			return
		}
	}
	for key := range s.writingAsks {
		if strings.HasPrefix(key, b.Path+"\x00") {
			http.Error(w, "An answer is already in progress.", 409)
			return
		}
	}
	var thread writing.Thread
	question := ""
	for _, t := range doc.Threads {
		if t.ID == b.Thread && t.State == "open" {
			thread = t
			for _, reply := range t.Replies {
				if reply.ID == b.Question && reply.Author == "owner" {
					question = reply.Body
				}
			}
		}
	}
	if question == "" {
		http.Error(w, "Choose an open comment to ask about.", 400)
		return
	}
	if len(s.writingAsks) >= 4 {
		http.Error(w, "Ask is busy. Try again shortly.", 429)
		return
	}
	packet, err := s.writingPacket(b.Path, thread, question)
	if err != nil {
		http.Error(w, "Could not prepare the passage. Try again.", 409)
		return
	}
	turn := writing.Turn{ID: b.ID, Thread: b.Thread, Question: b.Question, State: "running"}
	doc, err = s.writing.Append(b.Path, doc.Revision, writing.Event{Type: "ask", ID: b.ID, Turn: &turn}, false)
	if err != nil {
		http.Error(w, "The conversation changed. Try again.", 409)
		return
	}
	if s.writingAsks == nil {
		s.writingAsks = map[string]bool{}
	}
	key := b.Path + "\x00" + b.ID
	s.writingAsks[key] = true
	go func() {
		result, runErr := s.writingComplete(context.Background(), packet)
		s.writingAskMu.Lock()
		defer s.writingAskMu.Unlock()
		defer delete(s.writingAsks, key)
		turn.State = "complete"
		turn.Model = result.Model
		turn.InputTokens = result.InputTokens
		turn.OutputTokens = result.OutputTokens
		var reply *writing.Reply
		if runErr != nil {
			turn.State = "failed"
			turn.Error = "The model could not complete this request. Please retry."
			var failure *hermes.AnnotationError
			if errors.As(runErr, &failure) {
				turn.Error = failure.UserMessage()
				turn.ErrorCode = failure.Code
			}
			log.Printf("writing ask %s failed: %v", b.ID, runErr)
		} else {
			a := writing.NewReply(b.ID+"-answer", "alfred", result.Reply)
			reply = &a
		}
		if _, err := s.writing.Append(b.Path, "", writing.Event{Type: "ask_result", ID: b.ID + "-result", Turn: &turn, Reply: reply}, true); err != nil {
			log.Printf("writing: answer could not be saved: %v", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, doc)
}
