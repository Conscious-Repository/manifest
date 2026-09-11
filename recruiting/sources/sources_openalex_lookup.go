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

// LookupCandidate preserves ordinary name search, but anchors a PubMed
// draft to its paper: the work is fetched, and the ONE authorship whose
// printed name equals what PubMed printed for this person on that paper —
// the full name or the Medline byline, folded for case and punctuation only
// — at the same byline position, is the person. Initials alone are not
// identity and no initial is ever expanded; a paper where zero or several
// authorships match is an empty result, not a guess. At most one cited
// paper and one author are fetched, through the shared bounded/retrying
// transport. Transport/response errors remain visible as a failed lookup
// source.
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
	printed := pubmedPrintedOn(d, ref)
	if len(printed) == 0 {
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
	firstIndex := -1
	for i := range w.Authorships {
		a := &w.Authorships[i]
		if !openAlexAuthorshipPrinted(*a, printed) {
			continue
		}
		if first != nil {
			return nil, nil
		}
		first, firstIndex = a, i
	}
	if first == nil {
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
		Snippet: w.citation() + " · " + first.AuthorPosition + " author: " + first.RawAuthorName + " · resolved author: " + hit.Name + " (" + openAlexAuthorURL(authorID) + ")",
		Kind:    EvidencePublication, Trust: TrustMedium,
	})
	// The work's own byline is what makes the person's coauthors
	// nameable: the same durable-key claims a work sweep would emit, plus the
	// affiliation the paper printed for them (structured institution only).
	hit.Edges = append(hit.Edges, oa.workEdges(w, firstIndex)...)
	if org := first.org(); org != "" {
		hit.Evidence = append(hit.Evidence, Evidence{
			SourceID: oa.ID(), URLOrFile: w.url(), RetrievedAt: now,
			Snippet: "affiliation on " + w.citation() + ": " + org,
			Kind:    EvidenceAffiliation, Trust: TrustMedium,
		})
	}
	// Keep the finder's wording; the canonical name and the identity bridge
	// remain in the evidence. All fields use Lookup's existing merge path.
	hit.Name = d.Name
	return []CandidateDraft{hit}, nil
}

// openAlexAuthorshipPrinted reports whether one authorship is the person a
// PubMed draft printed on this paper: the raw author name equals, under the
// blunt fold, the printed name or the byline, AND the byline position
// agrees (a sole author is OpenAlex's "first"). A position the draft did
// not record — an older queue — constrains nothing.
func openAlexAuthorshipPrinted(a openAlexAuthorsh, printed []pubmedPrinted) bool {
	raw := foldName(a.RawAuthorName)
	if raw == "" {
		return false
	}
	for _, p := range printed {
		if raw != foldName(p.Name) && (p.Byline == "" || raw != foldName(p.Byline)) {
			continue
		}
		want := p.Position
		if want == "sole" {
			want = "first"
		}
		if want != "" && !strings.EqualFold(strings.TrimSpace(a.AuthorPosition), want) {
			continue
		}
		return true
	}
	return false
}
