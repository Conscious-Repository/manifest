// Package writing declares durable document conversations over record blocks.
package writing

import (
	"encoding/json"
	"errors"
	"fmt"
	"manifest/record"
	"manifest/vaultwriter"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

type Anchor struct {
	Revision string `json:"revision"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Quote    string `json:"quote"`
	Prefix   string `json:"prefix"`
	Suffix   string `json:"suffix"`
}
type Reply struct {
	ID     string `json:"id"`
	Author string `json:"author"`
	Body   string `json:"body"`
	At     string `json:"at"`
}
type Thread struct {
	ID      string  `json:"id"`
	Anchor  Anchor  `json:"anchor"`
	State   string  `json:"state"`
	Replies []Reply `json:"replies"`
}
type Event struct {
	Type     string  `json:"type"`
	ID       string  `json:"id"`
	Thread   string  `json:"thread,omitempty"`
	Document string  `json:"document,omitempty"`
	Anchor   *Anchor `json:"anchor,omitempty"`
	Reply    *Reply  `json:"reply,omitempty"`
	State    string  `json:"state,omitempty"`
}
type Document struct {
	Path     string   `json:"path"`
	Revision string   `json:"revision"`
	Threads  []Thread `json:"threads"`
}
type Store struct {
	Writer   *vaultwriter.Writer
	Root     string
	Excluded []string
}

func (s *Store) RecordPath(p string) string {
	return path.Join(s.Root, record.Slug(strings.TrimSuffix(path.Base(p), ".md"), 40)+"-"+vaultwriter.Revision([]byte(p))[:20]+".md")
}
func Parse(raw, p string) (Document, error) {
	out := Document{Path: p, Revision: vaultwriter.Revision([]byte(raw)), Threads: []Thread{}}
	blocks, err := record.JSONBlocks(raw, "manifest-writing")
	if err != nil {
		return out, err
	}
	seen := map[string]bool{}
	initialized := false
	for _, block := range blocks {
		var e Event
		if err = json.Unmarshal(block, &e); err != nil {
			return out, err
		}
		if e.ID == "" || seen[e.ID] {
			return out, errors.New("invalid or duplicate writing event ID")
		}
		seen[e.ID] = true
		switch e.Type {
		case "document":
			if initialized || e.Document != p {
				return out, errors.New("writing record document mismatch")
			}
			initialized = true
		case "thread":
			if !initialized || e.Anchor == nil || e.Reply == nil {
				return out, errors.New("invalid writing thread")
			}
			out.Threads = append(out.Threads, Thread{ID: e.ID, Anchor: *e.Anchor, State: "open", Replies: []Reply{*e.Reply}})
		case "reply", "state":
			found := false
			for i := range out.Threads {
				t := &out.Threads[i]
				if t.ID != e.Thread {
					continue
				}
				found = true
				if e.Type == "reply" {
					if e.Reply == nil {
						return out, errors.New("invalid reply")
					}
					t.Replies = append(t.Replies, *e.Reply)
				} else {
					if e.State != "open" && e.State != "resolved" && e.State != "dismissed" {
						return out, errors.New("invalid discussion state")
					}
					t.State = e.State
				}
			}
			if !found {
				return out, errors.New("missing thread")
			}
		default: // Future events remain opaque and byte-preserved.
		}
	}
	if raw != "" && !initialized {
		return out, errors.New("missing writing document identity")
	}
	return out, nil
}
func (s *Store) Read(p string) (Document, error) {
	b, err := s.Writer.ReadVaultFile(s.RecordPath(p))
	if err != nil {
		if !isMissing(err) {
			return Document{}, err
		}
		b = nil
	}
	return Parse(string(b), p)
}
func ValidateAnchor(raw []byte, a Anchor) error {
	if a.Revision != vaultwriter.Revision(raw) || a.Start < 0 || a.End <= a.Start || a.End > len(raw) || a.End-a.Start > 12000 {
		return errors.New("selection is stale or invalid")
	}
	if !utf8.Valid(raw[:a.Start]) || !utf8.Valid(raw[:a.End]) || string(raw[a.Start:a.End]) != a.Quote {
		return errors.New("selection does not match exact document bytes")
	}
	return nil
}
func StampAnchor(raw []byte, a Anchor) Anchor {
	start := a.Start - 120
	if start < 0 {
		start = 0
	}
	for start < a.Start && !utf8.RuneStart(raw[start]) {
		start++
	}
	end := a.End + 120
	if end > len(raw) {
		end = len(raw)
	}
	for end < len(raw) && !utf8.RuneStart(raw[end]) {
		end--
	}
	a.Prefix = string(raw[start:a.Start])
	a.Suffix = string(raw[a.End:end])
	return a
}
func (s *Store) Append(p, expected string, e Event, agent bool) (Document, error) {
	var out Document
	capName := "writing"
	if agent {
		capName = "writing-agent"
		if e.Type != "reply" || e.Reply == nil || e.Reply.Author != "alfred" {
			return out, errors.New("agent may append replies only")
		}
	}
	err := s.Writer.UpdateCap(capName, s.RecordPath(p), func(before []byte) ([]byte, error) {
		d, err := Parse(string(before), p)
		if err != nil {
			return nil, err
		}
		// Replay is idempotent, including a retry after an uncertain response.
		blocks, _ := record.JSONBlocks(string(before), "manifest-writing")
		for _, b := range blocks {
			var prior Event
			_ = json.Unmarshal(b, &prior)
			if prior.ID == e.ID {
				if prior.Reply != nil && e.Reply != nil {
					e.Reply.At = prior.Reply.At
				}
				pb, _ := json.Marshal(prior)
				eb, _ := json.Marshal(e)
				if string(pb) != string(eb) {
					return nil, errors.New("event ID collision")
				}
				out = d
				return before, nil
			}
		}
		if !agent && expected != d.Revision {
			return nil, fmt.Errorf("comments changed; reload before updating")
		}
		raw := string(before)
		if raw == "" {
			raw = "# Writing conversations\n"
			raw, err = record.AppendJSONBlock(raw, "manifest-writing", Event{Type: "document", ID: "document", Document: p})
			if err != nil {
				return nil, err
			}
		}
		raw, err = record.AppendJSONBlock(raw, "manifest-writing", e)
		if err != nil {
			return nil, err
		}
		out, err = Parse(raw, p)
		return []byte(raw), err
	})
	return out, err
}
func NewReply(id, author, body string) Reply {
	return Reply{ID: id, Author: author, Body: body, At: time.Now().UTC().Format(time.RFC3339)}
}

// RelocatedRecord changes only the document identity field. Replies, anchors,
// event IDs, unknown fields and hand-edited prose retain their original bytes.
func (s *Store) RelocatedRecord(from, to string) ([]byte, []byte, error) {
	before, err := s.Writer.ReadVaultFile(s.RecordPath(from))
	if isMissing(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if _, err = Parse(string(before), from); err != nil {
		return nil, nil, err
	}
	next, err := record.RewriteJSONBlocks(string(before), "manifest-writing", func(fields map[string]json.RawMessage) bool {
		var kind string
		_ = json.Unmarshal(fields["type"], &kind)
		if kind != "document" {
			return false
		}
		fields["document"], _ = json.Marshal(to)
		return true
	})
	return before, []byte(next), err
}
