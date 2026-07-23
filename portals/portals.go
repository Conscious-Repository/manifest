// Package portals is manifest's registry of external realms — ClickUp, Benchling,
// and (v2) Docusign — as credentialed, polled connections that feed the FEED.
//
// "Portal" is already excalibur canon (spirit cornerstones declare
// `portal:: claude-sub`); this package widens the term to every external service
// the app itself reaches. The calendar and the LLM portals are surfaced by the
// server (they have their own credential stores); THIS package owns the source
// portals that manifest polls directly over HTTP with an API key.
//
// Everything here is DETERMINISTIC: pollers diff cursor-based API reads and the
// cards they produce are script-rendered. No LLM is ever in the loop — the tune
// ritual's quality signal stays byte-identical no matter what ClickUp or
// Benchling does. We're building infrastructure, not a digest heuristic.
package portals

import (
	"strings"
	"time"
)

// Kind distinguishes how a portal authenticates and whether this package polls it.
type Kind string

const (
	KindAPIKey Kind = "apikey" // manifest holds a key and polls (clickup, benchling)
	KindOAuth  Kind = "oauth"  // browser sign-in, credentials owned elsewhere (calendar)
	KindLLM    Kind = "llm"    // conduit owned by the excalibur engine (claude-sub, gpt-sub)
)

// State is a portal's connection health, derived only from the last test/poll —
// there are no background health pings.
type State string

const (
	StateOpen     State = "open"     // last test/poll succeeded
	StateDegraded State = "degraded" // last attempt failed (Err carries the reason)
	StateSealed   State = "sealed"   // no credentials present
	StateDormant  State = "dormant"  // registered but not wired (docusign v2)
)

// CredField is one credential the panel form collects for an api-key portal.
// Secret fields render masked (last 4) and never leave the server in full.
type CredField struct {
	Key    string `json:"key"`    // e.g. "token", "apiKey", "tenant"
	Label  string `json:"label"`  // human label in the form
	Secret bool   `json:"secret"` // masked in the UI, redacted in logs
	Hint   string `json:"hint"`   // placeholder / help text
}

// Def is a portal definition — pure data, so github/docusign register later
// without code. The registry below is the whole v1 source-portal set.
type Def struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Kind     Kind        `json:"kind"`
	Fields   []CredField `json:"fields"` // credential form (api-key portals)
	Polled   bool        `json:"polled"` // a poller crosses it on a schedule
	Interval time.Duration
}

// primarySecret returns the field whose last 4 chars mask the row (the key).
func (d Def) primarySecret() string {
	for _, f := range d.Fields {
		if f.Secret {
			return f.Key
		}
	}
	return ""
}

// Registry is the source portals this package owns. Order is display order.
// Calendar + LLM portals are appended by the server (they live elsewhere).
var Registry = []Def{
	{
		ID: "clickup", Name: "ClickUp", Kind: KindAPIKey, Polled: true, Interval: 30 * time.Minute,
		Fields: []CredField{
			{Key: "token", Label: "Personal API token", Secret: true, Hint: "pk_…  (ClickUp → Settings → Apps)"},
		},
	},
	{
		ID: "benchling", Name: "Benchling", Kind: KindAPIKey, Polled: true, Interval: 30 * time.Minute,
		Fields: []CredField{
			{Key: "tenant", Label: "Tenant subdomain", Secret: false, Hint: "e.g. specialt  (from specialt.benchling.com)"},
			{Key: "apiKey", Label: "API key", Secret: true, Hint: "sk_…  (Benchling → Account → API keys)"},
		},
	},
	{
		// Docusign registers now but is dormant — nothing polls, no key form.
		ID: "docusign", Name: "Docusign", Kind: KindAPIKey, Polled: false,
	},
}

// Row is one line in the PORTALS panel, assembled from a Def + live state.
type Row struct {
	Def
	State        State    `json:"state"`
	Err          string   `json:"err,omitempty"`          // degraded reason (muted)
	Masked       string   `json:"masked,omitempty"`       // "····k7q2" or "" when sealed
	LastCrossing string   `json:"lastCrossing,omitempty"` // RFC3339 of last successful poll
	Have         []string `json:"have,omitempty"`         // keys currently set (never values)
	Delegated    bool     `json:"delegated,omitempty"`    // calendar/LLM: managed outside this pkg
}

// Event is one externally-sourced change, normalized across portals and cached
// on disk. Deterministic id → idempotent across re-polls and reloads.
type Event struct {
	ID     string    `json:"id"`     // stable: "<portal>:<kind>:<extID>[:<modifiedAt>]"
	Portal string    `json:"portal"` // "clickup" | "benchling"
	Kind   string    `json:"kind"`   // task-created | status-changed | result | entity | …
	Title  string    `json:"title"`  // the object's name
	Detail string    `json:"detail"` // secondary line (schema, list, status)
	Change string    `json:"change"` // deterministic "what changed" (snapshot diff): "Backlog → In Progress", "created", "new", "edited"
	URL    string    `json:"url"`    // web link into the source app
	Actor  string    `json:"actor"`  // creator / assignee
	At     time.Time `json:"at"`     // event timestamp (created/modified)
	ForMe  bool      `json:"forMe"`  // clickup: assigned to / mentions Benjamin
	List   string    `json:"list"`   // clickup: the list the task lives in
}

func maskLast4(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 4 {
		if s == "" {
			return ""
		}
		return "····" + s
	}
	return "····" + s[len(s)-4:]
}
