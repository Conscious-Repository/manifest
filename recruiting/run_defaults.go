package recruiting

import (
	"manifest/recruiting/sources"
	"net/http"
	"time"
)

// RegisterDefaults is the shared application and local MCP source catalog.
func (r *RunStore) RegisterDefaults() {
	c := http.Client{Timeout: 20 * time.Second}
	for _, a := range []sources.Adapter{sources.Manual{Owner: r.store.Owner()}, sources.OpenAlex{Client: c}, sources.ORCID{Client: c}, sources.GitHub{Client: c}, sources.PubMed{Client: c}, sources.NIHRePORTER{Client: c}, sources.Web{Client: c}, sources.Feed{Client: c}} {
		r.Register(a)
	}
}
