package calendar

// EventIssue identifies an incomplete source without retaining provider error
// bodies (which may contain credentials or unrelated request details).
type EventIssue struct {
	Account    string `json:"account"`
	CalendarID string `json:"calendarId,omitempty"`
	Scope      string `json:"scope"`
}
type EventSnapshot struct {
	Events  []Event
	Partial bool
	Issues  []EventIssue
}
