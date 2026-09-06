package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var openAlexLookupAuthorID = regexp.MustCompile(`^A[0-9]+$`)

// LookupCandidate preserves ordinary name search, but anchors PubMed's
// abbreviated first authors to their paper. Initials alone are not identity.
// At most one cited paper and one author are fetched, through the shared
// bounded/retrying transport. Missing or ambiguous attribution is an empty
// result; transport/response errors remain visible as a failed lookup source.
func (oa OpenAlex) LookupCandidate(ctx context.Context, d CandidateDraft, s Scope) ([]CandidateDraft, error) {
	if d.SourceID != "pubmed" {
		return oa.Search(ctx, s)
	}
	ref := ""
	for _, ev := range d.Evidence {
		if ev.SourceID == "pubmed" && ev.Kind == EvidencePublication &&
			strings.HasPrefix(ev.URLOrFile, PubMedArticleURL) {
			ref = ev.URLOrFile
			break
		}
	}
	if ref == "" {
		return nil, nil
	}
	path, err := openAlexWorkPath(ref)
	if err != nil {
		return nil, err
	}
	body, err := oa.get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	var w openAlexWork
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, fmt.Errorf("openalex: malformed lookup work: %w", err)
	}
	if strings.TrimSpace(w.ID) == "" {
		return nil, fmt.Errorf("openalex: %s returned no work", path)
	}
	var first *openAlexAuthorsh
	for i := range w.Authorships {
		a := &w.Authorships[i]
		if a.AuthorPosition == "first" {
			if first != nil {
				return nil, nil
			}
			first = a
		}
	}
	// Compare the printed name, never an initial expansion or a fuzzy match.
	if first == nil || !strings.EqualFold(strings.Join(strings.Fields(first.RawAuthorName), " "), strings.TrimSpace(d.Name)) {
		return nil, nil
	}
	authorID := strings.TrimPrefix(strings.TrimSpace(first.Author.ID), openAlexAuthorRoot)
	if !openAlexLookupAuthorID.MatchString(authorID) {
		return nil, nil
	}
	body, err = oa.get(ctx, "/authors/"+authorID, nil)
	if err != nil {
		return nil, err
	}
	var a openAlexAuthor
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("openalex: malformed lookup author: %w", err)
	}
	if openAlexAuthorURL(a.ID) != openAlexAuthorURL(authorID) {
		return nil, fmt.Errorf("openalex: lookup author ID does not match %s", authorID)
	}
	now := time.Now().UTC()
	hit, ok := oa.draft(a, s.Role, now)
	if !ok {
		return nil, nil
	}
	hit.Evidence = append(hit.Evidence, Evidence{
		SourceID: oa.ID(), URLOrFile: w.url(), RetrievedAt: now,
		Snippet: w.citation() + " · first author: " + first.RawAuthorName + " · resolved author: " + hit.Name + " (" + openAlexAuthorURL(authorID) + ")",
		Kind:    EvidencePublication, Trust: TrustMedium,
	})
	// Keep the finder's wording; the canonical name and the identity bridge
	// remain in the evidence. All fields use Lookup's existing merge path.
	hit.Name = d.Name
	return []CandidateDraft{hit}, nil
}
