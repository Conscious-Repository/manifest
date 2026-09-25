package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"manifest/artifacts"
	"manifest/calendar"
)

type contextCalendarFixture struct {
	snapshot   calendar.EventSnapshot
	calls      int
	start, end time.Time
	loc        *time.Location
}

func (f *contextCalendarFixture) Enabled() bool            { return true }
func (f *contextCalendarFixture) Location() *time.Location { return f.loc }
func (f *contextCalendarFixture) EventsSnapshot(_ context.Context, start, end time.Time) (calendar.EventSnapshot, error) {
	f.calls++
	f.start = start
	f.end = end
	return f.snapshot, nil
}
func TestCalendarContextSourceVersionAndPrivacy(t *testing.T) {
	s, se, sends := terminalReceiptFixture(t, false, func(text string) {
		if !strings.Contains(text, "FROZEN_CALENDAR") || strings.Contains(text, "OTHER_PRIVATE_ATTENDEE") {
			t.Error("wrong calendar snapshot")
		}
	})
	explicitArtifactFixture(t, s)
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 3, 8, 10, 0, 0, 0, loc)
	first := calendar.Event{ID: "same-id", Account: "one@example.test", CalendarID: "primary", Title: "FROZEN_CALENDAR", Start: start, End: start.Add(time.Hour), Attendees: []calendar.Attendee{{Name: "Selected attendee", Email: "selected@example.test"}}}
	other := first
	other.Account = "two@example.test"
	other.Attendees = []calendar.Attendee{{Name: "OTHER_PRIVATE_ATTENDEE", Email: "other@example.test"}}
	source := &contextCalendarFixture{loc: loc, snapshot: calendar.EventSnapshot{Events: []calendar.Event{first, other}}}
	s.calendarRecords = source
	code, results := artifactsDo(t, s, "GET", "/api/chat/records?kind=calendar&q=2026-03-08", "")
	if code != 200 || len(results["records"].([]any)) != 2 {
		t.Fatal(code, results)
	}
	rows := results["records"].([]any)
	if rows[0].(map[string]any)["id"] == rows[1].(map[string]any)["id"] {
		t.Fatal("colliding provider ids merged")
	}
	if source.end.Sub(source.start) != 23*time.Hour {
		t.Fatal("wrong local day", source.start, source.end)
	}
	id := "2026-03-08/" + first.Key()
	preview := contextPreview(t, s, "calendar", id)
	content := preview["content"].(string)
	if !strings.Contains(content, "one@example.test") || !strings.Contains(content, "selected@example.test") || strings.Contains(content, "OTHER_PRIVATE_ATTENDEE") {
		t.Fatal(content)
	}
	code, ref := contextRetain(t, s, "calendar", id, preview["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	a, _ := s.artifactReg.Get(ref["id"].(string))
	before := source.calls
	if links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]; len(links) != 1 || links[0].Route != "#/calendar/2026-03-08" || source.calls != before {
		t.Fatal("source link fetched or lost event", links)
	}
	source.snapshot.Partial = true
	if _, _, err := s.chatContextRecordPreview("calendar", id); err == nil {
		t.Fatal("partial preview accepted")
	}
	if code, _ := contextRetain(t, s, "calendar", id, preview["revision"].(string)); code == 200 {
		t.Fatal("partial retain accepted")
	}
	source.snapshot.Partial = false
	source.snapshot.Events[0].Title = "CHANGED_CALENDAR"
	if code, _ := contextRetain(t, s, "calendar", id, preview["revision"].(string)); code != 409 {
		t.Fatal("stale reviewed event accepted", code)
	}
	refs := []artifactContextRef{{ID: a.ID, Revision: a.Head}}
	payload, _ := json.Marshal(map[string]any{"text": "Review this event", "requestId": "receipt-input-001", "explicitArtifacts": true, "artifacts": refs})
	for i := 0; i < 2; i++ {
		if w := receiptInput(s, se.ID, string(payload)); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if sends.Load() != 1 {
		t.Fatal("duplicate input", sends.Load())
	}
	shared, sharedSE, _, sharedSends := sharedInputFixture(t, false)
	shared.artifactReg = s.artifactReg
	if w := receiptInput(shared, sharedSE.ID, string(payload)); w.Code == 200 || sharedSends.Load() != 0 {
		t.Fatal("private calendar shared", w.Code)
	}
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/chat/records?kind=calendar", nil))
	if w.Code < 400 {
		t.Fatal("public calendar records", w.Code)
	}
	source.snapshot.Events[0].Account = ""
	if _, _, err := s.chatContextRecordPreview("calendar", id); err == nil {
		t.Fatal("unqualified source accepted")
	}
}
