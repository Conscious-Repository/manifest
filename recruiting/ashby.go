package recruiting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The PRIVATE, authenticated Ashby client and the approved write path
// (aion-recruiting-ashby-mirroring.md, Phase 6). This is the two-way mirror
// doctrine made code — and the doctrine is NOT silent two-way sync. It is:
//
//	explicit field authority (FieldAuthority, below)
//	user action → read current Ashby state → preflight/dedupe →
//	proposal/diff → approval → write → immediate re-fetch →
//	persist returned ids + audit line
//
// If both Manifest and Ashby changed a shared field since the last sync, the
// diff row is `conflict` and nothing is written for it. A field Manifest does
// not own is never written back. There is no poller: every method here runs
// because a route was hit by the owner.
//
// Key source — ONE: the ASHBY_API_KEY environment variable, read once by
// main at startup and handed to NewAshby. On metis that means an
// `EnvironmentFile=` drop-in (mode 0600) beside deploy/manifest.service, never
// an `Environment=` line in the world-readable unit and never config.json,
// which is committed. An empty key is a legitimate state: the client reports
// Configured() == false and the probe answers `configured:false` with no
// error. The key is never logged, never echoed, and redacted from every error
// string this file can return.
//
// Sync state (syncTokens, per-candidate base snapshots for conflict
// detection, the audit tail) is derived data and lives at
// <dataDir>/recruiting/ashby.json — outside the vault, refused inside it,
// like the source-run cache. Durable ids (ashby_candidate_id,
// ashby_application_id, ashby_job_id) live on the records.

// AshbyAPIURL is the private API root. Every method is POST <root>/<method>.
const AshbyAPIURL = "https://api.ashbyhq.com"

// AshbyAPIVersionHeader / AshbyAPIVersion pin the API contract the client
// was written against. Verify both on the first live probe (Phase 6c) — no
// key exists today, so this file has only ever spoken to fixtures.
const (
	AshbyAPIVersionHeader = "Ashby-API-Version"
	AshbyAPIVersion       = "1"
)

// AshbyScoutSource is the source tag every Manifest handoff carries: as the
// prefix of the scout note, and — when the org has a source of that title —
// as the sourceId on the created candidate/application.
const AshbyScoutSource = "Manifest Scout"

// ErrAshbyUnconfigured is returned by every call when no key is installed.
var ErrAshbyUnconfigured = errors.New("ashby: no API key configured (ASHBY_API_KEY)")

// ErrAshbyConflict is returned when a push meets a `conflict` diff row and
// the request did not resolve it — the silent-overwrite refusal.
var ErrAshbyConflict = errors.New("ashby: both sides changed — resolve the conflict before pushing")

// ---- field authority (one place, encoded) ----

// Authority is who may change a field, and therefore which direction it may
// travel.
type Authority string

const (
	// AuthorityManifest: Manifest owns it; it is never read back from Ashby
	// and is pushed only where Ashby has a slot for it (the scout note).
	AuthorityManifest Authority = "manifest"
	// AuthorityAshby: Ashby owns it after handoff; Manifest only ever
	// mirrors it inbound, and no write path here may set it on Ashby.
	AuthorityAshby Authority = "ashby"
	// AuthorityShared: proposable in either direction through the diff; a
	// change on both sides since the last sync is a conflict.
	AuthorityShared Authority = "shared"
	// AuthorityNever: never synced silently. It leaves Manifest only inside
	// an explicitly approved proposal that names it (IncludeContact).
	AuthorityNever Authority = "never"
)

// FieldAuthority is THE authority map. Keys are Manifest's own field names
// (candidate frontmatter/profile keys, role keys prefixed `role.`) so the
// write path can look a record field up directly. Anything absent from the
// map is Manifest-owned by default and never written toward Ashby.
var FieldAuthority = map[string]Authority{
	// candidate identity + profile — shared/proposable
	"name":     AuthorityShared,
	"title":    AuthorityShared,
	"org":      AuthorityShared,
	"location": AuthorityShared,
	"linkedin": AuthorityShared,
	"github":   AuthorityShared,
	"website":  AuthorityShared,
	"note":     AuthorityShared, // scout summary → candidate.createNote

	// contact — never silently
	"email":  AuthorityNever,
	"phone":  AuthorityNever,
	"resume": AuthorityNever,
	"files":  AuthorityNever,

	// Manifest-authoritative scout intelligence
	"evidence": AuthorityManifest,
	"fit":      AuthorityManifest,
	"criteria": AuthorityManifest,
	"seeds":    AuthorityManifest,
	"network":  AuthorityManifest,
	"outreach": AuthorityManifest,
	"override": AuthorityManifest,
	"stage":    AuthorityManifest, // the scout board column, not the ATS stage
	"owner":    AuthorityManifest,

	// Ashby-authoritative after handoff
	"ashby_candidate_id":   AuthorityAshby,
	"ashby_application_id": AuthorityAshby,
	"ashby_stage":          AuthorityAshby, // official application stage
	"ashby_source_id":      AuthorityAshby,
	"ashby_pipeline":       AuthorityAshby,
	"ashby_synced":         AuthorityAshby, // the last push/sync-back date stamp

	// role — shared posting fields, Ashby ids, Manifest criteria
	"role.title":            AuthorityShared,
	"role.location":         AuthorityShared,
	"role.employment":       AuthorityShared,
	"role.posting":          AuthorityShared,
	"role.compensation":     AuthorityShared,
	"role.criteria":         AuthorityManifest,
	"role.sourcing":         AuthorityManifest,
	"role.handoff_mode":     AuthorityManifest,
	"role.ashby_job_id":     AuthorityAshby,
	"role.ashby_posting_id": AuthorityAshby,
	"role.ashby_project_id": AuthorityAshby,
}

// AuthorityOf answers the map, defaulting to Manifest-owned.
func AuthorityOf(field string) Authority {
	if a, ok := FieldAuthority[strings.ToLower(strings.TrimSpace(field))]; ok {
		return a
	}
	return AuthorityManifest
}

// Pushable reports whether a field may travel Manifest → Ashby through a
// proposal. Only shared fields do; `never` fields need the explicit contact
// flag on the request, and Ashby-owned fields never.
func Pushable(field string, includeContact bool) bool {
	switch AuthorityOf(field) {
	case AuthorityShared:
		return true
	case AuthorityNever:
		return includeContact
	}
	return false
}

// ashbyProfileFields are the candidate profile keys projected onto
// candidate.create, in the order the diff renders them.
var ashbyProfileFields = []string{"name", "linkedin", "github", "website", "email", "phone"}

// ---- wire shapes ----

// ashbyEnvelope is every Ashby response: success plus either results or
// errors. List endpoints add the cursor pair and, where supported, a
// syncToken.
type ashbyEnvelope struct {
	Success           bool            `json:"success"`
	Errors            []string        `json:"errors"`
	Results           json.RawMessage `json:"results"`
	MoreDataAvailable bool            `json:"moreDataAvailable"`
	NextCursor        string          `json:"nextCursor"`
	SyncToken         string          `json:"syncToken"`
}

// AshbyKeyInfo is apiKey.info reduced to what the probe reports. The key
// itself is not a field, on purpose.
type AshbyKeyInfo struct {
	Title       string   `json:"title,omitempty"`
	Permissions []string `json:"permissions"`
}

// AshbyCandidate is the candidate.* read shape reduced to what the diff and
// the link decision need.
type AshbyCandidate struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	PrimaryEmail   string   `json:"primaryEmail,omitempty"`
	Emails         []string `json:"emails,omitempty"`
	PrimaryPhone   string   `json:"primaryPhone,omitempty"`
	LinkedInURL    string   `json:"linkedInUrl,omitempty"`
	GitHubURL      string   `json:"githubUrl,omitempty"`
	Website        string   `json:"website,omitempty"`
	Position       string   `json:"position,omitempty"`
	Company        string   `json:"company,omitempty"`
	Location       string   `json:"location,omitempty"`
	ApplicationIDs []string `json:"applicationIds,omitempty"`
	UpdatedAt      string   `json:"updatedAt,omitempty"`
	CreatedAt      string   `json:"createdAt,omitempty"`
}

// UnmarshalJSON tolerates the shapes candidate.* actually return: contacts as
// `{value}` objects, location as `{locationSummary}`, and profile URLs under
// `socialLinks`.
func (c *AshbyCandidate) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID             string            `json:"id"`
		Name           string            `json:"name"`
		Position       string            `json:"position"`
		Company        string            `json:"company"`
		ApplicationIDs []string          `json:"applicationIds"`
		UpdatedAt      string            `json:"updatedAt"`
		CreatedAt      string            `json:"createdAt"`
		PrimaryEmail   json.RawMessage   `json:"primaryEmailAddress"`
		Emails         []json.RawMessage `json:"emailAddresses"`
		PrimaryPhone   json.RawMessage   `json:"primaryPhoneNumber"`
		Location       json.RawMessage   `json:"primaryLocation"`
		SocialLinks    []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"socialLinks"`
		Website string `json:"website"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	c.ID, c.Name, c.Position, c.Company = raw.ID, raw.Name, raw.Position, raw.Company
	c.ApplicationIDs, c.UpdatedAt, c.CreatedAt = raw.ApplicationIDs, raw.UpdatedAt, raw.CreatedAt
	c.PrimaryEmail = ashbyValue(raw.PrimaryEmail, "value")
	for _, e := range raw.Emails {
		if v := ashbyValue(e, "value"); v != "" {
			c.Emails = append(c.Emails, v)
		}
	}
	c.PrimaryPhone = ashbyValue(raw.PrimaryPhone, "value")
	c.Location = ashbyValue(raw.Location, "locationSummary")
	c.Website = raw.Website
	for _, l := range raw.SocialLinks {
		switch strings.ToLower(l.Type) {
		case "linkedin":
			c.LinkedInURL = l.URL
		case "github":
			c.GitHubURL = l.URL
		case "website", "other":
			if c.Website == "" {
				c.Website = l.URL
			}
		}
	}
	return nil
}

// ashbyValue reads either a bare string or an object's named string field.
func ashbyValue(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil {
		if v, ok := m[key].(string); ok {
			return v
		}
	}
	return ""
}

// AshbyJob is job.list reduced.
type AshbyJob struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	LocationID string `json:"locationId,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

// AshbyJobPosting is jobPosting.list reduced: the bridge from the public
// posting id the Phase 2 mirror wrote to the job id application.create needs.
type AshbyJobPosting struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	JobID    string `json:"jobId"`
	IsListed bool   `json:"isListed"`
}

// AshbyProject is project.list reduced (sourcing projects).
type AshbyProject struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// AshbySource is source.list reduced.
type AshbySource struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// AshbyApplication is application.* reduced: the id, whose it is, and the
// official stage — Ashby-authoritative, mirrored inbound only.
type AshbyApplication struct {
	ID            string `json:"id"`
	CandidateID   string `json:"candidateId"`
	JobID         string `json:"jobId"`
	Status        string `json:"status"`
	Stage         string `json:"stage,omitempty"`
	StageID       string `json:"stageId,omitempty"`
	ArchiveReason string `json:"archiveReason,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

func (a *AshbyApplication) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		UpdatedAt string `json:"updatedAt"`
		Candidate struct {
			ID string `json:"id"`
		} `json:"candidate"`
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
		CurrentInterviewStage struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"currentInterviewStage"`
		ArchiveReason struct {
			Text string `json:"text"`
		} `json:"archiveReason"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.ID, a.Status, a.UpdatedAt = raw.ID, raw.Status, raw.UpdatedAt
	a.CandidateID, a.JobID = raw.Candidate.ID, raw.Job.ID
	a.Stage, a.StageID = raw.CurrentInterviewStage.Title, raw.CurrentInterviewStage.ID
	a.ArchiveReason = raw.ArchiveReason.Text
	return nil
}

// AshbyPage is one page of a list call plus how to fetch the next.
type AshbyPage[T any] struct {
	Results    []T    `json:"results"`
	More       bool   `json:"moreDataAvailable"`
	NextCursor string `json:"nextCursor,omitempty"`
	SyncToken  string `json:"syncToken,omitempty"`
}

// AshbyError is a typed API failure: the method, the HTTP status, and what
// Ashby said. Its text has already been through the redactor.
type AshbyError struct {
	Method string
	Status int
	Errors []string
}

func (e *AshbyError) Error() string {
	if len(e.Errors) > 0 {
		return fmt.Sprintf("ashby %s: HTTP %d: %s", e.Method, e.Status, strings.Join(e.Errors, "; "))
	}
	return fmt.Sprintf("ashby %s: HTTP %d", e.Method, e.Status)
}

// ---- client ----

// Ashby is the authenticated client: base URL overridable for tests, the key
// held privately, an injected http.Client.
type Ashby struct {
	url  string
	key  string
	http *http.Client
}

// NewAshby builds the client. An empty url means the real API; a nil client
// means a 20 s timeout; an empty key means unconfigured (not an error).
func NewAshby(url, key string, hc *http.Client) *Ashby {
	if url == "" {
		url = AshbyAPIURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &Ashby{url: strings.TrimRight(url, "/"), key: strings.TrimSpace(key), http: hc}
}

// Configured reports whether a key is installed. It never reveals the key.
func (c *Ashby) Configured() bool { return c != nil && c.key != "" }

// redact scrubs the key from any text. Every error this client returns goes
// through it, so a transport or decode failure that somehow carried the
// credential cannot reach a log line.
func (c *Ashby) redact(s string) string {
	if c.key == "" {
		return s
	}
	return strings.ReplaceAll(s, c.key, "[redacted]")
}

func (c *Ashby) redactErr(method string, err error) error {
	return fmt.Errorf("ashby %s: %s", method, c.redact(err.Error()))
}

// call is the one RPC: POST <url>/<method> with a JSON body, Basic auth (key
// as username, empty password), the version header. It returns the raw
// results and the envelope's paging fields.
func (c *Ashby) call(ctx context.Context, method string, params any) (*ashbyEnvelope, error) {
	if !c.Configured() {
		return nil, ErrAshbyUnconfigured
	}
	if params == nil {
		params = map[string]any{}
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, c.redactErr(method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, c.redactErr(method, err)
	}
	req.SetBasicAuth(c.key, "")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(AshbyAPIVersionHeader, AshbyAPIVersion)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, c.redactErr(method, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, c.redactErr(method, err)
	}
	var env ashbyEnvelope
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil && resp.StatusCode == http.StatusOK {
			return nil, &AshbyError{Method: method, Status: resp.StatusCode, Errors: []string{"response is not JSON"}}
		}
	}
	if resp.StatusCode != http.StatusOK || !env.Success {
		errs := make([]string, 0, len(env.Errors))
		for _, e := range env.Errors {
			errs = append(errs, c.redact(e))
		}
		return nil, &AshbyError{Method: method, Status: resp.StatusCode, Errors: errs}
	}
	return &env, nil
}

// callInto decodes a single-object result.
func (c *Ashby) callInto(ctx context.Context, method string, params any, out any) error {
	env, err := c.call(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil || len(env.Results) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Results, out); err != nil {
		return &AshbyError{Method: method, Status: http.StatusOK, Errors: []string{"results have an unexpected shape"}}
	}
	return nil
}

// callPage decodes a list result with its cursor pair.
func callPage[T any](ctx context.Context, c *Ashby, method string, params map[string]any) (AshbyPage[T], error) {
	env, err := c.call(ctx, method, params)
	if err != nil {
		return AshbyPage[T]{}, err
	}
	page := AshbyPage[T]{Results: []T{}, More: env.MoreDataAvailable, NextCursor: env.NextCursor, SyncToken: env.SyncToken}
	if len(env.Results) > 0 {
		if err := json.Unmarshal(env.Results, &page.Results); err != nil {
			return AshbyPage[T]{}, &AshbyError{Method: method, Status: http.StatusOK, Errors: []string{"results have an unexpected shape"}}
		}
	}
	return page, nil
}

// maxAshbyPages bounds any paginated walk: a sync is a user action and
// should finish while the owner watches, not crawl an ATS for an hour.
const maxAshbyPages = 50

// listAll walks a cursor/syncToken list to its end (bounded).
func listAll[T any](ctx context.Context, c *Ashby, method string, params map[string]any, syncToken string) ([]T, string, error) {
	var out []T
	cursor := ""
	token := ""
	for i := 0; i < maxAshbyPages; i++ {
		p := map[string]any{}
		for k, v := range params {
			p[k] = v
		}
		if cursor != "" {
			p["cursor"] = cursor
		}
		if syncToken != "" {
			p["syncToken"] = syncToken
		}
		page, err := callPage[T](ctx, c, method, p)
		if err != nil {
			return nil, "", err
		}
		out = append(out, page.Results...)
		if page.SyncToken != "" {
			token = page.SyncToken
		}
		if !page.More || page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if out == nil {
		out = []T{}
	}
	return out, token, nil
}

// ---- read methods (least privilege) ----

// APIKeyInfo is apiKey.info: what this key may do. Tolerant of the two
// shapes seen in the docs (`permissions` / `scopes`).
func (c *Ashby) APIKeyInfo(ctx context.Context) (AshbyKeyInfo, error) {
	var raw struct {
		Title       string   `json:"title"`
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
		Scopes      []string `json:"scopes"`
	}
	if err := c.callInto(ctx, "apiKey.info", nil, &raw); err != nil {
		return AshbyKeyInfo{}, err
	}
	info := AshbyKeyInfo{Title: raw.Title, Permissions: raw.Permissions}
	if info.Title == "" {
		info.Title = raw.Name
	}
	if len(info.Permissions) == 0 {
		info.Permissions = raw.Scopes
	}
	if info.Permissions == nil {
		info.Permissions = []string{}
	}
	sort.Strings(info.Permissions)
	return info, nil
}

// ListJobs is job.list (syncToken-capable).
func (c *Ashby) ListJobs(ctx context.Context, syncToken string) ([]AshbyJob, string, error) {
	return listAll[AshbyJob](ctx, c, "job.list", nil, syncToken)
}

// ListJobPostings is jobPosting.list. listedOnly is passed EXPLICITLY every
// time: the endpoint includes unlisted postings by default and the doctrine
// says never rely on that.
func (c *Ashby) ListJobPostings(ctx context.Context, listedOnly bool) ([]AshbyJobPosting, error) {
	out, _, err := listAll[AshbyJobPosting](ctx, c, "jobPosting.list", map[string]any{"listedOnly": listedOnly}, "")
	return out, err
}

// ListProjects is project.list.
func (c *Ashby) ListProjects(ctx context.Context) ([]AshbyProject, error) {
	out, _, err := listAll[AshbyProject](ctx, c, "project.list", nil, "")
	return out, err
}

// ListSources is source.list.
func (c *Ashby) ListSources(ctx context.Context) ([]AshbySource, error) {
	out, _, err := listAll[AshbySource](ctx, c, "source.list", nil, "")
	return out, err
}

// ListCandidates is candidate.list (syncToken-capable).
func (c *Ashby) ListCandidates(ctx context.Context, syncToken string) ([]AshbyCandidate, string, error) {
	return listAll[AshbyCandidate](ctx, c, "candidate.list", nil, syncToken)
}

// ListApplications is application.list (syncToken-capable).
func (c *Ashby) ListApplications(ctx context.Context, syncToken string) ([]AshbyApplication, string, error) {
	return listAll[AshbyApplication](ctx, c, "application.list", nil, syncToken)
}

// SearchCandidates is candidate.search by email and/or name — the dedupe
// preflight. Either may be empty; both empty is refused locally.
func (c *Ashby) SearchCandidates(ctx context.Context, email, name string) ([]AshbyCandidate, error) {
	email, name = strings.TrimSpace(email), strings.TrimSpace(name)
	if email == "" && name == "" {
		return nil, errf("ashby candidate.search: an email or a name is required")
	}
	params := map[string]any{}
	if email != "" {
		params["email"] = email
	}
	if name != "" {
		params["name"] = name
	}
	page, err := callPage[AshbyCandidate](ctx, c, "candidate.search", params)
	if err != nil {
		return nil, err
	}
	return page.Results, nil
}

// GetCandidate is candidate.info — the immediate re-fetch after a write.
func (c *Ashby) GetCandidate(ctx context.Context, id string) (AshbyCandidate, error) {
	var out AshbyCandidate
	err := c.callInto(ctx, "candidate.info", map[string]any{"id": id}, &out)
	return out, err
}

// GetApplication is application.info — the re-fetch after a stage change.
func (c *Ashby) GetApplication(ctx context.Context, id string) (AshbyApplication, error) {
	var out AshbyApplication
	err := c.callInto(ctx, "application.info", map[string]any{"applicationId": id}, &out)
	return out, err
}

// ---- write methods ----

// AshbyCandidateCreate is the candidate.create body: only fields the
// authority map lets travel. Contact fields are set ONLY by an approved
// proposal that named them.
type AshbyCandidateCreate struct {
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	Phone       string `json:"phoneNumber,omitempty"`
	LinkedInURL string `json:"linkedInUrl,omitempty"`
	GitHubURL   string `json:"githubUrl,omitempty"`
	Website     string `json:"website,omitempty"`
	SourceID    string `json:"sourceId,omitempty"`
}

// CreateCandidate is candidate.create.
func (c *Ashby) CreateCandidate(ctx context.Context, in AshbyCandidateCreate) (AshbyCandidate, error) {
	if strings.TrimSpace(in.Name) == "" {
		return AshbyCandidate{}, errf("ashby candidate.create: a name is required")
	}
	var out AshbyCandidate
	err := c.callInto(ctx, "candidate.create", in, &out)
	return out, err
}

// CreateApplication is application.create: a formal application of a known
// candidate to a job, tagged with the source when one is known.
func (c *Ashby) CreateApplication(ctx context.Context, candidateID, jobID, sourceID string) (AshbyApplication, error) {
	if strings.TrimSpace(candidateID) == "" || strings.TrimSpace(jobID) == "" {
		return AshbyApplication{}, errf("ashby application.create: candidateId and jobId are required")
	}
	params := map[string]any{"candidateId": candidateID, "jobId": jobID}
	if sourceID != "" {
		params["sourceId"] = sourceID
	}
	var out AshbyApplication
	err := c.callInto(ctx, "application.create", params, &out)
	return out, err
}

// CreateNote is candidate.createNote. The note is prefixed with the scout
// source tag so it is legible as Manifest's in the Ashby timeline.
func (c *Ashby) CreateNote(ctx context.Context, candidateID, note string) (string, error) {
	note = strings.TrimSpace(note)
	if strings.TrimSpace(candidateID) == "" || note == "" {
		return "", errf("ashby candidate.createNote: candidateId and note are required")
	}
	if !strings.HasPrefix(note, AshbyScoutSource) {
		note = AshbyScoutSource + " — " + note
	}
	var out struct {
		ID string `json:"id"`
	}
	err := c.callInto(ctx, "candidate.createNote", map[string]any{
		"candidateId": candidateID, "note": note, "sendNotifications": false,
	}, &out)
	return out.ID, err
}

// AshbyStageChange is the application.changeStage body. Exactly one of the
// two ids is the move: a stage id advances, an archive reason id archives.
type AshbyStageChange struct {
	ApplicationID    string `json:"applicationId"`
	InterviewStageID string `json:"interviewStageId,omitempty"`
	ArchiveReasonID  string `json:"archiveReasonId,omitempty"`
}

// ChangeStage is application.changeStage, including the archive-reason path.
func (c *Ashby) ChangeStage(ctx context.Context, in AshbyStageChange) (AshbyApplication, error) {
	if strings.TrimSpace(in.ApplicationID) == "" {
		return AshbyApplication{}, errf("ashby application.changeStage: applicationId is required")
	}
	if (in.InterviewStageID == "") == (in.ArchiveReasonID == "") {
		return AshbyApplication{}, errf("ashby application.changeStage: exactly one of interviewStageId or archiveReasonId")
	}
	var out AshbyApplication
	err := c.callInto(ctx, "application.changeStage", in, &out)
	return out, err
}

// AddProject is candidate.addProject (sourcing-project membership). The
// doctrine flags this endpoint as probe-first: a 404 surfaces as a typed
// error the push reports rather than swallows.
func (c *Ashby) AddProject(ctx context.Context, candidateID, projectID string) error {
	if strings.TrimSpace(candidateID) == "" || strings.TrimSpace(projectID) == "" {
		return errf("ashby candidate.addProject: candidateId and projectId are required")
	}
	return c.callInto(ctx, "candidate.addProject", map[string]any{"candidateId": candidateID, "projectId": projectID}, nil)
}

// ---- proposal / diff ----

// Diff actions.
const (
	DiffWrite    = "write"    // Manifest changed, Ashby did not → push
	DiffKeep     = "keep"     // equal, nothing to do
	DiffPull     = "pull"     // Ashby changed, Manifest did not → mirror inbound
	DiffConflict = "conflict" // both changed → refuse, ask
	DiffSkip     = "skip"     // not pushable under the authority map
)

// AshbyDiff is one field of a proposal: what each side holds, who owns it,
// and what the write would do about it.
type AshbyDiff struct {
	Field     string    `json:"field"`
	Manifest  string    `json:"manifest"`
	Ashby     string    `json:"ashby"`
	Base      string    `json:"base,omitempty"`
	Authority Authority `json:"authority"`
	Action    string    `json:"action"`
}

// Reconcile is the three-way rule for one shared field. `base` is the value
// both sides agreed on at the last sync (hasBase=false on first contact).
//
//	equal                       → keep
//	no base, Ashby empty        → write
//	no base, Ashby set, differs → conflict (nothing to say who moved)
//	Manifest == base            → pull   (only Ashby moved)
//	Ashby == base               → write  (only Manifest moved)
//	otherwise                   → conflict
func Reconcile(field, manifest, ashby, base string, hasBase bool) AshbyDiff {
	d := AshbyDiff{Field: field, Manifest: manifest, Ashby: ashby, Authority: AuthorityOf(field)}
	if hasBase {
		d.Base = base
	}
	m, a := strings.TrimSpace(manifest), strings.TrimSpace(ashby)
	switch {
	case m == a:
		d.Action = DiffKeep
	case !hasBase && a == "":
		d.Action = DiffWrite
	case !hasBase:
		d.Action = DiffConflict
	case m == strings.TrimSpace(base):
		d.Action = DiffPull
	case a == strings.TrimSpace(base):
		d.Action = DiffWrite
	default:
		d.Action = DiffConflict
	}
	return d
}

// Handoff modes — the explicit per-candidate choice (D: never a preset).
const (
	HandoffProject     = "project"
	HandoffApplication = "application"
)

// Decisions for a preflight that found matches.
const (
	DecisionLink   = "link"   // adopt the found Ashby candidate's id
	DecisionCreate = "create" // create anyway (a namesake, not a duplicate)
)

// AshbyPushRequest is the body of a preflight or push. Preflight ignores
// Approve; Push refuses without it.
type AshbyPushRequest struct {
	Candidate string `json:"candidate"`
	// Handoff is HandoffProject or HandoffApplication — required on push,
	// chosen per candidate.
	Handoff string `json:"handoff"`
	// Decision resolves a found match: link or create. Required on push when
	// the preflight found candidates.
	Decision string `json:"decision"`
	// AshbyCandidateID names which match to link when Decision is link.
	AshbyCandidateID string `json:"ashbyCandidateId"`
	// JobID / ProjectID override the role's ashby_job_id / ashby_project_id.
	JobID     string `json:"jobId"`
	ProjectID string `json:"projectId"`
	// Note is the scout summary attached via candidate.createNote.
	Note string `json:"note"`
	// IncludeContact lets email/phone travel — the one explicit approval the
	// `never` authority accepts.
	IncludeContact bool `json:"includeContact"`
	// Approve is the approval step. A push without it is refused.
	Approve bool `json:"approve"`
	// Actor is who approved (audit line).
	Actor string `json:"actor"`
}

// AshbyProposal is the rendered preflight: matches, the diff, the conflict
// flag, and what a push would do. It is what the owner approves.
type AshbyProposal struct {
	Candidate string `json:"candidate"`
	Name      string `json:"name"`
	Linked    string `json:"linked,omitempty"` // ashby_candidate_id already on the record
	// ApplicationID is the ashby_application_id already on the record. When
	// set, an application handoff has nothing to create: application.create
	// leaves the write list and a push skips it instead of filing a second
	// application for the same person.
	ApplicationID string           `json:"applicationId,omitempty"`
	Matches       []AshbyCandidate `json:"matches"`
	Decision      string           `json:"decision,omitempty"`
	Handoff       string           `json:"handoff,omitempty"`
	JobID         string           `json:"jobId,omitempty"`
	ProjectID     string           `json:"projectId,omitempty"`
	SourceID      string           `json:"sourceId,omitempty"`
	Diff          []AshbyDiff      `json:"diff"`
	Conflict      bool             `json:"conflict"`
	Writes        []string         `json:"writes"` // the Ashby methods a push would call, in order
	NeedsChoice   []string         `json:"needsChoice"`
	Note          string           `json:"note,omitempty"`
}

// AshbyPushResult is what an approved push did.
type AshbyPushResult struct {
	Proposal      AshbyProposal  `json:"proposal"`
	CandidateID   string         `json:"ashbyCandidateId"`
	ApplicationID string         `json:"ashbyApplicationId,omitempty"`
	NoteID        string         `json:"noteId,omitempty"`
	Fetched       AshbyCandidate `json:"fetched"`
	Audit         []AshbyAudit   `json:"audit"`
	Record        Candidate      `json:"record"`
}

// AshbyAudit is one audit line: who did what to which Ashby object, when.
type AshbyAudit struct {
	At        string `json:"at"`
	Actor     string `json:"actor"`
	Candidate string `json:"candidate"`
	Method    string `json:"method"`
	AshbyID   string `json:"ashbyId,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// ---- sync state (dataDir, derived) ----

// AshbyCandidateState is the per-candidate base snapshot: the shared-field
// values both sides agreed on at the last sync, keyed by Manifest field. It
// is what makes "both sides changed" decidable.
type AshbyCandidateState struct {
	AshbyID  string            `json:"ashbyId"`
	Base     map[string]string `json:"base"`
	SyncedAt string            `json:"syncedAt"`
}

// AshbySyncState is <dataDir>/recruiting/ashby.json.
type AshbySyncState struct {
	SyncTokens map[string]string              `json:"syncTokens"`
	LastFull   string                         `json:"lastFull,omitempty"`
	LastSync   string                         `json:"lastSync,omitempty"`
	Candidates map[string]AshbyCandidateState `json:"candidates"`
	Audit      []AshbyAudit                   `json:"audit"`
	// Webhooks is the bounded set of delivery keys the receiver (Phase 7)
	// already applied, oldest first — a redelivery is skipped, not re-run.
	Webhooks []string `json:"webhooks,omitempty"`
}

const (
	maxAshbyAudit    = 500
	maxAshbyWebhooks = 1000
)

// AshbySync owns the write path and the sync-back trigger. It holds the
// record store (ids and audit lines land on records through the same
// capability-bound writer) and the private client.
type AshbySync struct {
	store  *Store
	client *Ashby
	path   string
	mu     sync.Mutex
}

// NewAshbySync roots the state file at path (<dataDir>/recruiting/ashby.json),
// refusing a path inside the vault — derived state never goes there.
func NewAshbySync(path string, store *Store, client *Ashby) (*AshbySync, error) {
	if store == nil {
		return nil, errors.New("recruiting: ashby sync needs the record store")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if vault, err := filepath.Abs(store.vaultRoot); err == nil && vault != "" {
		rel, err := filepath.Rel(vault, abs)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errf("recruiting: ashby sync state %q must live outside the vault %q", abs, vault)
		}
	}
	if client == nil {
		client = NewAshby("", "", nil)
	}
	return &AshbySync{store: store, client: client, path: abs}, nil
}

// UseClient swaps the client (tests bind it to httptest).
func (a *AshbySync) UseClient(c *Ashby) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c != nil {
		a.client = c
	}
}

// Configured mirrors the client.
func (a *AshbySync) Configured() bool { return a.client.Configured() }

func (a *AshbySync) load() AshbySyncState {
	st := AshbySyncState{SyncTokens: map[string]string{}, Candidates: map[string]AshbyCandidateState{}, Audit: []AshbyAudit{}}
	b, err := os.ReadFile(a.path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(b, &st)
	if st.SyncTokens == nil {
		st.SyncTokens = map[string]string{}
	}
	if st.Candidates == nil {
		st.Candidates = map[string]AshbyCandidateState{}
	}
	if st.Audit == nil {
		st.Audit = []AshbyAudit{}
	}
	return st
}

func (a *AshbySync) save(st AshbySyncState) error {
	if len(st.Audit) > maxAshbyAudit {
		st.Audit = st.Audit[len(st.Audit)-maxAshbyAudit:]
	}
	if len(st.Webhooks) > maxAshbyWebhooks {
		st.Webhooks = st.Webhooks[len(st.Webhooks)-maxAshbyWebhooks:]
	}
	// The derived sync state (<dataDir>/recruiting/ashby.json) is a direct file
	// write like the run cache, so it is rooted in runs.go via writeJSONDir0600
	// rather than calling os.* here. The writeless test enforces this.
	return writeJSONDir0600(a.path, st)
}

// State is the read projection of the sync state — safe to serve: it holds
// ids, tokens and audit lines, never the key.
func (a *AshbySync) State() AshbySyncState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.load()
}

// StatePath exposes the derived sync-state file path (dataDir outside the
// vault). Tests seed the file directly; production code never needs it.
func (a *AshbySync) StatePath() string { return a.path }

// ---- capability probe ----

// AshbyProbe is what the probe route answers. Unconfigured is a 200 with
// configured:false and no scopes; nothing here can carry the key.
type AshbyProbe struct {
	Configured bool     `json:"configured"`
	Scopes     []string `json:"scopes"`
	Title      string   `json:"title,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// Probe answers configured:false with no error when no key is installed;
// with a key it calls apiKey.info and reports the scopes. A failing call is
// reported in the body (redacted), not as a transport error, so the UI can
// show "key installed but rejected" as a state.
func (a *AshbySync) Probe(ctx context.Context) AshbyProbe {
	if !a.client.Configured() {
		return AshbyProbe{Configured: false, Scopes: []string{}}
	}
	info, err := a.client.APIKeyInfo(ctx)
	if err != nil {
		return AshbyProbe{Configured: true, Scopes: []string{}, Error: err.Error()}
	}
	return AshbyProbe{Configured: true, Scopes: info.Permissions, Title: info.Title}
}

// ---- preflight ----

// projection is the Manifest side of the diff: the pushable profile fields
// of one record, under the authority map.
func candidateProjection(doc *CandidateDoc, includeContact bool) map[string]string {
	p := doc.Profile()
	out := map[string]string{"name": strings.TrimSpace(doc.Get("name"))}
	for _, k := range ashbyProfileFields[1:] {
		if Pushable(k, includeContact) {
			out[k] = strings.TrimSpace(p[k])
		}
	}
	return out
}

// ashbySide reads the same fields off an Ashby candidate.
func ashbySide(c AshbyCandidate) map[string]string {
	return map[string]string{
		"name": c.Name, "linkedin": c.LinkedInURL, "github": c.GitHubURL, "website": c.Website,
		"email": c.PrimaryEmail, "phone": c.PrimaryPhone,
	}
}

// diffAgainst renders the field diff of a record against one Ashby
// candidate, using the stored base when the record is already linked.
func diffAgainst(manifest map[string]string, ashby AshbyCandidate, base map[string]string) ([]AshbyDiff, bool) {
	side := ashbySide(ashby)
	var out []AshbyDiff
	conflict := false
	for _, k := range ashbyProfileFields {
		m, ok := manifest[k]
		if !ok {
			out = append(out, AshbyDiff{Field: k, Manifest: "", Ashby: side[k], Authority: AuthorityOf(k), Action: DiffSkip})
			continue
		}
		b, hasBase := "", false
		if base != nil {
			b, hasBase = base[k]
		}
		d := Reconcile(k, m, side[k], b, hasBase)
		if d.Action == DiffConflict {
			conflict = true
		}
		out = append(out, d)
	}
	return out, conflict
}

// diffForCreate renders the diff of a record against nothing: every pushable
// field is a write, contact fields show as skipped unless included.
func diffForCreate(manifest map[string]string) []AshbyDiff {
	var out []AshbyDiff
	for _, k := range ashbyProfileFields {
		if m, ok := manifest[k]; ok {
			action := DiffWrite
			if strings.TrimSpace(m) == "" {
				action = DiffKeep
			}
			out = append(out, AshbyDiff{Field: k, Manifest: m, Authority: AuthorityOf(k), Action: action})
		} else {
			out = append(out, AshbyDiff{Field: k, Authority: AuthorityOf(k), Action: DiffSkip})
		}
	}
	return out
}

// Preflight is steps 2–4 of the write shape: read current Ashby state
// (candidate.search by email, then name), build the proposal, detect
// conflicts. It writes nothing anywhere. `Decision`/`Handoff` on the request
// are carried through so the rendered proposal shows what a push would do.
func (a *AshbySync) Preflight(ctx context.Context, req AshbyPushRequest) (AshbyProposal, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.preflight(ctx, req)
}

func (a *AshbySync) preflight(ctx context.Context, req AshbyPushRequest) (AshbyProposal, error) {
	if !a.client.Configured() {
		return AshbyProposal{}, ErrAshbyUnconfigured
	}
	slug, doc, err := a.store.resolve(req.Candidate)
	if err != nil {
		return AshbyProposal{}, err
	}
	st := a.load()
	prop := AshbyProposal{
		Candidate: CandidateID(slug), Name: doc.Get("name"), Linked: doc.Get("ashby_candidate_id"),
		ApplicationID: strings.TrimSpace(doc.Get("ashby_application_id")),
		Matches:       []AshbyCandidate{}, Diff: []AshbyDiff{}, Writes: []string{}, NeedsChoice: []string{},
		Handoff: strings.ToLower(strings.TrimSpace(req.Handoff)), Decision: strings.ToLower(strings.TrimSpace(req.Decision)),
		Note: strings.TrimSpace(req.Note),
	}
	manifest := candidateProjection(doc, req.IncludeContact)
	profile := doc.Profile()

	// 2. read current Ashby state — dedupe by email first, then name
	seen := map[string]bool{}
	add := func(cs []AshbyCandidate) {
		for _, c := range cs {
			if c.ID != "" && !seen[c.ID] {
				seen[c.ID] = true
				prop.Matches = append(prop.Matches, c)
			}
		}
	}
	if prop.Linked != "" {
		c, err := a.client.GetCandidate(ctx, prop.Linked)
		if err != nil {
			return prop, err
		}
		add([]AshbyCandidate{c})
	} else {
		if email := strings.TrimSpace(profile["email"]); email != "" {
			found, err := a.client.SearchCandidates(ctx, email, "")
			if err != nil {
				return prop, err
			}
			add(found)
		}
		if name := strings.TrimSpace(doc.Get("name")); name != "" {
			found, err := a.client.SearchCandidates(ctx, "", name)
			if err != nil {
				return prop, err
			}
			add(found)
		}
	}

	// 3. decide what the push would be
	switch {
	case prop.Linked != "":
		prop.Decision = DecisionLink
		if req.AshbyCandidateID == "" {
			req.AshbyCandidateID = prop.Linked
		}
	case len(prop.Matches) == 0:
		prop.Decision = DecisionCreate
	case prop.Decision == "":
		prop.NeedsChoice = append(prop.NeedsChoice, "decision")
	}
	if prop.Handoff == "" {
		prop.NeedsChoice = append(prop.NeedsChoice, "handoff")
	} else if prop.Handoff != HandoffProject && prop.Handoff != HandoffApplication {
		return prop, errf("handoff must be %s or %s", HandoffProject, HandoffApplication)
	}

	// the diff: against the chosen/linked match, or against nothing
	var target *AshbyCandidate
	if prop.Decision == DecisionLink {
		id := strings.TrimSpace(req.AshbyCandidateID)
		if id == "" && len(prop.Matches) == 1 {
			id = prop.Matches[0].ID
		}
		for i := range prop.Matches {
			if prop.Matches[i].ID == id {
				target = &prop.Matches[i]
			}
		}
		if target == nil {
			if id == "" {
				prop.NeedsChoice = append(prop.NeedsChoice, "ashbyCandidateId")
			} else {
				return prop, errf("ashby candidate %q is not among the matches", id)
			}
		}
	}
	if target != nil {
		var base map[string]string
		if cs, ok := st.Candidates[prop.Candidate]; ok && cs.AshbyID == target.ID {
			base = cs.Base
		}
		prop.Diff, prop.Conflict = diffAgainst(manifest, *target, base)
	} else if prop.Decision == DecisionCreate {
		prop.Diff = diffForCreate(manifest)
	} else {
		prop.Diff = diffForCreate(manifest)
	}

	// the role side: job id for an application, project id for a project
	role := a.store.roleDocFor(doc.Get("role"))
	prop.JobID = strings.TrimSpace(req.JobID)
	prop.ProjectID = strings.TrimSpace(req.ProjectID)
	if role != nil {
		if prop.JobID == "" {
			prop.JobID = strings.TrimSpace(role.Get("ashby_job_id"))
		}
		if prop.ProjectID == "" {
			prop.ProjectID = strings.TrimSpace(role.Get("ashby_project_id"))
		}
	}
	switch prop.Handoff {
	case HandoffApplication:
		// an application already on the record needs no job to file under
		if prop.JobID == "" && prop.ApplicationID == "" {
			prop.NeedsChoice = append(prop.NeedsChoice, "jobId")
		}
	case HandoffProject:
		if prop.ProjectID == "" {
			prop.NeedsChoice = append(prop.NeedsChoice, "projectId")
		}
	}

	// the Manifest Scout source, when the org has one
	if srcs, err := a.client.ListSources(ctx); err == nil {
		for _, s := range srcs {
			if strings.EqualFold(strings.TrimSpace(s.Title), AshbyScoutSource) {
				prop.SourceID = s.ID
				break
			}
		}
	}

	// the write list a push would run, in order
	if prop.Decision == DecisionCreate {
		prop.Writes = append(prop.Writes, "candidate.create")
	}
	switch prop.Handoff {
	case HandoffProject:
		prop.Writes = append(prop.Writes, "candidate.addProject")
	case HandoffApplication:
		if prop.ApplicationID == "" {
			prop.Writes = append(prop.Writes, "application.create")
		}
	}
	if prop.Note != "" {
		prop.Writes = append(prop.Writes, "candidate.createNote")
	}
	prop.Writes = append(prop.Writes, "candidate.info")
	return prop, nil
}

// ---- push (the approved write) ----

// Push is steps 5–8: it re-runs the preflight (state may have moved since
// the proposal was rendered), refuses without Approve, refuses a conflict,
// refuses an unresolved choice, then writes, re-fetches, and persists the
// ids plus an audit line on the record and in the state file.
func (a *AshbySync) Push(ctx context.Context, req AshbyPushRequest, now time.Time) (AshbyPushResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !req.Approve {
		return AshbyPushResult{}, errf("ashby push needs approval — preflight, review the proposal, then approve")
	}
	prop, err := a.preflight(ctx, req)
	if err != nil {
		return AshbyPushResult{Proposal: prop}, err
	}
	if prop.Conflict {
		return AshbyPushResult{Proposal: prop}, ErrAshbyConflict
	}
	if len(prop.NeedsChoice) > 0 {
		return AshbyPushResult{Proposal: prop}, errf("ashby push needs an explicit choice: %s", strings.Join(prop.NeedsChoice, ", "))
	}
	slug, doc, err := a.store.resolve(req.Candidate)
	if err != nil {
		return AshbyPushResult{Proposal: prop}, err
	}
	actor := strings.TrimSpace(req.Actor)
	if actor == "" {
		actor = a.store.owner
	}
	st := a.load()
	res := AshbyPushResult{Proposal: prop, Audit: []AshbyAudit{}}
	stamp := now.UTC().Format(time.RFC3339)
	audit := func(method, ashbyID, detail string) {
		res.Audit = append(res.Audit, AshbyAudit{At: stamp, Actor: actor, Candidate: prop.Candidate, Method: method, AshbyID: ashbyID, Detail: detail})
	}

	// 6. write — candidate first
	manifest := candidateProjection(doc, req.IncludeContact)
	switch prop.Decision {
	case DecisionCreate:
		in := AshbyCandidateCreate{Name: manifest["name"], LinkedInURL: manifest["linkedin"],
			GitHubURL: manifest["github"], Website: manifest["website"], SourceID: prop.SourceID}
		if req.IncludeContact {
			in.Email, in.Phone = manifest["email"], manifest["phone"]
		}
		created, err := a.client.CreateCandidate(ctx, in)
		if err != nil {
			return res, err
		}
		if created.ID == "" {
			return res, errf("ashby candidate.create returned no id")
		}
		res.CandidateID = created.ID
		audit("candidate.create", created.ID, "")
	case DecisionLink:
		id := strings.TrimSpace(req.AshbyCandidateID)
		if id == "" && prop.Linked != "" {
			id = prop.Linked
		}
		if id == "" && len(prop.Matches) == 1 {
			id = prop.Matches[0].ID
		}
		res.CandidateID = id
		if prop.Linked != id {
			audit("link", id, "linked to existing Ashby candidate")
		}
		// a `write` row against a linked candidate is a proposable field
		// update; candidate.update is NOT in this pass's least-privilege
		// surface, so it is reported in the diff and left for Phase 6c.
	default:
		return res, errf("ashby push: decision must be %s or %s", DecisionLink, DecisionCreate)
	}

	// then the handoff — explicit per candidate
	switch prop.Handoff {
	case HandoffProject:
		if err := a.client.AddProject(ctx, res.CandidateID, prop.ProjectID); err != nil {
			a.persistPartial(st, slug, res)
			return res, err
		}
		audit("candidate.addProject", prop.ProjectID, "")
	case HandoffApplication:
		if prop.ApplicationID != "" {
			// the record already carries its application (a re-push of a
			// linked candidate): nothing to create, nothing to audit
			res.ApplicationID = prop.ApplicationID
			break
		}
		app, err := a.client.CreateApplication(ctx, res.CandidateID, prop.JobID, prop.SourceID)
		if err != nil {
			a.persistPartial(st, slug, res)
			return res, err
		}
		res.ApplicationID = app.ID
		audit("application.create", app.ID, "job "+prop.JobID)
	}

	// the scout note
	if prop.Note != "" {
		id, err := a.client.CreateNote(ctx, res.CandidateID, prop.Note)
		if err != nil {
			a.persistPartial(st, slug, res)
			return res, err
		}
		res.NoteID = id
		audit("candidate.createNote", id, AshbyScoutSource)
	}

	// 7. immediate re-fetch
	fetched, err := a.client.GetCandidate(ctx, res.CandidateID)
	if err != nil {
		a.persistPartial(st, slug, res)
		return res, err
	}
	res.Fetched = fetched

	// 8. persist ids + audit on the record and the base snapshot in state.
	// The record is re-read under the store lock: `doc` above is as old as
	// the network round-trips, and an edit the owner made meanwhile must
	// not be overwritten by it.
	set := map[string]string{"ashby_candidate_id": res.CandidateID, "ashby_synced": now.UTC().Format("2006-01-02")}
	if res.ApplicationID != "" {
		set["ashby_application_id"] = res.ApplicationID
	}
	doc, err = a.applyToCandidate(slug, ashbyRecordPatch{set: set, stage: StageAshby, audit: res.Audit})
	if err != nil {
		return res, err
	}
	base := ashbySide(fetched)
	st.Candidates[prop.Candidate] = AshbyCandidateState{AshbyID: res.CandidateID, Base: base, SyncedAt: stamp}
	st.Audit = append(st.Audit, res.Audit...)
	if err := a.save(st); err != nil {
		return res, err
	}
	res.Record = a.store.candidateView(slug, doc)
	return res, nil
}

// persistPartial records what DID land when a multi-write push fails
// midway: a created candidate id must not be lost just because the note
// after it failed, or the next push would create a duplicate.
func (a *AshbySync) persistPartial(st AshbySyncState, slug string, res AshbyPushResult) {
	if res.CandidateID == "" {
		return
	}
	set := map[string]string{"ashby_candidate_id": res.CandidateID}
	if res.ApplicationID != "" {
		set["ashby_application_id"] = res.ApplicationID
	}
	_, _ = a.applyToCandidate(slug, ashbyRecordPatch{set: set, audit: res.Audit})
	st.Audit = append(st.Audit, res.Audit...)
	_ = a.save(st)
}

// ---- record writes: the ONE way this file touches a record ----

// ashbyRecordPatch is everything a push, a stage change or a sync-back may
// put on a candidate record: Ashby-owned keys, the board-stage move a push
// makes, and audit lines. A Manifest-owned or shared field has no slot
// here, so field authority holds by construction on the inbound side too.
type ashbyRecordPatch struct {
	set   map[string]string // AuthorityAshby keys only; anything else is refused
	stage string            // board stage to move to ("" leaves it); never applied to an archived record
	audit []AshbyAudit
}

// applyToCandidate is the lost-update guard. Every caller in this file has
// spent seconds in the network on a document it loaded before; instead of
// saving that stale copy it hands the patch here, which takes the STORE
// lock (the one every owner edit route holds), re-reads the record as it
// is now, applies only the patch, and saves. The owner's concurrent edit
// to a profile field is on disk by then and survives beside the ids.
func (a *AshbySync) applyToCandidate(slug string, p ashbyRecordPatch) (*CandidateDoc, error) {
	for k := range p.set {
		if AuthorityOf(k) != AuthorityAshby {
			return nil, errf("ashby: %q is not an Ashby-owned field — nothing inbound may set it", k)
		}
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	doc := a.store.LoadCandidate(slug)
	for k, v := range p.set {
		doc.Set(k, v)
	}
	if p.stage != "" && doc.Get("stage") != StageArchived {
		doc.Set("stage", p.stage)
	}
	appendAshbyAudit(doc, p.audit)
	if err := a.store.SaveCandidate(slug, doc); err != nil {
		return doc, err
	}
	return doc, nil
}

// applyToRole is applyToCandidate for a role record: Ashby-owned role keys
// only (`role.` in the authority map), re-read and saved under the lock.
func (a *AshbySync) applyToRole(slug string, set map[string]string) error {
	for k := range set {
		if AuthorityOf("role."+k) != AuthorityAshby {
			return errf("ashby: role %q is not an Ashby-owned field — nothing inbound may set it", k)
		}
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	role := a.store.LoadRole(slug)
	for k, v := range set {
		role.Set(k, v)
	}
	return a.store.SaveRole(slug, role)
}

// appendAshbyAudit writes audit rows into the record's `## ashby` section.
// The section is not a recognized row vocabulary, so the bullets round-trip
// verbatim — an audit line is append-only by construction.
func appendAshbyAudit(doc *CandidateDoc, lines []AshbyAudit) {
	if len(lines) == 0 {
		return
	}
	sec := ensureSection(&doc.Sections, "ashby")
	for _, l := range lines {
		r := newRow("at", l.At, "by", l.Actor, "method", l.Method)
		if l.AshbyID != "" {
			r.Set("ashby", l.AshbyID)
		}
		if l.Detail != "" {
			r.Set("detail", l.Detail)
		}
		raw := r.emitLines()
		at := len(sec.Lines)
		for at > 0 && sec.Lines[at-1].Row == nil && strings.TrimSpace(sec.Lines[at-1].Raw) == "" {
			at--
		}
		var ins []Line
		for _, s := range raw {
			ins = append(ins, Line{Raw: s})
		}
		sec.Lines = append(sec.Lines[:at], append(ins, sec.Lines[at:]...)...)
	}
}

// AshbyAuditOf reads the `## ashby` audit rows back off a record.
func AshbyAuditOf(doc *CandidateDoc) []AshbyAudit {
	out := []AshbyAudit{}
	for _, ln := range section(doc.Sections, "ashby").linesOrNil() {
		if ln.Row != nil {
			continue
		}
		r, ok := parseRow(ln.Raw)
		if !ok || !r.Has("method") {
			continue
		}
		out = append(out, AshbyAudit{At: r.Get("at"), Actor: r.Get("by"), Candidate: doc.Get("id"),
			Method: r.Get("method"), AshbyID: r.Get("ashby"), Detail: r.Get("detail")})
	}
	return out
}

func (s *Section) linesOrNil() []Line {
	if s == nil {
		return nil
	}
	return s.Lines
}

// ---- stage change (Ashby-authoritative, written only on explicit action) ----

// ChangeStage moves a linked candidate's application on the ATS side —
// advance by interview stage id, or archive by reason id — then re-fetches
// and mirrors the official stage onto the record's `ashby_stage`.
func (a *AshbySync) ChangeStage(ctx context.Context, candidate, interviewStageID, archiveReasonID, actor string, now time.Time) (AshbyApplication, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.client.Configured() {
		return AshbyApplication{}, ErrAshbyUnconfigured
	}
	slug, doc, err := a.store.resolve(candidate)
	if err != nil {
		return AshbyApplication{}, err
	}
	appID := strings.TrimSpace(doc.Get("ashby_application_id"))
	if appID == "" {
		return AshbyApplication{}, errf("%s has no Ashby application to move", candidate)
	}
	if _, err := a.client.ChangeStage(ctx, AshbyStageChange{ApplicationID: appID, InterviewStageID: interviewStageID, ArchiveReasonID: archiveReasonID}); err != nil {
		return AshbyApplication{}, err
	}
	app, err := a.client.GetApplication(ctx, appID)
	if err != nil {
		return AshbyApplication{}, err
	}
	if actor == "" {
		actor = a.store.owner
	}
	detail := "stage " + interviewStageID
	if archiveReasonID != "" {
		detail = "archived: " + archiveReasonID
	}
	line := AshbyAudit{At: now.UTC().Format(time.RFC3339), Actor: actor, Candidate: doc.Get("id"),
		Method: "application.changeStage", AshbyID: appID, Detail: detail}
	if _, err := a.applyToCandidate(slug, ashbyRecordPatch{set: map[string]string{"ashby_stage": ashbyStageOf(app)}, audit: []AshbyAudit{line}}); err != nil {
		return app, err
	}
	st := a.load()
	st.Audit = append(st.Audit, line)
	return app, a.save(st)
}

// ashbyStageOf is the official application state as the record's
// `ashby_stage` carries it — the one inbound value an Ashby-authoritative
// field gets.
func ashbyStageOf(app AshbyApplication) string {
	stage := strings.TrimSpace(app.Stage)
	if strings.EqualFold(app.Status, "Archived") {
		stage = "archived"
		if app.ArchiveReason != "" {
			stage += ": " + app.ArchiveReason
		}
	}
	return stage
}

// ---- sync-back trigger (user-actioned; the receiver is Phase 7) ----

// AshbySyncBackResult is what one sync-back did.
type AshbySyncBackResult struct {
	Full         bool     `json:"full"`
	Synced       string   `json:"synced"`
	Postings     int      `json:"postings"`
	Candidates   int      `json:"candidates"`
	Applications int      `json:"applications"`
	RolesLinked  []string `json:"rolesLinked"`
	Updated      []string `json:"updated"`
	Drifted      []string `json:"drifted"`
	Conflicts    []string `json:"conflicts"`
}

// SyncBack mirrors Ashby-authoritative state INBOUND for records already
// linked to Ashby, and nothing else:
//
//   - jobPosting.list (listedOnly:false, explicit) fills `ashby_job_id` on
//     roles whose `ashby_posting_id` the Phase 2 mirror wrote;
//   - candidate.list refreshes each linked candidate's base snapshot, and
//     reports shared fields where Ashby moved (`drifted`) or both moved
//     (`conflicts`) — it never rewrites a Manifest profile field;
//   - application.list mirrors the official stage onto `ashby_stage`.
//
// `full` ignores stored syncTokens; otherwise the incremental tokens from
// the last run are used where the endpoint returned one. There is no timer
// calling this — it runs when the owner hits the route, or when a verified
// webhook delivery lands (HandleWebhook), which is the same reconciliation
// with a dedupe key recorded beside it.
func (a *AshbySync) SyncBack(ctx context.Context, full bool, now time.Time) (AshbySyncBackResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.client.Configured() {
		return AshbySyncBackResult{}, ErrAshbyUnconfigured
	}
	st := a.load()
	res, err := a.syncBack(ctx, &st, full, now)
	if err != nil {
		return res, err
	}
	return res, a.save(st)
}

// syncBack is SyncBack's body under the lock: it mutates st and leaves the
// save to the caller, so a webhook can record its key in the same write.
func (a *AshbySync) syncBack(ctx context.Context, st *AshbySyncState, full bool, now time.Time) (AshbySyncBackResult, error) {
	res := AshbySyncBackResult{Full: full, Synced: now.UTC().Format(time.RFC3339),
		RolesLinked: []string{}, Updated: []string{}, Drifted: []string{}, Conflicts: []string{}}
	token := func(method string) string {
		if full {
			return ""
		}
		return st.SyncTokens[method]
	}

	// roles: posting id → job id
	postings, err := a.client.ListJobPostings(ctx, false)
	if err != nil {
		return res, err
	}
	res.Postings = len(postings)
	byPosting := map[string]AshbyJobPosting{}
	for _, p := range postings {
		byPosting[p.ID] = p
	}
	for _, slug := range a.store.RoleSlugs() {
		role := a.store.LoadRole(slug)
		p, ok := byPosting[strings.TrimSpace(role.Get("ashby_posting_id"))]
		if !ok || p.JobID == "" || role.Get("ashby_job_id") == p.JobID {
			continue
		}
		if err := a.applyToRole(slug, map[string]string{"ashby_job_id": p.JobID}); err != nil {
			return res, err
		}
		res.RolesLinked = append(res.RolesLinked, slug)
	}

	// linked candidates, by Ashby id
	linked := map[string]string{} // ashby id → slug
	docs := map[string]*CandidateDoc{}
	for _, slug := range a.store.CandidateSlugs() {
		d := a.store.LoadCandidate(slug)
		if id := strings.TrimSpace(d.Get("ashby_candidate_id")); id != "" {
			linked[id] = slug
			docs[slug] = d
		}
	}

	cands, ctoken, err := a.client.ListCandidates(ctx, token("candidate.list"))
	if err != nil {
		return res, err
	}
	res.Candidates = len(cands)
	// what each touched record gets, decided against the docs loaded above
	// and applied afterwards through applyToCandidate against the record as
	// it is then
	touched := map[string]bool{}
	patches := map[string]map[string]string{}
	patch := func(slug string) map[string]string {
		if patches[slug] == nil {
			patches[slug] = map[string]string{}
		}
		return patches[slug]
	}
	for _, c := range cands {
		slug, ok := linked[c.ID]
		if !ok {
			continue
		}
		doc := docs[slug]
		id := doc.Get("id")
		cs := st.Candidates[id]
		manifest := candidateProjection(doc, true)
		diff, conflict := diffAgainst(manifest, c, cs.Base)
		drift := false
		for _, d := range diff {
			if d.Action == DiffPull {
				drift = true
			}
		}
		if conflict {
			res.Conflicts = append(res.Conflicts, id)
		} else if drift {
			res.Drifted = append(res.Drifted, id)
		}
		// the base snapshot advances only where nothing is contested
		if !conflict {
			st.Candidates[id] = AshbyCandidateState{AshbyID: c.ID, Base: ashbySide(c), SyncedAt: res.Synced}
		}
		touched[slug] = true
	}

	apps, atoken, err := a.client.ListApplications(ctx, token("application.list"))
	if err != nil {
		return res, err
	}
	res.Applications = len(apps)
	for _, app := range apps {
		slug, ok := linked[app.CandidateID]
		if !ok {
			continue
		}
		doc := docs[slug]
		have := strings.TrimSpace(doc.Get("ashby_application_id"))
		if have != "" && have != app.ID {
			continue // a different application of the same person; not ours
		}
		changed := false
		if have == "" {
			// the in-memory doc advances too, so a second application of the
			// same person later in the list reads as "not ours"
			doc.Set("ashby_application_id", app.ID)
			patch(slug)["ashby_application_id"] = app.ID
			changed = true
		}
		if stage := ashbyStageOf(app); doc.Get("ashby_stage") != stage {
			doc.Set("ashby_stage", stage)
			patch(slug)["ashby_stage"] = stage
			changed = true
		}
		if changed {
			touched[slug] = true
			res.Updated = append(res.Updated, doc.Get("id"))
		}
	}
	for slug := range touched {
		set := patch(slug)
		set["ashby_synced"] = now.UTC().Format("2006-01-02")
		if _, err := a.applyToCandidate(slug, ashbyRecordPatch{set: set}); err != nil {
			return res, err
		}
	}
	sort.Strings(res.Updated)
	sort.Strings(res.Drifted)
	sort.Strings(res.Conflicts)

	if ctoken != "" {
		st.SyncTokens["candidate.list"] = ctoken
	}
	if atoken != "" {
		st.SyncTokens["application.list"] = atoken
	}
	st.LastSync = res.Synced
	if full || st.LastFull == "" {
		st.LastFull = res.Synced
	}
	return res, nil
}

// ---- webhook receiver (Phase 7) ----

// AshbyWebhookResult is what one verified delivery did. A redelivery of a
// key already in the state answers Duplicate:true with no sync.
type AshbyWebhookResult struct {
	Key       string               `json:"key"`
	Action    string               `json:"action,omitempty"`
	Duplicate bool                 `json:"duplicate"`
	Sync      *AshbySyncBackResult `json:"sync,omitempty"`
}

// HandleWebhook is the receiver's funnel: a delivery is a SIGNAL that
// something changed, never the change itself. Whatever the payload says,
// the only effect is an incremental SyncBack — the same reconciliation the
// owner's route runs — and the key is recorded in the derived state so a
// redelivery is skipped instead of applied twice. A failed sync records
// nothing, so the sender's retry runs it again.
func (a *AshbySync) HandleWebhook(ctx context.Context, key, action string, now time.Time) (AshbyWebhookResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return AshbyWebhookResult{}, errf("recruiting: webhook delivery has no key")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	res := AshbyWebhookResult{Key: key, Action: action}
	if !a.client.Configured() {
		return res, ErrAshbyUnconfigured
	}
	st := a.load()
	for _, k := range st.Webhooks {
		if k == key {
			res.Duplicate = true
			return res, nil
		}
	}
	sync, err := a.syncBack(ctx, &st, false, now)
	if err != nil {
		return res, err
	}
	res.Sync = &sync
	st.Webhooks = append(st.Webhooks, key)
	return res, a.save(st)
}
