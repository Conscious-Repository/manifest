package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"manifest/calendar"
)

// The private Records surface needs completeness as well as event data. This
// read-only seam also permits other calendar adapters to supply that contract.
type calendarRecordSource interface {
	Enabled() bool
	Location() *time.Location
	EventsSnapshot(context.Context, time.Time, time.Time) (calendar.EventSnapshot, error)
}

func (s *Server) chatCalendarRecords(query string) ([]chatContextRecord, error) {
	return s.chatCalendarRecordsFor(context.Background(), query)
}
func (s *Server) chatCalendarRecordsFor(parent context.Context, query string) ([]chatContextRecord, error) {
	source := s.calendarRecords
	if source == nil || !source.Enabled() {
		return nil, fmt.Errorf("calendar connection unavailable")
	}
	loc := source.Location()
	date := time.Now().In(loc).Format("2006-01-02")
	q := strings.TrimSpace(query)
	if len(q) >= 10 {
		if _, err := time.Parse("2006-01-02", q[:10]); err == nil {
			date = q[:10]
		}
	}
	day, _ := time.ParseInLocation("2006-01-02", date, loc)
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	snapshot, err := source.EventsSnapshot(ctx, day, day.AddDate(0, 0, 1))
	if err != nil || ctx.Err() != nil {
		return nil, fmt.Errorf("calendar could not be read; reconnect or retry before selecting context")
	}
	if snapshot.Partial || len(snapshot.Issues) > 0 {
		return nil, fmt.Errorf("calendar read is incomplete; retry after all sources can be read before selecting context")
	}
	rows := []chatContextRecord{}
	for _, event := range snapshot.Events {
		key := event.Key()
		if key == "" || event.Start.IsZero() || !event.End.After(event.Start) {
			return nil, fmt.Errorf("calendar event source identity or time is unavailable")
		}
		event := event
		id := date + "/" + key
		rows = append(rows, chatContextRecord{Kind: "calendar", ID: id, Title: event.Title, Detail: date + " · " + event.Start.In(loc).Format("15:04 MST") + " · " + event.Account + " · " + event.CalendarID, Route: calendarContextRoute(id), calendarEvent: &event})
	}
	return rows, nil
}
func calendarContextRoute(id string) string { return "#/calendar/" + strings.SplitN(id, "/", 2)[0] }
func renderCalendarContext(out *strings.Builder, event calendar.Event) {
	contextField(out, "Connected account", event.Account)
	contextField(out, "Calendar ID", event.CalendarID)
	contextField(out, "Provider event ID", event.ID)
	contextField(out, "Source key", event.Key())
	contextField(out, "Starts", event.Start.Format(time.RFC3339))
	contextField(out, "Ends", event.End.Format(time.RFC3339))
	fmt.Fprintf(out, "All day: %t\nDeclined by this account: %t\n", event.AllDay, event.Declined)
	attendees := []string{}
	for _, a := range event.Attendees {
		attendees = append(attendees, a.Name+" <"+a.Email+">")
	}
	sort.Strings(attendees)
	out.WriteString("\n## Participants reported by the calendar (excluding self)\n\n")
	for _, a := range attendees {
		out.WriteString("- " + a + "\n")
	}
	out.WriteString("\nThis snapshot includes the available title, times and participant fields. Event descriptions, attachments, linked notes and other events are excluded. Selection does not send invitations or change the calendar.\n")
}
