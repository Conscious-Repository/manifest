package transcriptsync

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// noteListItem is the summary shape from GET /notes.
type noteListItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

// ListNotesSince pages GET /notes?created_after=<ts> and returns the summaries,
// newest cutoff first. It bounds pages defensively so a runaway backlog can't
// spin forever.
func (c *GranolaClient) ListNotesSince(ctx context.Context, since time.Time) ([]noteListItem, error) {
	var all []noteListItem
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 40; page++ { // 40 * 50 = 2000 notes ceiling per run
		q := url.Values{}
		q.Set("limit", "50")
		if !since.IsZero() {
			q.Set("created_after", since.UTC().Format(time.RFC3339))
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var resp struct {
			Notes   []noteListItem `json:"notes"`
			HasMore *bool          `json:"hasMore"`
			Cursor  string         `json:"cursor"`
		}
		if err := c.get(ctx, "/notes?"+q.Encode(), &resp); err != nil {
			return nil, err
		}
		if resp.HasMore == nil || resp.Notes == nil {
			return nil, fmt.Errorf("granola: incomplete list response")
		}
		all = append(all, resp.Notes...)
		if !*resp.HasMore {
			return all, nil
		}
		if resp.Cursor == "" || seen[resp.Cursor] {
			return nil, fmt.Errorf("granola: incomplete pagination")
		}
		seen[resp.Cursor] = true
		cursor = resp.Cursor
	}
	return nil, fmt.Errorf("granola: page limit exceeded")
}

// FetchDetail gets GET /notes/<id>?include=transcript and maps it to a
// granolaNote (top-level object, no data wrapper).
func (c *GranolaClient) FetchDetail(ctx context.Context, id string) (granolaNote, error) {
	var raw struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		CreatedAt  string `json:"created_at"`
		Transcript []struct {
			Text    string `json:"text"`
			Speaker struct {
				Name   string `json:"name"`
				Source string `json:"source"`
			} `json:"speaker"`
		} `json:"transcript"`
		Participants []struct {
			Name string `json:"name"`
		} `json:"participants"`
	}
	if err := c.get(ctx, "/notes/"+url.PathEscape(id)+"?include=transcript", &raw); err != nil {
		return granolaNote{}, err
	}
	n := granolaNote{ID: raw.ID, Title: raw.Title, CreatedAt: parseGranolaTime(raw.CreatedAt)}
	for _, s := range raw.Transcript {
		n.Segments = append(n.Segments, granolaSegment{Text: s.Text, Name: s.Speaker.Name, Source: s.Speaker.Source})
	}
	for _, p := range raw.Participants {
		if strings.TrimSpace(p.Name) != "" {
			n.Participants = append(n.Participants, p.Name)
		}
	}
	return n, nil
}

// parseGranolaTime tolerates RFC3339 with or without fractional seconds; a bad
// value yields the zero time (sorts oldest, harmless for watermarking).
func parseGranolaTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
