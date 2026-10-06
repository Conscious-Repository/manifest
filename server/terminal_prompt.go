package server

import (
	"context"
	"io"
	"os/exec"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	Selected int                `json:"selected"` // the cursor, as a nav index; -1 when unseen
	Multi    bool               `json:"multi,omitempty"`
	// Clipped: the CLI scrolled its list to fit a short pane (↑/↓ marks);
	// scanTermPrompt reveals the hidden rows by moving the cursor.
	Clipped bool `json:"clipped,omitempty"`
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
	Edge     string `json:"-"`                  // "↑"/"↓": more rows beyond this one
}

var (
	promptNumbered = regexp.MustCompile(`^(\s*)(?:([❯›>↑↓])\s*)?(\d{1,2})\.\s+(.*)$`)
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
	first, numCol, clipped := -1, -1, false
	for i := footer - 1; i >= 0 && footer-i <= 40; i-- {
		l := lines[i]
		if m := promptNumbered.FindStringSubmatch(l); m != nil {
			marker := m[2] == "❯" || m[2] == "›" || m[2] == ">"
			clipped = clipped || m[2] == "↑" || m[2] == "↓"
			edge := ""
			if m[2] == "↑" || m[2] == "↓" {
				edge = m[2]
			}
			rows = append(rows, row{line: i, indent: utf8.RuneCountInString(m[1]), marked: marker, opt: termPromptOption{Number: m[3], Label: m[4], Edge: edge}})
			first = i
			if col := utf8.RuneCountInString(l[:strings.Index(l, m[3]+".")]); numCol < 0 || col < numCol {
				numCol = col
			}
			if m[3] == "1" {
				break
			}
			continue
		}
		// above the list: the question, then the conversation — a line
		// left of the numbers that is not a row, a rule or a cursor line
		if numCol >= 0 && strings.TrimSpace(l) != "" && !promptRule(l) && !promptMarked.MatchString(l) && indentOf(l) < numCol {
			break
		}
	}
	if first >= 0 && rows[len(rows)-1].opt.Number != "1" {
		clipped = true
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
	p := &termPrompt{Selected: -1, Clipped: clipped}
	numbered := rows[0].opt.Number != ""
	// Walk the block top-down: options, their description lines, the
	// unnumbered "Submit" row of a multi-select, separators. A numbered row's
	// position is its number (a scrolled list keeps them), shifted by any
	// unnumbered row drawn before it.
	idx, extra := 0, 0
	add := func(r row) {
		o := r.opt
		if n, err := strconv.Atoi(o.Number); err == nil && numbered {
			idx = n - 1 + extra
		} else if numbered && len(p.Options) > 0 {
			extra++
		}
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
	p.Revision = promptRevision(p, "")
	return p
}

// promptRevision names a chooser by what it asks and offers — not by the
// cursor or ticks (each step's read-back checks those) nor by which rows a
// scrolled list happens to show.
func promptRevision(p *termPrompt, source string) string {
	opts := make([][2]string, 0, len(p.Options))
	for _, o := range p.Options {
		if o.Text {
			opts = append(opts, [2]string{o.Number, "\x00text"})
			continue
		}
		opts = append(opts, [2]string{o.Number, o.Label})
	}
	if p.Clipped {
		opts = nil // the visible window moves as the cursor does
	}
	identity, _ := json.Marshal([]any{source, p.Header, p.Title, opts, p.Multi})
	return hashTerminalText(string(identity))[:16]
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
	return s.promptFromScreen(se, lines, false), lines, nil
}

// Reading size: a pane is as large as the last client that attached, and a
// phone or collapsed panel can leave it a few cells wide — Claude then draws
// its dialog scrolled to a sliver or not at all. While a prompt has to be
// read or answered, a control client holds the pane at this size; detaching
// hands it back to whoever is attached.
const promptReadCols, promptReadRows = 120, 40

// withReadableSize runs fn with the pane held at the reading size.
func (s *Server) withReadableSize(ctx context.Context, se termSession, fn func() error) error {
	binary, err := herdrExecutable()
	if err != nil {
		return err
	}
	h := s.terminal.herdr
	cmd := exec.CommandContext(ctx, binary, "--session", h.Session, "terminal", "session", "control", se.Runtime.Occupant,
		"--cols", strconv.Itoa(promptReadCols), "--rows", strconv.Itoa(promptReadRows))
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	// the agent redraws on the size change
	if err := promptPause(ctx, 900*time.Millisecond); err != nil {
		return err
	}
	return fn()
}

// readablePrompt is terminalScreenPrompt, and when the screen holds no
// readable chooser (or a scrolled one) while the agent waits, the same read
// at the reading size.
func (s *Server) readablePrompt(ctx context.Context, se termSession, blocked bool) (*termPrompt, error) {
	p, _, err := s.terminalScreenPrompt(ctx, se)
	if err != nil || (p != nil && !p.Clipped) || (p == nil && !blocked) {
		return p, err
	}
	var large *termPrompt
	if err := s.withReadableSize(ctx, se, func() error {
		var err error
		large, _, err = s.terminalScreenPrompt(ctx, se)
		return err
	}); err != nil || large == nil {
		return p, nil
	}
	return large, nil
}

type promptReadCacheEntry struct {
	at     time.Time
	prompt *termPrompt
}

var promptReadCache sync.Map // session id → promptReadCacheEntry

// polledPrompt: what the chat's screen poll shows. A chooser it cannot read
// at the pane's own size is read at the reading size at most every 10 s (each
// read makes the agent redraw twice), and never while an answer is going in.
func (s *Server) polledPrompt(ctx context.Context, se termSession, lines []string, blocked bool) *termPrompt {
	p := s.promptFromScreen(se, lines, blocked)
	if (p != nil && !p.Clipped) || (p == nil && !blocked) || s.terminal.herdr == nil {
		promptReadCache.Delete(se.ID)
		return p
	}
	if v, ok := promptReadCache.Load(se.ID); ok && time.Since(v.(promptReadCacheEntry).at) < 10*time.Second {
		if cached := v.(promptReadCacheEntry).prompt; cached != nil {
			return cached
		}
		return p
	}
	mu := s.termInputMutex(se.ID)
	if !mu.TryLock() {
		return p
	}
	defer mu.Unlock()
	var large *termPrompt
	_ = s.withReadableSize(ctx, se, func() error {
		large, _, _ = s.terminalScreenPrompt(ctx, se)
		return nil
	})
	promptReadCache.Store(se.ID, promptReadCacheEntry{at: time.Now(), prompt: large})
	if large != nil {
		return large
	}
	return p
}

// promptFromScreen: the chooser on screen. (Claude writes a pending
// AskUserQuestion to its transcript only once answered, so the screen is the
// one source; a scrolled list is revealed by scanTermPrompt.)
func (s *Server) promptFromScreen(se termSession, lines []string, blocked bool) *termPrompt {
	return parseTermPrompt(lines)
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
	// Label: the row as the card showed it, checked again once the cursor is
	// on it (a scrolled list's rows are read before they are chosen).
	Label string `json:"label,omitempty"`
	// Scan: reveal a scrolled list's hidden rows; the cursor moves and comes
	// back, nothing is confirmed.
	Scan bool `json:"scan,omitempty"`
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

// promptSame: the same dialog (cursor, ticks and scroll aside): the same
// question, and every row both reads show agrees on its label.
func promptSame(a, b *termPrompt) bool {
	if a == nil || b == nil || a.Title != b.Title || a.Multi != b.Multi {
		return false
	}
	byNumber := map[string]termPromptOption{}
	for _, o := range a.Options {
		if o.Number != "" {
			byNumber[o.Number] = o
		}
	}
	for _, o := range b.Options {
		if prev, ok := byNumber[o.Number]; ok && !prev.Text && !o.Text && prev.Label != o.Label {
			return false
		}
	}
	return true
}

func promptOption(p *termPrompt, index int) (termPromptOption, bool) {
	for _, o := range p.Options {
		if o.Index == index {
			return o, true
		}
	}
	return termPromptOption{}, false
}

// promptRowIs: the same row of the same chooser, wherever it is drawn.
func promptRowIs(a, b termPromptOption) bool {
	switch {
	case b.Submit || a.Submit:
		return a.Submit && b.Submit
	case a.Number != "" || b.Number != "":
		return a.Number == b.Number && (a.Label == b.Label || a.Text || b.Text)
	}
	return a.Label == b.Label
}

// promptMoveTo moves the cursor onto want and reads it back there. Positions
// come from the rows' numbers, so a move into a scrolled-away part of the
// list lands right; when it does not (an unnumbered row out of view), the
// next read corrects it, and nothing is ever pressed on a row not verified.
func (s *Server) promptMoveTo(ctx context.Context, se termSession, p *termPrompt, want termPromptOption) (*termPrompt, error) {
	h := s.terminal.herdr
	for attempt := 0; attempt < 4; attempt++ {
		if cur, ok := promptOption(p, p.Selected); ok && promptRowIs(cur, want) {
			return p, nil
		}
		target := want.Index
		for _, o := range p.Options {
			if promptRowIs(o, want) {
				target = o.Index
			}
		}
		key, n := "down", target-p.Selected
		if n < 0 {
			key, n = "up", -n
		}
		if n == 0 {
			break
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
	return nil, errors.New("could not move to that option; nothing was confirmed — open Terminal to answer")
}

// promptStep moves the cursor toward a position (no read-back of which row
// it lands on: only scanTermPrompt uses it, and it confirms nothing).
func (s *Server) promptStep(ctx context.Context, se termSession, p *termPrompt, target int) (*termPrompt, error) {
	key, n := "down", target-p.Selected
	if n < 0 {
		key, n = "up", -n
	}
	keys := make([]string, n)
	for i := range keys {
		keys[i] = key
	}
	if err := s.terminal.herdr.sendKeys(ctx, se.Runtime, keys); err != nil {
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
	return next, nil
}

// scanTermPrompt reads every row of a scrolled list by walking the cursor to
// each hidden one, then returns it where the owner left it. Nothing is
// confirmed. → the whole list, and the screen as it now stands.
func (s *Server) scanTermPrompt(ctx context.Context, se termSession, p *termPrompt) (*termPrompt, *termPrompt, error) {
	start, ok := promptOption(p, p.Selected)
	if !ok {
		return nil, nil, errPromptChanged
	}
	rows := map[int]termPromptOption{}
	note := func(q *termPrompt) {
		for _, o := range q.Options {
			rows[o.Index] = o
		}
	}
	note(p)
	for step := 0; step < 40; step++ {
		target, last := -1, 0
		for i := range rows {
			if i > last {
				last = i
			}
		}
		for i := 0; i <= last+1 && target < 0; i++ {
			o, seen := rows[i]
			switch {
			case !seen && i <= last:
				target = i
			case seen && o.Edge == "↓":
				if _, next := rows[i+1]; !next {
					target = i + 1
				}
			}
		}
		if target < 0 {
			break
		}
		next, err := s.promptStep(ctx, se, p, target)
		if err != nil {
			return nil, nil, err
		}
		if next.Selected == p.Selected {
			break // the list ends here
		}
		p = next
		note(p)
	}
	cur, err := s.promptMoveTo(ctx, se, p, start)
	if err != nil {
		return nil, nil, err
	}
	full := *cur
	full.Options = nil
	for i := 0; i < len(rows)+2; i++ {
		if o, ok := rows[i]; ok {
			o.Edge = ""
			full.Options = append(full.Options, o)
		}
	}
	return &full, cur, nil
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
	full := p
	if p.Clipped {
		if full, p, err = s.scanTermPrompt(ctx, se, p); err != nil {
			return err
		}
	}
	if p.Multi {
		want := map[int]bool{}
		for _, i := range a.Checked {
			o, ok := promptOption(full, i)
			if !ok || o.Checked == nil || o.Text {
				return errBadRequest("not a selectable row")
			}
			want[i] = true
		}
		for _, o := range full.Options {
			if o.Checked == nil || o.Text || *o.Checked == want[o.Index] {
				continue
			}
			if p, err = s.promptMoveTo(ctx, se, p, o); err != nil {
				return err
			}
			if cur, _ := promptOption(p, p.Selected); cur.Checked == nil {
				return errors.New("could not read that row; nothing was submitted — open Terminal")
			} else if *cur.Checked == want[o.Index] {
				continue
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
			cur, _ := promptOption(next, next.Selected)
			if !promptSame(p, next) || !promptRowIs(cur, o) || cur.Checked == nil || *cur.Checked != want[o.Index] {
				return errors.New("a choice did not register; nothing was submitted — review it again")
			}
			p = next
		}
		a.Option, a.Label = -1, ""
		for _, o := range full.Options {
			if o.Submit {
				a.Option = o.Index
			}
		}
		if a.Option < 0 {
			return errors.New("this chooser has no Submit row; open Terminal to answer")
		}
	}
	target, ok := promptOption(full, a.Option)
	if !ok || (a.Label != "" && !target.Text && target.Label != a.Label) {
		return errPromptChanged
	}
	if target.Text && strings.TrimSpace(a.Text) == "" {
		return errBadRequest("type an answer first")
	}
	if strings.ContainsAny(a.Text, "\r\n") || len(a.Text) > 4000 {
		return errBadRequest("a typed answer is one line of at most 4000 bytes")
	}
	if p, err = s.promptMoveTo(ctx, se, p, target); err != nil {
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

// answerAtReadableSize runs fn at the pane's own size when the chooser the
// card was drawn from is readable there, else holding the reading size.
func (s *Server) answerAtReadableSize(ctx context.Context, se termSession, revision string, fn func() error) error {
	if p, _, err := s.terminalScreenPrompt(ctx, se); err == nil && p != nil && !p.Clipped && p.Revision == revision {
		return fn()
	}
	return s.withReadableSize(ctx, se, fn)
}

func blockedNow(ctx context.Context, s *Server, se termSession) bool {
	ob, err := s.terminal.herdr.Inspect(ctx, se.Runtime)
	return err == nil && ob.AgentState == "blocked"
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
		p, err := s.readablePrompt(r.Context(), se, blockedNow(r.Context(), s, se))
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
	promptReadCache.Delete(se.ID)
	if a.Scan {
		var full *termPrompt
		err := s.answerAtReadableSize(r.Context(), se, a.Revision, func() error {
			p, _, err := s.terminalScreenPrompt(r.Context(), se)
			if err == nil && (p == nil || p.Revision != a.Revision) {
				err = errPromptChanged
			}
			if err == nil {
				full = p
				if p.Clipped {
					full, _, err = s.scanTermPrompt(r.Context(), se, p)
				}
			}
			return err
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "prompt": full})
		return
	}
	if err := s.answerAtReadableSize(r.Context(), se, a.Revision, func() error { return s.answerTermPrompt(r.Context(), se, a) }); err != nil {
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
	next, _ := s.readablePrompt(r.Context(), se, blockedNow(r.Context(), s, se))
	writeJSON(w, map[string]any{"ok": true, "prompt": next})
}
