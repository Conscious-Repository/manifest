package daily

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type forbiddenContextCalendar struct{}

func (forbiddenContextCalendar) Slots(string) ([]CalSlot, error) {
	panic("context must not fetch calendar")
}
func TestAuthoredScheduleContext(t *testing.T) {
	s, root := testService(t)
	s.UseEvents(forbiddenContextCalendar{})
	date := "2026-09-25"
	path := filepath.Join(root, "Daily", date+".md")
	raw := "PRIVATE JOURNAL\n" + dailyStart + "\n## Schedule\n| 9A | Reviewed work | x |\n| 9:30A | | |\n## Tasks\n- [ ] PRIVATE TASK\n" + dailyEnd
	if err := testWrite(path, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	rows, err := s.AuthoredSchedule(date)
	if err != nil || len(rows) != 1 || rows[0].Label != "Reviewed work" || !rows[0].Focused {
		t.Fatal(rows, err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != raw {
		t.Fatal("source changed")
	}
	for _, bad := range []string{dailyStart, dailyEnd + dailyStart, raw + dailyStart + dailyEnd, strings.Replace(raw, "| 9:30A | | |", "| 9:00A | duplicate | |", 1)} {
		if err := testWrite(path, []byte(bad)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AuthoredSchedule(date); err == nil {
			t.Fatal("ambiguous schedule accepted")
		}
	}
	if _, err := s.AuthoredSchedule("../private"); err == nil {
		t.Fatal("invalid date accepted")
	}
	if rows, err := s.AuthoredSchedule("2026-09-26"); err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	if _, err := os.Stat(filepath.Join(root, "Daily", "2026-09-26.md")); !os.IsNotExist(err) {
		t.Fatal("read created note")
	}
}
