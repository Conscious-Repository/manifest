package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := defaultConfig()
	cfg.VaultPath = dir
	return NewStore(cfg), dir
}

func TestSlotTokenRoundTrip(t *testing.T) {
	for min := 0; min < 1440; min += 30 {
		tok := slotToken(min)
		got, ok := parseSlot(tok)
		if !ok || got != min {
			t.Fatalf("min %d -> %q -> %d ok=%v", min, tok, got, ok)
		}
	}
	// a bare hour is the :00 slot
	if m, ok := parseSlot("9A"); !ok || m != 540 {
		t.Fatalf("bare 9A -> %d ok=%v", m, ok)
	}
}

func TestJournalPreserved(t *testing.T) {
	s, dir := testStore(t)
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

	// Re-save (simulating a second edit) must not duplicate the block.
	if err := s.SaveDay("2026-06-29", sched, tasks); err != nil {
		t.Fatal(err)
	}
	out2, _ := os.ReadFile(daily)
	if strings.Count(string(out2), dailyStart) != 1 {
		t.Fatal("manifest block was duplicated on re-save")
	}
}

func TestObsidianEditIsReadBack(t *testing.T) {
	s, dir := testStore(t)
	daily := filepath.Join(dir, "Daily", "2026-06-29.md")
	// Simulate the user editing the schedule by hand in Obsidian.
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

func TestGoalsAndMilestones(t *testing.T) {
	s, _ := testStore(t)
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
