package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NIHRePORTER is the Phase 3b.5 grant adapter: one bounded POST against the
// public NIH RePORTER v2 project search, each principal investigator on a
// returned project becoming a draft that cites the project's own RePORTER
// page. It follows the OpenAlex/ORCID/GitHub/PubMed shape — base URL and
// client injectable, every call bounded by the scope, nothing on the draft
// the returned JSON did not say outright.
//
// RePORTER returns funded projects, not people. A project with three PIs
// yields three drafts; a PI who leads two matching projects yields ONE draft
// with two evidence rows (folded by profile_id, or by name when the record
// carries none). Name is the PI's full_name, or first/middle/last joined;
// org is the project's organization; title is "Principal Investigator",
// which is what the `principal_investigators` array says. No location, no
// edges: same-grant and same-institution claims are Phase 4.
//
// A RePORTER project record is a primary federal funding record, so its
// citations are TrustHigh. The adapter decodes no contact field: a PI
// entry's `email`, if a record ever carried one, has no struct field to
// land in (D15).
type NIHRePORTER struct {
	// BaseURL overrides the API root ("" → NIHRePORTERBaseURL). Tests point
	// it at an httptest server.
	BaseURL string
	// Client is held BY VALUE, not as *http.Client: rule 1 forbids an adapter
	// carrying a pointer or interface field, because a field that can hold a
	// writer is a field that can write. A zero Client is the default
	// transport; a Timeout of zero gets nihTimeout applied per request.
	Client http.Client
}

// compile-time proof that the NIH RePORTER source satisfies the adapter contract.
var _ Adapter = NIHRePORTER{}

const (
	// NIHRePORTERBaseURL is the public RePORTER API root. No key, no auth.
	NIHRePORTERBaseURL = "https://api.reporter.nih.gov"
	// NIHRePORTERUserAgent is the polite identifier every request carries.
	NIHRePORTERUserAgent = "manifest-aion-recruiting/phase3b"
	// NIHRePORTERProjectURL is the human-readable project page, keyed by the
	// application id (`appl_id`). The API also returns it verbatim as
	// `project_detail_url`, which is preferred when present.
	NIHRePORTERProjectURL = "https://reporter.nih.gov/project-details/"
	// NIHRePORTERSearchURL is the fallback citation when a record has no
	// application id: the public search page filtered to the project number.
	NIHRePORTERSearchURL = "https://reporter.nih.gov/search/results?projects="
	// nihSearchPath is the v2 project-search endpoint under the root.
	nihSearchPath = "/v2/projects/search"
	// nihDefaultMax is requested when the scope names no cap.
	nihDefaultMax = 25
	// nihMaxResults is the most one run may ask for. The substrate caps at
	// MaxRunMax too, but the adapter never trusts that it did.
	nihMaxResults = 100
	// nihMaxBody bounds how much of a response is read.
	nihMaxBody = 8 << 20
	// nihTimeout applies when the injected client has none.
	nihTimeout = 30 * time.Second
	// nihTitleChars caps how much of a project title is quoted.
	nihTitleChars = 300
	// nihAbstractChars caps how much of an abstract is quoted.
	nihAbstractChars = 240
	// nihPITitle is the title every draft carries: the only thing the record
	// says about the person's role is that they are on the PI list.
	nihPITitle = "Principal Investigator"
)

// nihSearchFields is the comma-separated list of project fields the
// RePORTER full-text search matches against. It is the API's documented
// `advanced_text_search.search_field` value for "title, terms, abstract".
const nihSearchFields = "projecttitle,terms,abstracttext"

// nihIncludeFields names the response fields asked for, in the API's own
// PascalCase vocabulary, so a response carries only what the adapter reads.
var nihIncludeFields = []string{
	"ApplId", "ProjectNum", "ProjectTitle", "AbstractText", "Organization",
	"PrincipalInvestigators", "AgencyIcAdmin", "FiscalYear",
	"ProjectStartDate", "ProjectEndDate", "ProjectDetailUrl",
}

func (NIHRePORTER) ID() string { return "nihreporter" }

func (NIHRePORTER) Kind() Kind { return KindGrant }

func (NIHRePORTER) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role"},
		{Key: "query", Label: "RePORTER search", Placeholder: "e.g. diffusion MRI reconstruction", Required: true},
		{Key: "max", Label: "max results", Placeholder: strconv.Itoa(nihDefaultMax)},
	}
}

// nihSearchRequest is the v2 request body. The public API's full-text
// criterion is `advanced_text_search` (operator + fields + text); a plain
// `text_search` key is not a documented criterion, so it is not sent.
// Fixtures in testdata mirror the response to exactly this request.
type nihSearchRequest struct {
	Criteria struct {
		AdvancedTextSearch struct {
			Operator    string `json:"operator"`
			SearchField string `json:"search_field"`
			SearchText  string `json:"search_text"`
		} `json:"advanced_text_search"`
	} `json:"criteria"`
	IncludeFields []string `json:"include_fields"`
	Offset        int      `json:"offset"`
	Limit         int      `json:"limit"`
}

// nihSearchResponse is the v2 envelope. Results is a pointer so a response
// with NO results key (a shape change, an error page that happened to be
// JSON) is told apart from an honest empty list.
type nihSearchResponse struct {
	Meta *struct {
		Total int `json:"total"`
	} `json:"meta"`
	Results *[]nihProject `json:"results"`
	Message string        `json:"message"`
	Error   string        `json:"error"`
}

// nihInvestigator is the slice of a PI entry this adapter reads. There is
// deliberately no field for `email` or any other address: an unknown key is
// ignored by the decoder, so a published address structurally cannot reach
// a draft.
type nihInvestigator struct {
	ProfileID  json.Number `json:"profile_id"`
	FirstName  string      `json:"first_name"`
	MiddleName string      `json:"middle_name"`
	LastName   string      `json:"last_name"`
	FullName   string      `json:"full_name"`
}

// nihProject is the slice of one project record this adapter reads.
type nihProject struct {
	ApplID       json.Number `json:"appl_id"`
	ProjectNum   string      `json:"project_num"`
	ProjectTitle string      `json:"project_title"`
	AbstractText string      `json:"abstract_text"`
	Organization struct {
		OrgName string `json:"org_name"`
	} `json:"organization"`
	PrincipalInvestigators []nihInvestigator `json:"principal_investigators"`
	AgencyICAdmin          struct {
		Code         string `json:"code"`
		Abbreviation string `json:"abbreviation"`
	} `json:"agency_ic_admin"`
	FiscalYear       json.Number `json:"fiscal_year"`
	ProjectStartDate string      `json:"project_start_date"`
	ProjectEndDate   string      `json:"project_end_date"`
	ProjectDetailURL string      `json:"project_detail_url"`
}

// Search runs exactly one bounded POST /v2/projects/search with the query as
// a full-text criterion and the scope's Max as the limit. It never
// paginates: offset is always 0, and however many projects the server sends
// back, at most Max distinct drafts leave. A search that matches nothing
// returns an empty slice, not an error.
func (n NIHRePORTER) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	query := strings.TrimSpace(s.Query)
	if query == "" {
		return nil, errors.New("nihreporter: a search needs a query")
	}
	max := s.Max
	if max <= 0 {
		max = nihDefaultMax
	}
	if max > nihMaxResults {
		max = nihMaxResults
	}

	var reqBody nihSearchRequest
	reqBody.Criteria.AdvancedTextSearch.Operator = "and"
	reqBody.Criteria.AdvancedTextSearch.SearchField = nihSearchFields
	reqBody.Criteria.AdvancedTextSearch.SearchText = query
	reqBody.IncludeFields = nihIncludeFields
	reqBody.Offset = 0
	reqBody.Limit = max
	body, err := n.post(ctx, nihSearchPath, reqBody)
	if err != nil {
		return nil, err
	}
	var resp nihSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("nihreporter: malformed response from %s: %v", nihSearchPath, err)
	}
	if resp.Results == nil {
		for _, msg := range []string{resp.Error, resp.Message} {
			if msg = strings.TrimSpace(msg); msg != "" {
				return nil, fmt.Errorf("nihreporter: %s reported an error: %s", nihSearchPath, msg)
			}
		}
		return nil, fmt.Errorf("nihreporter: response from %s has no results field", nihSearchPath)
	}
	if len(*resp.Results) == 0 {
		return []CandidateDraft{}, nil
	}

	retrieved := time.Now().UTC()
	out := make([]CandidateDraft, 0, max)
	byKey := map[string]int{}
	// results order is the search's own ranking.
	for _, proj := range *resp.Results {
		num := strings.TrimSpace(proj.ProjectNum)
		if num == "" || strings.TrimSpace(proj.ProjectTitle) == "" {
			continue
		}
		link := proj.link()
		if link == "" {
			continue
		}
		for _, pi := range proj.PrincipalInvestigators {
			name, ok := pi.name()
			if !ok {
				continue
			}
			ev := n.evidence(proj, name, link, retrieved)
			key := pi.key(name)
			if i, seen := byKey[key]; seen {
				if !containsString(out[i].Links, link) {
					out[i].Links = append(out[i].Links, link)
				}
				out[i].Evidence = append(out[i].Evidence, ev)
				continue
			}
			if len(out) >= max {
				continue
			}
			byKey[key] = len(out)
			out = append(out, CandidateDraft{
				SourceID:   n.ID(),
				ExternalID: num + ":" + key,
				Name:       name,
				Org:        strings.Join(strings.Fields(proj.Organization.OrgName), " "),
				Title:      nihPITitle,
				Role:       strings.TrimSpace(s.Role),
				Links:      []string{link},
				Evidence:   []Evidence{ev},
			})
		}
	}
	return out, nil
}

// name returns the PI's display name: full_name when the record spells one,
// else first/middle/last joined. A name that looks like an address is not a
// name, and a PI with no name at all is not a draft.
func (p nihInvestigator) name() (string, bool) {
	name := strings.Join(strings.Fields(p.FullName), " ")
	if name == "" {
		name = strings.Join(strings.Fields(p.FirstName+" "+p.MiddleName+" "+p.LastName), " ")
	}
	if name == "" || containsAddress(name) {
		return "", false
	}
	return name, true
}

// key is the fold key for one person across projects: the RePORTER
// profile_id when present, else the name exactly as displayed.
func (p nihInvestigator) key(name string) string {
	if id := strings.TrimSpace(p.ProfileID.String()); id != "" && id != "0" {
		return id
	}
	return name
}

// link is the citation URL for a project: the API's own project_detail_url
// when it is an absolute http(s) URL, else the project page keyed by
// appl_id, else the public search page filtered to the project number.
func (p nihProject) link() string {
	if u := strings.TrimSpace(p.ProjectDetailURL); u != "" {
		if parsed, err := url.Parse(u); err == nil && parsed.Host != "" &&
			(parsed.Scheme == "https" || parsed.Scheme == "http") {
			return u
		}
	}
	if id := strings.TrimSpace(p.ApplID.String()); id != "" && id != "0" {
		return NIHRePORTERProjectURL + url.PathEscape(id)
	}
	if num := strings.TrimSpace(p.ProjectNum); num != "" {
		return NIHRePORTERSearchURL + url.QueryEscape(num)
	}
	return ""
}

// evidence builds the one row a project contributes to a PI's draft: the
// project page is the URL, and the snippet quotes the PI, title, project
// number, organization, administering IC, fiscal year and the opening of
// the abstract exactly as the record gave them.
func (n NIHRePORTER) evidence(p nihProject, name, link string, retrieved time.Time) Evidence {
	title := strings.Join(strings.Fields(p.ProjectTitle), " ")
	if r := []rune(title); len(r) > nihTitleChars {
		title = string(r[:nihTitleChars]) + "…"
	}
	parts := []string{"pi: " + name, "project: " + title, "project_num: " + strings.TrimSpace(p.ProjectNum)}
	if org := strings.Join(strings.Fields(p.Organization.OrgName), " "); org != "" {
		parts = append(parts, "org: "+org)
	}
	ic := strings.TrimSpace(p.AgencyICAdmin.Abbreviation)
	if ic == "" {
		ic = strings.TrimSpace(p.AgencyICAdmin.Code)
	}
	if ic != "" {
		parts = append(parts, "ic: "+ic)
	}
	if fy := strings.TrimSpace(p.FiscalYear.String()); fy != "" && fy != "0" {
		parts = append(parts, "fy: "+fy)
	}
	if abstract := strings.Join(strings.Fields(p.AbstractText), " "); abstract != "" {
		if r := []rune(abstract); len(r) > nihAbstractChars {
			abstract = string(r[:nihAbstractChars]) + "…"
		}
		parts = append(parts, "abstract: "+abstract)
	}
	return Evidence{
		SourceID: n.ID(), URLOrFile: link, RetrievedAt: retrieved,
		Snippet: strings.Join(parts, " · "), Kind: EvidenceGrant, Trust: TrustHigh,
	}
}

// Enrich is a no-op: everything the draft carries came back in the single
// search response, and nothing more is read in this phase.
func (NIHRePORTER) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) { return d, nil }

// GraphEdges returns what the draft already carries — nothing, in 3b.5. The
// co-PI list is read but not turned into same_grant claims; that is Phase 4.
func (NIHRePORTER) GraphEdges(_ context.Context, d CandidateDraft) ([]EdgeClaim, error) {
	return d.Edges, nil
}

// post performs one JSON POST against the API root with the polite
// User-Agent, a bounded read, and turns any non-200 into an error that
// names the status.
func (n NIHRePORTER) post(ctx context.Context, path string, payload any) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(n.BaseURL), "/")
	if base == "" {
		base = NIHRePORTERBaseURL
	}
	if n.Client.Timeout == 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, nihTimeout)
		defer cancel()
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("nihreporter: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("nihreporter: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", NIHRePORTERUserAgent)

	resp, err := n.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nihreporter: POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, nihMaxBody))
	if err != nil {
		return nil, fmt.Errorf("nihreporter: reading POST %s: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("nihreporter: POST %s returned HTTP %d", path, resp.StatusCode)
		if excerpt := strings.Join(strings.Fields(string(body)), " "); excerpt != "" {
			if len(excerpt) > 200 {
				excerpt = excerpt[:200] + "…"
			}
			msg += ": " + excerpt
		}
		return nil, errors.New(msg)
	}
	return body, nil
}

// containsString reports whether s is already in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
