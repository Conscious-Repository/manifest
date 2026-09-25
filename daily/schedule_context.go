package daily

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"manifest/vaultwriter"
)

// AuthoredSchedule reads only persisted rows from the daily manifest region.
// It never fetches calendar events, fills defaults, reads journal context or
// creates the would-be daily note returned by the locator.
func (s *Service) AuthoredSchedule(date string) ([]ScheduleRow, error) {
	if _, err := time.Parse(dateLayout, date); err != nil {
		return nil, fmt.Errorf("schedule date must be YYYY-MM-DD")
	}
	path, err := s.idx.DailyNote(date)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(s.cfg.VaultPath, path)
	if err != nil {
		return nil, err
	}
	path, err = vaultwriter.SafePath(s.cfg.VaultPath, rel)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return []ScheduleRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("daily source unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1024*1024 || !utf8.Valid(raw) || strings.IndexByte(string(raw), 0) >= 0 {
		return nil, fmt.Errorf("daily source exceeds supported text limits")
	}
	content := string(raw)
	if strings.Count(content, dailyStart) != strings.Count(content, dailyEnd) || strings.Count(content, dailyStart) > 1 {
		return nil, fmt.Errorf("daily manifest region is ambiguous or incomplete")
	}
	block, found := regionBetween(content, dailyStart, dailyEnd)
	if !found && strings.Contains(content, dailyStart) {
		return nil, fmt.Errorf("daily manifest region is incomplete")
	}
	rows, _, _ := parseBlock(block)
	out := []ScheduleRow{}
	seen := map[int]bool{}
	for _, row := range rows {
		slot, ok := parseSlot(row.Time)
		if !ok {
			continue
		}
		if seen[slot] {
			return nil, fmt.Errorf("daily schedule contains duplicate slot identities")
		}
		seen[slot] = true
		if strings.TrimSpace(row.Label) != "" || row.Focused {
			out = append(out, row)
		}
	}
	return out, nil
}
