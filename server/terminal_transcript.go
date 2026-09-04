package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Transcript projection (agent-chat Stage S): the CLI's own session file IS
// the transcript. Claude Code appends `~/.claude/projects/<cwd-encoded>/
// <resumeId>.jsonl`; codex appends `~/.codex/sessions/…/rollout-*.jsonl`.
// Manifest never writes either — it reads and projects the records into the
// agent-chat turn grammar per request (no persisted projection; a 1.3 MB
// file parses in milliseconds, and a (path, size, mtime) cache covers the
// 1.5 s poll while a session is live).
//
// Schema drift: the record types are undocumented and version-bound (Claude
// Code 2.1.259 / codex 0.147 observed). Only the message rows are trusted;
// every unknown record type is skipped, never an error.

// termTurn is one turn of the projected transcript. `who` is user |
// assistant; an assistant turn carries ordered blocks (say / step / think),
// a user turn carries text.
type termTurn struct {
	Who    string      `json:"who"`
	TS     string      `json:"ts,omitempty"`
	Text   string      `json:"text,omitempty"`
	Blocks []termBlock `json:"blocks,omitempty"`
}

// termBlock: t = say (markdown text) | think (thinking text) | step (a tool
// call: cast = tool name, input = one-line summary, result = the paired
// tool_result, trimmed; error when the tool reported one). ID is the tool_use
// id: a result whose call fell before ?after= arrives as a step with cast
// "result" and the same id, so the tailing client can pair it with the chip
// it already painted.
type termBlock struct {
	T      string `json:"t"`
	Text   string `json:"text,omitempty"`
	Cast   string `json:"cast,omitempty"`
	Input  string `json:"input,omitempty"`
	Result string `json:"result,omitempty"`
	Error  bool   `json:"error,omitempty"`
	ID     string `json:"id,omitempty"`
}

// termTranscript is the projection of one session file (or its tail).
type termTranscript struct {
	Turns []termTurn `json:"turns"`
	Title string     `json:"title,omitempty"` // claude ai-title
	Cost  float64    `json:"cost,omitempty"`  // claude cost-state totalCostUSD
	// Offset is the byte offset just past the last COMPLETE line parsed —
	// pass it back as ?after= to receive only newer records.
	Offset int64 `json:"offset"`
}

const (
	termStepResultMax = 1500 // chars of a tool result kept as step detail
	termStepInputMax  = 200  // chars of the one-line input summary
)

// --- claude ---

type claudeRecord struct {
	Type        string          `json:"type"`
	Timestamp   string          `json:"timestamp"`
	IsMeta      bool            `json:"isMeta"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
	AITitle     string          `json:"aiTitle"`
	TotalCost   float64         `json:"totalCostUSD"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string | []block
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"` // tool_result: string | []block
	IsError   bool            `json:"is_error"`
}

// transcriptBuilder accumulates turns, merging consecutive assistant records
// (one API message is often several rows) and pairing tool results with the
// step that asked for them.
type transcriptBuilder struct {
	out termTranscript
}

func (b *transcriptBuilder) assistant(ts string) *termTurn {
	if n := len(b.out.Turns); n > 0 && b.out.Turns[n-1].Who == "assistant" {
		return &b.out.Turns[n-1]
	}
	b.out.Turns = append(b.out.Turns, termTurn{Who: "assistant", TS: ts})
	return &b.out.Turns[len(b.out.Turns)-1]
}

func (b *transcriptBuilder) user(ts, text string) {
	b.out.Turns = append(b.out.Turns, termTurn{Who: "user", TS: ts, Text: text})
}

func (b *transcriptBuilder) step(ts, id, cast, input string) {
	t := b.assistant(ts)
	t.Blocks = append(t.Blocks, termBlock{T: "step", Cast: cast, Input: input, ID: id})
}

// result pairs a tool result with the most recent step carrying its id.
func (b *transcriptBuilder) result(ts, id, text string, isErr bool) {
	for ti := len(b.out.Turns) - 1; ti >= 0; ti-- {
		t := &b.out.Turns[ti]
		if t.Who != "assistant" {
			continue
		}
		for bi := range t.Blocks {
			if t.Blocks[bi].T == "step" && t.Blocks[bi].ID == id {
				t.Blocks[bi].Result = clip(text, termStepResultMax)
				t.Blocks[bi].Error = isErr
				return
			}
		}
	}
	// a result whose call fell before ?after= — still worth a chip; the id
	// lets a tailing client pair it with the step it already holds
	t := b.assistant(ts)
	t.Blocks = append(t.Blocks, termBlock{T: "step", Cast: "result", Result: clip(text, termStepResultMax), Error: isErr, ID: id})
}

func (b *transcriptBuilder) text(ts, kind, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	t := b.assistant(ts)
	if n := len(t.Blocks); n > 0 && t.Blocks[n-1].T == kind {
		t.Blocks[n-1].Text += "\n\n" + text
		return
	}
	t.Blocks = append(t.Blocks, termBlock{T: kind, Text: text})
}

// parseClaudeTranscript projects Claude Code session jsonl records. Unknown
// record types (attachment, file-history-*, mode, queue-operation, …) are
// skipped. A user row is either the owner's text (→ user turn) or the
// tool_result carrier for the previous step.
func parseClaudeTranscript(r io.Reader) termTranscript {
	b := &transcriptBuilder{}
	scanLines(r, &b.out.Offset, func(line []byte) {
		var rec claudeRecord
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		switch rec.Type {
		case "ai-title":
			if t := strings.TrimSpace(rec.AITitle); t != "" {
				b.out.Title = t
			}
		case "cost-state":
			if rec.TotalCost > 0 {
				b.out.Cost = rec.TotalCost
			}
		case "user", "assistant":
			if rec.IsMeta || rec.IsSidechain || len(rec.Message) == 0 {
				return
			}
			var m claudeMessage
			if json.Unmarshal(rec.Message, &m) != nil {
				return
			}
			blocks, text := claudeContent(m.Content)
			if rec.Type == "user" {
				if text != "" && !claudeNoiseRe.MatchString(text) {
					b.user(rec.Timestamp, text)
				}
				for _, bl := range blocks {
					if bl.Type == "tool_result" {
						_, rt := claudeContent(bl.Content)
						b.result(rec.Timestamp, bl.ToolUseID, rt, bl.IsError)
					} else if bl.Type == "text" && strings.TrimSpace(bl.Text) != "" && !claudeNoiseRe.MatchString(bl.Text) {
						b.user(rec.Timestamp, strings.TrimSpace(bl.Text))
					}
				}
				return
			}
			if text != "" {
				b.text(rec.Timestamp, "say", text)
			}
			for _, bl := range blocks {
				switch bl.Type {
				case "text":
					b.text(rec.Timestamp, "say", bl.Text)
				case "thinking":
					b.text(rec.Timestamp, "think", bl.Thinking)
				case "tool_use":
					b.step(rec.Timestamp, bl.ID, bl.Name, toolInputSummary(bl.Name, bl.Input))
				}
			}
		}
	})
	return b.out
}

// claudeNoiseRe: slash-command echo rows the CLI writes as user text.
var claudeNoiseRe = regexp.MustCompile(`^\s*<(command-name|command-message|local-command-stdout|local-command-caveat|system-reminder)>`)

// claudeContent splits a content field: a bare string → text; an array →
// its blocks, with the text blocks' text also joined for convenience when
// the array is nothing but text (a user turn with pasted parts).
func claudeContent(raw json.RawMessage) ([]claudeBlock, string) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return nil, strings.TrimSpace(s)
	}
	var blocks []claudeBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return nil, ""
	}
	allText := true
	var parts []string
	for _, bl := range blocks {
		if bl.Type != "text" {
			allText = false
			break
		}
		if t := strings.TrimSpace(bl.Text); t != "" {
			parts = append(parts, t)
		}
	}
	if allText {
		return nil, strings.Join(parts, "\n\n")
	}
	return blocks, ""
}

// toolInputSummary is the one-line chip detail: the command for Bash, the
// path for file tools, the pattern for searches, else the compact JSON.
func toolInputSummary(name string, raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil || len(in) == 0 {
		return clip(strings.TrimSpace(string(raw)), termStepInputMax)
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := in[k]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return s
				}
			}
		}
		return ""
	}
	s := pick("command", "file_path", "path", "pattern", "description", "prompt", "query", "url", "notebook_path")
	if s == "" {
		keys := make([]string, 0, len(in))
		for k := range in {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+"="+fmt.Sprint(in[k]))
		}
		s = strings.Join(parts, " ")
	}
	s = strings.Join(strings.Fields(s), " ")
	if name == "Bash" && in["description"] != nil {
		// a described command reads better as "<description> · <cmd>"
		if d, ok := in["description"].(string); ok && strings.TrimSpace(d) != "" && s != d {
			s = strings.TrimSpace(d) + " · " + s
		}
	}
	return clip(s, termStepInputMax)
}

// --- codex ---

type codexRecord struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		ID      string `json:"id"`
		CallID  string `json:"call_id"`
		Name    string `json:"name"`
		Input   string `json:"input"`
		Args    string `json:"arguments"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Output  json.RawMessage `json:"output"` // string | [{type,text}]
		Summary []struct {
			Text string `json:"text"`
		} `json:"summary"`
	} `json:"payload"`
}

// parseCodexTranscript projects a codex rollout: response_item.message by
// role (developer rows and the CLI's own <…> boilerplate user rows skipped),
// custom_tool_call / function_call as steps paired with their _output,
// reasoning summaries as thinking. event_msg rows duplicate the message
// rows and are ignored.
func parseCodexTranscript(r io.Reader) termTranscript {
	b := &transcriptBuilder{}
	scanLines(r, &b.out.Offset, func(line []byte) {
		var rec codexRecord
		if json.Unmarshal(line, &rec) != nil || rec.Type != "response_item" {
			return
		}
		p := rec.Payload
		switch p.Type {
		case "message":
			var parts []string
			for _, c := range p.Content {
				if t := strings.TrimSpace(c.Text); t != "" {
					parts = append(parts, t)
				}
			}
			text := strings.Join(parts, "\n\n")
			if text == "" {
				return
			}
			switch p.Role {
			case "user":
				if !strings.HasPrefix(text, "<") { // <recommended_plugins>, <environment_context>, …
					b.user(rec.Timestamp, text)
				}
			case "assistant":
				b.text(rec.Timestamp, "say", text)
			}
		case "reasoning":
			var parts []string
			for _, s := range p.Summary {
				if t := strings.TrimSpace(s.Text); t != "" {
					parts = append(parts, t)
				}
			}
			if len(parts) > 0 {
				b.text(rec.Timestamp, "think", strings.Join(parts, "\n\n"))
			}
		case "custom_tool_call", "function_call":
			in := p.Input
			if in == "" {
				in = p.Args
			}
			b.step(rec.Timestamp, p.CallID, p.Name, clip(strings.Join(strings.Fields(in), " "), termStepInputMax))
		case "custom_tool_call_output", "function_call_output":
			b.result(rec.Timestamp, p.CallID, codexOutputText(p.Output), false)
		}
	})
	return b.out
}

func codexOutputText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return strings.TrimSpace(s)
	}
	var arr []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &arr)
	var parts []string
	for _, a := range arr {
		if t := strings.TrimSpace(a.Text); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}

// --- shared ---

// scanLines feeds each COMPLETE newline-terminated line to fn and advances
// *offset past it; a trailing partial line (the CLI mid-write) is left for
// the next poll.
func scanLines(r io.Reader, offset *int64, fn func([]byte)) {
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			// io.EOF with a partial line: not complete → do not consume
			return
		}
		*offset += int64(len(line))
		if ln := bytes.TrimSpace(line); len(ln) > 0 {
			fn(ln)
		}
	}
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	// cut on a rune boundary
	cut := max
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// parseTranscript dispatches on the session kind.
func parseTranscript(kind string, r io.Reader) termTranscript {
	if kind == "codex" {
		return parseCodexTranscript(r)
	}
	return parseClaudeTranscript(r)
}

// --- locating the file ---

// claudeProjectDir encodes a cwd the way Claude Code names its project
// folder: every non-alphanumeric byte becomes '-'.
func claudeProjectDir(cwd string) string {
	var sb strings.Builder
	for i := 0; i < len(cwd); i++ {
		c := cwd[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			sb.WriteByte(c)
		} else {
			sb.WriteByte('-')
		}
	}
	return sb.String()
}

// transcriptPath resolves the session file for a registry row; "" when the
// kind has no discoverable file (codex rows mint no id yet — §7 Q8).
func (c *termCfg) transcriptPath(se termSession) string {
	if se.Device != "" {
		return ""
	}
	switch se.Kind {
	case "claude":
		if se.ResumeID == "" || !resumeIDRe.MatchString(se.ResumeID) {
			return ""
		}
		root := c.claudeProjects
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return ""
			}
			root = filepath.Join(home, ".claude", "projects")
		}
		cwd := se.Cwd
		if cwd == "" {
			cwd = c.defaultWd
		}
		return filepath.Join(root, claudeProjectDir(cwd), se.ResumeID+".jsonl")
	}
	return ""
}

// transcriptCache: (path,size,mtime) → the full projection, so a poll on an
// idle live session costs a stat, not a parse.
type transcriptCacheEnt struct {
	size  int64
	mtime int64
	tr    termTranscript
}

var (
	transcriptCacheMu sync.Mutex
	transcriptCache   = map[string]transcriptCacheEnt{}
)

// readTranscript projects the file from byte offset `after` (0 = whole
// file, cached). ok=false when the file does not exist yet — a virgin
// session that has not been started.
func readTranscript(kind, path string, after int64) (termTranscript, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return termTranscript{}, false
	}
	if after <= 0 {
		transcriptCacheMu.Lock()
		e, hit := transcriptCache[path]
		transcriptCacheMu.Unlock()
		if hit && e.size == st.Size() && e.mtime == st.ModTime().UnixNano() {
			return e.tr, true
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return termTranscript{}, false
	}
	defer f.Close()
	if after > 0 {
		if after > st.Size() { // truncated/rotated: start over
			after = 0
		} else if _, err := f.Seek(after, io.SeekStart); err != nil {
			after = 0
			_, _ = f.Seek(0, io.SeekStart)
		}
	}
	tr := parseTranscript(kind, f)
	tr.Offset += after
	if tr.Turns == nil {
		tr.Turns = []termTurn{}
	}
	if after == 0 {
		transcriptCacheMu.Lock()
		if len(transcriptCache) > 64 {
			transcriptCache = map[string]transcriptCacheEnt{}
		}
		transcriptCache[path] = transcriptCacheEnt{size: st.Size(), mtime: st.ModTime().UnixNano(), tr: tr}
		transcriptCacheMu.Unlock()
	}
	return tr, true
}
