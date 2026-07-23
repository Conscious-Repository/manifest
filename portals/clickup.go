package portals

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// clickUp polls the whole ClickUp workspace over the v2 REST API with a personal
// token, cursored on date_updated. It is entirely deterministic: it reads tasks
// changed since the high-water mark and classifies each by the API's own
// timestamps (created / closed / otherwise updated). No LLM, no heuristics.
type clickUp struct {
	token string
	base  string // https://api.clickup.com/api/v2 (overridable in tests)
	http  *http.Client
}

func newClickUp(token, base string, hc *http.Client) *clickUp {
	if base == "" {
		base = "https://api.clickup.com/api/v2"
	}
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &clickUp{token: token, base: base, http: hc}
}

func (c *clickUp) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.token) // ClickUp personal tokens: no "Bearer"
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("clickup %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Test is the cheapest authenticated read (GET /user).
func (c *clickUp) Test(ctx context.Context) error {
	var out struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	if err := c.get(ctx, "/user", nil, &out); err != nil {
		return err
	}
	if out.User.ID == 0 {
		return fmt.Errorf("clickup: no authorized user")
	}
	return nil
}

type cuTask struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status struct {
		Status string `json:"status"`
	} `json:"status"`
	DateCreated string `json:"date_created"` // ms epoch as string
	DateUpdated string `json:"date_updated"`
	DateClosed  string `json:"date_closed"`
	URL         string `json:"url"`
	List        struct {
		Name string `json:"name"`
	} `json:"list"`
	Assignees []struct {
		ID int64 `json:"id"`
	} `json:"assignees"`
}

func cuMillis(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// Poll pulls tasks updated since the cursor across every team the token can see.
// First poll (no cursor) backfills 24h so a freshly-connected portal shows recent
// activity without dumping the whole history.
func (c *clickUp) Poll(ctx context.Context, since time.Time, now time.Time) ([]Event, map[string]string, error) {
	if since.IsZero() {
		since = now.Add(-24 * time.Hour)
	}
	me, teams, err := c.identity(ctx)
	if err != nil {
		return nil, nil, err
	}
	var events []Event
	high := since
	for _, team := range teams {
		tasks, err := c.teamTasks(ctx, team, since)
		if err != nil {
			return nil, nil, err
		}
		for _, t := range tasks {
			updated := cuMillis(t.DateUpdated)
			if !updated.After(since) {
				continue
			}
			if updated.After(high) {
				high = updated
			}
			events = append(events, c.classify(t, me, updated))
		}
	}
	return events, map[string]string{"task": high.Format(time.RFC3339)}, nil
}

func (c *clickUp) identity(ctx context.Context) (me int64, teams []string, err error) {
	var u struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	if err = c.get(ctx, "/user", nil, &u); err != nil {
		return 0, nil, err
	}
	var t struct {
		Teams []struct {
			ID string `json:"id"`
		} `json:"teams"`
	}
	if err = c.get(ctx, "/team", nil, &t); err != nil {
		return 0, nil, err
	}
	for _, tm := range t.Teams {
		teams = append(teams, tm.ID)
	}
	return u.User.ID, teams, nil
}

// teamTasks is the "Get Filtered Team Tasks" endpoint: whole-workspace, ordered
// by update time, paged until we pass the cursor.
func (c *clickUp) teamTasks(ctx context.Context, team string, since time.Time) ([]cuTask, error) {
	var out []cuTask
	for page := 0; page < 50; page++ { // hard page cap — a poll is bounded work
		q := url.Values{}
		q.Set("order_by", "updated")
		q.Set("reverse", "true") // newest first
		q.Set("subtasks", "true")
		q.Set("include_closed", "true")
		q.Set("date_updated_gt", strconv.FormatInt(since.UnixMilli(), 10))
		q.Set("page", strconv.Itoa(page))
		var resp struct {
			Tasks    []cuTask `json:"tasks"`
			LastPage bool     `json:"last_page"`
		}
		if err := c.get(ctx, "/team/"+team+"/task", q, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Tasks...)
		if resp.LastPage || len(resp.Tasks) == 0 {
			break
		}
	}
	return out, nil
}

func (c *clickUp) classify(t cuTask, me int64, updated time.Time) Event {
	kind := "task-updated"
	at := updated
	if closed := cuMillis(t.DateClosed); !closed.IsZero() {
		kind, at = "task-closed", closed
	} else if created := cuMillis(t.DateCreated); created.After(updated.Add(-time.Second)) && !created.IsZero() {
		kind, at = "task-created", created
	}
	forMe := false
	for _, a := range t.Assignees {
		if a.ID == me {
			forMe = true
			break
		}
	}
	return Event{
		ID:     "clickup:" + kind + ":" + t.ID + ":" + strconv.FormatInt(at.UnixMilli(), 10),
		Portal: "clickup", Kind: kind, Title: t.Name, Detail: t.Status.Status,
		URL: t.URL, At: at, ForMe: forMe, List: t.List.Name,
	}
}

// ---- deterministic daily digest (script-rendered, no LLM) ----

// DigestLine is one promotable row in a digest card.
type DigestLine struct {
	Text  string `json:"text"` // "created · Close 743 N Euclid"
	URL   string `json:"url"`
	ForMe bool   `json:"forMe"`
}

// DigestGroup is a list's changes (or the "for you" block when List=="").
type DigestGroup struct {
	List  string       `json:"list"`
	Lines []DigestLine `json:"lines"`
}

// buildDigest turns a day's ClickUp events into one card: an "assigned to you /
// mentions you" block first, then per-list groups (created · status · closed).
// Pure function of the events + day — same input, same card, always.
func buildDigest(events []Event, day string, loc *time.Location) (id string, forYou []DigestLine, groups []DigestGroup, at time.Time) {
	id = "clickup-digest:" + day
	byList := map[string][]DigestLine{}
	var lists []string
	for _, e := range events {
		if e.Portal != "clickup" || e.At.In(loc).Format("2006-01-02") != day {
			continue
		}
		if e.At.After(at) {
			at = e.At
		}
		verb := map[string]string{"task-created": "created", "task-closed": "closed", "task-updated": "changed"}[e.Kind]
		line := DigestLine{Text: verb + " · " + e.Title, URL: e.URL, ForMe: e.ForMe}
		if e.ForMe {
			forYou = append(forYou, line)
		}
		list := e.List
		if list == "" {
			list = "—"
		}
		if _, ok := byList[list]; !ok {
			lists = append(lists, list)
		}
		byList[list] = append(byList[list], line)
	}
	sort.Strings(lists)
	for _, l := range lists {
		lines := byList[l]
		sort.Slice(lines, func(i, j int) bool { return lines[i].Text < lines[j].Text })
		groups = append(groups, DigestGroup{List: l, Lines: lines})
	}
	sort.Slice(forYou, func(i, j int) bool { return forYou[i].Text < forYou[j].Text })
	return id, forYou, groups, at
}

// digestDays returns the distinct America/Chicago days present in the events,
// newest first — each becomes at most one card.
func digestDays(events []Event, loc *time.Location) []string {
	seen := map[string]bool{}
	var days []string
	for _, e := range events {
		if e.Portal != "clickup" {
			continue
		}
		d := e.At.In(loc).Format("2006-01-02")
		if !seen[d] {
			seen[d] = true
			days = append(days, d)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	return days
}

var _ = strings.TrimSpace
