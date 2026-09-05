package sources

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// PrepareScope is the optional pure scope contract used by RunStore before
// execution and by conversational preparation. Network availability, robots
// and response validation remain execution-time checks.
func queryScope(s Scope, source string) (Scope, error) {
	if strings.TrimSpace(s.Query) == "" {
		return Scope{}, fmt.Errorf("%s: a search needs a query", source)
	}
	return s, nil
}
func (OpenAlex) PrepareScope(s Scope) (Scope, error) {
	if ref := strings.TrimSpace(s.Fields["work"]); ref != "" {
		_, err := openAlexWorkPath(ref)
		return s, err
	}
	return queryScope(s, "openalex")
}
func (GitHub) PrepareScope(s Scope) (Scope, error) {
	if ref := strings.TrimSpace(s.Fields["repo"]); ref != "" {
		_, _, err := SplitRepoRef(ref)
		return s, err
	}
	return queryScope(s, "github")
}
func (ORCID) PrepareScope(s Scope) (Scope, error)       { return queryScope(s, "orcid") }
func (PubMed) PrepareScope(s Scope) (Scope, error)      { return queryScope(s, "pubmed") }
func (NIHRePORTER) PrepareScope(s Scope) (Scope, error) { return queryScope(s, "nihreporter") }
func (m Manual) PrepareScope(s Scope) (Scope, error) {
	_, err := m.Search(context.Background(), s)
	return s, err
}
func (Feed) PrepareScope(s Scope) (Scope, error) {
	u := strings.TrimSpace(s.Fields["feed_url"])
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return s, err
	}
	if req.URL.Host == "" || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		return s, fmt.Errorf("feed: name an http(s) feed URL")
	}
	return s, nil
}
