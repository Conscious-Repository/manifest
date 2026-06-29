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
