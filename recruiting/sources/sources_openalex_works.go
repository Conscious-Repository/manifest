package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OPENALEX WORKS — a paper, and everyone on it (intake plan §5 stage 2).
//
// The author endpoint could never support a relationship claim: an author
// record names no coauthor, so 3b.1 emitted no edges and the whole closed
// edge vocabulary sat unused with an empty edges.md underneath it. A WORK
// names its authors outright. That is the canon for `coauthor`: not a guess,
// not a model's reading of a page — the registry's own author list, with
// ORCIDs and institutions attached, from one keyless GET.
//
// Two rules keep it honest:
//
//   - **The author cap.** A 26-author paper is normal and a 2,000-author one
//     exists; three works by one author yielded 138 distinct coauthors when
//     this was measured. Above openAlexMaxEdgeAuthors the drafts still land
//     (they are real people on a real paper) but NO coauthor edges are
//     claimed, because "co-signed a consortium paper" is not a relationship
//     and an edge that means nothing pollutes every path derived later. The
//     evidence says so in words.
//   - **Stable endpoints.** An edge names a person by a durable external key
//     (`ext/orcid/…`, else `ext/openalex/A…`), never by display name —
//     namesakes would silently merge, and the same person spelled two ways
//     would silently split. The store resolves those keys onto real record
//     ids when the person lands on the board (recruiting/edges_identity.go).
//     An author the registry gives NEITHER an ORCID nor an author id — which
//     happens, `author.id` comes back null on live records today — still
//     becomes a draft, but takes part in no edge: a node that cannot be named
//     again tomorrow is not a node.

const (
	// openAlexMaxEdgeAuthors is the most authors a work may carry before its
	// coauthorship stops being a relationship claim.
	openAlexMaxEdgeAuthors = 15
	// openAlexCoauthorConfidence is the weight of a stated coauthorship. It
	// sits well below OwnerConfidence: the registry proves they were on a
	// paper together, not that either would take the call.
	openAlexCoauthorConfidence = 0.55
	// ExtNodePrefix namespaces a person who is not (yet) a record here.
	ExtNodePrefix = "ext/"
)

// openAlexWork is the slice of a work object this adapter reads.
type openAlexWork struct {
	ID              string             `json:"id"`
	DOI             string             `json:"doi"`
	Title           string             `json:"title"`
	DisplayName     string             `json:"display_name"`
	PublicationYear int                `json:"publication_year"`
	PublicationDate string             `json:"publication_date"`
	Type            string             `json:"type"`
	PrimaryLocation openAlexLocation   `json:"primary_location"`
	Authorships     []openAlexAuthorsh `json:"authorships"`
}

type openAlexLocation struct {
	Source openAlexNamed `json:"source"`
}

// openAlexAuthorsh is one authorship row. `raw_affiliation_strings` is
// deliberately NOT read: the live API returns the department line as printed
// on the paper, and on the very first fixture pulled for this adapter that
// line ended "…, Berkeley, CA, USA. millman@berkeley.edu". A published
// address enters this system as evidence the owner promotes by hand (D15),
// never as a profile field an adapter filled — so only the STRUCTURED
// institution is read.
type openAlexAuthorsh struct {
	AuthorPosition string                `json:"author_position"`
	Author         openAlexWorkAuthor    `json:"author"`
	Institutions   []openAlexInstitution `json:"institutions"`
}

type openAlexWorkAuthor struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	ORCID       string `json:"orcid"`
}

// openAlexWorkIDRe matches an OpenAlex work id anywhere in a reference.
var openAlexWorkIDRe = regexp.MustCompile(`(?i)\b(W\d{4,})\b`)

// openAlexDOIRe matches a DOI anywhere in a reference.
var openAlexDOIRe = regexp.MustCompile(`(?i)(10\.\d{4,9}/[^\s"'<>]+)`)

// openAlexPMIDRe matches a PubMed id: a pubmed.ncbi URL, or `pmid:123`.
var openAlexPMIDRe = regexp.MustCompile(`(?i)(?:pubmed\.ncbi\.nlm\.nih\.gov/|pmid[:/ ])(\d{4,9})`)

// openAlexWorkPath turns whatever the owner pasted — a bare DOI, a doi.org
// link, an OpenAlex id or page — into the API path that resolves it.
func openAlexWorkPath(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("openalex: name a paper")
	}
	if m := openAlexDOIRe.FindStringSubmatch(ref); m != nil {
		doi := strings.TrimRight(m[1], ".,;)")
		return "/works/doi:" + doi, nil
	}
	if m := openAlexWorkIDRe.FindStringSubmatch(ref); m != nil {
		return "/works/" + strings.ToUpper(m[1]), nil
	}
	// a PubMed link or a bare pmid: OpenAlex resolves those too, and it is
	// the one that carries the full author list
	if m := openAlexPMIDRe.FindStringSubmatch(ref); m != nil {
		return "/works/pmid:" + m[1], nil
	}
	return "", fmt.Errorf("openalex: %q is not a DOI, a PubMed id or an OpenAlex work id", ref)
}

// searchWork fetches one work and returns a draft per author on it.
func (oa OpenAlex) searchWork(ctx context.Context, ref string, s Scope) ([]CandidateDraft, error) {
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
		return nil, fmt.Errorf("openalex: malformed response from %s: %v", path, err)
	}
	if strings.TrimSpace(w.ID) == "" {
		return nil, fmt.Errorf("openalex: %s returned no work", path)
	}
	max := s.Max
	if max <= 0 {
		max = openAlexDefaultMax
	}

	retrieved := time.Now().UTC()
	cite := w.citation()
	workURL := w.url()
	edgesAllowed := len(w.Authorships) <= openAlexMaxEdgeAuthors

	out := make([]CandidateDraft, 0, min(len(w.Authorships), max))
	for i, a := range w.Authorships {
		if len(out) >= max {
			break
		}
		name := strings.TrimSpace(a.Author.DisplayName)
		if name == "" {
			continue
		}
		key := ExtNodeKey(a.Author.ORCID, a.Author.ID)
		d := CandidateDraft{
			SourceID:   oa.ID(),
			ExternalID: strings.TrimPrefix(openAlexAuthorURL(a.Author.ID), openAlexAuthorRoot),
			Name:       name,
			Org:        a.org(),
			Role:       strings.TrimSpace(s.Role),
			Note:       a.position() + " on " + cite,
		}
		if u := openAlexAuthorURL(a.Author.ID); u != "" {
			d.Links = append(d.Links, u)
		}
		if orcid := openAlexORCIDURL(a.Author.ORCID); orcid != "" {
			d.Links = append(d.Links, orcid)
		}
		if d.Links == nil && workURL != "" {
			d.Links = append(d.Links, workURL)
		}

		snippet := cite + " · " + a.position()
		if org := a.org(); org != "" {
			snippet += " · " + org
		}
		snippet += " · " + strconv.Itoa(len(w.Authorships)) + " authors"
		if !edgesAllowed {
			snippet += " (too many to call coauthorship a relationship — no edges claimed)"
		}
		d.Evidence = append(d.Evidence, Evidence{
			SourceID: oa.ID(), URLOrFile: orStr(workURL, openAlexAuthorURL(a.Author.ID)),
			RetrievedAt: retrieved, Snippet: snippet,
			Kind: EvidencePublication, Trust: TrustMedium,
		})
		if a.org() != "" {
			d.Evidence = append(d.Evidence, Evidence{
				SourceID: oa.ID(), URLOrFile: orStr(workURL, openAlexAuthorURL(a.Author.ID)),
				RetrievedAt: retrieved,
				Snippet:     "affiliation on " + cite + ": " + a.org(),
				Kind:        EvidenceAffiliation, Trust: TrustMedium,
			})
		}

		if edgesAllowed {
			for j, other := range w.Authorships {
				if i == j {
					continue
				}
				otherName := strings.TrimSpace(other.Author.DisplayName)
				otherKey := ExtNodeKey(other.Author.ORCID, other.Author.ID)
				if otherName == "" || otherKey == "" || otherKey == key {
					continue
				}
				d.Edges = append(d.Edges, EdgeClaim{
					From:       otherKey,
					Type:       EdgeCoauthor,
					SourceID:   oa.ID(),
					Basis:      otherName + " and " + name + " are both authors on " + cite,
					Confidence: openAlexCoauthorConfidence,
					Inferred:   false,
				})
			}
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("openalex: %s names no authors", cite)
	}
	return out, nil
}

// ExtNodeKey is the durable graph key for a person who is not a record here:
// their ORCID when the source published one, else their OpenAlex author id.
// "" when the source gave neither — an edge endpoint that cannot be named
// again later is not worth writing.
func ExtNodeKey(orcid, openAlexID string) string {
	if id := orcidID(orcid); id != "" {
		return ExtNodePrefix + "orcid/" + id
	}
	if id := strings.TrimPrefix(openAlexAuthorURL(openAlexID), openAlexAuthorRoot); id != "" {
		return ExtNodePrefix + "openalex/" + id
	}
	return ""
}

// orcidID strips the URL an ORCID is usually published as.
func orcidID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

// citation is the one-line bibliographic string every evidence row on this
// work repeats verbatim.
func (w openAlexWork) citation() string {
	parts := []string{orStr(strings.TrimSpace(w.Title), strings.TrimSpace(w.DisplayName))}
	if src := strings.TrimSpace(w.PrimaryLocation.Source.DisplayName); src != "" {
		parts = append(parts, src)
	}
	if w.PublicationYear > 0 {
		parts = append(parts, strconv.Itoa(w.PublicationYear))
	}
	if doi := w.doi(); doi != "" {
		parts = append(parts, doi)
	}
	return strings.Join(parts, ", ")
}

func (w openAlexWork) doi() string {
	doi := strings.TrimSpace(w.DOI)
	doi = strings.TrimPrefix(doi, "https://doi.org/")
	doi = strings.TrimPrefix(doi, "http://doi.org/")
	return doi
}

// url is where the work cites from: its DOI when it has one (the canonical
// form), else its OpenAlex page.
func (w openAlexWork) url() string {
	if doi := w.doi(); doi != "" {
		return "https://doi.org/" + doi
	}
	return strings.TrimSpace(w.ID)
}

// org is the institution this authorship names, if any — structured only.
func (a openAlexAuthorsh) org() string {
	for _, i := range a.Institutions {
		if n := strings.TrimSpace(i.DisplayName); n != "" {
			return n
		}
	}
	return ""
}

func (a openAlexAuthorsh) position() string {
	switch strings.TrimSpace(a.AuthorPosition) {
	case "first":
		return "first author"
	case "last":
		return "last author"
	case "middle":
		return "co-author"
	}
	return "author"
}

func orStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// Preview resolves ONE reference — a paper, or a person by ORCID / OpenAlex
// id — into the facts a scaffold shows before anything is written.
func (oa OpenAlex) Preview(ctx context.Context, ref string) (PreviewFacts, error) {
	out := PreviewFacts{Ref: strings.TrimSpace(ref)}
	if path, err := openAlexWorkPath(ref); err == nil {
		body, err := oa.get(ctx, path, nil)
		if err != nil {
			return out, err
		}
		var w openAlexWork
		if err := json.Unmarshal(body, &w); err != nil {
			return out, fmt.Errorf("openalex: malformed response from %s: %v", path, err)
		}
		if strings.TrimSpace(w.ID) == "" {
			return out, fmt.Errorf("openalex: %s returned no work", path)
		}
		out.Kind = "work"
		out.Name = orStr(strings.TrimSpace(w.Title), strings.TrimSpace(w.DisplayName))
		out.Org = strings.TrimSpace(w.PrimaryLocation.Source.DisplayName)
		out.URL = w.url()
		out.Note = w.citation()
		out.Total = len(w.Authorships)
		out.fact("name", out.Name, oa.ID(), out.URL)
		out.fact("published in", out.Org, oa.ID(), out.URL)
		if w.PublicationYear > 0 {
			out.fact("year", strconv.Itoa(w.PublicationYear), oa.ID(), out.URL)
		}
		out.fact("doi", w.doi(), oa.ID(), out.URL)
		for _, a := range w.Authorships {
			name := strings.TrimSpace(a.Author.DisplayName)
			if name == "" {
				continue
			}
			out.People = append(out.People, PreviewPerson{
				Name: name, Org: a.org(), Key: ExtNodeKey(a.Author.ORCID, a.Author.ID),
				URL: orStr(openAlexORCIDURL(a.Author.ORCID), openAlexAuthorURL(a.Author.ID)),
			})
		}
		return out, nil
	}

	path, err := openAlexAuthorPath(ref)
	if err != nil {
		return out, err
	}
	body, err := oa.get(ctx, path, nil)
	if err != nil {
		return out, err
	}
	var a openAlexAuthor
	if err := json.Unmarshal(body, &a); err != nil {
		return out, fmt.Errorf("openalex: malformed response from %s: %v", path, err)
	}
	if strings.TrimSpace(a.DisplayName) == "" {
		return out, fmt.Errorf("openalex: %s returned no author", path)
	}
	inst := a.institution()
	out.Kind = "person"
	out.Name = strings.TrimSpace(a.DisplayName)
	out.Org = strings.TrimSpace(inst.DisplayName)
	out.URL = openAlexAuthorURL(a.ID)
	if orcid := openAlexORCIDURL(a.ORCID); orcid != "" {
		out.Links = append(out.Links, orcid)
	}
	if out.URL != "" {
		out.Links = append(out.Links, out.URL)
	}
	out.fact("name", out.Name, oa.ID(), out.URL)
	out.fact("org", out.Org, oa.ID(), out.URL)
	out.fact("location", strings.TrimSpace(inst.CountryCode), oa.ID(), out.URL)
	out.fact("works", strconv.Itoa(a.WorksCount), oa.ID(), out.URL)
	return out, nil
}

// openAlexAuthorPath resolves a person reference: an ORCID iD (with or
// without its URL) or an OpenAlex author id.
func openAlexAuthorPath(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if m := regexp.MustCompile(`\b(\d{4}-\d{4}-\d{4}-\d{3}[\dX])\b`).FindStringSubmatch(ref); m != nil {
		return "/authors/orcid:" + m[1], nil
	}
	if m := regexp.MustCompile(`(?i)\b(A\d{4,})\b`).FindStringSubmatch(ref); m != nil {
		return "/authors/" + strings.ToUpper(m[1]), nil
	}
	return "", fmt.Errorf("openalex: %q is not a paper, an ORCID iD or an author id", ref)
}
