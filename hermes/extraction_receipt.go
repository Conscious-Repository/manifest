package hermes

// ExtractionExecution records observed HTTP dispatch, not configured authority.
// Steps counts actual requests; completed means a response was received, not
// that the candidate contract or publication succeeded.
type ExtractionExecution struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	ResponseModel string `json:"responseModel"`
	Steps         int    `json:"steps"`
	Completed     bool   `json:"completed"`
	Status        int    `json:"status"`
}
