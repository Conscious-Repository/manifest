package daily

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/vault"
)

// Markers delimit the regions this app owns inside a note. Everything outside
// them (your journal) is read but never modified.
const (
	dailyStart = "<!-- manifest:start -->"
	dailyEnd   = "<!-- manifest:end -->"
	listStart  = "<!-- manifest:list:start -->"
	listEnd    = "<!-- manifest:list:end -->"
)

const dateLayout = "2006-01-02"

// Config holds the settings the Service needs. Daily-note PATH resolution lives
// in vault.Index; this struct only carries the schedule layout.
type Config struct {
	VaultPath     string
	ScheduleStart int
	ScheduleEnd   int
}

// ----- data model -----

type ScheduleRow struct {
	Time    string `json:"time"`              // display token, e.g. "8A"
	Label   string `json:"label"`             // what you planned / did
	Focused bool   `json:"focused"`           // "was I focused?" toggle
	Source  string `json:"source,omitempty"`  // "" | "calendar" (calendar-sourced, not persisted)
	EventID string `json:"eventId,omitempty"` // calendar event id (groups a multi-slot event in the UI; never persisted)
}

type Task struct {
	Text   string `json:"text"`
	Done   bool   `json:"done"`
	GoalID string `json:"goalId,omitempty"` // [goal:: id] backlink, if pulled from a goal
	Owner  string `json:"owner,omitempty"`  // [owner:: x], if present
}

// PoolItem is a 30-day owner==me goal offered for quick-add when planning an
// unplanned future day.
type PoolItem struct {
	GoalID string `json:"goalId"`
	Text   string `json:"text"`
	Area   string `json:"area"`
}

type Day struct {
	Date       string        `json:"date"`
	Schedule   []ScheduleRow `json:"schedule"`
	Tasks      []Task        `json:"tasks"`
	Focus      []FocusPick   `json:"focus"`      // the day's picked 90-day goals (cascade)
	FocusSlots int           `json:"focusSlots"` // how many focus slots the UI offers
	Goals      []string      `json:"goals"`      // legacy period-note panel (unused when a goals provider is wired)
	Milestones []string      `json:"milestones"`
	Quarter    string        `json:"quarter"` // e.g. "2026 Q2"
	Month      string        `json:"month"`   // e.g. "June 2026"
	Streak     int           `json:"streak"`
	Unplanned  bool          `json:"unplanned"`      // a future date with no active manifest
	Pool       []PoolItem    `json:"pool,omitempty"` // 30-day me goals to pull (when unplanned)
}

// defaultFocusSlots is how many 90-day focus goals the manifest offers per day.
const defaultFocusSlots = 3

// FocusNode is a resolved cascade node — a 30-day milestone or one of its tasks.
type FocusNode struct {
	GoalID  string `json:"goalId"`
	Text    string `json:"text"`
	Checked bool   `json:"checked,omitempty"`
}

// FocusPick is one picked 90-day goal in the day's Focus, resolved live from the
// cascade. The durable value persisted in the note is GoalID (a stable slug); Text
// is a reflection (kept for display when the slug no longer resolves).
type FocusPick struct {
	GoalID      string      `json:"goalId"`
	Text        string      `json:"text"`
	Resolved    bool        `json:"resolved"`
	MilestoneID string      `json:"milestoneId,omitempty"` // chosen 30-day slug (persisted)
	Milestone   *FocusNode  `json:"milestone,omitempty"`   // the selected 30-day
	Milestones  []FocusNode `json:"milestones,omitempty"`  // all 30-day children, for the picker
	Tasks       []FocusNode `json:"tasks,omitempty"`       // the selected milestone's open tasks
}

// CalSlot is a half-hour schedule slot derived from a timed calendar event.
type CalSlot struct {
	Token   string `json:"token"`   // canonical half-hour token, e.g. "9:30A"
	Title   string `json:"title"`   // event title
	EventID string `json:"eventId"` // for dedupe / idempotent re-sync
}

// EventSource supplies a date's calendar-derived schedule slots. The calendar
// integration (M3) plugs in here; NopEventSource is the disabled default.
type EventSource interface {
	Slots(date string) ([]CalSlot, error)
}

type NopEventSource struct{}

func (NopEventSource) Slots(string) ([]CalSlot, error) { return nil, nil }

// FocusResolution is what a GoalsProvider returns for a picked 90-day goal: its
// current text, the 30-day children to choose among, the selected milestone (by the
// requested milestoneID, else the first child), and that milestone's open tasks.
type FocusResolution struct {
	Text       string
	Milestone  *FocusNode
	Milestones []FocusNode
	Tasks      []FocusNode
}

// GoalsProvider resolves a picked 90-day goal (by stable slug) from the cascade,
// honoring a chosen 30-day milestone slug. It is optional: when unset, the legacy
// quarterly/monthly period notes are used.
type GoalsProvider interface {
	ResolveFocus(goalID, milestoneID string) (FocusResolution, bool)
}

// Service reads/writes the manifest regions of daily notes, resolving note paths
// through the vault Index so a YYYY-MM-DD note is found anywhere in the vault.
type Service struct {
	cfg    Config
	idx    *vault.Index
	goals  GoalsProvider
	events EventSource
}

func NewService(cfg Config, idx *vault.Index) *Service {
	return &Service{cfg: cfg, idx: idx, events: NopEventSource{}}
}

// UseGoals routes the daily Goals/Milestones panels through goals.md.
func (s *Service) UseGoals(p GoalsProvider) { s.goals = p }

// UseEvents plugs in a calendar event source for schedule auto-population (M3).
func (s *Service) UseEvents(e EventSource) {
	if e != nil {
		s.events = e
	}
}

// ----- period-note path helpers (goals/milestones; superseded in M1) -----

func quarterOf(d time.Time) (label, slug string) {
	q := (int(d.Month())-1)/3 + 1
	return fmt.Sprintf("%d Q%d", d.Year(), q), fmt.Sprintf("%d-Q%d", d.Year(), q)
}

func monthOf(d time.Time) (label, slug string) {
	return fmt.Sprintf("%s %d", d.Month().String(), d.Year()), d.Format("2006-01")
}

// ----- time token helpers -----

// slotToken renders minutes-from-midnight as a half-hour label, e.g.
// 540 -> "9:00A", 570 -> "9:30A", 1110 -> "6:30P".
func slotToken(min int) string {
	min = ((min % 1440) + 1440) % 1440
	h24, m := min/60, min%60
	suffix := "A"
	if h24 >= 12 {
		suffix = "P"
	}
	h12 := h24 % 12
	if h12 == 0 {
		h12 = 12
	}
	return fmt.Sprintf("%d:%02d%s", h12, m, suffix)
}

var slotRe = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?\s*([AaPp])$`)

// parseSlot converts "9A"/"9:30A"/"6:30P" to minutes-from-midnight.
// A bare hour ("9A") is treated as the :00 slot.
func parseSlot(tok string) (int, bool) {
	m := slotRe.FindStringSubmatch(strings.TrimSpace(tok))
	if m == nil {
		return 0, false
	}
	h, _ := strconv.Atoi(m[1])
	if h < 1 || h > 12 {
		return 0, false
	}
	min := 0
	if m[2] != "" {
		min, _ = strconv.Atoi(m[2])
		if min < 0 || min > 59 {
			return 0, false
		}
	}
	if strings.EqualFold(m[3], "A") {
		if h == 12 {
			h = 0
		}
	} else if h != 12 {
		h += 12
	}
	return h*60 + min, true
}

// configuredSlots lists every half-hour token from start to end (inclusive).
func (s *Service) configuredSlots() []string {
	var out []string
	for h := s.cfg.ScheduleStart; h <= s.cfg.ScheduleEnd; h++ {
		out = append(out, slotToken(h*60), slotToken(h*60+30))
	}
	return out
}

// ----- parsing -----

var (
	taskRe        = regexp.MustCompile(`^\s*-\s*\[([ xX])\]\s?(.*)$`)
	rowRe         = regexp.MustCompile(`^\s*\|(.*)\|\s*$`)
	inlineFieldRe = regexp.MustCompile(`\[([A-Za-z][\w-]*)\s*::\s*([^\]]*)\]`)
)

type inlineKV struct{ key, val string }

// stripFields pulls [key:: value] fields off a task line, returning the clean
// text and the fields. (Mirrors goals.parseFields; kept local to avoid a cross
// dependency on the goals package.)
func stripFields(text string) (string, []inlineKV) {
	var fields []inlineKV
	clean := inlineFieldRe.ReplaceAllStringFunc(text, func(m string) string {
		sm := inlineFieldRe.FindStringSubmatch(m)
		fields = append(fields, inlineKV{strings.TrimSpace(sm[1]), strings.TrimSpace(sm[2])})
		return ""
	})
	if len(fields) == 0 {
		return strings.TrimSpace(text), nil
	}
	return strings.Join(strings.Fields(clean), " "), fields
}

// parseBlock extracts the Focus picks, schedule rows, and tasks from the text
// between the daily markers. It is tolerant of edits made by hand in Obsidian.
// Focus lines are scoped to the `## Focus` section; schedule/task parsing stays
// global (so a hand-written task anywhere is still picked up).
func parseBlock(block string) ([]ScheduleRow, []Task, []FocusPick) {
	var rows []ScheduleRow
	var tasks []Task
	var focus []FocusPick
	section := ""
	for _, line := range strings.Split(block, "\n") {
		if h := blockHeading(line); h != "" {
			section = h
			continue
		}
		if section == "focus" {
			if p, ok := parseFocusLine(line); ok {
				focus = append(focus, p)
			}
			continue
		}
		if m := taskRe.FindStringSubmatch(line); m != nil {
			text, fields := stripFields(m[2])
			t := Task{Text: text, Done: strings.EqualFold(strings.TrimSpace(m[1]), "x")}
			for _, f := range fields {
				switch {
				case strings.EqualFold(f.key, "goal"):
					t.GoalID = f.val
				case strings.EqualFold(f.key, "owner"):
					t.Owner = f.val
				}
			}
			tasks = append(tasks, t)
			continue
		}
		if m := rowRe.FindStringSubmatch(line); m != nil {
			cells := splitRow(m[1])
			if len(cells) < 1 {
				continue
			}
			tok := strings.TrimSpace(cells[0])
			if _, ok := parseSlot(tok); !ok {
				continue // header row, separator row, or non-schedule table
			}
			label, focused := "", false
			if len(cells) >= 2 {
				label = strings.TrimSpace(cells[1])
			}
			if len(cells) >= 3 {
				focused = isCheck(cells[2])
			}
			rows = append(rows, ScheduleRow{Time: tok, Label: label, Focused: focused})
		}
	}
	return rows, tasks, focus
}

// blockHeading recognizes the "## Focus" / "## Schedule" / "## Tasks" subsection
// headings inside the manifest region; "" for any other line.
func blockHeading(line string) string {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "## focus":
		return "focus"
	case "## schedule":
		return "schedule"
	case "## tasks":
		return "tasks"
	}
	return ""
}

// focusLineRe matches a top-level (non-indented) Focus bullet, e.g.
// "- Series A 15M [goal:: aion/series-a-15m]". Indented reflection sub-lines and
// checkbox lines are ignored.
var focusLineRe = regexp.MustCompile(`^-\s+(.*\S)\s*$`)

func parseFocusLine(line string) (FocusPick, bool) {
	m := focusLineRe.FindStringSubmatch(line)
	if m == nil || taskRe.MatchString(line) {
		return FocusPick{}, false
	}
	text, fields := stripFields(m[1])
	var slug, milestone string
	for _, f := range fields {
		switch {
		case strings.EqualFold(f.key, "goal"):
			slug = f.val
		case strings.EqualFold(f.key, "milestone"):
			milestone = f.val
		}
	}
	if slug == "" {
		return FocusPick{}, false
	}
	return FocusPick{GoalID: slug, MilestoneID: milestone, Text: text}, true
}

func splitRow(s string) []string {
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isCheck(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "x", "✓", "yes", "y", "[x]", "true":
		return true
	}
	return false
}

// mergeSchedule combines the configured hours with whatever was parsed from the
// file, preserving labels/toggles by time token and keeping any extra rows the
// user added by hand. Result is ordered by hour of day.
func (s *Service) mergeSchedule(parsed []ScheduleRow) []ScheduleRow {
	byTok := map[string]ScheduleRow{}
	order := []string{}
	add := func(tok string) string {
		if min, ok := parseSlot(tok); ok {
			tok = slotToken(min)
		}
		if _, seen := byTok[tok]; !seen {
			byTok[tok] = ScheduleRow{Time: tok}
			order = append(order, tok)
		}
		return tok
	}
	for _, tok := range s.configuredSlots() {
		add(tok)
	}
	for _, r := range parsed {
		tok := add(r.Time)
		r.Time = tok
		byTok[tok] = r // parsed values win (they carry label/focused)
	}
	sort.SliceStable(order, func(i, j int) bool {
		hi, oki := parseSlot(order[i])
		hj, okj := parseSlot(order[j])
		if oki && okj {
			return hi < hj
		}
		return oki && !okj
	})
	out := make([]ScheduleRow, 0, len(order))
	for _, tok := range order {
		out = append(out, byTok[tok])
	}
	return out
}

// mergeCalendar overlays calendar-derived slots onto the schedule without
// overwriting manual labels: an empty slot is filled and flagged "calendar";
// events outside the configured range are appended, then rows are re-sorted by
// time. It does not persist — the UI drops calendar-flagged labels on save, so
// re-sync is idempotent and manual text is always preserved.
func (s *Service) mergeCalendar(rows []ScheduleRow, slots []CalSlot) []ScheduleRow {
	idx := map[string]int{}
	for i := range rows {
		idx[rows[i].Time] = i
	}
	// Group slots by event so we can treat a multi-slot event as a unit. Order is
	// preserved by first appearance (EventsToSlots already sorted by start time).
	order := []string{}
	byEvent := map[string][]CalSlot{}
	for _, cs := range slots {
		if _, ok := byEvent[cs.EventID]; !ok {
			order = append(order, cs.EventID)
		}
		byEvent[cs.EventID] = append(byEvent[cs.EventID], cs)
	}
	for _, eid := range order {
		ev := byEvent[eid]
		// Lead = the slot carrying the title (first non-empty), else the first slot.
		leadTok := ev[0].Token
		for _, cs := range ev {
			if strings.TrimSpace(cs.Title) != "" {
				leadTok = cs.Token
				break
			}
		}
		// Adopted: if the lead slot already holds a manual entry, the user has
		// hardened this event — don't re-overlay any of its slots (no stale bars).
		if eid != "" {
			if i, ok := idx[leadTok]; ok && rows[i].Source != "calendar" && strings.TrimSpace(rows[i].Label) != "" {
				continue
			}
		}
		for _, cs := range ev {
			if i, ok := idx[cs.Token]; ok {
				if strings.TrimSpace(rows[i].Label) == "" {
					rows[i].Label = cs.Title
					rows[i].Source = "calendar"
					rows[i].EventID = cs.EventID
				}
				continue
			}
			rows = append(rows, ScheduleRow{Time: cs.Token, Label: cs.Title, Source: "calendar", EventID: cs.EventID})
			idx[cs.Token] = len(rows) - 1
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, _ := parseSlot(rows[i].Time)
		b, _ := parseSlot(rows[j].Time)
		return a < b
	})
	return rows
}

// ----- serialization -----

func serializeBlock(d Day) string {
	var b strings.Builder
	if hasFocus(d.Focus) {
		b.WriteString("## Focus\n\n")
		for _, p := range d.Focus {
			if p.GoalID == "" {
				continue
			}
			b.WriteString("- " + p.Text + " [goal:: " + p.GoalID + "]")
			if p.MilestoneID != "" {
				b.WriteString(" [milestone:: " + p.MilestoneID + "]")
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## Schedule\n\n")
	b.WriteString("| Time | Focus | Focused |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, r := range d.Schedule {
		check := ""
		if r.Focused {
			check = "x"
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", r.Time, r.Label, check))
	}
	b.WriteString("\n## Tasks\n\n")
	if len(d.Tasks) == 0 {
		b.WriteString("- [ ] \n")
	}
	for _, t := range d.Tasks {
		box := " "
		if t.Done {
			box = "x"
		}
		b.WriteString("- [" + box + "] " + t.Text)
		if t.Owner != "" {
			b.WriteString(" [owner:: " + t.Owner + "]")
		}
		if t.GoalID != "" {
			b.WriteString(" [goal:: " + t.GoalID + "]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// upsertRegion replaces the text between start/end markers, preserving
// everything else. If the markers are absent it inserts a fresh region just
// after any YAML frontmatter (or at the very top).
func upsertRegion(content, start, end, inner string) string {
	region := start + "\n" + strings.TrimRight(inner, "\n") + "\n" + end
	si := strings.Index(content, start)
	ei := strings.Index(content, end)
	if si >= 0 && ei > si {
		return content[:si] + region + content[ei+len(end):]
	}
	insertAt := frontmatterEnd(content)
	prefix := content[:insertAt]
	suffix := content[insertAt:]
	sep := ""
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		sep = "\n"
	}
	trailing := "\n"
	if strings.TrimSpace(suffix) != "" {
		trailing = "\n\n"
	}
	return prefix + sep + region + trailing + suffix
}

// frontmatterEnd returns the byte offset just after a leading YAML frontmatter
// block, or 0 if there is none.
func frontmatterEnd(content string) int {
	if !strings.HasPrefix(content, "---\n") {
		return 0
	}
	rest := content[4:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return 0
	}
	after := 4 + idx + len("\n---")
	for after < len(content) && content[after] != '\n' {
		after++
	}
	if after < len(content) {
		after++ // include the newline
	}
	return after
}

func regionBetween(content, start, end string) (string, bool) {
	si := strings.Index(content, start)
	ei := strings.Index(content, end)
	if si >= 0 && ei > si {
		return content[si+len(start) : ei], true
	}
	return "", false
}

// ----- public read / write -----

func (s *Service) Load(date string) (Day, error) {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return Day{}, fmt.Errorf("bad date %q: %w", date, err)
	}
	day := Day{Date: date}
	day.Quarter, _ = quarterOf(d)
	day.Month, _ = monthOf(d)

	path, err := s.idx.DailyNote(date)
	if err != nil {
		return Day{}, err
	}
	content := readFile(path)
	block, _ := regionBetween(content, dailyStart, dailyEnd)
	parsedRows, tasks, focus := parseBlock(block)
	day.Schedule = s.mergeSchedule(parsedRows)
	if slots, err := s.events.Slots(date); err == nil && len(slots) > 0 {
		day.Schedule = s.mergeCalendar(day.Schedule, slots)
	}
	day.Tasks = tasks

	// A future date with no active manifest is "unplanned": landing on it offers
	// the 30-day pool (and, in M3, calendar prefill). Today/past are never marked.
	today := time.Now().Format(dateLayout)
	day.Unplanned = date > today && !dayActive(parsedRows, tasks) && len(focus) == 0

	// Focus picks are resolved live from goals.md through the goals ladder. The
	// legacy Manifest/Goals-<quarter>.md period notes are retired (§0) — never read
	// or written; the Day.Goals/Milestones fields stay empty scaffolding.
	if s.goals != nil {
		day.Focus = s.resolveFocus(focus)
		day.FocusSlots = defaultFocusSlots
	}
	day.Streak = s.streak(d)
	return day, nil
}

// resolveFocus turns the persisted picks (slug + stored text) into live FocusPicks
// by resolving each 90-day slug through the cascade. An unresolved slug keeps its
// stored text and is flagged so the UI can grey it out.
func (s *Service) resolveFocus(raw []FocusPick) []FocusPick {
	out := make([]FocusPick, 0, len(raw))
	for _, p := range raw {
		fp := FocusPick{GoalID: p.GoalID, Text: p.Text, MilestoneID: p.MilestoneID}
		if res, ok := s.goals.ResolveFocus(p.GoalID, p.MilestoneID); ok {
			fp.Resolved = true
			if res.Text != "" {
				fp.Text = res.Text
			}
			fp.Milestone = res.Milestone
			fp.Milestones = res.Milestones
			fp.Tasks = res.Tasks
			if res.Milestone != nil {
				fp.MilestoneID = res.Milestone.GoalID // the actually-selected one
			}
		}
		out = append(out, fp)
	}
	return out
}

func hasFocus(picks []FocusPick) bool {
	for _, p := range picks {
		if p.GoalID != "" {
			return true
		}
	}
	return false
}

// SaveDay writes the schedule + tasks into the daily note, preserving the
// surrounding journal. The note (and its folder) are created if missing.
func (s *Service) SaveDay(date string, schedule []ScheduleRow, tasks []Task) error {
	if _, err := time.Parse(dateLayout, date); err != nil {
		return err
	}
	path, err := s.idx.DailyNote(date)
	if err != nil {
		return err
	}
	content := readFile(path)
	_, _, focus := parseBlock(blockOf(content)) // preserve existing Focus picks
	day := Day{Focus: focus, Schedule: s.mergeSchedule(schedule), Tasks: tasks}
	updated := upsertRegion(content, dailyStart, dailyEnd, serializeBlock(day))
	return writeFile(path, updated)
}

// SetFocus sets (goalID != "") or clears (goalID == "") the Focus pick at a slot,
// preserving the day's schedule/tasks, then returns the reloaded, resolved day.
func (s *Service) SetFocus(date string, slot int, goalID string) (Day, error) {
	if _, err := time.Parse(dateLayout, date); err != nil {
		return Day{}, err
	}
	path, err := s.idx.DailyNote(date)
	if err != nil {
		return Day{}, err
	}
	content := readFile(path)
	rows, tasks, focus := parseBlock(blockOf(content))
	switch {
	case goalID == "":
		if slot >= 0 && slot < len(focus) {
			focus = append(focus[:slot], focus[slot+1:]...)
		}
	default:
		text := goalID
		if s.goals != nil {
			if res, ok := s.goals.ResolveFocus(goalID, ""); ok && res.Text != "" {
				text = res.Text
			}
		}
		p := FocusPick{GoalID: goalID, Text: text} // fresh pick: milestone defaults to first child
		if slot >= 0 && slot < len(focus) {
			focus[slot] = p
		} else {
			focus = append(focus, p)
		}
	}
	day := Day{Focus: focus, Schedule: s.mergeSchedule(rows), Tasks: tasks}
	updated := upsertRegion(content, dailyStart, dailyEnd, serializeBlock(day))
	if err := writeFile(path, updated); err != nil {
		return Day{}, err
	}
	return s.Load(date)
}

// SetMilestone records the chosen 30-day milestone for the Focus pick at a slot,
// preserving the day's schedule/tasks, then returns the reloaded, resolved day.
func (s *Service) SetMilestone(date string, slot int, milestoneID string) (Day, error) {
	if _, err := time.Parse(dateLayout, date); err != nil {
		return Day{}, err
	}
	path, err := s.idx.DailyNote(date)
	if err != nil {
		return Day{}, err
	}
	content := readFile(path)
	rows, tasks, focus := parseBlock(blockOf(content))
	if slot >= 0 && slot < len(focus) {
		focus[slot].MilestoneID = milestoneID
	}
	day := Day{Focus: focus, Schedule: s.mergeSchedule(rows), Tasks: tasks}
	updated := upsertRegion(content, dailyStart, dailyEnd, serializeBlock(day))
	if err := writeFile(path, updated); err != nil {
		return Day{}, err
	}
	return s.Load(date)
}

func blockOf(content string) string {
	b, _ := regionBetween(content, dailyStart, dailyEnd)
	return b
}

// AddTask appends a task to the day's manifest and returns the reloaded day.
// Used by "pull a goal into the day" — the goal itself is never auto-checked.
func (s *Service) AddTask(date string, t Task) (Day, error) {
	day, err := s.Load(date)
	if err != nil {
		return Day{}, err
	}
	day.Tasks = append(day.Tasks, t)
	if err := s.SaveDay(date, day.Schedule, day.Tasks); err != nil {
		return Day{}, err
	}
	return s.Load(date)
}

// ----- list (goals/milestones) notes -----

var bulletRe = regexp.MustCompile(`^\s*[-*]\s+(.*)$`)

func readList(path string) []string {
	content := readFile(path)
	scope := content
	if inner, ok := regionBetween(content, listStart, listEnd); ok {
		scope = inner
	}
	var items []string
	for _, line := range strings.Split(scope, "\n") {
		if m := bulletRe.FindStringSubmatch(line); m != nil {
			if t := strings.TrimSpace(m[1]); t != "" {
				items = append(items, t)
			}
		}
	}
	return items
}

func writeList(path, heading string, items []string) error {
	content := readFile(path)
	var inner strings.Builder
	if len(items) == 0 {
		inner.WriteString("- \n")
	}
	for _, it := range items {
		inner.WriteString("- " + it + "\n")
	}
	if content == "" {
		content = "# " + heading + "\n\n"
	}
	updated := upsertRegion(content, listStart, listEnd, inner.String())
	return writeFile(path, updated)
}

// ----- streak -----

// streak counts consecutive days, ending at d (inclusive), whose daily note
// (resolved anywhere via the Index) has an active manifest block.
func (s *Service) streak(d time.Time) int {
	count := 0
	cur := d
	for i := 0; i < 366; i++ {
		path, err := s.idx.DailyNote(cur.Format(dateLayout))
		if err != nil {
			break
		}
		content := readFile(path)
		block, ok := regionBetween(content, dailyStart, dailyEnd)
		if !ok {
			break
		}
		rows, tasks, _ := parseBlock(block)
		if !dayActive(rows, tasks) {
			break
		}
		count++
		cur = cur.AddDate(0, 0, -1)
	}
	return count
}

func dayActive(rows []ScheduleRow, tasks []Task) bool {
	for _, r := range rows {
		if r.Focused || strings.TrimSpace(r.Label) != "" {
			return true
		}
	}
	for _, t := range tasks {
		if t.Done || strings.TrimSpace(t.Text) != "" {
			return true
		}
	}
	return false
}

// ----- low-level file io -----

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
