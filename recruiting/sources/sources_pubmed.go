package sources

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PubMed is the scholarly adapter over NCBI E-utilities: one bounded esearch
// for PMIDs in PubMed's own relevance order, then batched XML efetch for a
// PAPER BUDGET of them, every person on every paper becoming a mention and
// the mentions aggregating into people (people_agg.go). It follows the
// OpenAlex/ORCID/GitHub shape — base URL and client injectable, every call
// bounded by the scope, nothing on a draft the record did not say outright.
//
// Before the sourcing-effectiveness plan's Phase 1 (2026-09-11) this adapter
// read one esummary page and emitted one draft per paper: the FIRST author,
// as the abbreviated byline ("Reyes DM"), with no affiliation, no ORCID and
// no position. A field-cycling probe found 32 papers and 147 distinct
// author strings where that produced 31 drafts. Now:
//
//   - The PAPER BUDGET (scope field "papers", default pubmedDefaultPapers,
//     ceiling pubmedMaxPapers) is what esearch asks for and what efetch
//     decodes. It is separate from Scope.Max, the PEOPLE/display cap: Read
//     counts papers decoded, PeopleSeen counts distinct people across ALL of
//     them, and only then are the drafts cut to Max.
//   - The full XML record gives the printed name (LastName + ForeName),
//     the Medline byline (LastName + Initials), ORCID when the author gave
//     one, affiliations as printed, the byline position, MeSH descriptors,
//     DOI/PMCID, journal and date. Collective names ("Radiology Consortium")
//     are on the byline and are recorded as such; they are not people and
//     become no draft.
//   - Identity is people_agg.go's: ORCID, else full name + first
//     affiliation's organization token, else a source-local key marked
//     ambiguous. One evidence row per paper per person, one affiliation row
//     per printed affiliation, each tied to its PMID.
//
// Rule 3 / D15: the XML is decoded into structs with no field for an email
// (an unknown element is skipped), and every affiliation string passes
// stripAddresses before it reaches a snippet, an Org or a key — PubMed
// affiliations routinely end "Electronic address: someone@somewhere.edu".
// The record is NLM's primary bibliographic record, so publication rows are
// TrustHigh; an affiliation is what the author printed on one paper on one
// date, so those rows are TrustMedium.
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

// compile-time proof that the PubMed source satisfies the adapter contract,
// and reports the size of its field (Phase 0).
var (
	_ Adapter = PubMed{}
	_ Counted = PubMed{}
)

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
	// pubmedFetchPath is the full-record endpoint under the root.
	pubmedFetchPath = "/entrez/eutils/efetch.fcgi"
	// pubmedDB is the Entrez database both calls name.
	pubmedDB = "pubmed"
	// pubmedDefaultMax is the people/display cap when the scope names none.
	pubmedDefaultMax = 25
	// pubmedMaxResults is the most people one run may show. The substrate
	// caps at MaxRunMax too, but the adapter never trusts that it did.
	pubmedMaxResults = 100
	// pubmedFieldPapers is the scope field naming the paper budget.
	pubmedFieldPapers = "papers"
	// pubmedDefaultPapers is the paper budget when the scope names none:
	// enough to see past the first page of a 30–250-paper field, small
	// enough that a run is two fetches.
	pubmedDefaultPapers = 100
	// pubmedMaxPapers is the most papers one run may read, however large the
	// budget asked for. At pubmedFetchBatch per request that is ten fetches.
	pubmedMaxPapers = 500
	// pubmedFetchBatch is how many PMIDs one efetch names. Full records with
	// references run 10–100 KB each; fifty keeps a response well under the
	// body bound and the URL well under any proxy's limit.
	pubmedFetchBatch = 50
	// pubmedMaxBody bounds how much of an esearch response is read.
	pubmedMaxBody = 8 << 20
	// pubmedFetchMaxBody bounds how much of an efetch response is read.
	pubmedFetchMaxBody = 16 << 20
	// pubmedTimeout applies per request when the injected client has none.
	pubmedTimeout = 30 * time.Second
	// pubmedTitleChars caps how much of a title is quoted.
	pubmedTitleChars = 300
	// pubmedMeshTerms caps how many MeSH descriptors a publication row
	// quotes — major topics first; the count of the rest is stated.
	pubmedMeshTerms = 10
	// pubmedAffiliationsPerMention caps the affiliation rows one mention
	// contributes: a paper listing six for one author is rare and the first
	// is the one that keys identity.
	pubmedAffiliationsPerMention = 3
)

func (PubMed) ID() string { return "pubmed" }

func (PubMed) Kind() Kind { return KindScholarly }

func (PubMed) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role"},
		{Key: "query", Label: "PubMed search", Placeholder: "e.g. diffusion MRI reconstruction[Title]", Required: true},
		{Key: "max", Label: "max people shown", Placeholder: strconv.Itoa(pubmedDefaultMax)},
		{Key: pubmedFieldPapers, Label: "papers to read", Placeholder: strconv.Itoa(pubmedDefaultPapers) + " — the paper budget, at most " + strconv.Itoa(pubmedMaxPapers) + "; separate from people shown"},
	}
}

// PrepareScope is the pure scope check: a query is required, and the paper
// budget is parsed and bounded here so the run records the EFFECTIVE budget
// — "papers: 100" on a run that named none — rather than an absent field
// that silently meant the default.
func (PubMed) PrepareScope(s Scope) (Scope, error) {
	s, err := queryScope(s, "pubmed")
	if err != nil {
		return Scope{}, err
	}
	budget, err := pubmedPaperBudget(s)
	if err != nil {
		return Scope{}, err
	}
	fields := make(map[string]string, len(s.Fields)+1)
	for k, v := range s.Fields {
		fields[k] = v
	}
	fields[pubmedFieldPapers] = strconv.Itoa(budget)
	s.Fields = fields
	return s, nil
}

// pubmedPaperBudget reads the paper budget from the scope: absent means the
// default, non-numeric is refused, and the result is clamped to
// [1, pubmedMaxPapers]. It never grows to cover Scope.Max: the two are
// separate numbers on purpose.
func pubmedPaperBudget(s Scope) (int, error) {
	raw := strings.TrimSpace(s.Fields[pubmedFieldPapers])
	if raw == "" {
		return pubmedDefaultPapers, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("pubmed: %s %q is not a number", pubmedFieldPapers, raw)
	}
	if n < 1 {
		n = pubmedDefaultPapers
	}
	if n > pubmedMaxPapers {
		n = pubmedMaxPapers
	}
	return n, nil
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

// ---- efetch XML ----

// pubmedArticleSet is the slice of a PubmedArticleSet this adapter reads.
// The root name is not pinned so that NCBI's `<eFetchResult><ERROR>` reply,
// which arrives with HTTP 200, decodes into Error rather than into nothing.
// Book articles (PubmedBookArticle) are not read.
type pubmedArticleSet struct {
	XMLName  xml.Name
	Error    string          `xml:"ERROR"`
	Articles []pubmedArticle `xml:"PubmedArticle"`
}

type pubmedArticle struct {
	Citation struct {
		PMID    string `xml:"PMID"`
		Article struct {
			Journal struct {
				Title           string `xml:"Title"`
				ISOAbbreviation string `xml:"ISOAbbreviation"`
				Issue           struct {
					PubDate pubmedDate `xml:"PubDate"`
				} `xml:"JournalIssue"`
			} `xml:"Journal"`
			Title       pubmedMarkup      `xml:"ArticleTitle"`
			Authors     []pubmedXMLAuthor `xml:"AuthorList>Author"`
			ELocations  []pubmedTypedID   `xml:"ELocationID"`
			ArticleDate []pubmedDate      `xml:"ArticleDate"`
		} `xml:"Article"`
		Mesh []struct {
			Descriptor struct {
				Major string `xml:"MajorTopicYN,attr"`
				Name  string `xml:",chardata"`
			} `xml:"DescriptorName"`
		} `xml:"MeshHeadingList>MeshHeading"`
	} `xml:"MedlineCitation"`
	IDs []pubmedTypedID `xml:"PubmedData>ArticleIdList>ArticleId"`
}

// pubmedXMLAuthor is the slice of an Author element this adapter reads.
// There is deliberately no field for an address: PubMed prints one inside
// Affiliation text, never as an element, and that text is stripped.
type pubmedXMLAuthor struct {
	LastName       string `xml:"LastName"`
	ForeName       string `xml:"ForeName"`
	Initials       string `xml:"Initials"`
	Suffix         string `xml:"Suffix"`
	CollectiveName string `xml:"CollectiveName"`
	Identifiers    []struct {
		Source string `xml:"Source,attr"`
		Value  string `xml:",chardata"`
	} `xml:"Identifier"`
	Affiliations []pubmedMarkup `xml:"AffiliationInfo>Affiliation"`
}

// pubmedTypedID is an id element with a type attribute — ArticleId's
// IdType, ELocationID's EIdType — read under either name.
type pubmedTypedID struct {
	IDType  string `xml:"IdType,attr"`
	EIDType string `xml:"EIdType,attr"`
	Value   string `xml:",chardata"`
}

func (t pubmedTypedID) kind() string {
	if t.IDType != "" {
		return strings.ToLower(strings.TrimSpace(t.IDType))
	}
	return strings.ToLower(strings.TrimSpace(t.EIDType))
}

// pubmedDate is PubDate/ArticleDate: parts, or a MedlineDate range.
type pubmedDate struct {
	Year        string `xml:"Year"`
	Month       string `xml:"Month"`
	Day         string `xml:"Day"`
	MedlineDate string `xml:"MedlineDate"`
}

func (d pubmedDate) String() string {
	parts := []string{}
	for _, p := range []string{d.Year, d.Month, d.Day} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}
	return strings.TrimSpace(d.MedlineDate)
}

// pubmedMarkup is an element whose text may carry inline markup — titles
// print <i>, <sub> and <sup>, affiliations occasionally do — read raw and
// flattened to its text, so no word inside a tag is lost. The tags are
// inline, so they are removed without adding a space: "<i>B</i><sub>0</sub>"
// stays "B0".
type pubmedMarkup struct {
	Inner string `xml:",innerxml"`
}

var pubmedTagRe = regexp.MustCompile(`<[^>]*>`)

func (m pubmedMarkup) text() string {
	return strings.Join(strings.Fields(html.UnescapeString(pubmedTagRe.ReplaceAllString(m.Inner, ""))), " ")
}

// pubmedORCIDRe finds the 16-character ORCID inside however the author's
// Identifier printed it: bare, with a URL, with or without dashes.
var pubmedORCIDRe = regexp.MustCompile(`(\d{4})-?(\d{4})-?(\d{4})-?(\d{3}[0-9Xx])`)

// pubmedORCID returns the bare dashed id, or "" when the text is not one.
func pubmedORCID(raw string) string {
	m := pubmedORCIDRe.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	return m[1] + "-" + m[2] + "-" + m[3] + "-" + strings.ToUpper(m[4])
}

// pubmedPaper is one decoded record: the bibliographic facts every mention
// on it will cite, and its people.
type pubmedPaper struct {
	PMID, Title, Journal, PubDate, DOI, PMCID string
	Mesh                                      []string
	Collectives                               []string
	Authors                                   []authorMention
}

// parsePubMedArticleSet decodes one efetch body into papers, in document
// order. Rows this adapter cannot use (no PMID) are dropped; a paper with no
// people or no title is kept — it was read — and simply has no mentions.
func parsePubMedArticleSet(body []byte) ([]pubmedPaper, error) {
	var set pubmedArticleSet
	if err := xml.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("pubmed: malformed response from %s: %v", pubmedFetchPath, err)
	}
	if msg := strings.TrimSpace(set.Error); msg != "" {
		return nil, fmt.Errorf("pubmed: %s reported an error: %s", pubmedFetchPath, msg)
	}
	if set.XMLName.Local != "PubmedArticleSet" {
		return nil, fmt.Errorf("pubmed: response from %s is not a PubmedArticleSet", pubmedFetchPath)
	}
	out := make([]pubmedPaper, 0, len(set.Articles))
	for _, a := range set.Articles {
		pmid := strings.TrimSpace(a.Citation.PMID)
		if pmid == "" {
			continue
		}
		art := a.Citation.Article
		p := pubmedPaper{PMID: pmid, Title: art.Title.text()}
		p.Journal = strings.TrimSpace(art.Journal.Title)
		if p.Journal == "" {
			p.Journal = strings.TrimSpace(art.Journal.ISOAbbreviation)
		}
		p.PubDate = art.Journal.Issue.PubDate.String()
		for _, d := range art.ArticleDate {
			if p.PubDate != "" {
				break
			}
			p.PubDate = d.String()
		}
		for _, id := range append(append([]pubmedTypedID{}, a.IDs...), art.ELocations...) {
			v := strings.TrimSpace(id.Value)
			switch id.kind() {
			case "doi":
				if p.DOI == "" && v != "" {
					p.DOI = v
				}
			case "pmc":
				if p.PMCID == "" && v != "" {
					p.PMCID = v
				}
			}
		}
		var major, minor []string
		for _, h := range a.Citation.Mesh {
			name := strings.Join(strings.Fields(h.Descriptor.Name), " ")
			if name == "" {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(h.Descriptor.Major), "Y") {
				major = append(major, name+"*")
			} else {
				minor = append(minor, name)
			}
		}
		p.Mesh = append(major, minor...)

		total := len(art.Authors)
		for i, au := range art.Authors {
			if c := strings.Join(strings.Fields(au.CollectiveName), " "); c != "" {
				if !containsAddress(c) {
					p.Collectives = append(p.Collectives, c)
				}
				continue
			}
			last := strings.Join(strings.Fields(au.LastName), " ")
			fore := strings.Join(strings.Fields(au.ForeName), " ")
			initials := strings.Join(strings.Fields(au.Initials), " ")
			if last == "" || containsAddress(last) || containsAddress(fore) {
				continue
			}
			if initials == "" {
				for _, seg := range strings.Fields(fore) {
					initials += string([]rune(seg)[:1])
				}
			}
			byline := strings.TrimSpace(last + " " + strings.ToUpper(initials))
			m := authorMention{Byline: byline, Position: i + 1, Total: total, FullName: forenameIsFull(fore, initials)}
			if m.FullName {
				m.Name = strings.TrimSpace(fore + " " + last)
				if suffix := strings.Join(strings.Fields(au.Suffix), " "); suffix != "" {
					m.Name += " " + suffix
				}
			} else {
				m.Name = byline
			}
			for _, id := range au.Identifiers {
				if strings.EqualFold(strings.TrimSpace(id.Source), "ORCID") {
					if orcid := pubmedORCID(id.Value); orcid != "" {
						m.ORCID = orcid
						break
					}
				}
			}
			for _, af := range au.Affiliations {
				if len(m.Affiliations) >= pubmedAffiliationsPerMention {
					break
				}
				if text := stripAddresses(af.text()); text != "" && !containsAddress(text) {
					m.Affiliations = append(m.Affiliations, text)
				}
			}
			p.Authors = append(p.Authors, m)
		}
		out = append(out, p)
	}
	return out, nil
}

// ---- search ----

// Search runs one bounded esearch for up to the paper budget of PMIDs and
// then efetches them in batches of pubmedFetchBatch, so a run makes at most
// 1 + ⌈budget / batch⌉ logical fetches, each with at most 4 attempts. It
// never paginates esearch: the budget is the retmax it asks for and the
// most PMIDs it will fetch, whatever the server sent. A search that finds
// nothing returns an empty slice, not an error.
func (p PubMed) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	out, _, err := p.SearchCounted(ctx, s)
	return out, err
}

// SearchCounted is Search plus the size of the field (Phase 0): Available is
// esearchresult.count — every paper PubMed holds for the query, however few
// were fetched — Read is the papers whose record decoded, and PeopleSeen is
// the distinct people those records named BEFORE the display cap. Drafts
// beyond Scope.Max are cut here; the counts say what the cut hid.
func (p PubMed) SearchCounted(ctx context.Context, s Scope) ([]CandidateDraft, Retrieval, error) {
	ret := Retrieval{Unit: "papers"}
	query := strings.TrimSpace(s.Query)
	if query == "" {
		return nil, ret, errors.New("pubmed: a search needs a query")
	}
	max := s.Max
	if max <= 0 {
		max = pubmedDefaultMax
	}
	if max > pubmedMaxResults {
		max = pubmedMaxResults
	}
	budget, err := pubmedPaperBudget(s)
	if err != nil {
		return nil, ret, err
	}

	params := url.Values{}
	params.Set("db", pubmedDB)
	params.Set("term", query)
	params.Set("retmax", strconv.Itoa(budget))
	params.Set("retmode", "json")
	body, err := p.get(ctx, pubmedSearchPath, params)
	if err != nil {
		return nil, ret, err
	}
	var search pubmedSearchResponse
	if err := json.Unmarshal(body, &search); err != nil {
		return nil, ret, fmt.Errorf("pubmed: malformed response from %s: %v", pubmedSearchPath, err)
	}
	if search.Result == nil {
		if msg := strings.TrimSpace(search.Error); msg != "" {
			return nil, ret, fmt.Errorf("pubmed: %s reported an error: %s", pubmedSearchPath, msg)
		}
		return nil, ret, fmt.Errorf("pubmed: response from %s has no esearchresult field", pubmedSearchPath)
	}
	if msg := strings.TrimSpace(search.Result.Error); msg != "" {
		return nil, ret, fmt.Errorf("pubmed: %s reported an error: %s", pubmedSearchPath, msg)
	}
	if search.Result.IDList == nil {
		return nil, ret, fmt.Errorf("pubmed: response from %s has no idlist field", pubmedSearchPath)
	}
	// the count is a decimal string in the envelope; anything else is "the
	// source did not say", never zero
	if n, err := strconv.Atoi(strings.TrimSpace(search.Result.Count)); err == nil && n >= 0 {
		ret.Available = Known(n)
	}

	ids := make([]string, 0, budget)
	seenID := map[string]bool{}
	for _, id := range *search.Result.IDList {
		if len(ids) >= budget {
			break
		}
		if id = strings.TrimSpace(id); id != "" && !seenID[id] {
			seenID[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return []CandidateDraft{}, ret, nil
	}

	// batched efetch; the whole run fails rather than returning a partial
	// field as if it were the field
	byPMID := map[string]pubmedPaper{}
	for start := 0; start < len(ids); start += pubmedFetchBatch {
		if err := ctx.Err(); err != nil {
			return nil, ret, fmt.Errorf("pubmed: %v", err)
		}
		end := min(start+pubmedFetchBatch, len(ids))
		params = url.Values{}
		params.Set("db", pubmedDB)
		params.Set("id", strings.Join(ids[start:end], ","))
		params.Set("retmode", "xml")
		body, err := p.fetch(ctx, pubmedFetchPath, params, pubmedFetchMaxBody)
		if err != nil {
			return nil, ret, err
		}
		papers, err := parsePubMedArticleSet(body)
		if err != nil {
			return nil, ret, err
		}
		for _, paper := range papers {
			if _, dup := byPMID[paper.PMID]; !dup {
				byPMID[paper.PMID] = paper
			}
		}
	}

	// idlist order is the search's own ranking; the XML's is whatever the
	// server chose
	papers := make([]pubmedPaper, 0, len(ids))
	var mentions []authorMention
	for _, id := range ids {
		paper, ok := byPMID[id]
		if !ok {
			continue
		}
		ret.Read++ // a record that decoded is a paper read, usable author or not
		if paper.Title == "" {
			continue
		}
		at := len(papers)
		papers = append(papers, paper)
		for _, m := range paper.Authors {
			m.Paper = at
			mentions = append(mentions, m)
		}
	}

	retrieved := time.Now().UTC()
	people := aggregatePeople(mentions)
	ret.PeopleSeen = len(people)
	out := make([]CandidateDraft, 0, min(len(people), max))
	for _, person := range people {
		if len(out) >= max {
			break
		}
		out = append(out, p.draft(person, papers, s.Role, retrieved))
	}
	return out, ret, nil
}

// draft turns one aggregated person into a draft: the durable key as
// ExternalID, the printed name, the organization the first mention named,
// the ORCID as a link, and one publication row per paper plus one
// affiliation row per printed affiliation — every row citing its PMID's
// page. An ambiguous key says so in the note, in words.
func (p PubMed) draft(person person, papers []pubmedPaper, role string, retrieved time.Time) CandidateDraft {
	d := CandidateDraft{
		SourceID:   p.ID(),
		ExternalID: person.Key.String(),
		Name:       person.Name,
		Org:        person.Org,
		Role:       strings.TrimSpace(role),
	}
	if person.Key.Ambiguous {
		d.Note = "identity: ambiguous — " + person.Key.Reason
	}
	seenPaper := map[int]bool{}
	for _, m := range person.Mentions {
		if m.Paper < 0 || m.Paper >= len(papers) || seenPaper[m.Paper] {
			continue
		}
		seenPaper[m.Paper] = true
		paper := papers[m.Paper]
		pageURL := PubMedArticleURL + url.PathEscape(paper.PMID) + "/"
		d.Links = append(d.Links, pageURL)
		d.Evidence = append(d.Evidence, Evidence{
			SourceID: p.ID(), URLOrFile: pageURL, RetrievedAt: retrieved,
			Snippet: p.publicationSnippet(paper, m), Kind: EvidencePublication, Trust: TrustHigh,
		})
		for _, af := range m.Affiliations {
			d.Evidence = append(d.Evidence, Evidence{
				SourceID: p.ID(), URLOrFile: pageURL, RetrievedAt: retrieved,
				Snippet: "affiliation on pmid " + paper.PMID + " (" + m.positionLabel() + " author): " + af,
				Kind:    EvidenceAffiliation, Trust: TrustMedium,
			})
		}
	}
	if person.ORCID != "" {
		d.Orcid = "https://orcid.org/" + person.ORCID
		d.Links = append(d.Links, d.Orcid)
	}
	return d
}

// publicationSnippet quotes what the record said about this person on this
// paper, as labelled parts the UI can split: author, byline, position,
// title, journal, pubdate, pmid, doi, pmcid, mesh, collective. Every value
// is the record's own text.
func (p PubMed) publicationSnippet(paper pubmedPaper, m authorMention) string {
	title := paper.Title
	if r := []rune(title); len(r) > pubmedTitleChars {
		title = string(r[:pubmedTitleChars]) + "…"
	}
	parts := []string{"author: " + m.Name}
	if m.Byline != "" && m.Byline != m.Name {
		parts = append(parts, "byline: "+m.Byline)
	}
	parts = append(parts, "position: "+m.positionLabel()+" of "+strconv.Itoa(m.Total), "title: "+title)
	if paper.Journal != "" {
		parts = append(parts, "journal: "+paper.Journal)
	}
	if paper.PubDate != "" {
		parts = append(parts, "pubdate: "+paper.PubDate)
	}
	parts = append(parts, "pmid: "+paper.PMID)
	if paper.DOI != "" {
		parts = append(parts, "doi: "+paper.DOI)
	}
	if paper.PMCID != "" {
		parts = append(parts, "pmcid: "+paper.PMCID)
	}
	if len(paper.Mesh) > 0 {
		mesh := paper.Mesh
		more := ""
		if len(mesh) > pubmedMeshTerms {
			more = " (+" + strconv.Itoa(len(mesh)-pubmedMeshTerms) + " more)"
			mesh = mesh[:pubmedMeshTerms]
		}
		parts = append(parts, "mesh: "+strings.Join(mesh, "; ")+more)
	}
	if len(paper.Collectives) > 0 {
		parts = append(parts, "collective: "+strings.Join(paper.Collectives, "; "))
	}
	return strings.Join(parts, " · ")
}

// pubmedPrinted is what one publication row says this draft was called on
// one paper, and where it sat — read back from the row's own labelled
// snippet by the same package that wrote it.
type pubmedPrinted struct {
	Name, Byline, Position string
}

// pubmedPrintedOn returns the printed forms a PubMed draft carries for one
// paper page URL ("" for every paper). A lookup anchors on these — the
// exact byline the paper printed — never on an initial expansion.
func pubmedPrintedOn(d CandidateDraft, pageURL string) []pubmedPrinted {
	var out []pubmedPrinted
	for _, ev := range d.Evidence {
		if ev.SourceID != "pubmed" || ev.Kind != EvidencePublication || (pageURL != "" && ev.URLOrFile != pageURL) {
			continue
		}
		pr := pubmedPrinted{Name: strings.TrimSpace(d.Name)}
		for _, part := range strings.Split(ev.Snippet, " · ") {
			switch {
			case strings.HasPrefix(part, "author: "):
				pr.Name = strings.TrimSpace(strings.TrimPrefix(part, "author: "))
			case strings.HasPrefix(part, "byline: "):
				pr.Byline = strings.TrimSpace(strings.TrimPrefix(part, "byline: "))
			case strings.HasPrefix(part, "position: "):
				pr.Position = strings.TrimSpace(strings.Fields(strings.TrimPrefix(part, "position: "))[0])
			}
		}
		out = append(out, pr)
	}
	return out
}

// Enrich is a no-op: the record fetch already happens inside Search, where
// it is bounded by the scope, and nothing more is read in this phase.
func (PubMed) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) { return d, nil }

// GraphEdges returns what the draft already carries — nothing. The byline
// is read but not turned into claims: a coauthor edge needs a durable key
// on both ends, and that is the OpenAlex work path's job.
func (PubMed) GraphEdges(_ context.Context, d CandidateDraft) ([]EdgeClaim, error) {
	return d.Edges, nil
}

// get is the JSON-bounded fetch every esearch goes through; fetch is the
// same with the XML bound and Accept. Both preserve the polite headers and
// the body bound, retrying transient statuses before returning an error
// that names the final status.
func (p PubMed) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	return p.fetch(ctx, path, params, pubmedMaxBody)
}

func (p PubMed) fetch(ctx context.Context, path string, params url.Values, maxBody int64) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		base = PubMedBaseURL
	}
	timeout := p.Client.Timeout
	if timeout == 0 {
		timeout = pubmedTimeout
	}
	// Bound the entire fetch, including Retry-After waits.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	target := base + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("pubmed: %v", err)
	}
	if params.Get("retmode") == "xml" {
		req.Header.Set("Accept", "application/xml, text/xml")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("User-Agent", PubMedUserAgent)

	return scholarlyGet(p.Client, req, "pubmed", path, maxBody)
}
