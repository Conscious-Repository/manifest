package calendar

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/daily"
)

// Source adapts the Google client to daily.EventSource. When the client is
// disabled or a fetch fails, it falls back to the offline cache mirror so the
// schedule still shows the last-known events.
type Source struct {
	client   *Client
	cacheDir string // <dataDir>/calendar-cache (derived data — outside the vault)
	timeout  time.Duration
}

func NewSource(c *Client, cacheDir string) *Source {
	return &Source{client: c, cacheDir: cacheDir, timeout: 10 * time.Second}
}

func (s *Source) Slots(date string) ([]daily.CalSlot, error) {
	day, err := time.ParseInLocation("2006-01-02", date, s.client.Location())
	if err != nil {
		return nil, err
	}
	if !s.client.Enabled() {
		return s.readCache(date), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	events, err := s.client.Events(ctx, day, day.Add(24*time.Hour))
	if err != nil {
		return s.readCache(date), nil // offline / API error -> cached mirror
	}
	slots := EventsToSlots(events, day, s.client.Location())
	s.writeCache(date, slots) // best-effort offline mirror, only on change
	out := make([]daily.CalSlot, 0, len(slots))
	for _, sl := range slots {
		out = append(out, daily.CalSlot{Token: sl.Token, Title: sl.Title, EventID: sl.EventID})
	}
	return out, nil
}

// ----- offline mirror cache (read-only snapshot; Google is the source of truth) -----

const (
	calStart = "<!-- manifest:cal:start -->"
	calEnd   = "<!-- manifest:cal:end -->"
)

func (s *Source) cachePath(date string) string { return filepath.Join(s.cacheDir, date+".md") }

func buildCache(date string, slots []Slot) string {
	var b strings.Builder
	b.WriteString("---\ntype: calendar-cache\ndate: " + date + "\n")
	b.WriteString("note: Read-only mirror. Google Calendar is the source of truth.\n---\n\n")
	b.WriteString(calStart + "\n")
	b.WriteString("| Slot | Title | EventID |\n| --- | --- | --- |\n")
	for _, sl := range slots {
		b.WriteString("| " + sl.Token + " | " + strings.ReplaceAll(sl.Title, "|", "\\|") + " | " + sl.EventID + " |\n")
	}
	b.WriteString(calEnd + "\n")
	return b.String()
}

func (s *Source) writeCache(date string, slots []Slot) {
	content := buildCache(date, slots)
	path := s.cachePath(date)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		return // unchanged — avoid vault churn
	}
	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(content), 0o644)
}

func (s *Source) readCache(date string) []daily.CalSlot {
	data, err := os.ReadFile(s.cachePath(date))
	if err != nil {
		return nil
	}
	content := string(data)
	si, ei := strings.Index(content, calStart), strings.Index(content, calEnd)
	if si < 0 || ei <= si {
		return nil
	}
	var out []daily.CalSlot
	for _, line := range strings.Split(content[si:ei], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitPipes(line)
		if len(cells) < 2 || cells[0] == "Slot" || strings.HasPrefix(cells[0], "--") {
			continue
		}
		cs := daily.CalSlot{Token: cells[0], Title: cells[1]}
		if len(cells) >= 3 {
			cs.EventID = cells[2]
		}
		out = append(out, cs)
	}
	return out
}

func splitPipes(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
