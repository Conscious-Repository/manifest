package hermes

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Session is a Hermes run's telemetry from /api/sessions. `Source` distinguishes
// cron runs ("cron") from interactive ("telegram") and API ("api_server") — the
// feed backfill filters on this. Unknown fields ignored.
type Session struct {
	ID              string  `json:"id"`
	Source          string  `json:"source"`
	Model           string  `json:"model"`
	Title           string  `json:"title"`
	StartedAt       float64 `json:"started_at"`
	EndedAt         float64 `json:"ended_at"`
	MessageCount    int     `json:"message_count"`
	ToolCallCount   int     `json:"tool_call_count"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	EstimatedCost   float64 `json:"estimated_cost_usd"`
	ActualCost      float64 `json:"actual_cost_usd"`
	Preview         string  `json:"preview"`
	ParentSessionID string  `json:"parent_session_id"`
}

// SessionMessage is one turn within a session (/api/sessions/{id}/messages).
type SessionMessage struct {
	ID           string  `json:"id"`
	Role         string  `json:"role"`
	Content      string  `json:"content"`
	ToolName     string  `json:"tool_name"`
	Timestamp    float64 `json:"timestamp"`
	FinishReason string  `json:"finish_reason"`
}

// ListSessions returns recent sessions, optionally filtered by source ("" = all).
func (c *Client) ListSessions(ctx context.Context, source string) ([]Session, error) {
	if !c.cfg.Configured() {
		return nil, ErrNotConfigured
	}
	q := url.Values{}
	if source != "" {
		q.Set("source", source)
	}
	q.Set("limit", strconv.Itoa(50))
	path := "/api/sessions"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out List[Session]
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// SessionMessages returns the messages of one session (the run's full output).
func (c *Client) SessionMessages(ctx context.Context, id string) ([]SessionMessage, error) {
	var out struct {
		Data []SessionMessage `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/sessions/"+id+"/messages", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// LastAssistantText returns the final assistant message text of a session — the
// run's deliverable, used by the feed backfill to materialize items.
func (c *Client) LastAssistantText(ctx context.Context, id string) (string, error) {
	msgs, err := c.SessionMessages(ctx, id)
	if err != nil {
		return "", err
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && msgs[i].Content != "" {
			return msgs[i].Content, nil
		}
	}
	return "", nil
}
