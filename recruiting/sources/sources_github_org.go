package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GITHUB ORGANISATION — who has made their membership public (social graph
// plan D-E, phase 3, the fourth company source). An org's public member
// list is the one thing GitHub states about employment outright; it is
// short (people opt in), current by construction (a member who leaves is
// removed), and the same rate budget as a repo: 1 + max requests.
//
// Members of one org are claimed `same_company` under the repo cap: a
// dozen public members is a team, a hundred is a directory.

const (
	githubFieldOrg   = "org"
	githubOrgPath    = "/orgs/"
	githubOrgMembers = "/public_members"
)

// SplitOrgRef turns `org`, `github.com/org`, or `@org` into the login.
func SplitOrgRef(ref string) (string, error) {
	s := strings.TrimSpace(ref)
	if i := strings.Index(strings.ToLower(s), "github.com/"); i >= 0 {
		s = s[i+len("github.com/"):]
	}
	s = strings.Trim(strings.TrimPrefix(s, "@"), "/")
	if s == "" || strings.ContainsAny(s, "/ ") {
		return "", fmt.Errorf("github: %q is not an organisation login", ref)
	}
	return s, nil
}

// searchOrg returns a draft per public member of one organisation.
func (g GitHub) searchOrg(ctx context.Context, ref string, s Scope) ([]CandidateDraft, error) {
	org, err := SplitOrgRef(ref)
	if err != nil {
		return nil, err
	}
	max := s.Max
	if max <= 0 {
		max = githubDefaultMax
	}
	if max > githubMaxPerPage {
		max = githubMaxPerPage
	}
	params := url.Values{}
	params.Set("per_page", strconv.Itoa(githubMaxPerPage))
	path := githubOrgPath + url.PathEscape(org) + githubOrgMembers
	body, err := g.get(ctx, path, params)
	if err != nil {
		return nil, err
	}
	var members []githubUser
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, fmt.Errorf("github: malformed response from %s: %v", path, err)
	}
	var people []githubUser
	for _, m := range members {
		if strings.TrimSpace(m.Login) == "" || strings.EqualFold(m.Type, "Bot") || strings.HasSuffix(strings.ToLower(m.Login), "[bot]") {
			continue
		}
		people = append(people, m)
	}
	if len(people) == 0 {
		return nil, errors.New("github: " + org + " lists no public members")
	}
	orgURL := "https://github.com/" + org
	retrieved := time.Now().UTC()
	edgesAllowed := len(people) <= githubMaxEdgeContributors

	out := make([]CandidateDraft, 0, min(len(people), max))
	for i, m := range people {
		if len(out) >= max {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("github: %v", err)
		}
		u := m
		if strings.TrimSpace(u.HTMLURL) == "" {
			u.HTMLURL = "https://github.com/" + m.Login
		}
		if strings.TrimSpace(u.Type) == "" {
			u.Type = "User"
		}
		if detail, derr := g.profile(ctx, m.Login); derr == nil {
			u = detail.merge(u)
		}
		d := g.draft(u, s.Role, retrieved)
		if strings.TrimSpace(d.Org) == "" {
			d.Org = org
		}
		snippet := "public member of " + org + " · " + strconv.Itoa(len(people)) + " public members"
		if !edgesAllowed {
			snippet += " (too many to call it one team — no edges claimed)"
		}
		d.Evidence = append(d.Evidence, Evidence{
			SourceID: g.ID(), URLOrFile: orgURL, RetrievedAt: retrieved,
			Snippet: snippet, Kind: EvidenceAffiliation, Trust: TrustHigh,
		})
		if !containsString(d.Links, orgURL) {
			d.Links = append(d.Links, orgURL)
		}
		if edgesAllowed {
			for j, other := range people {
				if i == j {
					continue
				}
				d.Edges = append(d.Edges, EdgeClaim{
					From:       ExtNodePrefix + "github/" + strings.ToLower(other.Login),
					Type:       EdgeSameCompany,
					SourceID:   g.ID(),
					Basis:      other.Login + " and " + m.Login + " are both public members of " + org,
					Confidence: githubSameRepoConfidence,
					Inferred:   false,
					Evidence:   orgURL,
				})
			}
		}
		out = append(out, d)
	}
	return out, nil
}
