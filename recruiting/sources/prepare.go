package sources

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
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

// OpenAlex's check also resolves the branch (sourcing-effectiveness plan
// Phase 2): the effective mode is written back into the scope, and for a
// works search so are the effective work budget, the text field and the
// exact filter sent upstream — so a run records "mode: works · works: 200 ·
// filter: title_and_abstract.search:…" rather than absences that silently
// meant defaults, and a reader can see which branch a query took.
func (OpenAlex) PrepareScope(s Scope) (Scope, error) {
	if ref := strings.TrimSpace(s.Fields["work"]); ref != "" {
		_, err := openAlexWorkPath(ref)
		return s, err
	}
	plan, err := openAlexPlanScope(s)
	if err != nil {
		return Scope{}, err
	}
	fields := make(map[string]string, len(s.Fields)+4)
	for k, v := range s.Fields {
		fields[k] = v
	}
	fields[openAlexFieldMode] = plan.Mode
	if plan.Mode == openAlexModeWorks {
		fields[openAlexFieldWorks] = strconv.Itoa(plan.Budget)
		fields[openAlexFieldText] = plan.Text
		delete(fields, openAlexFieldFilter)
		if plan.Filter != "" {
			fields[openAlexFieldFilter] = plan.Filter
		}
	}
	s.Fields = fields
	return s, nil
}
func (GitHub) PrepareScope(s Scope) (Scope, error) {
	if ref := strings.TrimSpace(s.Fields["repo"]); ref != "" {
		_, _, err := SplitRepoRef(ref)
		return s, err
	}
	return queryScope(s, "github")
}
func (ORCID) PrepareScope(s Scope) (Scope, error)       { return queryScope(s, "orcid") }
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
