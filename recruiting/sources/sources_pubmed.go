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

// PubMed is the Phase 3b.4 scholarly adapter: one bounded esearch against
// NCBI E-utilities for PMIDs, then one esummary for those PMIDs, each usable
// paper becoming a draft for its FIRST author that cites the paper's own
// PubMed page. It follows the OpenAlex/ORCID/GitHub shape — base URL and
// client injectable, every call bounded by the scope, nothing on the draft
// the returned JSON did not say outright.
//
// PubMed returns papers, not people. This phase is deliberately narrow: one
// author per paper (the first author of type "Author"), the name exactly as
// the summary spells it ("Reyes DM"), no affiliation (esummary carries
// none, and an affiliation not present is not inferred), no edges (the
// coauthor graph is Phase 4). When the same first-author string heads more
// than one paper in a run, those papers become extra evidence rows on one
// draft rather than duplicate drafts.
//
// The summary is a primary bibliographic record maintained by NLM, so its
// citations are TrustHigh. The adapter decodes no contact field: an author
// entry's `email` (which some summaries carry) has no struct field to land
// in (D15).
type PubMed struct {
	// BaseURL overrides the E-utilities root ("" → PubMedBaseURL). Tests
	// point it at an httptest server.
	BaseURL string
	// Client is held BY VALUE, not as *http.Client: rule 1 forbids an adapter
	// carrying a pointer or interface field, because a field that can hold a
	// writer is a field that can write. A zero Client is the default
	// transport; a Timeout of zero gets pubmedTimeout applied per request.
	Client http.Client
}

// compile-time proof that the PubMed source satisfies the adapter contract.
var _ Adapter = PubMed{}

const (
	// PubMedBaseURL is the public E-utilities root. No key, no auth.
	PubMedBaseURL = "https://eutils.ncbi.nlm.nih.gov"
	// PubMedUserAgent is the polite identifier every request carries.
	PubMedUserAgent = "manifest-aion-recruiting/phase3b"
	// PubMedArticleURL is the human-readable page a PMID resolves to; the
	// citation on every draft is this URL plus the PMID.
	PubMedArticleURL = "https://pubmed.ncbi.nlm.nih.gov/"
	// pubmedSearchPath is the PMID search endpoint under the root.
	pubmedSearchPath = "/entrez/eutils/esearch.fcgi"
	// pubmedSummaryPath is the document-summary endpoint under the root.
	pubmedSummaryPath = "/entrez/eutils/esummary.fcgi"
	// pubmedDB is the Entrez database both calls name.
	pubmedDB = "pubmed"
	// pubmedDefaultMax is requested when the scope names no cap.
	pubmedDefaultMax = 25
	// pubmedMaxResults is the most one run may ask for. The substrate caps
	// at MaxRunMax too, but the adapter never trusts that it did.
	pubmedMaxResults = 100
	// pubmedMaxBody bounds how much of a response is read.
	pubmedMaxBody = 8 << 20
	// pubmedTimeout applies when the injected client has none.
	pubmedTimeout = 30 * time.Second
	// pubmedTitleChars caps how much of a title is quoted.
	pubmedTitleChars = 300
)

func (PubMed) ID() string { return "pubmed" }

func (PubMed) Kind() Kind { return KindScholarly }

func (PubMed) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role"},
		{Key: "query", Label: "PubMed search", Placeholder: "e.g. diffusion MRI reconstruction[Title]", Required: true},
		{Key: "max", Label: "max results", Placeholder: strconv.Itoa(pubmedDefaultMax)},
	}
}

// pubmedSearchResponse is the esearch envelope. IDList is a pointer so a
// response with NO idlist key (a shape change, an error page that happened
// to be JSON) is told apart from an honest empty list.
type pubmedSearchResponse struct {
	Result *struct {
		Count  string    `json:"count"`
		IDList *[]string `json:"idlist"`
		Error  string    `json:"ERROR"`
	} `json:"esearchresult"`
	Error string `json:"error"`
}

// pubmedSummaryResponse is the esummary envelope: `result` maps each PMID
// to its summary, plus a `uids` array that is decoded separately.
type pubmedSummaryResponse struct {
	Result map[string]json.RawMessage `json:"result"`
	Error  string                     `json:"error"`
}

// pubmedAuthor is the slice of an author entry this adapter reads. There is
// deliberately no field for `email`: an unknown key is ignored by the
// decoder, so a published address structurally cannot reach a draft.
type pubmedAuthor struct {
	Name     string `json:"name"`
	AuthType string `json:"authtype"`
}

// pubmedSummary is the slice of one paper's summary this adapter reads.
// An entry for a PMID the server could not summarize carries `error`
// instead of the record.
type pubmedSummary struct {
	UID             string         `json:"uid"`
	Title           string         `json:"title"`
	Source          string         `json:"source"`
	FullJournalName string         `json:"fulljournalname"`
	PubDate         string         `json:"pubdate"`
	EPubDate        string         `json:"epubdate"`
	Authors         []pubmedAuthor `json:"authors"`
	ArticleIDs      []struct {
		IDType string `json:"idtype"`
		Value  string `json:"value"`
	} `json:"articleids"`
	Error string `json:"error"`
}

// Search runs one bounded GET esearch.fcgi?db=pubmed&term=…&retmax=… and,
// when it names any PMID, one GET esummary.fcgi?db=pubmed&id=… for at most
// the scope's Max of them. Total calls are therefore at most 2. It never
// paginates: the scope's Max is both the retmax it asks for and the most
// PMIDs it will summarize, whatever the server sent. A search that finds
// nothing returns an empty slice, not an error.
func (p PubMed) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	query := strings.TrimSpace(s.Query)
	if query == "" {
		return nil, errors.New("pubmed: a search needs a query")
	}
	max := s.Max
	if max <= 0 {
		max = pubmedDefaultMax
	}
	if max > pubmedMaxResults {
		max = pubmedMaxResults
	}

	params := url.Values{}
	params.Set("db", pubmedDB)
	params.Set("term", query)
	params.Set("retmax", strconv.Itoa(max))
	params.Set("retmode", "json")
	body, err := p.get(ctx, pubmedSearchPath, params)
	if err != nil {
		return nil, err
	}
	var search pubmedSearchResponse
	if err := json.Unmarshal(body, &search); err != nil {
		return nil, fmt.Errorf("pubmed: malformed response from %s: %v", pubmedSearchPath, err)
	}
	if search.Result == nil {
		if msg := strings.TrimSpace(search.Error); msg != "" {
			return nil, fmt.Errorf("pubmed: %s reported an error: %s", pubmedSearchPath, msg)
		}
		return nil, fmt.Errorf("pubmed: response from %s has no esearchresult field", pubmedSearchPath)
	}
	if msg := strings.TrimSpace(search.Result.Error); msg != "" {
		return nil, fmt.Errorf("pubmed: %s reported an error: %s", pubmedSearchPath, msg)
	}
	if search.Result.IDList == nil {
		return nil, fmt.Errorf("pubmed: response from %s has no idlist field", pubmedSearchPath)
	}

	ids := make([]string, 0, max)
	for _, id := range *search.Result.IDList {
		if len(ids) >= max {
			break
		}
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []CandidateDraft{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("pubmed: %v", err)
	}

	params = url.Values{}
	params.Set("db", pubmedDB)
	params.Set("id", strings.Join(ids, ","))
	params.Set("retmode", "json")
	body, err = p.get(ctx, pubmedSummaryPath, params)
	if err != nil {
		return nil, err
	}
	var summary pubmedSummaryResponse
	if err := json.Unmarshal(body, &summary); err != nil {
		return nil, fmt.Errorf("pubmed: malformed response from %s: %v", pubmedSummaryPath, err)
	}
	if summary.Result == nil {
		if msg := strings.TrimSpace(summary.Error); msg != "" {
			return nil, fmt.Errorf("pubmed: %s reported an error: %s", pubmedSummaryPath, msg)
		}
		return nil, fmt.Errorf("pubmed: response from %s has no result field", pubmedSummaryPath)
	}

	retrieved := time.Now().UTC()
	out := make([]CandidateDraft, 0, len(ids))
	byName := map[string]int{}
	// idlist order is the search's own ranking; the result map has none.
	for _, id := range ids {
		raw, ok := summary.Result[id]
		if !ok {
			continue
		}
		var paper pubmedSummary
		if err := json.Unmarshal(raw, &paper); err != nil || strings.TrimSpace(paper.Error) != "" {
			continue
		}
		if paper.UID == "" {
			paper.UID = id
		}
		author, ok := paper.firstAuthor()
		if !ok || strings.TrimSpace(paper.Title) == "" {
			continue
		}
		ev := p.evidence(paper, author, retrieved)
		if i, seen := byName[author]; seen {
			out[i].Links = append(out[i].Links, ev.URLOrFile)
			out[i].Evidence = append(out[i].Evidence, ev)
			continue
		}
		byName[author] = len(out)
		out = append(out, CandidateDraft{
			SourceID:   p.ID(),
			ExternalID: paper.UID + ":" + author,
			Name:       author,
			Role:       strings.TrimSpace(s.Role),
			Links:      []string{ev.URLOrFile},
			Evidence:   []Evidence{ev},
		})
	}
	return out, nil
}

// firstAuthor returns the first entry that names a person — authtype
// "Author", or absent — skipping collective names, editors and
// investigators. A name that looks like an address is not a name.
func (s pubmedSummary) firstAuthor() (string, bool) {
	for _, a := range s.Authors {
		name := strings.Join(strings.Fields(a.Name), " ")
		if name == "" || containsAddress(name) {
			continue
		}
		if t := strings.TrimSpace(a.AuthType); t != "" && !strings.EqualFold(t, "Author") {
			continue
		}
		return name, true
	}
	return "", false
}

// evidence builds the one row a paper contributes: the PubMed page is the
// URL, and the snippet quotes the author, title, journal, date, PMID and
// DOI exactly as the summary gave them.
func (p PubMed) evidence(s pubmedSummary, author string, retrieved time.Time) Evidence {
	title := strings.Join(strings.Fields(s.Title), " ")
	if r := []rune(title); len(r) > pubmedTitleChars {
		title = string(r[:pubmedTitleChars]) + "…"
	}
	parts := []string{"author: " + author, "title: " + title}
	journal := strings.TrimSpace(s.FullJournalName)
	if journal == "" {
		journal = strings.TrimSpace(s.Source)
	}
	if journal != "" {
		parts = append(parts, "journal: "+journal)
	}
	date := strings.TrimSpace(s.PubDate)
	if date == "" {
		date = strings.TrimSpace(s.EPubDate)
	}
	if date != "" {
		parts = append(parts, "pubdate: "+date)
	}
	parts = append(parts, "pmid: "+s.UID)
	for _, id := range s.ArticleIDs {
		if strings.EqualFold(strings.TrimSpace(id.IDType), "doi") && strings.TrimSpace(id.Value) != "" {
			parts = append(parts, "doi: "+strings.TrimSpace(id.Value))
			break
		}
	}
	return Evidence{
		SourceID: p.ID(), URLOrFile: PubMedArticleURL + url.PathEscape(s.UID) + "/", RetrievedAt: retrieved,
		Snippet: strings.Join(parts, " · "), Kind: EvidencePublication, Trust: TrustHigh,
	}
}

// Enrich is a no-op: the summary fetch already happens inside Search, where
// it is bounded by the scope, and nothing more is read in this phase.
func (PubMed) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) { return d, nil }

// GraphEdges returns what the draft already carries — nothing, in 3b.4. The
// coauthor list is read but not turned into claims; that is Phase 4.
func (PubMed) GraphEdges(_ context.Context, d CandidateDraft) ([]EdgeClaim, error) {
	return d.Edges, nil
}

// get performs one GET against the E-utilities root with the polite
// User-Agent, a bounded read, and turns any non-200 into an error that
// names the status.
func (p PubMed) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		base = PubMedBaseURL
	}
	if p.Client.Timeout == 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, pubmedTimeout)
		defer cancel()
	}
	target := base + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("pubmed: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", PubMedUserAgent)

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pubmed: GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, pubmedMaxBody))
	if err != nil {
		return nil, fmt.Errorf("pubmed: reading GET %s: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("pubmed: GET %s returned HTTP %d", path, resp.StatusCode)
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
