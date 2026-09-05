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

// GitHub is the Phase 3b.3 code adapter: one search against the public,
// no-key GitHub user-search endpoint, then one bounded profile fetch per hit,
// each hit becoming a draft that cites its own GitHub profile page. It
// follows the OpenAlex/ORCID shape — base URL and client injectable, every
// call bounded by the scope, nothing on the draft the returned JSON did not
// say outright.
//
// GitHub profile fields are self-asserted and unverified (anyone may type
// any company into the box), which is why its citations are TrustMedium. The
// profile endpoint carries a public email field; this adapter never decodes
// it (D15). A search hit names no collaborator, so no edge is supported by
// it — same-repo edges are Phase 4.
type GitHub struct {
	// BaseURL overrides the API root ("" → GitHubBaseURL). Tests point it at
	// an httptest server.
	BaseURL string
	// Client is held BY VALUE, not as *http.Client: rule 1 forbids an adapter
	// carrying a pointer or interface field, because a field that can hold a
	// writer is a field that can write. A zero Client is the default
	// transport; a Timeout of zero gets githubTimeout applied per request.
	Client http.Client
}

// compile-time proof that the GitHub source satisfies the adapter contract.
var _ Adapter = GitHub{}

const (
	// GitHubBaseURL is the public API root. No key, no auth.
	GitHubBaseURL = "https://api.github.com"
	// GitHubUserAgent is the polite identifier every request carries.
	GitHubUserAgent = "manifest-aion-recruiting/phase3b"
	// githubAccept is the media type GitHub documents for its REST API.
	githubAccept = "application/vnd.github+json"
	// githubSearchPath is the user-search endpoint under the API root.
	githubSearchPath = "/search/users"
	// githubUsersPath is the per-login profile endpoint under the API root.
	githubUsersPath = "/users/"
	// githubDefaultMax is requested when the scope names no cap.
	githubDefaultMax = 25
	// githubMaxPerPage is the most one run may ask for — also the endpoint's
	// own per_page ceiling. The substrate caps at MaxRunMax too, but the
	// adapter never trusts that it did.
	githubMaxPerPage = 100
	// githubMaxBody bounds how much of a response is read.
	githubMaxBody = 8 << 20
	// githubTimeout applies when the injected client has none.
	githubTimeout = 30 * time.Second
	// githubBioChars caps how much of a bio is quoted.
	githubBioChars = 240
)

func (GitHub) ID() string { return "github" }

func (GitHub) Kind() Kind { return KindCode }

func (GitHub) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role"},
		{Key: "query", Label: "GitHub user search", Placeholder: "e.g. location:boston language:python mri"},
		{Key: "repo", Label: "or one repo", Placeholder: "owner/repo, or its link"},
		{Key: "max", Label: "max results", Placeholder: strconv.Itoa(githubDefaultMax)},
	}
}

// githubUser is the slice of a search hit (and of a profile) this adapter
// reads. There is deliberately no field for `email` or `twitter_username`:
// an unknown key is ignored by the decoder, so a published address
// structurally cannot reach a draft. The profile endpoint returns a superset
// of the search hit, so one struct serves both.
type githubUser struct {
	Login       string  `json:"login"`
	ID          int64   `json:"id"`
	HTMLURL     string  `json:"html_url"`
	Type        string  `json:"type"`
	Score       float64 `json:"score"`
	Name        string  `json:"name"`
	Company     string  `json:"company"`
	Location    string  `json:"location"`
	Bio         string  `json:"bio"`
	Blog        string  `json:"blog"`
	PublicRepos *int    `json:"public_repos"`
	Followers   *int    `json:"followers"`
}

// githubSearchResponse is the envelope. Items is a pointer so a response
// with NO items key (a shape change, an error page that happened to be
// JSON) is told apart from an honest empty list.
type githubSearchResponse struct {
	TotalCount *int          `json:"total_count"`
	Items      *[]githubUser `json:"items"`
}

// Search runs one bounded GET /search/users?q=…&per_page=… and then, for
// each usable hit up to the scope's Max, one GET /users/<login> to read the
// profile fields the search omits. Total calls are therefore at most
// 1 + Max. It never paginates: the scope's Max is both the per_page it asks
// for and the most it will return, whatever the server sent.
//
// A profile fetch that fails (GitHub's unauthenticated limits are tight)
// degrades that one hit to what the search said — login, id, page, score —
// rather than failing the run: the draft is still cited, just thinner.
func (g GitHub) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	if ref := strings.TrimSpace(s.Fields["repo"]); ref != "" {
		return g.searchRepo(ctx, ref, s)
	}
	query := strings.TrimSpace(s.Query)
	if query == "" {
		return nil, errors.New("github: a search needs a query, or one repo")
	}
	max := s.Max
	if max <= 0 {
		max = githubDefaultMax
	}
	if max > githubMaxPerPage {
		max = githubMaxPerPage
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("per_page", strconv.Itoa(max))
	body, err := g.get(ctx, githubSearchPath, params)
	if err != nil {
		return nil, err
	}
	var resp githubSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("github: malformed response from %s: %v", githubSearchPath, err)
	}
	if resp.Items == nil {
		return nil, fmt.Errorf("github: response from %s has no items field", githubSearchPath)
	}

	retrieved := time.Now().UTC()
	out := make([]CandidateDraft, 0, min(len(*resp.Items), max))
	for _, hit := range *resp.Items {
		if len(out) >= max {
			break
		}
		if !hit.usable() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("github: %v", err)
		}
		u := hit
		if detail, err := g.profile(ctx, hit.Login); err == nil {
			u = detail.merge(hit)
		}
		out = append(out, g.draft(u, s.Role, retrieved))
	}
	return out, nil
}

// usable reports whether a search hit can become a draft: it must be a
// person (an Organization is not a candidate), and it must carry the login
// and page URL the citation is built from.
func (u githubUser) usable() bool {
	return strings.TrimSpace(u.Login) != "" && strings.TrimSpace(u.HTMLURL) != "" &&
		strings.EqualFold(strings.TrimSpace(u.Type), "User")
}

// merge overlays the profile on the search hit: the profile is the richer
// record, but the score and (defensively) the identity come from the hit
// the search actually returned.
func (u githubUser) merge(hit githubUser) githubUser {
	u.Score = hit.Score
	if strings.TrimSpace(u.Login) == "" {
		u.Login = hit.Login
	}
	if u.ID == 0 {
		u.ID = hit.ID
	}
	if strings.TrimSpace(u.HTMLURL) == "" {
		u.HTMLURL = hit.HTMLURL
	}
	if strings.TrimSpace(u.Type) == "" {
		u.Type = hit.Type
	}
	return u
}

// profile fetches GET /users/<login> and decodes the fields this adapter
// reads. Any failure — transport, status, shape — is returned for the
// caller to shrug off.
func (g GitHub) profile(ctx context.Context, login string) (githubUser, error) {
	body, err := g.get(ctx, githubUsersPath+url.PathEscape(login), nil)
	if err != nil {
		return githubUser{}, err
	}
	var u githubUser
	if err := json.Unmarshal(body, &u); err != nil {
		return githubUser{}, fmt.Errorf("github: malformed profile for %s: %v", login, err)
	}
	return u, nil
}

// AccountType answers rung 4 of the intake cascade: github.com/<login> is a
// person or an organisation, and only GitHub knows which. It is the one place
// in the cascade where a single cheap API call settles an ambiguity the URL
// genuinely cannot — github.com/numpy and github.com/torvalds are the same
// shape. Returns "User", "Organization", or whatever GitHub said; an error is
// the caller's cue to keep the guess it already had.
func (g GitHub) AccountType(ctx context.Context, login string) (string, error) {
	login = strings.TrimSpace(login)
	if login == "" {
		return "", fmt.Errorf("github: no login to look up")
	}
	u, err := g.profile(ctx, login)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(u.Type) == "" {
		return "", fmt.Errorf("github: %s carries no account type", login)
	}
	return strings.TrimSpace(u.Type), nil
}

// draft converts one (possibly profile-enriched) hit into a draft. Every
// draft is cited: the profile page is the URL, and the one row quotes
// exactly the fields GitHub returned.
func (g GitHub) draft(u githubUser, role string, retrieved time.Time) CandidateDraft {
	login := strings.TrimSpace(u.Login)
	pageURL := strings.TrimSpace(u.HTMLURL)
	name := strings.TrimSpace(u.Name)
	if name == "" {
		name = login
	}
	externalID := login
	if u.ID > 0 {
		externalID = strconv.FormatInt(u.ID, 10)
	}
	d := CandidateDraft{
		SourceID:   g.ID(),
		ExternalID: externalID,
		Name:       name,
		Role:       strings.TrimSpace(role),
		Links:      []string{pageURL},
		Github:     pageURL,
	}
	if c := strings.TrimSpace(u.Company); c != "" && !containsAddress(c) {
		d.Org = c
	}
	if l := strings.TrimSpace(u.Location); l != "" && !containsAddress(l) {
		d.Location = l
	}
	if blog := httpURL(u.Blog); blog != "" {
		// the profile's "blog" is whatever the person typed: a personal domain,
		// a LinkedIn, a lab page. The host decides which field it is.
		d.Links = append(d.Links, blog)
		d = ClassifyLinks(d)
	}
	// The bio is quoted only when it carries no address (D15): a profile
	// that publishes an email in its bio keeps it on GitHub, not here.
	bio := strings.Join(strings.Fields(u.Bio), " ")
	if containsAddress(bio) {
		bio = ""
	}
	if r := []rune(bio); len(r) > githubBioChars {
		bio = string(r[:githubBioChars]) + "…"
	}
	if bio != "" {
		d.Note = "bio: " + bio
	}

	// One row, always: it quotes what the search and profile said about
	// this login, in the API's own field names.
	parts := []string{"login: " + login}
	if n := strings.TrimSpace(u.Name); n != "" {
		parts = append(parts, "name: "+n)
	}
	if d.Org != "" {
		parts = append(parts, "company: "+d.Org)
	}
	if d.Location != "" {
		parts = append(parts, "location: "+d.Location)
	}
	if bio != "" {
		parts = append(parts, "bio: "+bio)
	}
	if u.PublicRepos != nil {
		parts = append(parts, "public_repos: "+strconv.Itoa(*u.PublicRepos))
	}
	if u.Followers != nil {
		parts = append(parts, "followers: "+strconv.Itoa(*u.Followers))
	}
	if u.Score != 0 {
		parts = append(parts, "score: "+strconv.FormatFloat(u.Score, 'f', -1, 64))
	}
	d.Evidence = append(d.Evidence, Evidence{
		SourceID: g.ID(), URLOrFile: pageURL, RetrievedAt: retrieved,
		Snippet: strings.Join(parts, " · "), Kind: EvidencePage, Trust: TrustMedium,
	})
	return d
}

// httpURL returns the trimmed value when it is an absolute http(s) URL with
// a host, else "". GitHub's blog field is free text — "example.com",
// "mailto:…" and prose all occur — and only a real web link is a link.
func httpURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return raw
}

// containsAddress reports whether free text carries something shaped like
// an email address: a token with text on both sides of an "@" and a dot in
// the domain part. A bare GitHub mention ("@example-institute") has no dot
// after the "@" and is not an address.
func containsAddress(s string) bool {
	for _, tok := range strings.Fields(s) {
		at := strings.Index(tok, "@")
		if at <= 0 {
			continue
		}
		domain := strings.TrimRight(tok[at+1:], ".,;:)>]")
		if i := strings.Index(domain, "."); i > 0 && i < len(domain)-1 {
			return true
		}
	}
	return false
}

// Enrich is a no-op: the profile fetch already happens inside Search, where
// it is bounded by the scope, and nothing more is read in this phase.
func (GitHub) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) { return d, nil }

// GraphEdges returns what the draft already carries — nothing, in 3b.3. A
// user search names no collaborator; same-repo edges are Phase 4.
func (GitHub) GraphEdges(_ context.Context, d CandidateDraft) ([]EdgeClaim, error) {
	return d.Edges, nil
}

// get performs one GET against the API root with the documented media type
// and the polite User-Agent, a bounded read, and turns any non-200 into an
// error that names the status.
func (g GitHub) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(g.BaseURL), "/")
	if base == "" {
		base = GitHubBaseURL
	}
	if g.Client.Timeout == 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, githubTimeout)
		defer cancel()
	}
	target := base + path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("github: %v", err)
	}
	req.Header.Set("Accept", githubAccept)
	req.Header.Set("User-Agent", GitHubUserAgent)

	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, githubMaxBody))
	if err != nil {
		return nil, fmt.Errorf("github: reading GET %s: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("github: GET %s returned HTTP %d", path, resp.StatusCode)
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
