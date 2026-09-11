package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OpenAlex is the scholarly adapter over the public, no-key OpenAlex API. It
// was the first network adapter (Phase 3b.1), so it also set the shape for
// the ones after it: bounded GETs, the base URL and client injectable so
// tests never leave the process, and nothing on the draft that the returned
// JSON did not say outright. It has three branches, chosen by the scope:
//
//   - one paper (scope field "work") → everyone on it, with coauthor and
//     same_lab claims (sources_openalex_works.go);
//   - a name-shaped query, or mode=authors → /authors?search=<name>, one
//     page, each hit a draft citing its own author page (this file);
//   - a keyword query, or mode=works → /works searched by text under a
//     work budget, authorships aggregated into people by author id
//     (sources_openalex_people.go — sourcing-effectiveness plan Phase 2).
//
// OpenAlex is an aggregator over primary records, which is why its citations
// are TrustMedium rather than TrustHigh. It exposes no contact details and
// this adapter would drop them if it did (D15). An author record alone
// supports no relationship claim, so the name branch emits no edges.
type OpenAlex struct {
	// BaseURL overrides the API root ("" → OpenAlexBaseURL). Tests point it
	// at an httptest server.
	BaseURL string
	// Client is held BY VALUE, not as *http.Client: rule 1 forbids an adapter
	// carrying a pointer or interface field, because a field that can hold a
	// writer is a field that can write. A zero Client is the default
	// transport; a Timeout of zero gets openAlexTimeout applied per request.
	Client http.Client
}

// compile-time proof that the OpenAlex source satisfies the adapter contract,
// and reports the size of its field (Phase 0).
var (
	_ Adapter = OpenAlex{}
	_ Counted = OpenAlex{}
)

const (
	// OpenAlexBaseURL is the public API root. No key, no auth.
	OpenAlexBaseURL = "https://api.openalex.org"
	// OpenAlexUserAgent is the polite identifier every request carries.
	OpenAlexUserAgent = "manifest-aion-recruiting/phase3b"
	// openAlexAuthorRoot is where an author id resolves as a page.
	openAlexAuthorRoot = "https://openalex.org/"
	// openAlexDefaultMax is requested when the scope names no cap.
	openAlexDefaultMax = 25
	// openAlexMaxPerPage is the most one run may ask for. The substrate caps
	// at MaxRunMax too, but the adapter never trusts that it did.
	openAlexMaxPerPage = 100
	// openAlexMaxBody bounds how much of a response is read.
	openAlexMaxBody = 8 << 20
	// openAlexTimeout applies when the injected client has none.
	openAlexTimeout = 30 * time.Second
	// openAlexTopics caps how many topics reach the snippet.
	openAlexTopics = 5
)

func (OpenAlex) ID() string { return "openalex" }

func (OpenAlex) Kind() Kind { return KindScholarly }

func (OpenAlex) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role"},
		{Key: "query", Label: "keyword, or an author name", Placeholder: "e.g. field cycling MRI — a name (Dana Reyes) looks up authors"},
		{Key: "work", Label: "or one paper", Placeholder: "DOI, OpenAlex id, or link"},
		{Key: "max", Label: "max people shown", Placeholder: strconv.Itoa(openAlexDefaultMax)},
		{Key: openAlexFieldMode, Label: "search", Placeholder: openAlexModeWorks + " (papers → people; the default for a keyword) or " + openAlexModeAuthors + " (name lookup; the default for a name)"},
		{Key: openAlexFieldWorks, Label: "works to read", Placeholder: strconv.Itoa(openAlexDefaultWorks) + " — the work budget, at most " + strconv.Itoa(openAlexMaxWorks) + "; separate from people shown"},
		{Key: openAlexFieldYears, Label: "publication years", Placeholder: "e.g. 2018-2026 (works search)"},
		{Key: openAlexFieldType, Label: "work type", Placeholder: "e.g. article (works search)"},
		{Key: openAlexFieldText, Label: "match in", Placeholder: openAlexTextTitleAbstract + " (default) or " + openAlexTextFulltext + " (works search)"},
	}
}

// openAlexAuthor is the slice of an OpenAlex author object this adapter
// reads. Unknown fields — including anything contact-shaped — are ignored by
// the decoder and so cannot reach a draft.
type openAlexAuthor struct {
	ID                      string                `json:"id"`
	DisplayName             string                `json:"display_name"`
	DisplayNameAlternatives []string              `json:"display_name_alternatives"`
	ORCID                   string                `json:"orcid"`
	WorksCount              int                   `json:"works_count"`
	CitedByCount            int                   `json:"cited_by_count"`
	LastKnownInstitution    openAlexInstitution   `json:"last_known_institution"`
	LastKnownInstitutions   []openAlexInstitution `json:"last_known_institutions"`
	SummaryStats            openAlexSummaryStats  `json:"summary_stats"`
	Topics                  []openAlexNamed       `json:"topics"`
	Affiliations            []openAlexAffiliation `json:"affiliations"`
}

type openAlexInstitution struct {
	// ID is the registry's durable institution key (https://openalex.org/I…).
	// It is what makes "same affiliation" a resolvable claim rather than a
	// string match: two authors share an institution when their IDs agree,
	// never when their display names happen to.
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	CountryCode string `json:"country_code"`
}

// key is the institution id without the registry root ("I134446601"), or ""
// when the record carries none.
func (i openAlexInstitution) key() string {
	id := strings.TrimSpace(i.ID)
	if j := strings.LastIndex(id, "/"); j >= 0 {
		id = id[j+1:]
	}
	if !strings.HasPrefix(strings.ToUpper(id), "I") {
		return ""
	}
	return strings.ToUpper(id)
}

type openAlexSummaryStats struct {
	HIndex float64 `json:"h_index"`
}

type openAlexNamed struct {
	DisplayName string `json:"display_name"`
}

type openAlexAffiliation struct {
	Institution openAlexInstitution `json:"institution"`
}

// openAlexAuthorsResponse is the envelope. Results is a pointer so a response
// with NO results key (a shape change, an error page that happened to be
// JSON) is told apart from an honest empty list. Meta.count is the total the
// registry matched, pointer for the same reason: absent is "did not say".
type openAlexAuthorsResponse struct {
	Meta *struct {
		Count *int `json:"count"`
	} `json:"meta"`
	Results *[]openAlexAuthor `json:"results"`
}

// Search is SearchCounted without the counts.
func (oa OpenAlex) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	out, _, err := oa.SearchCounted(ctx, s)
	return out, err
}

// SearchCounted is Search plus the size of the field (Phase 0), on whichever
// branch the scope selects. The single-paper path reports the one work and
// everyone named on it; the name path reports meta.count — every author the
// registry matched — the author rows the one page carried, and how many of
// those were citable, counted past the cap; the keyword path reports
// matching works, works read, and distinct people seen before the cap.
func (oa OpenAlex) SearchCounted(ctx context.Context, s Scope) ([]CandidateDraft, Retrieval, error) {
	if ref := strings.TrimSpace(s.Fields["work"]); ref != "" {
		return oa.searchWork(ctx, ref, s)
	}
	plan, err := openAlexPlanScope(s)
	if err != nil {
		return nil, Retrieval{}, err
	}
	if plan.Mode == openAlexModeWorks {
		return oa.searchWorks(ctx, s, plan)
	}
	return oa.searchAuthors(ctx, s, plan.Query)
}

// searchAuthors is the name branch: one bounded GET /authors?search=…, each
// returned author a cited draft. It never paginates: the scope's Max is
// both the per-page it asks for and the most it will return, whatever the
// server sent.
func (oa OpenAlex) searchAuthors(ctx context.Context, s Scope, query string) ([]CandidateDraft, Retrieval, error) {
	ret := Retrieval{Unit: "authors"}
	if query = strings.TrimSpace(query); query == "" {
		return nil, ret, errors.New("openalex: a search needs a query, or one paper")
	}
	max := s.Max
	if max <= 0 {
		max = openAlexDefaultMax
	}
	if max > openAlexMaxPerPage {
		max = openAlexMaxPerPage
	}

	params := url.Values{}
	params.Set("search", query)
	params.Set("per-page", strconv.Itoa(max))
	body, err := oa.get(ctx, "/authors", params)
	if err != nil {
		return nil, ret, err
	}
	var resp openAlexAuthorsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, ret, fmt.Errorf("openalex: malformed response from /authors: %v", err)
	}
	if resp.Results == nil {
		return nil, ret, errors.New("openalex: response from /authors has no results field")
	}
	if resp.Meta != nil && resp.Meta.Count != nil && *resp.Meta.Count >= 0 {
		ret.Available = Known(*resp.Meta.Count)
	}
	ret.Read = len(*resp.Results)

	retrieved := time.Now().UTC()
	out := make([]CandidateDraft, 0, min(len(*resp.Results), max))
	for _, a := range *resp.Results {
		d, ok := oa.draft(a, s.Role, retrieved)
		if !ok {
			continue
		}
		ret.PeopleSeen++
		if len(out) >= max {
			continue
		}
		out = append(out, d)
	}
	return out, ret, nil
}

// draft converts one returned author into a draft. It reports false for an
// author it cannot cite (no id) or cannot name: a draft with no evidence is
// refused downstream, and an unnamed one is not a candidate.
func (oa OpenAlex) draft(a openAlexAuthor, role string, retrieved time.Time) (CandidateDraft, bool) {
	name := strings.TrimSpace(a.DisplayName)
	authorURL := openAlexAuthorURL(a.ID)
	if name == "" || authorURL == "" {
		return CandidateDraft{}, false
	}
	inst := a.institution()

	d := CandidateDraft{
		SourceID:   oa.ID(),
		ExternalID: strings.TrimPrefix(authorURL, openAlexAuthorRoot),
		Name:       name,
		Org:        strings.TrimSpace(inst.DisplayName),
		Location:   strings.TrimSpace(inst.CountryCode),
		Role:       strings.TrimSpace(role),
		Links:      []string{authorURL},
	}
	if orcid := openAlexORCIDURL(a.ORCID); orcid != "" {
		d.Links = append(d.Links, orcid)
		d.Orcid = orcid
	}

	// The author record's own topics (OpenAlex derives them from the works
	// it attributes to THIS author id) — author-canonical, so they may ride
	// the structured field as well as the note (O1/O4).
	topics := make([]string, 0, openAlexTopics)
	for _, t := range a.Topics {
		if len(topics) >= openAlexTopics {
			break
		}
		if s := strings.TrimSpace(t.DisplayName); s != "" {
			topics = append(topics, s)
		}
	}
	if len(topics) > 0 {
		d.Note = "topics: " + strings.Join(topics, "; ")
		d.Topics = append([]string(nil), topics...)
	}

	// The affiliation row exists only when the record names an institution;
	// the publication row always exists, because counts of zero are still
	// what the source said.
	if d.Org != "" {
		snippet := "last_known_institution: " + d.Org
		if d.Location != "" {
			snippet += " (" + d.Location + ")"
		}
		d.Evidence = append(d.Evidence, Evidence{
			SourceID: oa.ID(), URLOrFile: authorURL, RetrievedAt: retrieved,
			Snippet: snippet, Kind: EvidenceAffiliation, Trust: TrustMedium,
		})
	}
	parts := []string{
		"works_count: " + strconv.Itoa(a.WorksCount),
		"cited_by_count: " + strconv.Itoa(a.CitedByCount),
		"h_index: " + strconv.FormatFloat(a.SummaryStats.HIndex, 'f', -1, 64),
	}
	if len(topics) > 0 {
		parts = append(parts, "topics: "+strings.Join(topics, "; "))
	}
	d.Evidence = append(d.Evidence, Evidence{
		SourceID: oa.ID(), URLOrFile: authorURL, RetrievedAt: retrieved,
		Snippet: strings.Join(parts, " · "), Kind: EvidencePublication, Trust: TrustMedium,
	})
	return d, true
}

// institution picks the institution the record names, preferring the
// singular field the API documents, then the plural it is moving to, then
// the first listed affiliation. Nothing is inferred: absent everywhere means
// no org.
func (a openAlexAuthor) institution() openAlexInstitution {
	if strings.TrimSpace(a.LastKnownInstitution.DisplayName) != "" {
		return a.LastKnownInstitution
	}
	for _, i := range a.LastKnownInstitutions {
		if strings.TrimSpace(i.DisplayName) != "" {
			return i
		}
	}
	for _, af := range a.Affiliations {
		if strings.TrimSpace(af.Institution.DisplayName) != "" {
			return af.Institution
		}
	}
	return openAlexInstitution{}
}

// Enrich is a no-op: the author record is already everything the search
// returned, and a second call per draft is Phase 4's works fetch, not this.
func (OpenAlex) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) { return d, nil }

// GraphEdges returns what the draft already carries — nothing, in 3b.1. An
// author record names no coauthor, so no edge is supported by it.
func (OpenAlex) GraphEdges(_ context.Context, d CandidateDraft) ([]EdgeClaim, error) {
	return d.Edges, nil
}

// get preserves the polite headers and body bound, retrying transient
// statuses before returning an error that names the final status. fetch is
// the same with a caller-chosen body bound, for the works pages.
func (oa OpenAlex) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	return oa.fetch(ctx, path, params, openAlexMaxBody)
}

func (oa OpenAlex) fetch(ctx context.Context, path string, params url.Values, maxBody int64) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(oa.BaseURL), "/")
	if base == "" {
		base = OpenAlexBaseURL
	}
	timeout := oa.Client.Timeout
	if timeout == 0 {
		timeout = openAlexTimeout
	}
	// Bound the entire fetch, including Retry-After waits.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("openalex: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", OpenAlexUserAgent)

	return scholarlyGet(oa.Client, req, "openalex", path, maxBody)
}

// openAlexAuthorURL normalises an author id — the API returns it as the full
// "https://openalex.org/A…" URL, but a bare "A…" is accepted too — into the
// page it cites. "" when there is no id.
func openAlexAuthorURL(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "https://") || strings.HasPrefix(id, "http://") {
		return id
	}
	return openAlexAuthorRoot + id
}

// openAlexORCIDURL returns the ORCID as a URL, or "" when the record has
// none. A bare identifier is prefixed; a URL is kept as returned.
func openAlexORCIDURL(orcid string) string {
	orcid = strings.TrimSpace(orcid)
	if orcid == "" {
		return ""
	}
	if strings.HasPrefix(orcid, "https://") || strings.HasPrefix(orcid, "http://") {
		return orcid
	}
	return "https://orcid.org/" + orcid
}
