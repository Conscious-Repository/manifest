package sources

import (
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

// OpenAlex is the Phase 3b.1 scholarly adapter: one search against the
// public, no-key OpenAlex authors endpoint, each hit becoming a draft that
// cites its own author page. It is the first network adapter, so it also
// sets the shape for the ones after it: one bounded GET per run, the base
// URL and client injectable so tests never leave the process, and nothing
// on the draft that the returned JSON did not say outright.
//
// OpenAlex is an aggregator over primary records, which is why its citations
// are TrustMedium rather than TrustHigh. It exposes no contact details and
// this adapter would drop them if it did (D15). Coauthor edges need the
// works endpoint and are Phase 4's; the author record alone supports no
// relationship claim, so this adapter emits none.
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

// compile-time proof that the OpenAlex source satisfies the adapter contract.
var _ Adapter = OpenAlex{}

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
		{Key: "query", Label: "author name or keyword", Placeholder: "e.g. diffusion MRI reconstruction"},
		{Key: "work", Label: "or one paper", Placeholder: "DOI, OpenAlex id, or link"},
		{Key: "max", Label: "max results", Placeholder: strconv.Itoa(openAlexDefaultMax)},
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
	DisplayName string `json:"display_name"`
	CountryCode string `json:"country_code"`
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
// JSON) is told apart from an honest empty list.
type openAlexAuthorsResponse struct {
	Results *[]openAlexAuthor `json:"results"`
}

// Search runs one bounded GET /authors?search=… and converts each returned
// author into a cited draft. It never paginates: the scope's Max is both the
// per-page it asks for and the most it will return, whatever the server sent.
func (oa OpenAlex) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	if ref := strings.TrimSpace(s.Fields["work"]); ref != "" {
		return oa.searchWork(ctx, ref, s)
	}
	query := strings.TrimSpace(s.Query)
	if query == "" {
		return nil, errors.New("openalex: a search needs a query, or one paper")
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
		return nil, err
	}
	var resp openAlexAuthorsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("openalex: malformed response from /authors: %v", err)
	}
	if resp.Results == nil {
		return nil, errors.New("openalex: response from /authors has no results field")
	}

	retrieved := time.Now().UTC()
	out := make([]CandidateDraft, 0, min(len(*resp.Results), max))
	for _, a := range *resp.Results {
		if len(out) >= max {
			break
		}
		d, ok := oa.draft(a, s.Role, retrieved)
		if !ok {
			continue
		}
		out = append(out, d)
	}
	return out, nil
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

// get performs one GET against the API root with the polite headers and a
// bounded read, and turns any non-200 into an error that names the status.
func (oa OpenAlex) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(oa.BaseURL), "/")
	if base == "" {
		base = OpenAlexBaseURL
	}
	if oa.Client.Timeout == 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, openAlexTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("openalex: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", OpenAlexUserAgent)

	resp, err := oa.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openalex: GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, openAlexMaxBody))
	if err != nil {
		return nil, fmt.Errorf("openalex: reading GET %s: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("openalex: GET %s returned HTTP %d", path, resp.StatusCode)
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
