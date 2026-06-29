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
// in vault.Index; this struct only carries period-note and schedule layout.
type Config struct {
	VaultPath     string
	PeriodNoteDir string
	ScheduleStart int
	ScheduleEnd   int
}

// ----- data model -----

type ScheduleRow struct {
	Time    string `json:"time"`    // display token, e.g. "8A"
	Label   string `json:"label"`   // what you planned / did
	Focused bool   `json:"focused"` // "was I focused?" toggle
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
	Goals      []string      `json:"goals"`
	Milestones []string      `json:"milestones"`
	Quarter    string        `json:"quarter"` // e.g. "2026 Q2"
	Month      string        `json:"month"`   // e.g. "June 2026"
	Streak     int           `json:"streak"`
	Unplanned  bool          `json:"unplanned"`      // a future date with no active manifest
	Pool       []PoolItem    `json:"pool,omitempty"` // 30-day me goals to pull (when unplanned)
}

// ScheduleEvent is a normalized timed calendar event (M3 fills these in).
type ScheduleEvent struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Title string `json:"title"`
	ID    string `json:"id"`
}

// EventSource supplies timed calendar events for a date. It is the seam the
// calendar integration (M3) plugs into; until then NopEventSource is used.
type EventSource interface {
	TimedEvents(date string) ([]ScheduleEvent, error)
}

type NopEventSource struct{}

func (NopEventSource) TimedEvents(string) ([]ScheduleEvent, error) { return nil, nil }

// GoalsProvider supplies the goals.md-derived data for the read-only daily
// panels (90-day / 30-day, owner==me). It is optional: when unset, the legacy
// quarterly/monthly period notes are used instead.
type GoalsProvider interface {
	HorizonForMe(horizon string) []string
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

func (s *Service) goalsPath(d time.Time) string {
	_, slug := quarterOf(d)
	return filepath.Join(s.cfg.VaultPath, s.cfg.PeriodNoteDir, "Goals-"+slug+".md")
}

func (s *Service) milestonesPath(d time.Time) string {
	_, slug := monthOf(d)
	return filepath.Join(s.cfg.VaultPath, s.cfg.PeriodNoteDir, "Milestones-"+slug+".md")
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

// parseBlock extracts schedule rows and tasks from the text between the daily
// markers. It is tolerant of edits made by hand in Obsidian.
func parseBlock(block string) ([]ScheduleRow, []Task) {
	var rows []ScheduleRow
	var tasks []Task
	for _, line := range strings.Split(block, "\n") {
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
	return rows, tasks
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

// ----- serialization -----

func serializeBlock(d Day) string {
	var b strings.Builder
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
	parsedRows, tasks := parseBlock(block)
	day.Schedule = s.mergeSchedule(parsedRows)
	day.Tasks = tasks

	// A future date with no active manifest is "unplanned": landing on it offers
	// the 30-day pool (and, in M3, calendar prefill). Today/past are never marked.
	today := time.Now().Format(dateLayout)
	day.Unplanned = date > today && !dayActive(parsedRows, tasks)

	if s.goals != nil {
		day.Goals = s.goals.HorizonForMe("90-day")
		day.Milestones = s.goals.HorizonForMe("30-day")
	} else {
		day.Goals = readList(s.goalsPath(d))
		day.Milestones = readList(s.milestonesPath(d))
	}
	day.Streak = s.streak(d)
	return day, nil
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
	day := Day{Schedule: s.mergeSchedule(schedule), Tasks: tasks}
	updated := upsertRegion(content, dailyStart, dailyEnd, serializeBlock(day))
	return writeFile(path, updated)
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

func (s *Service) SaveGoals(date string, items []string) error {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return err
	}
	label, _ := quarterOf(d)
	return writeList(s.goalsPath(d), "Goals — "+label, items)
}

func (s *Service) SaveMilestones(date string, items []string) error {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return err
	}
	label, _ := monthOf(d)
	return writeList(s.milestonesPath(d), "Milestones — "+label, items)
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
		rows, tasks := parseBlock(block)
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
