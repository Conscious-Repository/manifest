package recruiting

import (
	"net/http"
	"os"
	"strings"
	"time"

	"manifest/internal/labmodel"
	"manifest/recruiting/sources"
)

// RegisterDefaults is the shared application and local MCP source catalog.
func (r *RunStore) RegisterDefaults() {
	c := http.Client{Timeout: 20 * time.Second}
	for _, a := range []sources.Adapter{sources.Manual{Owner: r.store.Owner()}, sources.OpenAlex{Client: c}, sources.ORCID{Client: c}, sources.GitHub{Client: c}, sources.PubMed{Client: c}, sources.NIHRePORTER{Client: c}, sources.ClinicalTrials{Client: c}, sources.Web{Client: c}, sources.Feed{Client: c}} {
		r.Register(a)
	}
	// PatentsView needs a (free) key; a rail entry that cannot run is worse
	// than none, so it registers only when the key is present
	if key := strings.TrimSpace(os.Getenv(sources.PatentsViewKeyEnv)); key != "" {
		r.Register(sources.PatentsView{Key: key, Client: c})
	}
	if base := labmodel.BaseURL(); base != "" {
		r.Register(sources.DeepSeek{BaseURL: base, Model: labmodel.Model(), Client: c})
	}
}
