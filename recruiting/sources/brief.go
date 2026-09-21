package sources

import "time"

// CandidateBrief is derived interpretation, never raw evidence or a hiring score.
// The precise bounded input is retained so citations remain auditable on retry.
type CandidateBrief struct {
	Model       string      `json:"model"`
	GeneratedAt time.Time   `json:"generatedAt"`
	Items       []BriefItem `json:"items"`
	Evidence    []Evidence  `json:"evidence"`
}
type BriefItem struct {
	Section string `json:"section"`
	Text    string `json:"text"`
	Quote   string `json:"quote"`
	URL     string `json:"url"`
}
