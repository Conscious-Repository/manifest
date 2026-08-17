// Package fundraising implements the private, vault-backed Aion fundraising
// CRM. It deliberately has no dependency on the public/team Aion projection.
package fundraising

const (
	StatusProspect  = "prospect"
	StatusActive    = "active"
	StatusCommitted = "committed"
	StatusPassed    = "passed"

	InterestUnknown = "unknown"
	InterestHigh    = "high"
	InterestMedium  = "medium"
	InterestLow     = "low"
)

var Statuses = []string{StatusProspect, StatusActive, StatusCommitted, StatusPassed}
var Interests = []string{InterestUnknown, InterestHigh, InterestMedium, InterestLow}

// PersonRef is an explicit CRM→contact edge. NotePath is optional: the shared
// CRM registry carries note-less people until the owner elects to create a
// personal note.
type PersonRef struct {
	Key      string   `json:"key"`
	Display  string   `json:"display"`
	NotePath string   `json:"notePath,omitempty"`
	Emails   []string `json:"emails,omitempty"`
}

// SourceRef records how an opportunity entered the pipeline. Exactly one of
// Contact or Text is set: linked introductions retain contact identity while
// channel-style sources such as "DM" or "cold intro" remain honest free text.
type SourceRef struct {
	Contact *PersonRef `json:"contact,omitempty"`
	Text    string     `json:"text,omitempty"`
}

// Opportunity is one Markdown record under system/crm/fundraising/.
type Opportunity struct {
	ID                     string      `json:"id"`
	Path                   string      `json:"path"`
	Firm                   string      `json:"firm"`
	Status                 string      `json:"status"`
	Interest               string      `json:"interest"`
	Amount                 float64     `json:"amount,omitempty"`
	Currency               string      `json:"currency"`
	People                 []PersonRef `json:"people"`
	Source                 *SourceRef  `json:"source,omitempty"`
	IntroVia               string      `json:"introVia"`
	LastTouchpoint         string      `json:"lastTouchpoint"`
	LastTouchpointDate     string      `json:"lastTouchpointDate"`
	ComputedLastTouchpoint string      `json:"computedLastTouchpoint,omitempty"`
	NextStep               string      `json:"nextStep"`
	NextStepDue            string      `json:"nextStepDue"`
	Notes                  string      `json:"notes"`
	Archived               bool        `json:"archived"`
	SourceRows             []int       `json:"sourceRows,omitempty"`
	ImportReview           bool        `json:"importReview"`
}

// RegistryPerson is a row in system/crm/contacts.md.
type RegistryPerson struct {
	Key      string   `json:"key"`
	Display  string   `json:"display"`
	NotePath string   `json:"notePath,omitempty"`
	Emails   []string `json:"emails,omitempty"`
}

// Resource is a private fundraising reference link.
type Resource struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}
