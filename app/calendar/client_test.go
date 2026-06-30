package calendar

import (
	"testing"

	gcal "google.golang.org/api/calendar/v3"
)

func TestSelfDeclined(t *testing.T) {
	att := func(self bool, status string) *gcal.EventAttendee {
		return &gcal.EventAttendee{Self: self, ResponseStatus: status}
	}
	cases := []struct {
		name string
		ev   *gcal.Event
		want bool
	}{
		{"self declined", &gcal.Event{Attendees: []*gcal.EventAttendee{att(true, "declined")}}, true},
		{"self accepted", &gcal.Event{Attendees: []*gcal.EventAttendee{att(true, "accepted")}}, false},
		{"self needsAction", &gcal.Event{Attendees: []*gcal.EventAttendee{att(true, "needsAction")}}, false},
		{"other declined", &gcal.Event{Attendees: []*gcal.EventAttendee{{Email: "x@y.com", ResponseStatus: "declined"}}}, false},
		{"no attendees", &gcal.Event{}, false},
	}
	for _, c := range cases {
		if got := selfDeclined(c.ev); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
