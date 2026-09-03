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

// ORCID is the Phase 3b.2 scholarly adapter: one search against the public,
// no-key ORCID expanded-search endpoint, each hit becoming a draft that cites
// its own ORCID profile. It follows the OpenAlex shape exactly — one bounded
// GET per run, base URL and client injectable, nothing on the draft the
// returned JSON did not say outright.
//
// ORCID is a registry of researcher-asserted records, a primary source for
// the identifier and for what the researcher chose to publish about
// themselves, which is why its citations are TrustHigh. The expanded-search
// result can carry an email list; this adapter never decodes it (D15). A
// search hit names no coauthor, so no edge is supported by it.
type ORCID struct {
	// BaseURL overrides the API root ("" → ORCIDBaseURL). Tests point it at
	// an httptest server.
	BaseURL string
	// Client is held BY VALUE, not as *http.Client: rule 1 forbids an adapter
	// carrying a pointer or interface field, because a field that can hold a
	// writer is a field that can write. A zero Client is the default
	// transport; a Timeout of zero gets orcidTimeout applied per request.
	Client http.Client
}

// compile-time proof that the ORCID source satisfies the adapter contract.
var _ Adapter = ORCID{}

const (
	// ORCIDBaseURL is the public API root. No key, no auth.
	ORCIDBaseURL = "https://pub.orcid.org/v3.0"
	// ORCIDUserAgent is the polite identifier every request carries.
	ORCIDUserAgent = "manifest-aion-recruiting/phase3b"
	// orcidProfileRoot is where an ORCID iD resolves as a page.
	orcidProfileRoot = "https://orcid.org/"
	// orcidSearchPath is the expanded-search endpoint under the API root.
	// The trailing slash is what the API documents.
	orcidSearchPath = "/expanded-search/"
	// orcidDefaultMax is requested when the scope names no cap.
	orcidDefaultMax = 25
	// orcidMaxRows is the most one run may ask for. The substrate caps at
	// MaxRunMax too, but the adapter never trusts that it did.
	orcidMaxRows = 100
	// orcidMaxBody bounds how much of a response is read.
	orcidMaxBody = 8 << 20
	// orcidTimeout applies when the injected client has none.
	orcidTimeout = 30 * time.Second
	// orcidOtherNames caps how many other-names reach the note.
	orcidOtherNames = 5
)

func (ORCID) ID() string { return "orcid" }

func (ORCID) Kind() Kind { return KindScholarly }

func (ORCID) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role"},
		{Key: "query", Label: "name, keyword or affiliation", Placeholder: "e.g. diffusion MRI reconstruction", Required: true},
		{Key: "max", Label: "max results", Placeholder: strconv.Itoa(orcidDefaultMax)},
	}
}

// orcidResult is the slice of an expanded-search hit this adapter reads.
// There is deliberately no field for `email`: an unknown key is ignored by
// the decoder, so a published address structurally cannot reach a draft.
type orcidResult struct {
	ORCIDID      string   `json:"orcid-id"`
	GivenNames   string   `json:"given-names"`
	FamilyNames  string   `json:"family-names"`
	CreditName   string   `json:"credit-name"`
	OtherNames   []string `json:"other-name"`
	Institutions []string `json:"institution-name"`
}

// orcidSearchResponse is the envelope. ExpandedResult is a pointer so a
// response with NO expanded-result key (a shape change, an error page that
// happened to be JSON) is told apart from an honest empty list. ORCID sends
// `"expanded-result": null` for zero hits, which decodes to a nil pointer,
// so NumFound is consulted to tell "nothing matched" from "not the shape we
// expected".
type orcidSearchResponse struct {
	NumFound       *int           `json:"num-found"`
	ExpandedResult *[]orcidResult `json:"expanded-result"`
}

// Search runs one bounded GET /expanded-search/?q=…&rows=… and converts each
// returned hit into a cited draft. It never paginates: the scope's Max is
// both the rows it asks for and the most it will return, whatever the
// server sent.
func (o ORCID) Search(ctx context.Context, s Scope) ([]CandidateDraft, error) {
	query := strings.TrimSpace(s.Query)
	if query == "" {
		return nil, errors.New("orcid: a search needs a query")
	}
	max := s.Max
	if max <= 0 {
		max = orcidDefaultMax
	}
	if max > orcidMaxRows {
		max = orcidMaxRows
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("rows", strconv.Itoa(max))
	body, err := o.get(ctx, orcidSearchPath, params)
	if err != nil {
		return nil, err
	}
	var resp orcidSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("orcid: malformed response from %s: %v", orcidSearchPath, err)
	}
	if resp.ExpandedResult == nil {
		if resp.NumFound != nil && *resp.NumFound == 0 {
			return []CandidateDraft{}, nil
		}
		return nil, fmt.Errorf("orcid: response from %s has no expanded-result field", orcidSearchPath)
	}

	retrieved := time.Now().UTC()
	out := make([]CandidateDraft, 0, min(len(*resp.ExpandedResult), max))
	for _, r := range *resp.ExpandedResult {
		if len(out) >= max {
			break
		}
		d, ok := o.draft(r, s.Role, retrieved)
		if !ok {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// draft converts one hit into a draft. It reports false for a hit it cannot
// cite (no ORCID iD) or cannot name: a draft with no evidence is refused
// downstream, and an unnamed one is not a candidate.
func (o ORCID) draft(r orcidResult, role string, retrieved time.Time) (CandidateDraft, bool) {
	id := strings.TrimSpace(r.ORCIDID)
	name := r.name()
	if id == "" || name == "" {
		return CandidateDraft{}, false
	}
	profileURL := orcidProfileRoot + id

	insts := make([]string, 0, len(r.Institutions))
	for _, in := range r.Institutions {
		if s := strings.TrimSpace(in); s != "" {
			insts = append(insts, s)
		}
	}
	d := CandidateDraft{
		SourceID:   o.ID(),
		ExternalID: id,
		Name:       name,
		Role:       strings.TrimSpace(role),
		Links:      []string{profileURL},
	}
	if len(insts) > 0 {
		d.Org = insts[0]
	}

	others := make([]string, 0, orcidOtherNames)
	for _, n := range r.OtherNames {
		if len(others) >= orcidOtherNames {
			break
		}
		if s := strings.TrimSpace(n); s != "" {
			others = append(others, s)
		}
	}
	if len(others) > 0 {
		d.Note = "other names: " + strings.Join(others, "; ")
	}

	// One row, always: it quotes the identifier and name the registry
	// returned, and the institutions when it listed any. Kind follows what
	// the row actually attests — an affiliation when there is one, otherwise
	// just the profile page.
	parts := []string{"orcid-id: " + id, "name: " + name}
	kind := EvidencePage
	if len(insts) > 0 {
		parts = append(parts, "institution-name: "+strings.Join(insts, "; "))
		kind = EvidenceAffiliation
	}
	d.Evidence = append(d.Evidence, Evidence{
		SourceID: o.ID(), URLOrFile: profileURL, RetrievedAt: retrieved,
		Snippet: strings.Join(parts, " · "), Kind: kind, Trust: TrustHigh,
	})
	return d, true
}

// name picks the name the record shows: the credit name the researcher
// chose, else given + family. Nothing is inferred: absent both means no
// name.
func (r orcidResult) name() string {
	if s := strings.TrimSpace(r.CreditName); s != "" {
		return s
	}
	return strings.TrimSpace(strings.Join(strings.Fields(r.GivenNames+" "+r.FamilyNames), " "))
}

// Enrich is a no-op: the search hit is already everything this phase reads,
// and a per-record fetch is a later phase's, not this.
func (ORCID) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) { return d, nil }

// GraphEdges returns what the draft already carries — nothing, in 3b.2. A
// search hit names no coauthor, so no edge is supported by it.
func (ORCID) GraphEdges(_ context.Context, d CandidateDraft) ([]EdgeClaim, error) {
	return d.Edges, nil
}

// get performs one GET against the API root with the polite headers and a
// bounded read, and turns any non-200 into an error that names the status.
func (o ORCID) get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(o.BaseURL), "/")
	if base == "" {
		base = ORCIDBaseURL
	}
	if o.Client.Timeout == 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, orcidTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("orcid: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", ORCIDUserAgent)

	resp, err := o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("orcid: GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, orcidMaxBody))
	if err != nil {
		return nil, fmt.Errorf("orcid: reading GET %s: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("orcid: GET %s returned HTTP %d", path, resp.StatusCode)
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
