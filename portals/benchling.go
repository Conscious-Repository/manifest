package portals

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// benchling polls a Benchling tenant over the v2 REST API (API key as HTTP Basic
// username). It casts the widest deterministic net the list API allows: registry
// entities, sequences, assay results, notebook entries, and requests — sorted by
// modifiedAt, paged until older than the cursor, filtered client-side. "New +
// edited": an object's modifiedAt is folded into the event id, so an edit
// surfaces a fresh card. No LLM, no digest heuristic — one card per change.
type benchling struct {
	key  string
	base string // https://<tenant>.benchling.com/api/v2
	http *http.Client
}

// benchResource is one pollable object-type: its list path, the JSON array key
// the tenant returns it under, and the normalized Event.Kind.
type benchResource struct {
	kind    string // Event.Kind: entity | sequence | result | entry | request
	path    string // /custom-entities …
	listKey string // "customEntities" …
}

// pollable is the "everything" set the user chose.
var benchResources = []benchResource{
	{"entity", "/custom-entities", "customEntities"},
	{"sequence", "/dna-sequences", "dnaSequences"},
	{"sequence", "/aa-sequences", "aaSequences"},
	{"result", "/assay-results", "assayResults"},
	{"entry", "/entries", "entries"},
	{"request", "/requests", "requests"},
}

func newBenchling(key, tenant, base string, hc *http.Client) *benchling {
	if base == "" {
		base = "https://" + tenant + ".benchling.com/api/v2"
	}
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &benchling{key: key, base: base, http: hc}
}

func (b *benchling) get(ctx context.Context, path string, q url.Values, out any) error {
	u := b.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(b.key, "") // Benchling: API key is the basic-auth username
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("benchling %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Test is a page-size-1 registry read (cheapest authenticated call).
func (b *benchling) Test(ctx context.Context) error {
	q := url.Values{"pageSize": {"1"}}
	var out map[string]json.RawMessage
	return b.get(ctx, "/custom-entities", q, &out)
}

// benchObj is the defensively-parsed subset every resource shares.
type benchObj struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"createdAt"`
	ModifiedAt string `json:"modifiedAt"`
	WebURL     string `json:"webURL"`
	Creator    struct {
		Name string `json:"name"`
	} `json:"creator"`
	Schema struct {
		Name string `json:"name"`
	} `json:"schema"`
}

func benchTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// Poll walks every resource type. First poll (no cursor) backfills 24h.
func (b *benchling) Poll(ctx context.Context, since time.Time, now time.Time) ([]Event, map[string]string, error) {
	if since.IsZero() {
		since = now.Add(-24 * time.Hour)
	}
	var events []Event
	high := since
	for _, r := range benchResources {
		objs, err := b.list(ctx, r, since)
		if err != nil {
			return nil, nil, err
		}
		for _, o := range objs {
			mod := benchTime(o.ModifiedAt)
			if mod.IsZero() {
				mod = benchTime(o.CreatedAt)
			}
			if !mod.After(since) {
				continue
			}
			if mod.After(high) {
				high = mod
			}
			events = append(events, b.event(r, o, mod))
		}
	}
	return events, map[string]string{"modified": high.Format(time.RFC3339)}, nil
}

// list pages modifiedAt-desc and stops once objects fall at/under the cursor.
func (b *benchling) list(ctx context.Context, r benchResource, since time.Time) ([]benchObj, error) {
	var out []benchObj
	token := ""
	for page := 0; page < 50; page++ { // bounded work per poll
		q := url.Values{}
		q.Set("sort", "modifiedAt:desc")
		q.Set("pageSize", "100")
		if token != "" {
			q.Set("nextToken", token)
		}
		var raw map[string]json.RawMessage
		if err := b.get(ctx, r.path, q, &raw); err != nil {
			return nil, err
		}
		var objs []benchObj
		if arr, ok := raw[r.listKey]; ok {
			_ = json.Unmarshal(arr, &objs)
		}
		reachedCursor := false
		for _, o := range objs {
			mod := benchTime(o.ModifiedAt)
			if mod.IsZero() {
				mod = benchTime(o.CreatedAt)
			}
			if !mod.After(since) {
				reachedCursor = true // sorted desc — everything after is older
				break
			}
			out = append(out, o)
		}
		if reachedCursor || len(objs) == 0 {
			break
		}
		if t, ok := raw["nextToken"]; ok {
			_ = json.Unmarshal(t, &token)
		}
		if token == "" {
			break
		}
	}
	return out, nil
}

func (b *benchling) event(r benchResource, o benchObj, mod time.Time) Event {
	title := o.Name
	if title == "" { // assay results carry no name — identify by schema + id
		title = o.Schema.Name
		if title == "" {
			title = r.kind
		}
		title += " " + o.ID
	}
	detail := o.Schema.Name
	url := o.WebURL
	if url == "" {
		url = b.base // fall back to the tenant root rather than a dead link
	}
	// modifiedAt in the id → an edit is a distinct event (re-surfaces as a card).
	id := "benchling:" + r.kind + ":" + o.ID + ":" + strconv.FormatInt(mod.Unix(), 10)
	return Event{
		ID: id, Portal: "benchling", Kind: r.kind, Title: title, Detail: detail,
		URL: url, Actor: o.Creator.Name, At: mod,
	}
}
