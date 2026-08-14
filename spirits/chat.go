package spirits

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/mdfm"
)

// Chat (cmd-ctr import P2): a conversation is a session file in the harness
// (artifacts/chats/<id>.md, ENGINE-written turn by turn) plus spooled user
// messages (vessel/spool kind:"chat"). Manifest's role mirrors runs exactly:
// read the files, write the spool — never invoke the engine. The one
// manifest-side write is the session SKELETON on create (a user-owned
// artifact, like feed verdicts); the engine is the sole writer thereafter.

// chatSessionIDRe mirrors the engine's session-id grammar.
var chatSessionIDRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-z]{2,8}$`)

// ChatSpirit is one chattable spirit (spirits/<name>/chat.md present).
type ChatSpirit struct {
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models,omitempty"` // chat.md models: — per-session override choices
}

// ChatSessionSummary is a parsed session frontmatter row.
type ChatSessionSummary struct {
	ID         string  `json:"id"`
	Spirit     string  `json:"spirit"`
	Title      string  `json:"title"`
	Created    string  `json:"created"`
	Updated    string  `json:"updated"`
	Status     string  `json:"status"` // idle | thinking | error
	Turns      int     `json:"turns"`
	SpentUSD   float64 `json:"spentUsd"`
	CeilingUSD float64 `json:"ceilingUsd"`
	Model      string  `json:"model"`
}

// ChatSpirits scans for spirits carrying a chat.md.
func (s *Store) ChatSpirits() []ChatSpirit {
	var out []ChatSpirit
	entries, _ := os.ReadDir(filepath.Join(s.root, "spirits"))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.root, "spirits", e.Name(), "chat.md"))
		if err != nil {
			continue
		}
		fm, _ := mdfm.Split(string(b))
		enabled := strings.ToLower(strings.TrimSpace(fm["enabled"])) != "false"
		out = append(out, ChatSpirit{Name: e.Name(), Enabled: enabled, Models: mdfm.List(fm["models"])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) chatDir() string { return filepath.Join(s.root, "artifacts", "chats") }

func (s *Store) parseChatSession(path string) (ChatSessionSummary, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ChatSessionSummary{}, "", err
	}
	fm, body := mdfm.Split(string(b))
	sum := ChatSessionSummary{
		ID: fm["session"], Spirit: fm["spirit"], Title: fm["title"],
		Created: fm["created"], Updated: fm["updated"], Status: fm["status"],
		Model: fm["model"],
	}
	if sum.ID == "" {
		sum.ID = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	sum.Turns, _ = strconv.Atoi(fm["turns"])
	sum.SpentUSD, _ = strconv.ParseFloat(fm["charge_spent_usd"], 64)
	sum.CeilingUSD, _ = strconv.ParseFloat(fm["charge_ceiling_usd"], 64)
	return sum, strings.TrimSpace(body), nil
}

// ChatSessions lists sessions newest-updated first.
func (s *Store) ChatSessions() []ChatSessionSummary {
	entries, _ := os.ReadDir(s.chatDir())
	var out []ChatSessionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if sum, _, err := s.parseChatSession(filepath.Join(s.chatDir(), e.Name())); err == nil {
			out = append(out, sum)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Updated, out[j].Updated
		if a == "" {
			a = out[i].Created
		}
		if b == "" {
			b = out[j].Created
		}
		return a > b
	})
	return out
}

// ChatSession returns one session's summary + raw turn body.
func (s *Store) ChatSession(id string) (ChatSessionSummary, string, bool) {
	if !chatSessionIDRe.MatchString(id) {
		return ChatSessionSummary{}, "", false
	}
	sum, body, err := s.parseChatSession(filepath.Join(s.chatDir(), id+".md"))
	if err != nil {
		return ChatSessionSummary{}, "", false
	}
	return sum, body, true
}

// CreateChatSession writes the session skeleton and returns its id. The
// engine fills ceilings/portal on the first turn; a non-empty model pins a
// per-session override (must be on the spirit's chat.md models whitelist —
// the engine enforces, fail closed).
func (s *Store) CreateChatSession(spirit, title, model string) (string, error) {
	if !validID(spirit) {
		return "", fmt.Errorf("bad spirit name")
	}
	if _, err := os.Stat(filepath.Join(s.root, "spirits", spirit, "chat.md")); err != nil {
		return "", fmt.Errorf("spirit %s is not chattable", spirit)
	}
	if err := os.MkdirAll(s.chatDir(), 0o755); err != nil {
		return "", err
	}
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	id := time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "new conversation"
	}
	now := time.Now().Format(time.RFC3339)
	out := (&mdfm.Writer{}).
		Set("session", id).
		Set("spirit", spirit).
		Set("title", title).
		Set("created", now).
		Set("updated", now).
		SetRaw("status", "idle").
		SetRaw("turns", "0").
		SetRaw("charge_spent_usd", "0.0000").
		SetRaw("charge_ceiling_usd", "0.00").
		Set("model", strings.TrimSpace(model)).
		String("")
	if err := os.WriteFile(filepath.Join(s.chatDir(), id+".md"), []byte(out), 0o644); err != nil {
		return "", err
	}
	return id, nil
}

// SpoolChatMessage drops one user message for the engine (kind "chat").
// Ordering per session rides the unixnano filename; the engine serializes
// turns per session and leaves messages spooled while a turn runs.
func (s *Store) SpoolChatMessage(spirit, session, text, source string) error {
	if !validID(spirit) || !chatSessionIDRe.MatchString(session) {
		return fmt.Errorf("bad spirit/session")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty message")
	}
	if len(text) > maxRequestChars {
		text = text[:maxRequestChars]
	}
	if source == "" {
		source = "dashboard"
	}
	dir := filepath.Join(s.root, "vessel", "spool")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{
		"kind":         "chat",
		"spirit":       spirit,
		"session":      session,
		"message":      text,
		"source":       source,
		"requested_at": time.Now().Format(time.RFC3339),
	})
	name := fmt.Sprintf("%d-chat-%s.json", time.Now().UnixNano(), session)
	return os.WriteFile(filepath.Join(dir, name), payload, 0o644)
}

// QueuedChatMessages lists spooled-but-unclaimed messages for a session
// (oldest first) — the composer's optimistic echo derives from these, so a
// refresh mid-queue loses nothing.
func (s *Store) QueuedChatMessages(session string) []string {
	entries, _ := os.ReadDir(filepath.Join(s.root, "vessel", "spool"))
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(s.root, "vessel", "spool", n))
		if err != nil {
			continue
		}
		var req struct {
			Kind    string `json:"kind"`
			Session string `json:"session"`
			Message string `json:"message"`
		}
		if json.Unmarshal(b, &req) == nil && req.Kind == "chat" && req.Session == session {
			out = append(out, req.Message)
		}
	}
	return out
}

// ChatEvents returns the session's machine events after seq (the bridge/SSE
// seam — resumable by construction).
func (s *Store) ChatEvents(id string, after int) []json.RawMessage {
	if !chatSessionIDRe.MatchString(id) {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(s.root, "vessel", "state", "chat", id, "events.jsonl"))
	if err != nil {
		return nil
	}
	var out []json.RawMessage
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var seq struct {
			Seq int `json:"seq"`
		}
		if json.Unmarshal([]byte(line), &seq) != nil || seq.Seq <= after {
			continue
		}
		out = append(out, json.RawMessage(line))
	}
	return out
}

// RenameChatSession retitles a session (frontmatter only; refused while the
// engine is mid-turn — it would clobber the rewrite).
func (s *Store) RenameChatSession(id, title string) error {
	sum, body, ok := s.ChatSession(id)
	if !ok {
		return fmt.Errorf("no such session")
	}
	if sum.Status == "thinking" {
		return fmt.Errorf("session is thinking — try again in a moment")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("title cannot be empty")
	}
	out := (&mdfm.Writer{}).
		Set("session", sum.ID).
		Set("spirit", sum.Spirit).
		Set("title", title).
		Set("created", sum.Created).
		Set("updated", sum.Updated).
		SetRaw("status", sum.Status).
		SetRaw("turns", strconv.Itoa(sum.Turns)).
		SetRaw("charge_spent_usd", fmt.Sprintf("%.4f", sum.SpentUSD)).
		SetRaw("charge_ceiling_usd", fmt.Sprintf("%.2f", sum.CeilingUSD)).
		Set("portal", ""). // Set skips blanks; engine restores on next turn
		Set("model", sum.Model).
		String(body)
	return os.WriteFile(filepath.Join(s.chatDir(), id+".md"), []byte(out), 0o644)
}

// DeleteChatSession removes the session file + its event stream (refused
// while thinking).
func (s *Store) DeleteChatSession(id string) error {
	sum, _, ok := s.ChatSession(id)
	if !ok {
		return fmt.Errorf("no such session")
	}
	if sum.Status == "thinking" {
		return fmt.Errorf("session is thinking — try again in a moment")
	}
	if err := os.Remove(filepath.Join(s.chatDir(), id+".md")); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(s.root, "vessel", "state", "chat", id))
	return nil
}
