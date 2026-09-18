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

// PatentsView is the patent-office adapter (social graph plan D-E, phase 3,
// the FIRST company source in the build order): one bounded GET against
// the USPTO PatentsView search API, filtered to an ASSIGNEE organisation,
// each named inventor on a returned patent becoming a draft that cites the
// patent's public page. It follows the RePORTER shape — base URL and
// client injectable, every call bounded by the scope, nothing on the draft
// the returned JSON did not say outright.
//
// Why patents first for a biotech/deep-tech company: the assignee line is
// the one public, dated, primary record of who did the technical work AT
// the company, and it names them years before a paper would. Inventors
// carry PatentsView's own disambiguated inventor_id, so — unlike a trial
// official — a coinventor tie has a durable far endpoint and the
// fractional weight of D-I applies (Works: patent number, grant year,
// inventor count).
//
// PatentsView requires a free API key (search.patentsview.org). The adapter
// is REGISTERED only when PATENTSVIEW_API_KEY is set (run_defaults.go), the
// same way the local model is; a rail entry that cannot run is worse than
// none. The key rides the request header and never a draft.
type PatentsView struct {
	BaseURL string
	Key     string
	Client  http.Client
}

var (
	_ Adapter = PatentsView{}
	_ Counted = PatentsView{}
)

const (
	PatentsViewBaseURL   = "https://search.patentsview.org"
	PatentsViewUserAgent = "manifest-aion-recruiting/phase3"
	// PatentsViewKeyEnv names the environment variable holding the key.
	PatentsViewKeyEnv = "PATENTSVIEW_API_KEY"
	// PatentsViewPatentURL is the public page for one US patent.
	PatentsViewPatentURL = "https://patents.google.com/patent/US"
	pvPatentPath         = "/api/v1/patent/"
	pvDefaultMax         = 25
	// pvMaxPatents is the most patents one run reads: the newest hundred
	// assigned to a company name its current inventors.
	pvMaxPatents = 100
	pvMaxBody    = 8 << 20
	pvTimeout    = 30 * time.Second
	pvTitleChars = 300
	// pvMaxEdgeInventors is the 15-author rule for patents: a patent with
	// more inventors than this is a department, and no coinventor tie is
	// claimed from it. The drafts still land.
	pvMaxEdgeInventors     = 15
	pvCoinventorConfidence = 0.6
	// pvFieldAssignee is the scope field naming the assignee; the query is
	// the same thing, so a place row and a hand-built run spell it once.
	pvFieldAssignee = "assignee"
)

// pvFields is the `f` list asked for — only what the adapter reads.
var pvFields = []string{
	"patent_id", "patent_title", "patent_date",
	"inventors.inventor_id", "inventors.inventor_name_first", "inventors.inventor_name_last",
	"assignees.assignee_organization",
}

func (PatentsView) ID() string { return "patents" }

func (PatentsView) Kind() Kind { return KindPatent }

func (PatentsView) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role"},
		{Key: "query", Label: "assignee", Placeholder: "e.g. Hyperfine — the company the patents are assigned to", Required: true},
		{Key: "max", Label: "max people shown", Placeholder: strconv.Itoa(pvDefaultMax)},
	}
}

// Configured reports whether the adapter can run at all.
func (p PatentsView) Configured() bool { return strings.TrimSpace(p.Key) != "" }

type pvInventor struct {
	ID    string `json:"inventor_id"`
	First string `json:"inventor_name_first"`
	Last  string `json:"inventor_name_last"`
}

type pvPatent struct {
	ID        string       `json:"patent_id"`
	Title     string       `json:"patent_title"`
	Date      string       `json:"patent_date"`
	Inventors []pvInventor `json:"inventors"`
	Assignees []struct {
		Organization string `json:"assignee_organization"`
	} `json:"assignees"`
}

// pvResponse is the envelope. Patents is a pointer so a response with NO
// patents key is told apart from an honest empty list; TotalHits is the
// field.
type pvResponse struct {
	Error     bool        `json:"error"`
	Message   string      `json:"message"`
	TotalHits *int        `json:"total_hits"`
	Patents   *[]pvPatent `json:"patents"`
}

func (p PatentsView) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	out, _, err := p.SearchCounted(ctx, s)
	return out, err
}

// SearchCounted runs exactly one GET /api/v1/patent/ for the assignee,
// newest first, and folds inventors into people by inventor_id. Available
// is total_hits, Read the patents the page carried, PeopleSeen the distinct
// inventors named before the cap.
func (p PatentsView) SearchCounted(ctx context.Context, s Scope) ([]CandidateDraft, Retrieval, error) {
	ret := Retrieval{Unit: "patents"}
	if !p.Configured() {
		return nil, ret, errors.New("patents: no " + PatentsViewKeyEnv + " — PatentsView needs a (free) key")
	}
	assignee := strings.TrimSpace(s.Fields[pvFieldAssignee])
	if assignee == "" {
		assignee = strings.TrimSpace(s.Query)
	}
	if assignee == "" {
		return nil, ret, errors.New("patents: name the assignee")
	}
	max := s.Max
	if max <= 0 {
		max = pvDefaultMax
	}
	if max > pvMaxPatents {
		max = pvMaxPatents
	}
	q, _ := json.Marshal(map[string]any{"_contains": map[string]string{"assignees.assignee_organization": assignee}})
	f, _ := json.Marshal(pvFields)
	so, _ := json.Marshal([]map[string]string{{"patent_date": "desc"}})
	o, _ := json.Marshal(map[string]int{"size": pvMaxPatents})
	params := url.Values{}
	params.Set("q", string(q))
	params.Set("f", string(f))
	params.Set("s", string(so))
	params.Set("o", string(o))
	body, err := p.get(ctx, pvPatentPath, params)
	if err != nil {
		return nil, ret, err
	}
	var resp pvResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, ret, fmt.Errorf("patents: malformed response from %s: %v", pvPatentPath, err)
	}
	if resp.Error || resp.Patents == nil {
		if msg := strings.TrimSpace(resp.Message); msg != "" {
			return nil, ret, fmt.Errorf("patents: %s reported an error: %s", pvPatentPath, msg)
		}
		return nil, ret, fmt.Errorf("patents: response from %s has no patents field", pvPatentPath)
	}
	if resp.TotalHits != nil && *resp.TotalHits >= 0 {
		ret.Available = Known(*resp.TotalHits)
	}
	ret.Read = len(*resp.Patents)
	if ret.Read == 0 {
		return []CandidateDraft{}, ret, nil
	}

	retrieved := time.Now().UTC()
	out := make([]CandidateDraft, 0, max)
	byKey := map[string]int{}
	people := map[string]bool{}
	for _, pt := range *resp.Patents {
		id := strings.TrimSpace(pt.ID)
		if id == "" {
			continue
		}
		link := PatentsViewPatentURL + url.PathEscape(id)
		year := pt.year()
		var team []pvTeammate
		for _, inv := range pt.Inventors {
			if name, key, ok := inv.identity(); ok {
				team = append(team, pvTeammate{name: name, key: key})
			}
		}
		for _, inv := range pt.Inventors {
			name, key, ok := inv.identity()
			if !ok {
				continue
			}
			people[key] = true
			ev := p.evidence(pt, id, name, link, retrieved)
			edges := pvCoinventorEdges(p.ID(), team, key, name, pt, id)
			if i, seen := byKey[key]; seen {
				if !containsString(out[i].Links, link) {
					out[i].Links = append(out[i].Links, link)
				}
				out[i].Evidence = append(out[i].Evidence, ev)
				out[i].Edges = append(out[i].Edges, edges...)
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
				SourceID:   p.ID(),
				ExternalID: key,
				Name:       name,
				Org:        pt.assignee(),
				Title:      "Inventor",
				Role:       strings.TrimSpace(s.Role),
				Links:      []string{link},
				Active:     year,
				Evidence:   []Evidence{ev},
				Edges:      edges,
			})
		}
	}
	ret.PeopleSeen = len(people)
	return out, ret, nil
}

// pvTeammate is one named inventor on a patent, with the key an edge uses.
type pvTeammate struct{ name, key string }

// identity is the inventor's display name and fold key. The key is
// PatentsView's own inventor_id — the disambiguated identity the office
// maintains — and an inventor without one is not a draft, because nothing
// could point at them again.
func (i pvInventor) identity() (name, key string, ok bool) {
	name = strings.Join(strings.Fields(i.First+" "+i.Last), " ")
	key = strings.TrimSpace(i.ID)
	if name == "" || key == "" || containsAddress(name) {
		return "", "", false
	}
	return name, key, true
}

// PatentsViewExtKey is the durable graph key for an inventor.
func PatentsViewExtKey(inventorID string) string {
	id := strings.TrimSpace(inventorID)
	if id == "" {
		return ""
	}
	return ExtNodePrefix + "patentsview/" + id
}

func (pt pvPatent) year() int {
	d := strings.TrimSpace(pt.Date)
	if len(d) >= 4 {
		if y, err := strconv.Atoi(d[:4]); err == nil && y > 1800 {
			return y
		}
	}
	return 0
}

func (pt pvPatent) assignee() string {
	for _, a := range pt.Assignees {
		if org := strings.Join(strings.Fields(a.Organization), " "); org != "" {
			return org
		}
	}
	return ""
}

func (p PatentsView) evidence(pt pvPatent, id, name, link string, retrieved time.Time) Evidence {
	title := strings.Join(strings.Fields(pt.Title), " ")
	if r := []rune(title); len(r) > pvTitleChars {
		title = string(r[:pvTitleChars]) + "…"
	}
	parts := []string{"inventor: " + name, "patent: US" + id, "title: " + title}
	if org := pt.assignee(); org != "" {
		parts = append(parts, "assignee: "+org)
	}
	if d := strings.TrimSpace(pt.Date); d != "" {
		parts = append(parts, "granted: "+d)
	}
	parts = append(parts, "inventors: "+strconv.Itoa(len(pt.Inventors)))
	return Evidence{
		SourceID: p.ID(), URLOrFile: link, RetrievedAt: retrieved,
		Snippet: strings.Join(parts, " · "), Kind: EvidencePatent, Trust: TrustHigh,
	}
}

// pvCoinventorEdges names the other inventors on one patent, under the
// 15-inventor rule, each carrying the patent as the shared work.
func pvCoinventorEdges(sourceID string, team []pvTeammate, key, name string, pt pvPatent, id string) []EdgeClaim {
	if len(team) < 2 || len(team) > pvMaxEdgeInventors {
		return nil
	}
	title := strings.Join(strings.Fields(pt.Title), " ")
	var out []EdgeClaim
	for _, m := range team {
		if m.key == key {
			continue
		}
		out = append(out, EdgeClaim{
			From:       PatentsViewExtKey(m.key),
			Type:       EdgeCoinventor,
			SourceID:   sourceID,
			Basis:      m.name + " and " + name + " are both inventors on US" + id + " — " + title,
			Confidence: pvCoinventorConfidence,
			Inferred:   false,
			Evidence:   PatentsViewPatentURL + id,
			Works:      []WorkRef{{Ref: "US" + id, Year: pt.year(), Authors: len(team)}},
		})
	}
	return out
}

func (PatentsView) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) { return d, nil }

func (PatentsView) GraphEdges(_ context.Context, d CandidateDraft) ([]EdgeClaim, error) {
	return d.Edges, nil
}

func (p PatentsView) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		base = PatentsViewBaseURL
	}
	if p.Client.Timeout == 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, pvTimeout)
		defer cancel()
	}
	target := base + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("patents: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", PatentsViewUserAgent)
	req.Header.Set("X-Api-Key", strings.TrimSpace(p.Key))
	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("patents: GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, pvMaxBody))
	if err != nil {
		return nil, fmt.Errorf("patents: reading GET %s: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("patents: GET %s returned HTTP %d", path, resp.StatusCode)
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			msg += " — check " + PatentsViewKeyEnv
		}
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
