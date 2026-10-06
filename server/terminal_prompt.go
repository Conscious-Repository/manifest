package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Screen prompts: a coding CLI's interactive chooser (a Claude Code permission
// dialog, an AskUserQuestion tab, a folder-trust check, a Codex approval) is
// drawn on the pane, not recorded in the transcript. termPrompt is a
// projection of the visible screen — never stored — so chat can show the
// choice and answer it with the same keys a person would press in Terminal.
//
// Answering is guarded twice: the request names the revision it was drawn
// from (any change on screen refuses it), and the cursor is moved and then
// read back before Enter, so a mis-parse can never confirm a different option.
type termPrompt struct {
	Revision string             `json:"revision"`
	Header   string             `json:"header,omitempty"` // "Bash command", "Accessing workspace:"
	Title    string             `json:"title"`
	Body     []termPromptLine   `json:"body,omitempty"`
	Tabs     []termPromptTab    `json:"tabs,omitempty"` // AskUserQuestion progress strip
	Options  []termPromptOption `json:"options"`
	Selected int                `json:"selected"` // the cursor, as a nav index
	Multi    bool               `json:"multi,omitempty"`
}
type termPromptLine struct {
	Text string `json:"text"`
	Code bool   `json:"code,omitempty"`
}
type termPromptTab struct {
	Label string `json:"label"`
	Done  bool   `json:"done,omitempty"`
}
type termPromptOption struct {
	Index    int    `json:"index"`            // position in arrow-key order
	Number   string `json:"number,omitempty"` // as drawn: "1", "2"…
	Label    string `json:"label"`
	Detail   string `json:"detail,omitempty"`
	Checked  *bool  `json:"checked,omitempty"`  // multi-select rows only
	Text     bool   `json:"text,omitempty"`     // "Type something." — the answer is typed into the row
	Followup bool   `json:"followup,omitempty"` // a refusal the agent follows with "what should I do instead?"
	Submit   bool   `json:"submit,omitempty"`   // the multi-select "Submit" row
}

var (
	promptNumbered = regexp.MustCompile(`^(\s*)(?:([❯›>])\s*)?(\d{1,2})\.\s+(.*)$`)
	promptMarked   = regexp.MustCompile(`^(\s*)([❯›])\s+(\S.*)$`)
	promptCheckbox = regexp.MustCompile(`^\[([ ✔✓xX])\]\s*(.*)$`)
	promptShortcut = regexp.MustCompile(`\s+\((?:esc|[a-z])\)$`)
	promptTab      = regexp.MustCompile(`[☐☒✔]\s*[^☐☒✔→]+`)
)

// promptFooter: the hint line every chooser draws under its options.
func promptFooter(line string) bool {
	l := strings.ToLower(line)
	for _, hint := range []string{"esc to cancel", "enter to select", "enter to confirm", "press enter to continue", "press enter to confirm"} {
		if strings.Contains(l, hint) {
			return true
		}
	}
	return false
}

func promptRule(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	for _, r := range t {
		if !strings.ContainsRune("─━╌╍┄┈-═", r) {
			return false
		}
	}
	return true
}

// promptTranscriptLine: a line of the conversation above the dialog (Codex
// draws its approval inline, under "• Running …").
func promptTranscriptLine(line string) bool {
	t := strings.TrimSpace(line)
	// Claude Code fences its dialogs with a rule, so its "●" lines inside one
	// (an AskUserQuestion review) are content, not conversation.
	for _, p := range []string{"• ", "✔ ", "└ ", "╭", "╰", "│"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// promptLogical joins lines the terminal wrapped at its width back into one.
func promptLogical(lines []string) []string {
	width := 0
	for _, l := range lines {
		if n := utf8.RuneCountInString(strings.TrimRight(l, " ")); n > width {
			width = n
		}
	}
	var out []string
	wrapped := false
	for _, l := range lines {
		l = strings.TrimRight(l, " ")
		if wrapped && len(out) > 0 && strings.TrimSpace(l) != "" && !promptRule(l) && !promptNumbered.MatchString(l) {
			out[len(out)-1] += strings.TrimLeft(l, " ")
		} else {
			out = append(out, l)
		}
		wrapped = width >= 40 && utf8.RuneCountInString(l) >= width-1 && !promptRule(l)
	}
	return out
}

func indentOf(line string) int {
	return utf8.RuneCountInString(line) - utf8.RuneCountInString(strings.TrimLeft(line, " "))
}

// parseTermPrompt reads a chooser off the visible screen, or nil.
func parseTermPrompt(raw []string) *termPrompt {
	lines := promptLogical(raw)
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	footer := -1
	for i, seen := len(lines)-1, 0; i >= 0 && seen < 4; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		seen++
		if promptFooter(lines[i]) {
			footer = i
			break
		}
	}
	if footer < 0 && len(lines) > 0 && promptNumbered.MatchString(lines[len(lines)-1]) {
		// AskUserQuestion's review step draws no hint: a numbered list that
		// ends the screen (the cursor check below still has to pass)
		footer = len(lines)
	}
	if footer < 0 {
		return nil
	}
	// The option block: walk up from the footer to the first option.
	type row struct {
		line, indent int
		opt          termPromptOption
		marked       bool
	}
	var rows []row
	first := -1
	for i := footer - 1; i >= 0 && footer-i <= 40; i-- {
		l := lines[i]
		if m := promptNumbered.FindStringSubmatch(l); m != nil {
			rows = append(rows, row{line: i, indent: utf8.RuneCountInString(m[1]), marked: m[2] != "", opt: termPromptOption{Number: m[3], Label: m[4]}})
			first = i
			if m[3] == "1" {
				break
			}
		}
	}
	if first < 0 {
		// An unnumbered chooser (Claude's folder trust): the marked row and
		// its neighbours at the same indent.
		marked := -1
		for i := footer - 1; i >= 0 && footer-i <= 12; i-- {
			if promptMarked.MatchString(lines[i]) {
				marked = i
				break
			}
		}
		if marked < 0 {
			return nil
		}
		m := promptMarked.FindStringSubmatch(lines[marked])
		col := utf8.RuneCountInString(lines[marked]) - utf8.RuneCountInString(m[3])
		at := func(i int) (string, bool) {
			l := lines[i]
			if mm := promptMarked.FindStringSubmatch(l); mm != nil {
				return mm[3], true
			}
			if strings.TrimSpace(l) != "" && indentOf(l) == col {
				return strings.TrimSpace(l), false
			}
			return "", false
		}
		lo, hi := marked, marked
		for lo > 0 {
			if label, _ := at(lo - 1); label == "" {
				break
			}
			lo--
		}
		for hi < footer-1 {
			if label, _ := at(hi + 1); label == "" {
				break
			}
			hi++
		}
		for i := lo; i <= hi; i++ {
			label, isMarked := at(i)
			rows = append(rows, row{line: i, marked: isMarked, opt: termPromptOption{Label: label}})
		}
		first = lo
	} else {
		// collected bottom-up; put them in screen order
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	p := &termPrompt{Selected: -1}
	numbered := rows[0].opt.Number != ""
	// Walk the block top-down: options, their description lines, the
	// unnumbered "Submit" row of a multi-select, separators.
	idx := 0
	add := func(r row) {
		o := r.opt
		o.Index = idx
		o.Label = strings.TrimSpace(promptShortcut.ReplaceAllString(o.Label, ""))
		if m := promptCheckbox.FindStringSubmatch(o.Label); m != nil {
			checked := m[1] != " "
			o.Checked, o.Label, p.Multi = &checked, strings.TrimSpace(m[2]), true
		}
		lower := strings.ToLower(strings.TrimSuffix(o.Label, "."))
		o.Text = lower == "type something" || lower == "other"
		o.Followup = lower == "no" || strings.Contains(lower, "what to do differently") || strings.Contains(lower, "what to do instead")
		if r.marked {
			p.Selected = idx
		}
		p.Options = append(p.Options, o)
		idx++
	}
	if numbered {
		next := 0
		for i := rows[0].line; i < footer; i++ {
			l := lines[i]
			if next < len(rows) && rows[next].line == i {
				add(rows[next])
				next++
				continue
			}
			t := strings.TrimSpace(l)
			if t == "" || promptRule(l) || len(p.Options) == 0 {
				continue
			}
			if mm := promptMarked.FindStringSubmatch(l); mm != nil || (p.Multi && t == "Submit") {
				label := t
				if mm != nil {
					label = mm[3]
				}
				add(row{marked: mm != nil, opt: termPromptOption{Label: label}})
				p.Options[len(p.Options)-1].Submit = label == "Submit"
				continue
			}
			last := &p.Options[len(p.Options)-1]
			if last.Detail != "" {
				last.Detail += " "
			}
			last.Detail += t
		}
	} else {
		for _, r := range rows {
			add(r)
		}
	}
	if len(p.Options) < 2 || p.Selected < 0 {
		return nil
	}
	// The question and its context: up from the first option to a rule, the
	// conversation, or a run of blank lines.
	var body []string
	blank := 0
	for i := first - 1; i >= 0 && first-i <= 30; i-- {
		l := lines[i]
		if promptRule(l) && !strings.Contains(l, "╌") {
			break
		}
		if strings.TrimSpace(l) == "" {
			if blank++; blank >= 3 && len(body) > 0 {
				break
			}
			if blank == 1 && len(body) > 0 {
				body = append([]string{""}, body...) // a paragraph break
			}
			continue
		}
		blank = 0
		if promptTranscriptLine(l) {
			break
		}
		body = append([]string{l}, body...)
	}
	code, joinable, prevLen := false, false, 0
	var text []termPromptLine
	for _, l := range body {
		t := strings.TrimSpace(l)
		if t == "" {
			joinable = false
			continue
		}
		switch {
		case strings.Contains(l, "╌"):
			code = !code
			continue
		case strings.HasPrefix(t, "←") || strings.HasPrefix(t, "☐") || strings.HasPrefix(t, "☒"):
			for _, part := range promptTab.FindAllString(t, -1) {
				if strings.HasSuffix(strings.TrimSpace(part), "Submit") {
					continue
				}
				p.Tabs = append(p.Tabs, termPromptTab{Label: strings.TrimSpace(strings.TrimLeft(part, "☐☒✔ ")), Done: !strings.HasPrefix(part, "☐")})
			}
			continue
		case strings.HasPrefix(t, "Tip:"):
			continue
		}
		line := termPromptLine{Text: t, Code: code || strings.HasPrefix(t, "$ ")}
		long := prevLen >= 60
		prevLen = utf8.RuneCountInString(l)
		if n := len(text); joinable && long && !line.Code && !text[n-1].Code && !strings.ContainsAny(text[n-1].Text[len(text[n-1].Text)-1:], ".:?!)") && !strings.HasPrefix(t, "●") && !strings.HasPrefix(t, "→") {
			// a sentence the dialog wrapped short of the screen edge
			text[n-1].Text += " " + t
			continue
		}
		text = append(text, line)
		joinable = true
	}
	if len(text) == 0 {
		return nil
	}
	// The title is the last question asked; a short first line above it
	// that is not code is the dialog's header.
	title := -1
	for i := len(text) - 1; i >= 0; i-- {
		if !text[i].Code && strings.HasSuffix(text[i].Text, "?") {
			title = i
			break
		}
	}
	if title < 0 {
		// a question run on into its explanation: split at the first "? "
		title = 0
		for i, l := range text {
			if at := strings.Index(l.Text, "? "); at > 0 && !l.Code {
				title = i
				text = append(text[:i+1], append([]termPromptLine{{Text: strings.TrimSpace(l.Text[at+1:])}}, text[i+1:]...)...)
				text[i].Text = l.Text[:at+1]
				break
			}
		}
	}
	p.Title = text[title].Text
	rest := append(append([]termPromptLine(nil), text[:title]...), text[title+1:]...)
	if len(rest) > 0 && title > 0 && !rest[0].Code && utf8.RuneCountInString(rest[0].Text) <= 40 && !strings.HasPrefix(rest[0].Text, "●") && !strings.HasSuffix(rest[0].Text, "?") {
		p.Header, rest = rest[0].Text, rest[1:]
	}
	p.Body = rest
	identity, _ := json.Marshal([]any{p.Header, p.Title, p.Body, p.Tabs, p.Options})
	p.Revision = hashTerminalText(string(identity))[:16]
	return p
}

// ---- runtime ----

// terminalScreenPrompt reads the pane's chooser for a live herdr session.
func (s *Server) terminalScreenPrompt(ctx context.Context, se termSession) (*termPrompt, []string, error) {
	if se.backend() != "herdr" || s.terminal.herdr == nil || se.Device != "" {
		return nil, nil, errors.New("screen prompts need a local herdr session")
	}
	lines, err := s.terminal.herdr.Screen(ctx, se.Runtime)
	if err != nil {
		return nil, nil, err
	}
	return parseTermPrompt(lines), lines, nil
}

func (h *herdrTerminalRuntime) sendKeys(ctx context.Context, id terminalIdentity, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := h.checked(ctx, id); err != nil {
		return err
	}
	_, err := h.callGeneration(ctx, "pane.send_keys", map[string]any{"pane_id": id.Pane, "keys": keys}, id.Generation)
	return err
}

func (h *herdrTerminalRuntime) typeText(ctx context.Context, id terminalIdentity, text string) error {
	if err := h.checked(ctx, id); err != nil {
		return err
	}
	_, err := h.callGeneration(ctx, "pane.send_input", map[string]any{"pane_id": id.Pane, "text": text}, id.Generation)
	return err
}

type termPromptAnswer struct {
	Revision string `json:"revision"`
	Option   int    `json:"option"`
	Checked  []int  `json:"checked,omitempty"` // multi-select: the rows to leave ticked
	Text     string `json:"text,omitempty"`    // a "Type something." answer
	Dismiss  bool   `json:"dismiss,omitempty"` // Esc
}

var errPromptChanged = errors.New("the prompt changed on screen; nothing was confirmed — review it again")

const promptSettle = 250 * time.Millisecond

func promptPause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// promptSame: the same dialog (cursor and ticks aside).
func promptSame(a, b *termPrompt) bool {
	if a == nil || b == nil || a.Title != b.Title || len(a.Options) != len(b.Options) {
		return false
	}
	for i := range a.Options {
		if a.Options[i].Label != b.Options[i].Label && !a.Options[i].Text {
			return false
		}
	}
	return true
}

// promptMoveTo moves the cursor to target and reads it back there.
func (s *Server) promptMoveTo(ctx context.Context, se termSession, p *termPrompt, target int) (*termPrompt, error) {
	h := s.terminal.herdr
	for attempt := 0; attempt < 3; attempt++ {
		if p.Selected == target {
			return p, nil
		}
		key, n := "down", target-p.Selected
		if n < 0 {
			key, n = "up", -n
		}
		keys := make([]string, n)
		for i := range keys {
			keys[i] = key
		}
		if err := h.sendKeys(ctx, se.Runtime, keys); err != nil {
			return nil, err
		}
		if err := promptPause(ctx, promptSettle); err != nil {
			return nil, err
		}
		next, _, err := s.terminalScreenPrompt(ctx, se)
		if err != nil {
			return nil, err
		}
		if !promptSame(p, next) {
			return nil, errPromptChanged
		}
		p = next
	}
	if p.Selected != target {
		return nil, errors.New("could not move to that option; nothing was confirmed — open Terminal to answer")
	}
	return p, nil
}

// answerTermPrompt drives the chooser. Called under the session input lock.
func (s *Server) answerTermPrompt(ctx context.Context, se termSession, a termPromptAnswer) error {
	h := s.terminal.herdr
	p, _, err := s.terminalScreenPrompt(ctx, se)
	if err != nil {
		return err
	}
	if p == nil || p.Revision != a.Revision {
		return errPromptChanged
	}
	if a.Dismiss {
		return h.sendKeys(ctx, se.Runtime, []string{"esc"})
	}
	if a.Option < 0 || a.Option >= len(p.Options) {
		return errBadRequest("no such option")
	}
	if p.Multi {
		want := map[int]bool{}
		for _, i := range a.Checked {
			if i < 0 || i >= len(p.Options) || p.Options[i].Checked == nil || p.Options[i].Text {
				return errBadRequest("not a selectable row")
			}
			want[i] = true
		}
		for _, o := range p.Options {
			if o.Checked == nil || o.Text || *o.Checked == want[o.Index] {
				continue
			}
			if p, err = s.promptMoveTo(ctx, se, p, o.Index); err != nil {
				return err
			}
			if err = h.sendKeys(ctx, se.Runtime, []string{"enter"}); err != nil {
				return err
			}
			if err = promptPause(ctx, promptSettle); err != nil {
				return err
			}
			next, _, err := s.terminalScreenPrompt(ctx, se)
			if err != nil {
				return err
			}
			if !promptSame(p, next) || next.Options[o.Index].Checked == nil || *next.Options[o.Index].Checked != want[o.Index] {
				return errors.New("a choice did not register; nothing was submitted — review it again")
			}
			p = next
		}
		submit := -1
		for _, o := range p.Options {
			if o.Submit {
				submit = o.Index
			}
		}
		if submit < 0 {
			return errors.New("this chooser has no Submit row; open Terminal to answer")
		}
		a.Option = submit
	}
	target := p.Options[a.Option]
	if target.Text && strings.TrimSpace(a.Text) == "" {
		return errBadRequest("type an answer first")
	}
	if strings.ContainsAny(a.Text, "\r\n") || len(a.Text) > 4000 {
		return errBadRequest("a typed answer is one line of at most 4000 bytes")
	}
	if p, err = s.promptMoveTo(ctx, se, p, a.Option); err != nil {
		return err
	}
	if target.Text {
		if err = h.typeText(ctx, se.Runtime, a.Text); err != nil {
			return err
		}
		if err = promptPause(ctx, 300*time.Millisecond); err != nil {
			return err
		}
	}
	return h.sendKeys(ctx, se.Runtime, []string{"enter"})
}

// handleTermPrompt (GET/POST /api/terminal/session/{id}/prompt): GET is the
// chooser on screen (null when none); POST answers it → the next one.
func (s *Server) handleTermPrompt(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); r.Method == http.MethodPost && o != "" && !sameOrigin(o, r.Host) {
		http.Error(w, "cross-origin refused", http.StatusForbidden)
		return
	}
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if se.backend() != "herdr" || se.Device != "" || s.terminal.herdr == nil {
		http.Error(w, "this runtime's prompts need Terminal", http.StatusConflict)
		return
	}
	if r.Method != http.MethodPost {
		p, _, err := s.terminalScreenPrompt(r.Context(), se)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{"prompt": p})
		return
	}
	if s.terminalSharedConversation(se) != nil {
		http.Error(w, "a shared conversation's prompts are answered by its owner in Terminal", http.StatusForbidden)
		return
	}
	release, allowed := s.guardTerminalShare(w, se)
	if !allowed {
		return
	}
	defer release()
	var a termPromptAnswer
	if err := decode(r, &a); err != nil {
		httpError(w, err)
		return
	}
	mu := s.termInputMutex(se.ID)
	mu.Lock()
	defer mu.Unlock()
	if err := s.answerTermPrompt(r.Context(), se, a); err != nil {
		var bad badRequest
		switch {
		case errors.As(err, &bad):
			httpError(w, err)
		default:
			http.Error(w, err.Error(), http.StatusConflict)
		}
		return
	}
	// what the screen shows next: another tab, a review, or nothing
	_ = promptPause(r.Context(), 450*time.Millisecond)
	next, _, _ := s.terminalScreenPrompt(r.Context(), se)
	writeJSON(w, map[string]any{"ok": true, "prompt": next})
}
