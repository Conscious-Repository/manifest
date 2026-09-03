package recruiting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"manifest/record"
)

// The PUBLIC Ashby job-board mirror (plan §4.7, Phase 2). No key, no auth,
// no LLM: one GET against the posting API every Ashby careers page reads,
// decoded and mapped onto roles/<slug>.md. It is a user action behind
// POST …/roles/sync — never a poller — because AION postings change on the
// order of months and a ticker would be motion without information.
//
// What it owns on a role record is exactly the fields the board publishes:
// `title`, `location`, `employment`, `ashby_posting_id`, `source`, `synced`
// and the `## posting` body. Everything else — `## criteria` above all, which
// is Benjamin's translation of the posting into must/nice/disqualifier — is
// carried through byte-for-byte. The private, authenticated client
// (recruiting/ashby.go) is Phase 6 and does not exist yet.

// AshbyPublicBoardURL is the one endpoint this file reads.
const AshbyPublicBoardURL = "https://api.ashbyhq.com/posting-api/job-board/AION%20Biosciences?includeCompensation=true"

// SourceAshbyPublic is the `source:` value a mirrored role carries.
const SourceAshbyPublic = "ashby-public"

// ashbyPostingKeys are the frontmatter keys the public mirror owns, in the
// order they are (re)written. `ashby_job_id` is NOT here: the public API has
// no job id, only a posting id, and a key this file cannot fill it must not
// touch.
var ashbyPostingKeys = []string{"title", "location", "employment", "ashby_posting_id", "source", "synced"}

// AshbyPosting is one public job-board posting, reduced to what a role record
// mirrors plus the public identity fields the sync report shows.
type AshbyPosting struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Location    string `json:"location"`
	Employment  string `json:"employment"`
	Department  string `json:"department,omitempty"`
	Team        string `json:"team,omitempty"`
	Listed      bool   `json:"listed"`
	Remote      bool   `json:"remote"`
	PublishedAt string `json:"publishedAt,omitempty"`
	JobURL      string `json:"jobUrl,omitempty"`
	ApplyURL    string `json:"applyUrl,omitempty"`
	Description string `json:"-"`
}

// ashbyBoard is the wire shape of the posting API (apiVersion 1), pinned by
// testdata/ashby-jobboard.json — a scrubbed capture of the real AION board.
// Jobs is a pointer so a body with no `jobs` key at all is distinguishable
// from an honest empty board.
type ashbyBoard struct {
	APIVersion json.Number `json:"apiVersion"`
	Jobs       *[]ashbyJob `json:"jobs"`
}

type ashbyJob struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Department       string `json:"department"`
	Team             string `json:"team"`
	EmploymentType   string `json:"employmentType"`
	Location         string `json:"location"`
	PublishedAt      string `json:"publishedAt"`
	IsListed         bool   `json:"isListed"`
	IsRemote         bool   `json:"isRemote"`
	WorkplaceType    string `json:"workplaceType"`
	JobURL           string `json:"jobUrl"`
	ApplyURL         string `json:"applyUrl"`
	DescriptionPlain string `json:"descriptionPlain"`
	Address          struct {
		PostalAddress struct {
			Locality string `json:"addressLocality"`
			Region   string `json:"addressRegion"`
			Country  string `json:"addressCountry"`
		} `json:"postalAddress"`
	} `json:"address"`
}

// AshbyPublic is the public job-board client, in the portals/clickup.go
// client shape: a base URL overridable for tests and an injected http.Client.
type AshbyPublic struct {
	url  string
	http *http.Client
}

// NewAshbyPublic builds the client. An empty url means the real AION board;
// a nil client means a 20 s timeout.
func NewAshbyPublic(url string, hc *http.Client) *AshbyPublic {
	if url == "" {
		url = AshbyPublicBoardURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &AshbyPublic{url: url, http: hc}
}

// Fetch reads the public board. Errors name the failure and never echo the
// body: an HTTP status, a decode failure, or a response with no `jobs` field.
func (c *AshbyPublic) Fetch(ctx context.Context) ([]AshbyPosting, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("ashby public: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ashby public: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ashby public: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("ashby public: %w", err)
	}
	return ParseAshbyBoard(body)
}

// ParseAshbyBoard maps a posting-API body onto postings. It is honest about
// what it was given: an empty or non-JSON body, or JSON without a `jobs`
// array, is an error rather than "no postings". A `jobs: []` board is a
// legitimate empty result and returns an empty slice.
func ParseAshbyBoard(body []byte) ([]AshbyPosting, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("ashby public: empty response")
	}
	var board ashbyBoard
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&board); err != nil {
		return nil, fmt.Errorf("ashby public: response is not JSON: %w", err)
	}
	if board.Jobs == nil {
		return nil, fmt.Errorf("ashby public: response carries no jobs field")
	}
	out := make([]AshbyPosting, 0, len(*board.Jobs))
	for i, j := range *board.Jobs {
		if strings.TrimSpace(j.ID) == "" || strings.TrimSpace(j.Title) == "" {
			return nil, fmt.Errorf("ashby public: job %d has no id or title", i)
		}
		out = append(out, AshbyPosting{
			ID:          strings.TrimSpace(j.ID),
			Title:       strings.TrimSpace(j.Title),
			Location:    ashbyLocation(j),
			Employment:  ashbyEmployment(j.EmploymentType, j.WorkplaceType, j.IsRemote),
			Department:  strings.TrimSpace(j.Department),
			Team:        strings.TrimSpace(j.Team),
			Listed:      j.IsListed,
			Remote:      j.IsRemote,
			PublishedAt: strings.TrimSpace(j.PublishedAt),
			JobURL:      strings.TrimSpace(j.JobURL),
			ApplyURL:    strings.TrimSpace(j.ApplyURL),
			Description: j.DescriptionPlain,
		})
	}
	return out, nil
}

// ashbyLocation prefers the structured address ("St. Louis, Missouri") and
// falls back to the board's display string.
func ashbyLocation(j ashbyJob) string {
	loc := strings.TrimSpace(j.Address.PostalAddress.Locality)
	if region := strings.TrimSpace(j.Address.PostalAddress.Region); region != "" {
		if loc == "" {
			loc = region
		} else {
			loc += ", " + region
		}
	}
	if loc == "" {
		loc = strings.TrimSpace(j.Location)
	}
	return loc
}

// ashbyEmployment renders Ashby's enums in the record's own vocabulary
// ("full-time on-site"), passing an unknown value through lower-cased rather
// than dropping it.
func ashbyEmployment(employmentType, workplaceType string, remote bool) string {
	var parts []string
	switch strings.ToLower(strings.TrimSpace(employmentType)) {
	case "":
	case "fulltime":
		parts = append(parts, "full-time")
	case "parttime":
		parts = append(parts, "part-time")
	default:
		parts = append(parts, strings.ToLower(strings.TrimSpace(employmentType)))
	}
	switch strings.ToLower(strings.TrimSpace(workplaceType)) {
	case "":
		if remote {
			parts = append(parts, "remote")
		}
	case "onsite":
		parts = append(parts, "on-site")
	default:
		parts = append(parts, strings.ToLower(strings.TrimSpace(workplaceType)))
	}
	return strings.Join(parts, " ")
}

// ---- role record surgery ----

// SetPosting replaces the `## posting` body and nothing else. Blank lines
// trailing the section (the separator before whatever the owner wrote after
// it) are kept where they were; a line that would read as a new `## `
// heading is escaped so mirrored text can never swallow a later section.
func (d *RoleDoc) SetPosting(text string) {
	sec := ensureSection(&d.Sections, "posting")
	var trailing []Line
	for i := len(sec.Lines) - 1; i >= 0; i-- {
		ln := sec.Lines[i]
		if ln.Row != nil || strings.TrimSpace(ln.Raw) != "" {
			break
		}
		trailing = append([]Line{ln}, trailing...)
	}
	var built []Line
	for _, raw := range postingLines(text) {
		built = append(built, Line{Raw: raw})
	}
	sec.Lines = append(built, trailing...)
}

// postingLines normalizes mirrored text into record lines: CRLF folded,
// trailing whitespace dropped, outer blank lines trimmed, headings escaped.
func postingLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimRight(ln, " \t")
		if strings.HasPrefix(ln, "## ") {
			ln = "\\" + ln
		}
		out = append(out, ln)
	}
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// ApplyAshbyPosting writes the Ashby-owned fields onto the record. It is the
// ONLY place those keys are set from a posting, and it touches no other key
// and no other section.
func (d *RoleDoc) ApplyAshbyPosting(p AshbyPosting, now time.Time) {
	for _, key := range ashbyPostingKeys {
		switch key {
		case "title":
			d.Set(key, p.Title)
		case "location":
			d.Set(key, p.Location)
		case "employment":
			d.Set(key, p.Employment)
		case "ashby_posting_id":
			d.Set(key, p.ID)
		case "source":
			d.Set(key, SourceAshbyPublic)
		case "synced":
			d.Set(key, now.UTC().Format("2006-01-02"))
		}
	}
	d.SetPosting(p.Description)
}

// ---- store action ----

// AshbySyncResult is what one sync did, by role slug. A posting the vault had
// no role for becomes a new role record (empty criteria, owner's to fill);
// a role the board no longer lists is REPORTED, not changed — `status` is not
// an Ashby-owned key, and closing a lane is Benjamin's call.
type AshbySyncResult struct {
	Synced    string   `json:"synced"`
	Postings  int      `json:"postings"`
	Updated   []string `json:"updated"`
	Created   []string `json:"created"`
	Unchanged []string `json:"unchanged"`
	Unlisted  []string `json:"unlisted"`
}

// SyncAshbyPostings mirrors the public postings onto the role records.
// Matching order: an existing `ashby_posting_id`, then a case-insensitive
// title; a posting that matches nothing gets a fresh slug. A role is written
// only when its bytes change.
func (s *Store) SyncAshbyPostings(postings []AshbyPosting, now time.Time) (AshbySyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res := AshbySyncResult{
		Synced: now.UTC().Format("2006-01-02"), Postings: len(postings),
		Updated: []string{}, Created: []string{}, Unchanged: []string{}, Unlisted: []string{},
	}
	slugs := s.RoleSlugs()
	docs := map[string]*RoleDoc{}
	taken := map[string]bool{}
	for _, slug := range slugs {
		docs[slug] = s.LoadRole(slug)
		taken[slug] = true
	}
	claimed := map[string]bool{}
	match := func(p AshbyPosting) string {
		for _, slug := range slugs {
			if !claimed[slug] && docs[slug].Get("ashby_posting_id") == p.ID {
				return slug
			}
		}
		for _, slug := range slugs {
			if !claimed[slug] && strings.EqualFold(strings.TrimSpace(docs[slug].Get("title")), p.Title) {
				return slug
			}
		}
		return ""
	}

	for _, p := range postings {
		slug := match(p)
		if slug == "" {
			slug = NewRoleSlug(p.Title, taken)
			taken[slug] = true
			doc := ParseRole(roleSeed("role/"+slug, p.Title, false, ownerCriteria))
			doc.Slug = slug
			doc.ApplyAshbyPosting(p, now)
			if err := s.SaveRole(slug, doc); err != nil {
				return res, err
			}
			claimed[slug] = true
			res.Created = append(res.Created, slug)
			continue
		}
		claimed[slug] = true
		doc := docs[slug]
		before := SerializeRole(doc)
		doc.ApplyAshbyPosting(p, now)
		after := SerializeRole(doc)
		if after == before {
			res.Unchanged = append(res.Unchanged, slug)
			continue
		}
		if err := s.SaveRole(slug, doc); err != nil {
			return res, err
		}
		res.Updated = append(res.Updated, slug)
	}
	for _, slug := range slugs {
		if !claimed[slug] {
			res.Unlisted = append(res.Unlisted, slug)
		}
	}
	sort.Strings(res.Unlisted)
	return res, nil
}

// NewRoleSlug derives a free roles/<slug>.md name from a posting title.
func NewRoleSlug(title string, taken map[string]bool) string {
	base := record.Slug(title, 60)
	if base == "" {
		base = "role"
	}
	slug := base
	for n := 2; taken[slug]; n++ {
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	return slug
}
