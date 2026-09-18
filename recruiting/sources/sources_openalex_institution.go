package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// OPENALEX INSTITUTION SWEEP (social graph plan D-E, phase 3): every work
// the registry files under ONE institution, newest first, and the people on
// them — the way to read a university lab or a company's research output
// without a website to crawl. It is the works path with a different filter:
// the same aggregation, the same author records, the same coauthor claims
// with fractional works, and the same caps. What is new is the resolution
// of the institution itself and the order (publication date, not
// relevance), so a hundred-work budget names who is publishing there NOW
// and the year on each person's newest work is what draws them current or
// former.

const (
	// openAlexFieldInstitution is the scope field: a ROR link, an OpenAlex
	// institution id, or a name. A name is resolved through the registry's
	// own institution search and the FIRST match is taken — the evidence
	// row on every draft names which, so a wrong match is visible.
	openAlexFieldInstitution = "institution"
	openAlexInstitutionSort  = "publication_date:desc"
)

var (
	openAlexInstitutionIDRe = regexp.MustCompile(`(?i)\bI\d{5,}\b`)
	openAlexRORRe           = regexp.MustCompile(`(?i)(?:ror\.org/|ror:)\s*(0[0-9a-hjkmnp-tv-z]{8})\b`)
)

// openAlexInstitutionRef is the resolved institution: the registry key
// ("I…"), its display name, and how it was resolved.
type openAlexInstitutionRef struct {
	ID, Name, Via string
}

// resolveInstitution turns what the owner named into a registry key. An
// I-id needs no fetch; a ROR is one GET; a name is one search.
func (oa OpenAlex) resolveInstitution(ctx context.Context, ref string) (openAlexInstitutionRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return openAlexInstitutionRef{}, errors.New("openalex: name an institution")
	}
	var rec struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	}
	decode := func(body []byte, path string) (openAlexInstitutionRef, error) {
		if err := json.Unmarshal(body, &rec); err != nil {
			return openAlexInstitutionRef{}, fmt.Errorf("openalex: malformed institution from %s: %v", path, err)
		}
		key := openAlexInstitution{ID: rec.ID}.key()
		if key == "" {
			return openAlexInstitutionRef{}, fmt.Errorf("openalex: %s named no institution", path)
		}
		return openAlexInstitutionRef{ID: key, Name: strings.TrimSpace(rec.DisplayName)}, nil
	}
	if m := openAlexInstitutionIDRe.FindString(ref); m != "" && (strings.Contains(strings.ToLower(ref), "openalex") || !strings.Contains(ref, " ")) {
		path := "/institutions/" + strings.ToUpper(m)
		body, err := oa.get(ctx, path, nil)
		if err != nil {
			return openAlexInstitutionRef{}, err
		}
		out, err := decode(body, path)
		out.Via = "openalex id " + strings.ToUpper(m)
		return out, err
	}
	if m := openAlexRORRe.FindStringSubmatch(ref); m != nil {
		path := "/institutions/ror:" + strings.ToLower(m[1])
		body, err := oa.get(ctx, path, nil)
		if err != nil {
			return openAlexInstitutionRef{}, err
		}
		out, err := decode(body, path)
		out.Via = "ror " + strings.ToLower(m[1])
		return out, err
	}
	params := url.Values{}
	params.Set("search", ref)
	params.Set("per-page", "1")
	params.Set("select", "id,display_name")
	body, err := oa.get(ctx, "/institutions", params)
	if err != nil {
		return openAlexInstitutionRef{}, err
	}
	var resp struct {
		Results *[]struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return openAlexInstitutionRef{}, fmt.Errorf("openalex: malformed response from /institutions: %v", err)
	}
	if resp.Results == nil || len(*resp.Results) == 0 {
		return openAlexInstitutionRef{}, fmt.Errorf("openalex: no institution matches %q", ref)
	}
	first := (*resp.Results)[0]
	key := openAlexInstitution{ID: first.ID}.key()
	if key == "" {
		return openAlexInstitutionRef{}, fmt.Errorf("openalex: the first match for %q carries no id", ref)
	}
	return openAlexInstitutionRef{ID: key, Name: strings.TrimSpace(first.DisplayName), Via: "search " + ref}, nil
}

// searchInstitution resolves the institution, then runs the works path
// under its filter, newest first. A query narrows it to matching titles
// and abstracts; the years window applies as on any works search.
func (oa OpenAlex) searchInstitution(ctx context.Context, ref string, s Scope) ([]CandidateDraft, Retrieval, error) {
	inst, err := oa.resolveInstitution(ctx, ref)
	if err != nil {
		return nil, Retrieval{Unit: "works"}, err
	}
	budget, err := openAlexWorkBudget(s)
	if err != nil {
		return nil, Retrieval{Unit: "works"}, err
	}
	plan := openAlexPlan{Mode: openAlexModeWorks, Query: strings.TrimSpace(s.Query), Budget: budget,
		Text: openAlexTextTitleAbstract, Sort: openAlexInstitutionSort}
	filters := []string{"authorships.institutions.id:" + inst.ID}
	if plan.Query != "" {
		q := strings.Join(strings.FieldsFunc(plan.Query, func(r rune) bool { return r == ',' || r == '|' }), " ")
		filters = append(filters, "title_and_abstract.search:"+strings.Join(strings.Fields(q), " "))
	}
	if years := strings.TrimSpace(s.Fields[openAlexFieldYears]); years != "" {
		m := openAlexYearsRe.FindStringSubmatch(years)
		if m == nil {
			return nil, Retrieval{Unit: "works"}, fmt.Errorf("openalex: %s %q is not a year (2020) or a window (2018-2026)", openAlexFieldYears, years)
		}
		from, to := m[1], m[2]
		if to == "" {
			to = from
		}
		filters = append(filters, "from_publication_date:"+from+"-01-01", "to_publication_date:"+to+"-12-31")
	}
	plan.Filter = strings.Join(filters, ",")
	out, ret, err := oa.searchWorks(ctx, s, plan)
	if err != nil {
		return nil, ret, err
	}
	if ret.Read == 0 && plan.Query == "" {
		// a silent zero would hide a wrong name match; say what was read
		return nil, ret, fmt.Errorf("openalex: %s (%s, resolved by %s) has no works in the registry — name the institution by its ROR link or OpenAlex id if this is the wrong one", orStr(inst.Name, inst.ID), inst.ID, inst.Via)
	}
	// every draft says which institution it was read under, and how that
	// institution was resolved — the one place a wrong name match shows
	for i := range out {
		out[i].Evidence = append(out[i].Evidence, Evidence{
			SourceID: oa.ID(), URLOrFile: openAlexInstitutionRoot + inst.ID, RetrievedAt: out[i].Evidence[0].RetrievedAt,
			Snippet: "swept under institution " + inst.Name + " (" + inst.ID + ", resolved by " + inst.Via + ") · works newest first",
			Kind:    EvidenceAffiliation, Trust: TrustMedium,
		})
	}
	return out, ret, nil
}
