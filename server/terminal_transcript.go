package server

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	ID  string `json:"id"`
	Who string `json:"who"`
	TS  string `json:"ts,omitempty"`
	End string `json:"end,omitempty"` // assistant: the latest record time in this turn ("Worked for …")
	// Run is the provider run (codex turn_id) an assistant turn belongs to,
	// when the transcript records one. Two runs never share a turn, so a
	// goal continuation days later does not stretch the last reply's time.
	Run    string      `json:"run,omitempty"`
	Text   string      `json:"text,omitempty"`
	Blocks []termBlock `json:"blocks,omitempty"`
	// WorkOrder marks a board run's launch prompt whose Text has been replaced
	// by the work order it pointed at (boardTranscriptOverlay)
	WorkOrder bool `json:"workOrder,omitempty"`
}

// termBlock: t = say (markdown text) | think (thinking text) | step (a tool
// call: cast = tool name, input = one-line summary, result = the paired
// tool_result, trimmed; error when the tool reported one). ID is the tool_use
// id: a result whose call fell before ?after= arrives as a step with cast
// "result" and the same id, so the tailing client can pair it with the chip
// it already painted.
type termBlock struct {
	Done   bool   `json:"done,omitempty"`
	T      string `json:"t"`
	Text   string `json:"text,omitempty"`
	Cast   string `json:"cast,omitempty"`
	Input  string `json:"input,omitempty"`
	Result string `json:"result,omitempty"`
	Error  bool   `json:"error,omitempty"`
	ID     string `json:"id,omitempty"`
}

// terminalRunEvidence is derived only from explicit provider lifecycle records.
// IDs bind to the provider turn or immutable source record, never screen text.
type terminalRunEvidence struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	At       string `json:"at"`
	Evidence string `json:"evidence"`
	// Error is the runtime's own message when State is failed (a board run
	// the CLI abandoned before its rollout could say so)
	Error string `json:"error,omitempty"`
}

// termTranscript is the projection of one session file (or its tail).
type termTranscript struct {
	Questions []terminalQuestion `json:"questions,omitempty"`
	// Set by the live projection, not the parser cache. An unreadable history
	// must not be mistaken for an empty conversation during sharing review.
	Run       *terminalRunEvidence `json:"run,omitempty"`
	Available bool                 `json:"-"`
	Turns     []termTurn           `json:"turns"`
	Title     string               `json:"title,omitempty"` // claude ai-title
	// Settings the CLI itself recorded most recently in this read: the model,
	// effort and permission the session is actually running with. Absent
	// means this read saw no record of them, never "default".
	Settings *termSettings `json:"settings,omitempty"`
	// Context is the latest token accounting the CLI recorded: tokens in the
	// model's context for its last call, and the window when the CLI names it
	// (Codex does; Claude Code does not, so Window stays 0 — unknown).
	Context *termContext `json:"context,omitempty"`
	Cost    float64      `json:"cost,omitempty"` // claude cost-state totalCostUSD
	// Offset is the byte offset just past the last COMPLETE line parsed —
	// pass it back as ?after= to receive only newer records.
	Offset int64 `json:"offset"`
}

type termContext struct {
	Used   int    `json:"used"`
	Window int    `json:"window,omitempty"`
	At     string `json:"at,omitempty"`
}

type termSettings struct {
	// First is the first model this session recorded: what its launch alias
	// resolved to, before any /model switch (chat_models.go claudeLastRan).
	First      string `json:"first,omitempty"`
	FirstAt    string `json:"firstAt,omitempty"`
	Model      string `json:"model,omitempty"`
	Effort     string `json:"effort,omitempty"`
	Permission string `json:"permission,omitempty"`
	At         string `json:"at,omitempty"`
}

// observe folds one record's settings into the latest known set.
func (b *transcriptBuilder) observe(ts, model, effort, permission string) {
	if model == "" && effort == "" && permission == "" {
		return
	}
	if b.out.Settings == nil {
		b.out.Settings = &termSettings{}
	}
	st := b.out.Settings
	if model != "" && st.First == "" {
		st.First, st.FirstAt = model, ts
	}
	if model != "" {
		st.Model = model
	}
	if effort != "" {
		st.Effort = effort
	}
	if permission != "" {
		st.Permission = permission
	}
	if ts != "" {
		st.At = ts
	}
}

const (
	termStepResultMax = 1500 // chars of a tool result kept as step detail
	termStepInputMax  = 200  // chars of the one-line input summary
)

// --- claude ---

type claudeRecord struct {
	Type         string          `json:"type"`
	Timestamp    string          `json:"timestamp"`
	IsMeta       bool            `json:"isMeta"`
	IsSidechain  bool            `json:"isSidechain"`
	Message      json.RawMessage `json:"message"`
	AITitle      string          `json:"aiTitle"`
	TotalCost    float64         `json:"totalCostUSD"`
	PromptSource string          `json:"promptSource"`   // "system" when the harness wrote the user turn; "typed" for the owner
	Effort       string          `json:"effort"`         // assistant rows: the effort this reply ran at
	Permission   string          `json:"permissionMode"` // permission-mode rows
}

type claudeMessage struct {
	Usage *struct {
		Input         int `json:"input_tokens"`
		CacheCreation int `json:"cache_creation_input_tokens"`
		CacheRead     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
	Model      string          `json:"model"`
	StopReason string          `json:"stop_reason"`
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string | []block
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
	out      termTranscript
	recordID string
	// runKeyed: the format records run lifecycle events (codex task_started),
	// so out.Run.ID names the run the next records belong to
	runKeyed bool
}

func (b *transcriptBuilder) record(line []byte, base []int64) {
	offset := b.out.Offset
	if len(base) > 0 {
		offset += base[0]
	}
	hash := sha256.Sum256(line)
	b.recordID = fmt.Sprintf("%x-%x", offset, hash[:8])
}

func (b *transcriptBuilder) assistant(ts string) *termTurn {
	run := ""
	if b.runKeyed && b.out.Run != nil {
		run = b.out.Run.ID
	}
	if n := len(b.out.Turns); n > 0 && b.out.Turns[n-1].Who == "assistant" && sameRun(b.out.Turns[n-1].Run, run) {
		t := &b.out.Turns[n-1]
		t.touch(ts)
		if t.Run == "" {
			t.Run = run
		}
		return t
	}
	b.out.Turns = append(b.out.Turns, termTurn{ID: b.recordID, Who: "assistant", TS: ts, End: ts, Run: run})
	return &b.out.Turns[len(b.out.Turns)-1]
}

// sameRun: records join the open reply unless both name a run and the runs
// differ (a goal continuation, a harness-started turn with no owner text).
func sameRun(a, b string) bool { return a == "" || b == "" || a == b }

// touch extends an assistant turn's end to a later record time.
func (t *termTurn) touch(ts string) {
	if ts != "" && t.TS == "" { // opened by a synthetic row: starts at the first real one
		t.TS = ts
	}
	if ts != "" && ts > t.End {
		t.End = ts
	}
}

func (b *transcriptBuilder) user(ts, text string) {
	b.out.Turns = append(b.out.Turns, termTurn{ID: b.recordID, Who: "user", TS: ts, Text: text})
}

func (b *transcriptBuilder) system(ts, text string) {
	b.out.Turns = append(b.out.Turns, termTurn{ID: b.recordID, Who: "system", TS: ts, Text: text})
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
				// a late result (a resumed session closing a dangling call, a
				// background task reporting after the owner spoke again) marks
				// the step done; only the open reply's time runs on
				if ti == len(b.out.Turns)-1 {
					t.touch(ts)
				}
				t.Blocks[bi].Result = clip(text, termStepResultMax)
				t.Blocks[bi].Error = isErr
				t.Blocks[bi].Done = true
				return
			}
		}
	}
	// a result whose call fell before ?after= — still worth a chip; the id
	// lets a tailing client pair it with the step it already holds
	t := b.assistant(ts)
	t.Blocks = append(t.Blocks, termBlock{T: "step", Cast: "result", Done: true, Result: clip(text, termStepResultMax), Error: isErr, ID: id})
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
func parseClaudeTranscript(r io.Reader, base ...int64) termTranscript {
	b := &transcriptBuilder{}
	b.parseClaude(r, base)
	return b.out
}

// parseClaude feeds the records in r to the builder, which may already hold
// the projection of the bytes before them (readTranscript's resume).
func (b *transcriptBuilder) parseClaude(r io.Reader, base []int64) {
	scanLines(r, &b.out.Offset, func(line []byte) {
		b.record(line, base)
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
		case "permission-mode":
			b.observe(rec.Timestamp, "", "", rec.Permission)
		case "user", "assistant":
			if rec.IsMeta || rec.IsSidechain || len(rec.Message) == 0 {
				return
			}
			var m claudeMessage
			if json.Unmarshal(rec.Message, &m) != nil {
				return
			}
			blocks, text := claudeContent(m.Content, rec.Type == "user")
			if rec.Type == "assistant" && !strings.HasPrefix(m.Model, "<") {
				b.observe(rec.Timestamp, m.Model, rec.Effort, "")
				if u := m.Usage; u != nil && u.Input+u.CacheCreation+u.CacheRead > 0 {
					b.out.Context = &termContext{Used: u.Input + u.CacheCreation + u.CacheRead, At: rec.Timestamp}
				}
			}
			if rec.Type == "assistant" {
				state := "running"
				if m.StopReason == "end_turn" {
					state = "completed"
				}
				b.out.Run = &terminalRunEvidence{ID: b.recordID, State: state, At: rec.Timestamp, Evidence: b.recordID}
			}
			if rec.Type == "user" && rec.PromptSource == "system" {
				// the harness speaking in the user's slot (a background task's
				// notification, a scheduled wake-up): a system line, not a bubble.
				// Owner turns are promptSource "typed" (origin kind "human") or
				// carry neither field — those stay user turns.
				if text == "" {
					for _, bl := range blocks {
						if bl.Type == "text" {
							text = bl.Text
							break
						}
					}
				}
				if n := claudeSystemNotice(text); n != "" {
					b.system(rec.Timestamp, n)
				}
				return
			}
			if rec.Type == "user" {
				if strings.TrimSpace(text) != "" && !claudeNoiseRe.MatchString(text) {
					b.user(rec.Timestamp, text)
					b.out.Run = &terminalRunEvidence{ID: b.recordID, State: "running", At: rec.Timestamp, Evidence: b.recordID}
				}
				for _, bl := range blocks {
					if bl.Type == "tool_result" {
						_, rt := claudeContent(bl.Content)
						b.result(rec.Timestamp, bl.ToolUseID, rt, bl.IsError)
					} else if bl.Type == "text" && strings.TrimSpace(bl.Text) != "" && !claudeNoiseRe.MatchString(bl.Text) {
						b.user(rec.Timestamp, bl.Text)
						b.out.Run = &terminalRunEvidence{ID: b.recordID, State: "running", At: rec.Timestamp, Evidence: b.recordID}
					}
				}
				return
			}
			// "<synthetic>" rows are the CLI speaking, not the model: resuming
			// a session writes "No response requested." onto the old reply
			// (dropped), and no synthetic row extends a reply's worked time
			ts := rec.Timestamp
			if m.Model == "<synthetic>" {
				if strings.TrimSpace(text) == "No response requested." {
					return
				}
				ts = ""
			}
			if text != "" {
				b.text(ts, "say", text)
			}
			for _, bl := range blocks {
				switch bl.Type {
				case "text":
					b.text(ts, "say", bl.Text)
				case "thinking":
					b.text(ts, "think", bl.Thinking)
				case "tool_use":
					b.step(ts, bl.ID, bl.Name, toolInputSummary(bl.Name, bl.Input))
				}
			}
		}
	})
}

// claudeSystemNotice reduces a harness-written user turn to one readable
// line: a <task-notification> to its summary (falling back to its status),
// anything else to its text with the tags stripped, clipped.
var (
	claudeTagRe     = regexp.MustCompile(`<[^>\n]{1,60}>`)
	claudeSummaryRe = regexp.MustCompile(`(?s)<summary>\s*(.*?)\s*</summary>`)
	claudeStatusRe  = regexp.MustCompile(`(?s)<status>\s*(.*?)\s*</status>`)
)

func claudeSystemNotice(text string) string {
	if strings.Contains(text, "<task-notification>") {
		if m := claudeSummaryRe.FindStringSubmatch(text); m != nil && strings.TrimSpace(m[1]) != "" {
			return clip(strings.Join(strings.Fields(m[1]), " "), 400)
		}
		if m := claudeStatusRe.FindStringSubmatch(text); m != nil {
			return "Background task " + strings.TrimSpace(m[1])
		}
		return "Background task update"
	}
	plain := strings.Join(strings.Fields(claudeTagRe.ReplaceAllString(text, " ")), " ")
	return clip(plain, 400)
}

// claudeNoiseRe: slash-command echo rows the CLI writes as user text.
var claudeNoiseRe = regexp.MustCompile(`^\s*<(command-name|command-message|local-command-stdout|local-command-caveat|system-reminder)>`)

// claudeContent splits a content field: a bare string → text; an array →
// its blocks, with the text blocks' text also joined for convenience when
// the array is nothing but text (a user turn with pasted parts).
func claudeContent(raw json.RawMessage, preserve ...bool) ([]claudeBlock, string) {
	// Owner text participates in exact delivery-receipt matching. Keep its
	// whitespace as recorded; formatting cleanup belongs to the renderer.
	keep := len(preserve) > 0 && preserve[0]
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		if !keep {
			s = strings.TrimSpace(s)
		}
		return nil, s
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
			if keep {
				t = bl.Text
			}
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
		TurnID  string `json:"turn_id"`
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
		// token_count events: the last call's context and the model's window
		Info *struct {
			Last struct {
				Input int `json:"input_tokens"`
			} `json:"last_token_usage"`
			Window int `json:"model_context_window"`
		} `json:"info"`
		// turn_context rows: what the turn actually ran with
		Model          string `json:"model"`
		Effort         string `json:"effort"`
		ApprovalPolicy string `json:"approval_policy"`
		SandboxPolicy  struct {
			Type string `json:"type"`
		} `json:"sandbox_policy"`
		Collaboration struct {
			Settings struct {
				Effort *string `json:"reasoning_effort"`
			} `json:"settings"`
		} `json:"collaboration_mode"`
	} `json:"payload"`
}

// codexPermission names a turn's sandbox/approval pair with the chat's
// access presets (chat_models.go), or the raw pair when it is neither.
func codexPermission(approval, sandbox string) string {
	switch {
	case sandbox == "danger-full-access" && approval == "never":
		return "full"
	case sandbox == "workspace-write" && approval == "on-request":
		return "auto"
	case sandbox == "read-only" && approval == "on-request":
		return "read-only"
	case approval == "" && sandbox == "":
		return ""
	}
	return strings.Trim(sandbox+" · "+approval, " ·")
}

// parseCodexTranscript projects a codex rollout: response_item.message by
// role (developer rows and the CLI's own <…> boilerplate user rows skipped),
// custom_tool_call / function_call as steps paired with their _output,
// reasoning summaries as thinking. Duplicate event messages are ignored; explicit
// task lifecycle events provide separate run evidence.
func parseCodexTranscript(r io.Reader, base ...int64) termTranscript {
	b := &transcriptBuilder{}
	b.parseCodex(r, base)
	return b.out
}

// parseCodex feeds the records in r to the builder, which may already hold
// the projection of the bytes before them (readTranscript's resume).
func (b *transcriptBuilder) parseCodex(r io.Reader, base []int64) {
	b.runKeyed = true
	scanLines(r, &b.out.Offset, func(line []byte) {
		b.record(line, base)
		var rec codexRecord
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		p := rec.Payload
		if rec.Type == "turn_context" {
			effort := p.Effort
			if effort == "" && p.Collaboration.Settings.Effort != nil {
				effort = *p.Collaboration.Settings.Effort
			}
			b.observe(rec.Timestamp, p.Model, effort, codexPermission(p.ApprovalPolicy, p.SandboxPolicy.Type))
			return
		}
		if rec.Type == "event_msg" {
			if p.Type == "token_count" && p.Info != nil && p.Info.Last.Input > 0 {
				b.out.Context = &termContext{Used: p.Info.Last.Input, Window: p.Info.Window, At: rec.Timestamp}
				return
			}
			switch p.Type {
			case "task_started":
				id := p.TurnID
				if id == "" {
					id = b.recordID
				}
				b.out.Run = &terminalRunEvidence{ID: id, State: "running", At: rec.Timestamp, Evidence: b.recordID}
			case "task_complete":
				if p.TurnID != "" && b.out.Run != nil && b.out.Run.ID != p.TurnID {
					return
				}
				id := p.TurnID
				if id == "" && b.out.Run != nil {
					id = b.out.Run.ID
				}
				if id == "" {
					id = b.recordID
				}
				b.out.Run = &terminalRunEvidence{ID: id, State: "completed", At: rec.Timestamp, Evidence: b.recordID}
			}
			return
		}
		if rec.Type != "response_item" {
			return
		}
		switch p.Type {
		case "message":
			var parts []string
			for _, c := range p.Content {
				if t := strings.TrimSpace(c.Text); t != "" {
					if p.Role == "user" {
						t = c.Text // preserve exact owner input for delivery receipts
					}
					parts = append(parts, t)
				}
			}
			text := strings.Join(parts, "\n\n")
			if text == "" {
				return
			}
			switch p.Role {
			case "user":
				if replies := parseQuestionReply(text); len(replies) > 0 {
					for _, reply := range replies {
						for i := range b.out.Questions {
							if b.out.Questions[i].ID == reply.ID {
								b.out.Questions[i].State = "answered"
								b.out.Questions[i].Answer = reply.Answer
							}
						}
					}
					b.user(rec.Timestamp, text)
				} else if !strings.HasPrefix(strings.TrimSpace(text), "<") { // <recommended_plugins>, <environment_context>, …
					b.user(rec.Timestamp, text)
					if b.out.Run != nil && b.out.Run.State == "completed" {
						b.out.Run = &terminalRunEvidence{ID: b.recordID, State: "unknown", At: rec.Timestamp, Evidence: b.recordID}
					}
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
			b.out.Questions = append(b.out.Questions, projectQuestionCall(p.Name, p.CallID, in)...)
			b.step(rec.Timestamp, p.CallID, p.Name, clip(strings.Join(strings.Fields(in), " "), termStepInputMax))
		case "custom_tool_call_output", "function_call_output":
			b.result(rec.Timestamp, p.CallID, codexOutputText(p.Output), false)
		}
	})
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
func parseTranscript(kind string, r io.Reader, base ...int64) termTranscript {
	if kind == "codex" {
		return parseCodexTranscript(r, base...)
	}
	return parseClaudeTranscript(r, base...)
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
// kind has no discoverable exact conversation identity.
func (c *termCfg) transcriptPath(se termSession) string {
	if se.isDraft() {
		return ""
	}
	if se.Device != "" {
		return ""
	}
	switch se.Kind {
	case "codex":
		return c.codexTranscriptPath(se)
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
// idle live session costs a stat, not a parse. A session that is writing
// grows its file by appending records, so a changed file whose bytes up to
// the cached offset are provably the same resumes the projection from that
// offset instead of re-reading tens of MB (2026-09-27: the inbox re-parsed
// 117 MB of transcripts whenever one of them grew — 1.5 s a poll).
type transcriptCacheEnt struct {
	size  int64
	mtime int64
	file  os.FileInfo // same file, not a replacement at the same path
	kind  string
	tail  []byte // the bytes just before tr.Offset, as the resume proof
	tr    termTranscript
}

// transcriptTailProof bytes before the offset must match for a resume: a
// rewrite that keeps the same inode and grows the file changes them.
const transcriptTailProof = 4096

var (
	transcriptCacheMu sync.Mutex
	transcriptCache   = map[string]transcriptCacheEnt{}
	// transcriptParsedBytes counts bytes fed to a full-file projection
	// (readTranscript after=0), for the budget test.
	transcriptParsedBytes atomic.Int64
)

// readTranscript projects the file from byte offset `after` (0 = whole
// file, cached). ok=false when the file does not exist yet — a virgin
// session that has not been started.
func readTranscript(kind, path string, after int64) (termTranscript, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return termTranscript{}, false
	}
	if after < 0 { // a negative offset means the whole file, as a cache hit always answered
		after = 0
	}
	var resume *transcriptCacheEnt
	if after == 0 {
		transcriptCacheMu.Lock()
		e, hit := transcriptCache[path]
		transcriptCacheMu.Unlock()
		if hit && e.size == st.Size() && e.mtime == st.ModTime().UnixNano() {
			return e.tr, true
		}
		if hit && e.kind == kind && os.SameFile(e.file, st) && st.Size() >= e.tr.Offset {
			resume = &e
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return termTranscript{}, false
	}
	defer f.Close()
	if resume != nil && !transcriptPrefixSame(f, resume.tr.Offset, resume.tail) {
		resume = nil
	}
	if after > 0 {
		if after > st.Size() { // truncated/rotated: start over
			after = 0
		} else if _, err := f.Seek(after, io.SeekStart); err != nil {
			after = 0
			_, _ = f.Seek(0, io.SeekStart)
		}
	}
	var tr termTranscript
	if after <= 0 {
		b := &transcriptBuilder{}
		if resume != nil {
			b.out = cloneTranscript(resume.tr) // the cached value is shared with earlier callers
		}
		if _, err := f.Seek(b.out.Offset, io.SeekStart); err != nil {
			b = &transcriptBuilder{}
			_, _ = f.Seek(0, io.SeekStart)
		}
		from := b.out.Offset
		if kind == "codex" {
			b.parseCodex(f, nil)
		} else {
			b.parseClaude(f, nil)
		}
		transcriptParsedBytes.Add(b.out.Offset - from)
		tr = b.out
	} else {
		tr = parseTranscript(kind, f, after)
		tr.Offset += after
	}
	if tr.Turns == nil {
		tr.Turns = []termTurn{}
	}
	if after == 0 {
		tail := make([]byte, min(tr.Offset, transcriptTailProof))
		if _, err := f.ReadAt(tail, tr.Offset-int64(len(tail))); err != nil {
			tail = nil
		}
		transcriptCacheMu.Lock()
		if len(transcriptCache) > 64 {
			transcriptCache = map[string]transcriptCacheEnt{}
		}
		if tail != nil {
			transcriptCache[path] = transcriptCacheEnt{size: st.Size(), mtime: st.ModTime().UnixNano(), file: st, kind: kind, tail: tail, tr: tr}
		}
		transcriptCacheMu.Unlock()
	}
	return tr, true
}

// transcriptPrefixSame reports whether the bytes just before offset are
// still the ones the cached projection read.
func transcriptPrefixSame(f *os.File, offset int64, tail []byte) bool {
	if int64(len(tail)) > offset {
		return false
	}
	got := make([]byte, len(tail))
	if _, err := f.ReadAt(got, offset-int64(len(tail))); err != nil {
		return false
	}
	return bytes.Equal(got, tail)
}

// cloneTranscript deep-copies what a builder mutates in place: the turns and
// their blocks (touch, result pairing, text merging) and the settings.
func cloneTranscript(tr termTranscript) termTranscript {
	out := tr
	out.Turns = make([]termTurn, len(tr.Turns))
	for i, t := range tr.Turns {
		t.Blocks = append([]termBlock(nil), t.Blocks...)
		out.Turns[i] = t
	}
	if tr.Settings != nil {
		st := *tr.Settings
		out.Settings = &st
	}
	if tr.Context != nil {
		c := *tr.Context
		out.Context = &c
	}
	if tr.Run != nil {
		r := *tr.Run
		out.Run = &r
	}
	return out
}
