package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Job mirrors a Hermes cron job from /api/jobs. Note: the jobs endpoints use a
// `{"jobs":[…]}` / `{"job":{…}}` envelope, NOT the {object,data} list wrapper.
// Unknown fields are ignored (versioned contract).
type Job struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Prompt          string      `json:"prompt"`
	Skills          []string    `json:"skills"`
	Model           string      `json:"model,omitempty"`
	Schedule        JobSchedule `json:"schedule"`
	ScheduleDisplay string      `json:"schedule_display"`
	Enabled         bool        `json:"enabled"`
	State           string      `json:"state"`
	Repeat          JobRepeat   `json:"repeat"`
	CreatedAt       string      `json:"created_at"`
	NextRunAt       string      `json:"next_run_at"`
	LastRunAt       string      `json:"last_run_at"`
	LastStatus      string      `json:"last_status"`
	LastError       string      `json:"last_error"`
	PausedReason    string      `json:"paused_reason"`
}

type JobSchedule struct {
	Kind    string `json:"kind"`
	Expr    string `json:"expr"`
	Display string `json:"display"`
}

type JobRepeat struct {
	Times     *int `json:"times"`
	Completed int  `json:"completed"`
}

// JobInput is the payload for creating/updating a job. Only non-nil/non-empty
// fields are sent, so UpdateJob can PATCH a single field (e.g. enabled).
type JobInput struct {
	Name     string   `json:"name,omitempty"`
	Prompt   string   `json:"prompt,omitempty"`
	Schedule string   `json:"schedule,omitempty"` // cron expr, e.g. "0 7 * * *"
	Skills   []string `json:"skills,omitempty"`
	Enabled  *bool    `json:"enabled,omitempty"`
}

// ListJobs returns the box's cron jobs (for observability + management).
func (c *Client) ListJobs(ctx context.Context) ([]Job, error) {
	if !c.cfg.Configured() {
		return nil, ErrNotConfigured
	}
	var out struct {
		Jobs []Job `json:"jobs"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/jobs", nil, &out); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

// CreateJob schedules a new cron job (e.g. a profile on a cron). Returns the created job.
func (c *Client) CreateJob(ctx context.Context, in JobInput) (Job, error) {
	var out struct {
		Job Job `json:"job"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/jobs", in, &out); err != nil {
		return Job{}, err
	}
	return out.Job, nil
}

// UpdateJob patches a job (pause/enable/edit). Only set fields change.
func (c *Client) UpdateJob(ctx context.Context, id string, in JobInput) (Job, error) {
	var out struct {
		Job Job `json:"job"`
	}
	if err := c.doJSON(ctx, http.MethodPatch, "/api/jobs/"+id, in, &out); err != nil {
		return Job{}, err
	}
	return out.Job, nil
}

// DeleteJob removes a cron job.
func (c *Client) DeleteJob(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/jobs/"+id, nil, nil)
}

// doJSON is a small helper for the JSON (non-streaming) endpoints: optional JSON
// body in, optional JSON decode out, bearer auth, graceful status errors.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	if !c.cfg.Configured() {
		return ErrNotConfigured
	}
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := c.newRequest(ctx, method, path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusError(method+" "+path, resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}
