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

// ClinicalTrials is the trials-registry adapter (social graph plan D-E,
// phase 3): one bounded GET against the public ClinicalTrials.gov v2 study
// search, filtered to a SPONSOR, each named study official becoming a draft
// that cites the study's own registry page. It follows the RePORTER shape —
// base URL and client injectable, every call bounded by the scope, nothing
// on the draft the returned JSON did not say outright.
//
// Why a sponsor and not a keyword: the question this adapter answers is
// "who runs the trials this COMPANY sponsors" — the people a biotech's
// science actually goes through. Officials are the named humans on a study
// (principal investigator, study director, study chair); the registry also
// lists central contacts, which carry a phone and an email and are
// therefore NOT decoded — there is no struct field for them to land in
// (D15).
//
// A study official has no durable registry id, so this adapter claims NO
// edges: two officials on one trial would be a `same_trial` claim with a
// name for a far endpoint, and a name is not a key (the same rule that
// keeps an ORCID-less OpenAlex author out of every coauthor edge). The
// drafts still hang off the source node through member_of, which is what
// the place row counts.
type ClinicalTrials struct {
	// BaseURL overrides the API root ("" → ClinicalTrialsBaseURL). Tests
	// point it at an httptest server.
	BaseURL string
	// Client is held BY VALUE (rule 1: no pointer field can hold a writer).
	Client http.Client
}

var (
	_ Adapter = ClinicalTrials{}
	_ Counted = ClinicalTrials{}
)

const (
	// ClinicalTrialsBaseURL is the public v2 API root. No key, no auth.
	ClinicalTrialsBaseURL = "https://clinicaltrials.gov"
	// ClinicalTrialsUserAgent is the polite identifier every request carries.
	ClinicalTrialsUserAgent = "manifest-aion-recruiting/phase3"
	// ClinicalTrialsStudyURL is the human-readable study page, keyed by NCT id.
	ClinicalTrialsStudyURL = "https://clinicaltrials.gov/study/"
	ctStudiesPath          = "/api/v2/studies"
	ctDefaultMax           = 25
	// ctMaxStudies is the most studies one run reads — the registry's own
	// pageSize ceiling is 1000; a sponsor with more than this many trials
	// is a pharma, and the newest hundred name its current people.
	ctMaxStudies = 100
	ctMaxBody    = 8 << 20
	ctTimeout    = 30 * time.Second
	ctTitleChars = 300
	// ctFieldSponsor is the scope field naming the sponsor. The query is the
	// same thing — `query.spons` upstream — so a place row and a hand-built
	// run spell it once.
	ctFieldSponsor = "sponsor"
)

// ctFields is the `fields` list asked for, as dotted paths into the v2
// study record — so a response carries only what the adapter reads. The
// contacts module is asked for by its officials member ONLY; the sibling
// centralContacts (phone, email) is never requested.
var ctFields = []string{
	"protocolSection.identificationModule.nctId",
	"protocolSection.identificationModule.briefTitle",
	"protocolSection.statusModule.overallStatus",
	"protocolSection.statusModule.startDateStruct",
	"protocolSection.statusModule.completionDateStruct",
	"protocolSection.sponsorCollaboratorsModule.leadSponsor",
	"protocolSection.contactsLocationsModule.overallOfficials",
}

func (ClinicalTrials) ID() string { return "clinicaltrials" }

func (ClinicalTrials) Kind() Kind { return KindRegistry }

func (ClinicalTrials) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role"},
		{Key: "query", Label: "sponsor", Placeholder: "e.g. Hyperfine — the company or institution sponsoring the trials", Required: true},
		{Key: "max", Label: "max people shown", Placeholder: strconv.Itoa(ctDefaultMax)},
	}
}

// ctOfficial is the slice of one study official this adapter reads. There
// is deliberately no field for a phone or an email: officials carry
// neither, and the sibling list that does is never requested.
type ctOfficial struct {
	Name        string `json:"name"`
	Affiliation string `json:"affiliation"`
	Role        string `json:"role"`
}

// ctStudy is the slice of one v2 study record this adapter reads.
type ctStudy struct {
	ProtocolSection struct {
		IdentificationModule struct {
			NCTID      string `json:"nctId"`
			BriefTitle string `json:"briefTitle"`
		} `json:"identificationModule"`
		StatusModule struct {
			OverallStatus   string `json:"overallStatus"`
			StartDateStruct struct {
				Date string `json:"date"`
			} `json:"startDateStruct"`
			CompletionDateStruct struct {
				Date string `json:"date"`
			} `json:"completionDateStruct"`
		} `json:"statusModule"`
		SponsorCollaboratorsModule struct {
			LeadSponsor struct {
				Name string `json:"name"`
			} `json:"leadSponsor"`
		} `json:"sponsorCollaboratorsModule"`
		ContactsLocationsModule struct {
			OverallOfficials []ctOfficial `json:"overallOfficials"`
		} `json:"contactsLocationsModule"`
	} `json:"protocolSection"`
}

// ctResponse is the v2 envelope. Studies is a pointer so a response with
// NO studies key is told apart from an honest empty list; TotalCount is
// present only when countTotal=true was asked, and is the field.
type ctResponse struct {
	Studies    *[]ctStudy `json:"studies"`
	TotalCount *int       `json:"totalCount"`
	Message    string     `json:"message"`
}

func (c ClinicalTrials) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	out, _, err := c.SearchCounted(ctx, s)
	return out, err
}

// SearchCounted runs exactly one GET /api/v2/studies?query.spons=… sorted
// newest-first, and folds officials into people by name + affiliation. It
// never paginates. Available is totalCount, Read is the studies the page
// carried, PeopleSeen the distinct officials named on them before the cap.
func (c ClinicalTrials) SearchCounted(ctx context.Context, s Scope) ([]CandidateDraft, Retrieval, error) {
	ret := Retrieval{Unit: "studies"}
	sponsor := strings.TrimSpace(s.Fields[ctFieldSponsor])
	if sponsor == "" {
		sponsor = strings.TrimSpace(s.Query)
	}
	if sponsor == "" {
		return nil, ret, errors.New("clinicaltrials: name the sponsor")
	}
	max := s.Max
	if max <= 0 {
		max = ctDefaultMax
	}
	if max > ctMaxStudies {
		max = ctMaxStudies
	}
	params := url.Values{}
	params.Set("query.spons", sponsor)
	params.Set("fields", strings.Join(ctFields, ","))
	params.Set("sort", "StartDate:desc")
	params.Set("countTotal", "true")
	params.Set("pageSize", strconv.Itoa(ctMaxStudies))
	params.Set("format", "json")
	body, err := c.get(ctx, ctStudiesPath, params)
	if err != nil {
		return nil, ret, err
	}
	var resp ctResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, ret, fmt.Errorf("clinicaltrials: malformed response from %s: %v", ctStudiesPath, err)
	}
	if resp.Studies == nil {
		if msg := strings.TrimSpace(resp.Message); msg != "" {
			return nil, ret, fmt.Errorf("clinicaltrials: %s reported an error: %s", ctStudiesPath, msg)
		}
		return nil, ret, fmt.Errorf("clinicaltrials: response from %s has no studies field", ctStudiesPath)
	}
	if resp.TotalCount != nil && *resp.TotalCount >= 0 {
		ret.Available = Known(*resp.TotalCount)
	}
	ret.Read = len(*resp.Studies)
	if ret.Read == 0 {
		return []CandidateDraft{}, ret, nil
	}

	retrieved := time.Now().UTC()
	out := make([]CandidateDraft, 0, max)
	byKey := map[string]int{}
	people := map[string]bool{}
	for _, st := range *resp.Studies {
		nct := strings.ToUpper(strings.TrimSpace(st.ProtocolSection.IdentificationModule.NCTID))
		if nct == "" {
			continue
		}
		link := ClinicalTrialsStudyURL + url.PathEscape(nct)
		year := st.year()
		for _, o := range st.ProtocolSection.ContactsLocationsModule.OverallOfficials {
			name := strings.Join(strings.Fields(o.Name), " ")
			if name == "" || containsAddress(name) {
				continue
			}
			org := strings.Join(strings.Fields(o.Affiliation), " ")
			key := strings.ToLower(name) + "\x00" + strings.ToLower(org)
			people[key] = true
			ev := c.evidence(st, nct, name, o, link, retrieved)
			if i, seen := byKey[key]; seen {
				if !containsString(out[i].Links, link) {
					out[i].Links = append(out[i].Links, link)
				}
				out[i].Evidence = append(out[i].Evidence, ev)
				if year > out[i].Active {
					out[i].Active = year
				}
				continue
			}
			if len(out) >= max {
				continue
			}
			byKey[key] = len(out)
			out = append(out, CandidateDraft{
				SourceID:   c.ID(),
				ExternalID: nct + ":" + strings.ToLower(name),
				Name:       name,
				Org:        org,
				Title:      ctRoleTitle(o.Role),
				Role:       strings.TrimSpace(s.Role),
				Links:      []string{link},
				Active:     year,
				Evidence:   []Evidence{ev},
			})
		}
	}
	ret.PeopleSeen = len(people)
	return out, ret, nil
}

// year is the study's start year, else its completion year, else 0. A
// trial dates a person at its sponsor by when it began.
func (st ctStudy) year() int {
	for _, d := range []string{st.ProtocolSection.StatusModule.StartDateStruct.Date,
		st.ProtocolSection.StatusModule.CompletionDateStruct.Date} {
		d = strings.TrimSpace(d)
		if len(d) >= 4 {
			if y, err := strconv.Atoi(d[:4]); err == nil && y > 1900 {
				return y
			}
		}
	}
	return 0
}

// ctRoleTitle spells the registry's role enum the way a card can print it.
func ctRoleTitle(role string) string {
	switch strings.ToUpper(strings.TrimSpace(role)) {
	case "PRINCIPAL_INVESTIGATOR":
		return "Principal Investigator"
	case "STUDY_DIRECTOR":
		return "Study Director"
	case "STUDY_CHAIR":
		return "Study Chair"
	case "SUB_INVESTIGATOR":
		return "Sub-Investigator"
	}
	words := strings.Fields(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(role), "_", " ")))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// evidence is the one row a study contributes to an official's draft: the
// study page is the URL, and the snippet quotes the official, their role
// and affiliation, the title, the NCT id, the sponsor, status and start
// exactly as the registry gave them.
func (c ClinicalTrials) evidence(st ctStudy, nct, name string, o ctOfficial, link string, retrieved time.Time) Evidence {
	title := strings.Join(strings.Fields(st.ProtocolSection.IdentificationModule.BriefTitle), " ")
	if r := []rune(title); r != nil && len(r) > ctTitleChars {
		title = string(r[:ctTitleChars]) + "…"
	}
	parts := []string{"official: " + name}
	if t := ctRoleTitle(o.Role); t != "" {
		parts = append(parts, "role: "+t)
	}
	if aff := strings.Join(strings.Fields(o.Affiliation), " "); aff != "" {
		parts = append(parts, "affiliation: "+aff)
	}
	parts = append(parts, "study: "+title, "nct: "+nct)
	if sp := strings.Join(strings.Fields(st.ProtocolSection.SponsorCollaboratorsModule.LeadSponsor.Name), " "); sp != "" {
		parts = append(parts, "sponsor: "+sp)
	}
	if status := strings.TrimSpace(st.ProtocolSection.StatusModule.OverallStatus); status != "" {
		parts = append(parts, "status: "+strings.ToLower(status))
	}
	if start := strings.TrimSpace(st.ProtocolSection.StatusModule.StartDateStruct.Date); start != "" {
		parts = append(parts, "start: "+start)
	}
	return Evidence{
		SourceID: c.ID(), URLOrFile: link, RetrievedAt: retrieved,
		Snippet: strings.Join(parts, " · "), Kind: EvidenceTrial, Trust: TrustHigh,
	}
}

func (ClinicalTrials) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) {
	return d, nil
}

// GraphEdges is empty by construction: see the type comment.
func (ClinicalTrials) GraphEdges(_ context.Context, _ CandidateDraft) ([]EdgeClaim, error) {
	return nil, nil
}

func (c ClinicalTrials) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = ClinicalTrialsBaseURL
	}
	if c.Client.Timeout == 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ctTimeout)
		defer cancel()
	}
	target := base + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("clinicaltrials: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", ClinicalTrialsUserAgent)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clinicaltrials: GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, ctMaxBody))
	if err != nil {
		return nil, fmt.Errorf("clinicaltrials: reading GET %s: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("clinicaltrials: GET %s returned HTTP %d", path, resp.StatusCode)
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
