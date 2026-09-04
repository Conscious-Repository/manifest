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

// GITHUB REPO — who actually built the thing (intake plan §5 stage 2).
//
// The user search answers "who says they do X"; a repo's contributor list
// answers "who wrote X", which is the stronger signal and the one a paste of
// `github.com/org/project` is asking for. Same discipline as the works path:
// contributors are named by a durable key (their GitHub login), the far
// endpoint is left empty until accept, and a project with a crowd of drive-by
// contributors claims no relationship at all.
//
// Unauthenticated GitHub is rate-limited to 60 requests an hour per IP, so
// this path spends at most 1 + max requests and degrades a failed profile
// fetch to what the contributors list already said.

const (
	githubContribPath = "/repos/"
	// githubMaxEdgeContributors caps how many contributors make a repo a
	// working relationship. Above it, the drafts still land; no edges do.
	githubMaxEdgeContributors = 12
	// githubSameRepoConfidence — shipping code together is a working
	// relationship, and a weaker claim than the owner's own word.
	githubSameRepoConfidence = 0.6
	// githubMinContributions is the floor for being a contributor at all: a
	// single typo fix in a README is not collaboration.
	githubMinContributions = 2
)

// githubContributor is one row of /repos/{owner}/{repo}/contributors.
type githubContributor struct {
	Login         string `json:"login"`
	ID            int64  `json:"id"`
	HTMLURL       string `json:"html_url"`
	Type          string `json:"type"`
	Contributions int    `json:"contributions"`
}

// SplitRepoRef turns what the owner pasted — `owner/repo`, a GitHub URL, or
// the API form — into its two segments.
func SplitRepoRef(ref string) (owner, repo string, err error) {
	s := strings.TrimSpace(ref)
	if s == "" {
		return "", "", errors.New("github: name a repo")
	}
	if i := strings.Index(strings.ToLower(s), "github.com/"); i >= 0 {
		s = s[i+len("github.com/"):]
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "repos/"), "/")
	s = strings.TrimSuffix(strings.TrimSuffix(s, ".git"), "/")
	parts := strings.Split(s, "/")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("github: %q is not an owner/repo reference", ref)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

// searchRepo returns a draft per contributor to one repository.
func (g GitHub) searchRepo(ctx context.Context, ref string, s Scope) ([]CandidateDraft, error) {
	owner, repo, err := SplitRepoRef(ref)
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
	params.Set("per_page", strconv.Itoa(max))
	path := githubContribPath + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/contributors"
	body, err := g.get(ctx, path, params)
	if err != nil {
		return nil, err
	}
	var contribs []githubContributor
	if err := json.Unmarshal(body, &contribs); err != nil {
		return nil, fmt.Errorf("github: malformed response from %s: %v", path, err)
	}

	slug := owner + "/" + repo
	repoURL := "https://github.com/" + slug
	retrieved := time.Now().UTC()

	// people first, bots never: a repo's top "contributor" is often
	// dependabot, and an account that is not a person is not a candidate
	var people []githubContributor
	for _, c := range contribs {
		if strings.EqualFold(c.Type, "Bot") || strings.HasSuffix(strings.ToLower(c.Login), "[bot]") {
			continue
		}
		if strings.TrimSpace(c.Login) == "" || c.Contributions < githubMinContributions {
			continue
		}
		people = append(people, c)
	}
	if len(people) == 0 {
		return nil, fmt.Errorf("github: %s names no human contributors", slug)
	}
	edgesAllowed := len(people) <= githubMaxEdgeContributors

	out := make([]CandidateDraft, 0, min(len(people), max))
	for i, c := range people {
		if len(out) >= max {
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("github: %v", err)
		}
		u := githubUser{Login: c.Login, ID: c.ID, HTMLURL: c.HTMLURL, Type: c.Type}
		if detail, derr := g.profile(ctx, c.Login); derr == nil {
			u = detail.merge(u)
		}
		d := g.draft(u, s.Role, retrieved)
		snippet := "contributor to " + slug + " · " + strconv.Itoa(c.Contributions) + " commits · " +
			strconv.Itoa(len(people)) + " contributors"
		if !edgesAllowed {
			snippet += " (too many to call it collaboration — no edges claimed)"
		}
		d.Evidence = append(d.Evidence, Evidence{
			SourceID: g.ID(), URLOrFile: repoURL, RetrievedAt: retrieved,
			Snippet: snippet, Kind: EvidenceRepo, Trust: TrustHigh,
		})
		if !containsString(d.Links, repoURL) {
			d.Links = append(d.Links, repoURL)
		}
		if edgesAllowed {
			for j, other := range people {
				if i == j {
					continue
				}
				d.Edges = append(d.Edges, EdgeClaim{
					From:       ExtNodePrefix + "github/" + strings.ToLower(other.Login),
					Type:       EdgeSameRepo,
					SourceID:   g.ID(),
					Basis:      other.Login + " and " + c.Login + " both contribute to " + slug,
					Confidence: githubSameRepoConfidence,
					Inferred:   false,
				})
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// Preview resolves ONE reference — `owner/repo`, or a bare login — into the
// facts the intake scaffold shows before anything is written.
func (g GitHub) Preview(ctx context.Context, ref string) (PreviewFacts, error) {
	out := PreviewFacts{Ref: strings.TrimSpace(ref)}
	if owner, repo, err := SplitRepoRef(ref); err == nil {
		slug := owner + "/" + repo
		repoURL := "https://github.com/" + slug
		params := url.Values{}
		params.Set("per_page", strconv.Itoa(githubDefaultMax))
		path := githubContribPath + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/contributors"
		body, err := g.get(ctx, path, params)
		if err != nil {
			return out, err
		}
		var contribs []githubContributor
		if err := json.Unmarshal(body, &contribs); err != nil {
			return out, fmt.Errorf("github: malformed response from %s: %v", path, err)
		}
		out.Kind = "repo"
		out.Name = slug
		out.Org = owner
		out.URL = repoURL
		out.Links = []string{repoURL}
		out.fact("name", slug, g.ID(), repoURL)
		for _, c := range contribs {
			if strings.EqualFold(c.Type, "Bot") || strings.HasSuffix(strings.ToLower(c.Login), "[bot]") ||
				c.Contributions < githubMinContributions {
				continue
			}
			out.Total++
			out.People = append(out.People, PreviewPerson{
				Name: c.Login, Key: ExtNodePrefix + "github/" + strings.ToLower(c.Login),
				URL: "https://github.com/" + c.Login,
			})
		}
		out.fact("contributors", strconv.Itoa(out.Total), g.ID(), repoURL)
		return out, nil
	}

	login := strings.TrimSpace(ref)
	if i := strings.Index(strings.ToLower(login), "github.com/"); i >= 0 {
		login = strings.Trim(login[i+len("github.com/"):], "/")
	}
	if login == "" || strings.Contains(login, "/") {
		return out, fmt.Errorf("github: %q is not an account or a repo", ref)
	}
	u, err := g.profile(ctx, login)
	if err != nil {
		return out, err
	}
	page := strings.TrimSpace(u.HTMLURL)
	if page == "" {
		page = "https://github.com/" + login
	}
	out.Kind = "person"
	out.Name = orStr(strings.TrimSpace(u.Name), login)
	out.Org = strings.TrimSpace(u.Company)
	out.URL = page
	out.Links = []string{page}
	if blog := strings.TrimSpace(u.Blog); blog != "" && strings.HasPrefix(blog, "http") {
		out.Links = append(out.Links, blog)
		out.fact("website", blog, g.ID(), page)
	}
	out.fact("name", out.Name, g.ID(), page)
	out.fact("org", out.Org, g.ID(), page)
	out.fact("location", strings.TrimSpace(u.Location), g.ID(), page)
	out.fact("github", page, g.ID(), page)
	return out, nil
}
