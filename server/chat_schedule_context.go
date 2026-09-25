package server

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

func (s *Server) chatScheduleRecords(query string) ([]chatContextRecord, error) {
	if s.svc == nil {
		return nil, fmt.Errorf("daily schedule unavailable")
	}
	date := time.Now().Format("2006-01-02")
	q := strings.TrimSpace(query)
	if len(q) >= 10 {
		if _, err := time.Parse("2006-01-02", q[:10]); err == nil {
			date = q[:10]
		}
	}
	rows, err := s.svc.AuthoredSchedule(date)
	if err != nil {
		return nil, err
	}
	out := []chatContextRecord{}
	for _, row := range rows {
		row := row
		id := date + "/" + row.Time
		title := row.Label
		if title == "" {
			title = "Focused slot"
		}
		out = append(out, chatContextRecord{Kind: "schedule", ID: id, Title: title, Detail: date + " · " + row.Time, Route: scheduleContextRoute(id), schedule: &row})
	}
	return out, nil
}
func scheduleContextRoute(id string) string {
	return "#/day/" + url.PathEscape(strings.SplitN(id, "/", 2)[0])
}
