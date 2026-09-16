package transcriptsync

import (
	"context"
	"fmt"
	"net/url"
)

// pocketRecording is one list-endpoint row (M0-verified shape: the readiness
// marker is state=="completed" — there is NO has_transcription field, and the
// timestamp field is recording_at).
type pocketRecording struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Duration    float64  `json:"duration"`
	State       string   `json:"state"` // "pending" | "completed"
	RecordingAt string   `json:"recording_at"`
	CreatedAt   string   `json:"created_at"`
	Tags        []string `json:"tags"`
}

// ListRecordings pages through /public/recordings from startDate (UTC,
// day-granular — plan §2's watermark design absorbs the coarseness).
func (c *PocketClient) ListRecordings(ctx context.Context, startDate string) ([]pocketRecording, error) {
	var out []pocketRecording
	for page := 1; ; page++ {
		var resp struct {
			Success    bool              `json:"success"`
			Data       []pocketRecording `json:"data"`
			Pagination struct {
				HasMore    *bool `json:"has_more"`
				Page       int   `json:"page"`
				TotalPages int   `json:"total_pages"`
			} `json:"pagination"`
		}
		q := url.Values{"start_date": {startDate}, "page": {fmt.Sprint(page)}, "limit": {"100"}}
		if err := c.get(ctx, "/public/recordings", q, &resp); err != nil {
			return nil, err
		}
		if !resp.Success || resp.Data == nil || resp.Pagination.HasMore == nil || (resp.Pagination.Page != 0 && resp.Pagination.Page != page) {
			return nil, fmt.Errorf("pocket: unsuccessful list response")
		}
		out = append(out, resp.Data...)
		if !*resp.Pagination.HasMore {
			return out, nil
		}
		if page >= 100 {
			return nil, fmt.Errorf("pocket: page limit exceeded")
		}
	}
}

// pocketDetail is the detail endpoint's transcript-bearing payload
// (M0 fixture: transcript.segments[] {start, end, speaker, text}; speaker is
// "SPEAKER_NN" until Pocket's async labeling resolves real names).
type pocketDetail struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Duration    float64 `json:"duration"`
	State       string  `json:"state"`
	RecordingAt string  `json:"recording_at"`
	Transcript  *struct {
		Segments []pocketSegment `json:"segments"`
	} `json:"transcript"`
}

type pocketSegment struct {
	Speaker string  `json:"speaker"`
	Text    string  `json:"text"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
}

// FetchDetail pulls one recording with its transcript, summaries explicitly
// excluded (raw transcript only — same rule as Granola).
func (c *PocketClient) FetchDetail(ctx context.Context, id string) (pocketDetail, error) {
	var resp struct {
		Success bool         `json:"success"`
		Data    pocketDetail `json:"data"`
	}
	q := url.Values{"include_transcript": {"true"}, "include_summarizations": {"false"}}
	if err := c.get(ctx, "/public/recordings/"+url.PathEscape(id), q, &resp); err != nil {
		return pocketDetail{}, err
	}
	if !resp.Success {
		return pocketDetail{}, fmt.Errorf("pocket: unsuccessful detail response")
	}
	return resp.Data, nil
}
