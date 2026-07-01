package daily

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/vault"
)

func testService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	idx, err := vault.NewIndex(vault.Config{Root: dir, NewDailyDir: "Daily", GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{VaultPath: dir, PeriodNoteDir: "Manifest", ScheduleStart: 8, ScheduleEnd: 18}
	return NewService(cfg, idx), dir
}

func TestSlotTokenRoundTrip(t *testing.T) {
	for min := 0; min < 1440; min += 30 {
		tok := slotToken(min)
		got, ok := parseSlot(tok)
		if !ok || got != min {
			t.Fatalf("min %d -> %q -> %d ok=%v", min, tok, got, ok)
		}
	}
	if m, ok := parseSlot("9A"); !ok || m != 540 {
		t.Fatalf("bare 9A -> %d ok=%v", m, ok)
	}
}

func TestJournalPreserved(t *testing.T) {
	s, dir := testService(t)
	daily := filepath.Join(dir, "Daily", "2026-06-29.md")
	journal := "---\ntags: [daily]\n---\n\n# 2026-06-29\n\nWoke up early. Felt good about the day ahead.\n"
	if err := os.MkdirAll(filepath.Dir(daily), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daily, []byte(journal), 0o644); err != nil {
		t.Fatal(err)
	}

	sched := []ScheduleRow{{Time: "9:30A", Label: "Deep work", Focused: true}}
	tasks := []Task{{Text: "Ship manifest", Done: false}, {Text: "Review PR", Done: true}}
	if err := s.SaveDay("2026-06-29", sched, tasks); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(daily)
	got := string(out)
	if !strings.Contains(got, "Woke up early.") {
		t.Fatal("journal text was lost")
	}
	if !strings.Contains(got, "tags: [daily]") {
		t.Fatal("frontmatter was lost")
	}
	if !strings.Contains(got, "| 9:30A | Deep work | x |") {
		t.Fatalf("schedule not written:\n%s", got)
	}
	if !strings.Contains(got, "| 8:00A |  |  |") {
		t.Fatalf("half-hour slots not generated:\n%s", got)
	}
	if !strings.Contains(got, "- [x] Review PR") {
		t.Fatalf("task not written:\n%s", got)
	}

	if err := s.SaveDay("2026-06-29", sched, tasks); err != nil {
		t.Fatal(err)
	}
	out2, _ := os.ReadFile(daily)
	if strings.Count(string(out2), dailyStart) != 1 {
		t.Fatal("manifest block was duplicated on re-save")
	}
}

func TestObsidianEditIsReadBack(t *testing.T) {
	s, dir := testService(t)
	daily := filepath.Join(dir, "Daily", "2026-06-29.md")
	hand := dailyStart + "\n## Schedule\n\n| Time | Focus | Focused |\n| --- | --- | --- |\n| 8A | Gym | x |\n| 9:30A | Email |  |\n\n## Tasks\n\n- [x] Hand-written task\n" + dailyEnd + "\n"
	if err := os.MkdirAll(filepath.Dir(daily), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daily, []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}

	day, err := s.Load("2026-06-29")
	if err != nil {
		t.Fatal(err)
	}
	byTok := map[string]ScheduleRow{}
	for _, r := range day.Schedule {
		byTok[r.Time] = r
	}
	if r := byTok["8:00A"]; r.Label != "Gym" || !r.Focused {
		t.Fatalf("8:00A not read back: %+v", r)
	}
	if r := byTok["9:30A"]; r.Label != "Email" {
		t.Fatalf("9:30A not read back: %+v", r)
	}
	if len(day.Tasks) != 1 || day.Tasks[0].Text != "Hand-written task" || !day.Tasks[0].Done {
		t.Fatalf("hand-written task not read: %+v", day.Tasks)
	}
	if day.Streak != 1 {
		t.Fatalf("expected streak 1, got %d", day.Streak)
	}
}

func TestUnplannedDetection(t *testing.T) {
	s, _ := testService(t)
	future := time.Now().AddDate(0, 0, 5).Format("2006-01-02")
	if day, _ := s.Load(future); !day.Unplanned {
		t.Fatal("an empty future day should be unplanned")
	}
	if day, _ := s.Load(time.Now().Format("2006-01-02")); day.Unplanned {
		t.Fatal("today should never be unplanned")
	}
	// Plan the future day; it should no longer be unplanned (and not be clobbered).
	if err := s.SaveDay(future, []ScheduleRow{{Time: "9:00A", Label: "Deep work"}}, nil); err != nil {
		t.Fatal(err)
	}
	if day, _ := s.Load(future); day.Unplanned {
		t.Fatal("a planned future day should not be unplanned")
	}
}

func TestTaskGoalBacklinkRoundTrip(t *testing.T) {
	s, dir := testService(t)
	const date = "2026-06-29"
	if _, err := s.AddTask(date, Task{Text: "Draft contract", GoalID: "aion/draft-contract"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "Daily", date+".md"))
	if !strings.Contains(string(raw), "[goal:: aion/draft-contract]") {
		t.Fatalf("backlink not written to disk:\n%s", raw)
	}
	day, err := s.Load(date)
	if err != nil {
		t.Fatal(err)
	}
	var found *Task
	for i := range day.Tasks {
		if day.Tasks[i].GoalID == "aion/draft-contract" {
			found = &day.Tasks[i]
		}
	}
	if found == nil || found.Text != "Draft contract" {
		t.Fatalf("backlink not read back with clean text: %+v", day.Tasks)
	}
}

func TestMergeCalendar(t *testing.T) {
	s, _ := testService(t)
	rows := []ScheduleRow{
		{Time: "9:00A", Label: "Standup (manual)"},
		{Time: "9:30A", Label: ""},
		{Time: "10:00A", Label: ""},
	}
	slots := []CalSlot{
		{Token: "9:00A", Title: "Cal event"},  // collides with manual -> manual wins
		{Token: "9:30A", Title: "Meeting"},    // fills empty -> flagged calendar
		{Token: "7:00A", Title: "Early call"}, // outside range -> appended, sorts first
	}
	out := s.mergeCalendar(rows, slots)
	byTok := map[string]ScheduleRow{}
	for _, r := range out {
		byTok[r.Time] = r
	}
	if byTok["9:00A"].Label != "Standup (manual)" || byTok["9:00A"].Source == "calendar" {
		t.Fatalf("manual slot must win: %+v", byTok["9:00A"])
	}
	if byTok["9:30A"].Label != "Meeting" || byTok["9:30A"].Source != "calendar" {
		t.Fatalf("empty slot should be filled + flagged: %+v", byTok["9:30A"])
	}
	if byTok["7:00A"].Label != "Early call" || out[0].Time != "7:00A" {
		t.Fatalf("out-of-range event should be appended and sorted first: %+v", out)
	}
}

func TestMergeCalendarCarriesEventID(t *testing.T) {
	s, _ := testService(t)
	rows := []ScheduleRow{{Time: "11:00A", Label: ""}}
	out := s.mergeCalendar(rows, []CalSlot{{Token: "11:00A", Title: "Zoom", EventID: "evtZ"}})
	byTok := map[string]ScheduleRow{}
	for _, r := range out {
		byTok[r.Time] = r
	}
	if byTok["11:00A"].Source != "calendar" || byTok["11:00A"].EventID != "evtZ" {
		t.Fatalf("calendar row should carry its EventID: %+v", byTok["11:00A"])
	}
}

// Once the user has hardened (adopted) a calendar event — its lead slot is now a
// manual entry — re-merging the still-present calendar event must NOT re-overlay
// any of its slots, so no stale soft bars reappear on reload.
func TestMergeCalendarSuppressesAdoptedEvent(t *testing.T) {
	s, _ := testService(t)
	rows := []ScheduleRow{
		{Time: "1:30P", Label: "Ben x Isaac Podcast"}, // adopted -> manual lead
		{Time: "2:00P", Label: ""},
		{Time: "2:30P", Label: ""},
	}
	slots := []CalSlot{
		{Token: "1:30P", Title: "Ben x Isaac Podcast", EventID: "evt1"},
		{Token: "2:00P", Title: "", EventID: "evt1"},
		{Token: "2:30P", Title: "", EventID: "evt1"},
	}
	out := s.mergeCalendar(rows, slots)
	for _, r := range out {
		if r.Source == "calendar" {
			t.Fatalf("adopted event must not re-overlay any slot: %+v", r)
		}
	}
	byTok := map[string]ScheduleRow{}
	for _, r := range out {
		byTok[r.Time] = r
	}
	if byTok["1:30P"].Label != "Ben x Isaac Podcast" || byTok["1:30P"].Source != "" {
		t.Fatalf("lead must stay a manual entry: %+v", byTok["1:30P"])
	}
}

func TestGoalsAndMilestones(t *testing.T) {
	s, _ := testService(t)
	if err := s.SaveGoals("2026-06-29", []string{"Launch v1", "Read more"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMilestones("2026-06-29", []string{"Ship beta"}); err != nil {
		t.Fatal(err)
	}
	day, err := s.Load("2026-06-29")
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Goals) != 2 || day.Goals[0] != "Launch v1" {
		t.Fatalf("goals not read: %+v", day.Goals)
	}
	if len(day.Milestones) != 1 || day.Milestones[0] != "Ship beta" {
		t.Fatalf("milestones not read: %+v", day.Milestones)
	}
	if day.Quarter != "2026 Q2" {
		t.Fatalf("quarter: %s", day.Quarter)
	}
}

type fakeMilestone struct {
	id, text string
	tasks    []FocusNode
}
type fakeGoal struct {
	text     string
	children []fakeMilestone
}
type fakeGoals struct{ goals map[string]fakeGoal }

// ResolveFocus mirrors the real adapter: it picks the requested milestone (else the
// first child) and returns that milestone's tasks plus the full child list.
func (f fakeGoals) ResolveFocus(id, milestoneID string) (FocusResolution, bool) {
	g, ok := f.goals[id]
	if !ok {
		return FocusResolution{}, false
	}
	res := FocusResolution{Text: g.text}
	if len(g.children) == 0 {
		return res, true
	}
	for _, c := range g.children {
		res.Milestones = append(res.Milestones, FocusNode{GoalID: c.id, Text: c.text})
	}
	sel := g.children[0]
	for _, c := range g.children {
		if c.id == milestoneID {
			sel = c
			break
		}
	}
	res.Milestone = &FocusNode{GoalID: sel.id, Text: sel.text}
	res.Tasks = sel.tasks
	return res, true
}

func TestFocusPersistResolveAndClear(t *testing.T) {
	s, _ := testService(t)
	s.UseGoals(fakeGoals{goals: map[string]fakeGoal{
		"aion/series-a": {text: "Series A 15M", children: []fakeMilestone{
			{id: "aion/series-a/deck", text: "Draft deck", tasks: []FocusNode{{GoalID: "aion/series-a/deck/ff", Text: "Intro to FF"}}},
		}},
	}})

	// Pick a 90-day goal into slot 0 → resolves text + milestone + tasks.
	day, err := s.SetFocus("2026-06-29", 0, "aion/series-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Focus) != 1 || !day.Focus[0].Resolved || day.Focus[0].Text != "Series A 15M" {
		t.Fatalf("focus not resolved: %+v", day.Focus)
	}
	if day.Focus[0].Milestone == nil || day.Focus[0].Milestone.Text != "Draft deck" || len(day.Focus[0].Tasks) != 1 {
		t.Fatalf("milestone/tasks not resolved: %+v", day.Focus[0])
	}

	// The durable slug persists; a reload re-resolves it from the cascade.
	day2, _ := s.Load("2026-06-29")
	if len(day2.Focus) != 1 || day2.Focus[0].GoalID != "aion/series-a" || day2.Focus[0].Text != "Series A 15M" {
		t.Fatalf("focus didn't persist/re-resolve: %+v", day2.Focus)
	}

	// An unresolved slug keeps its stored text but is flagged.
	day3, _ := s.SetFocus("2026-06-29", 1, "aion/ghost")
	var ghostResolved = true
	for _, p := range day3.Focus {
		if p.GoalID == "aion/ghost" {
			ghostResolved = p.Resolved
		}
	}
	if ghostResolved {
		t.Fatalf("unresolved slug should not be Resolved: %+v", day3.Focus)
	}

	// Clearing slot 0 removes that pick.
	day4, _ := s.SetFocus("2026-06-29", 0, "")
	for _, p := range day4.Focus {
		if p.GoalID == "aion/series-a" {
			t.Fatalf("clear failed: %+v", day4.Focus)
		}
	}
}

func TestFocusMilestoneSelectionCascadesTasks(t *testing.T) {
	s, _ := testService(t)
	s.UseGoals(fakeGoals{goals: map[string]fakeGoal{
		"home/backyard": {text: "Backyard", children: []fakeMilestone{
			{id: "home/backyard/metal-up", text: "Metal up"}, // first child, no tasks
			{id: "home/backyard/yard-done", text: "Yard done", tasks: []FocusNode{{GoalID: "home/backyard/yard-done/dirt", Text: "Dirt for backyard"}}},
			{id: "home/backyard/pavers", text: "Pavers"},
		}},
	}})

	// Picking the 90-day defaults the milestone to the first child (no tasks) and
	// surfaces all 3 milestone options for the picker.
	day, err := s.SetFocus("2026-06-29", 0, "home/backyard")
	if err != nil {
		t.Fatal(err)
	}
	if day.Focus[0].Milestone == nil || day.Focus[0].Milestone.Text != "Metal up" || len(day.Focus[0].Tasks) != 0 {
		t.Fatalf("default milestone should be first child with no tasks: %+v", day.Focus[0])
	}
	if len(day.Focus[0].Milestones) != 3 {
		t.Fatalf("want 3 milestone options: %+v", day.Focus[0].Milestones)
	}

	// Selecting "Yard done" switches the milestone and cascades its task.
	day2, err := s.SetMilestone("2026-06-29", 0, "home/backyard/yard-done")
	if err != nil {
		t.Fatal(err)
	}
	if day2.Focus[0].Milestone.Text != "Yard done" || len(day2.Focus[0].Tasks) != 1 || day2.Focus[0].Tasks[0].Text != "Dirt for backyard" {
		t.Fatalf("selected milestone + cascading task not resolved: %+v", day2.Focus[0])
	}

	// The choice persists (## Focus gains [milestone:: …]) and re-resolves on reload.
	day3, _ := s.Load("2026-06-29")
	if day3.Focus[0].MilestoneID != "home/backyard/yard-done" || day3.Focus[0].Milestone.Text != "Yard done" {
		t.Fatalf("milestone selection didn't persist: %+v", day3.Focus[0])
	}
}
