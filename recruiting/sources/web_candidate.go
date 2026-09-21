package sources

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

// LookupCandidate reads known profile pages, then a small same-host frontier.
// Never searches by guessed handles or attributes a roster to a single person.
func (w Web) LookupCandidate(ctx context.Context, d CandidateDraft, _ Scope) ([]CandidateDraft, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	name := candidatePageName(d.Name)
	if len(strings.Fields(name)) < 2 {
		return nil, nil
	}
	for _, part := range strings.Fields(name) {
		if len([]rune(strings.Trim(part, "."))) < 2 {
			return nil, nil
		}
	}
	frontier := append([]string{}, d.Homepage, d.Site)
	for _, raw := range d.Links {
		kind, u := ClassifyLink(raw)
		if kind == LinkHomepage || kind == LinkSite {
			frontier = append(frontier, u)
		}
	}
	// Evidence pages can be the original faculty profile even when no Links exist.
	for _, e := range d.Evidence {
		if e.Kind == EvidencePage || e.Kind == EvidenceAffiliation {
			frontier = append(frontier, e.URLOrFile)
		}
	}
	sort.Strings(frontier)
	seen := map[string]bool{}
	robots := map[string]*webRobots{}
	h := CandidateDraft{Name: d.Name, SourceID: "web"}
	var lastErr error
	attempts := 0
	for len(frontier) > 0 && attempts < 5 {
		raw := frontier[0]
		frontier = frontier[1:]
		u, err := url.Parse(raw)
		if err != nil || raw == "" || seen[raw] || webRefuse(u) != "" {
			continue
		}
		seen[raw] = true
		attempts++
		if !w.allowedByRobots(ctx, robots, u) {
			continue
		}
		page, err := w.fetch(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		// A page about this person must name them in its main heading; mere mention
		// in a directory, publication byline or footer isn't identity evidence.
		identified := false
		for _, line := range page.lines {
			if line.level == 1 && !line.chrome && strings.Contains(" "+candidatePageName(line.text)+" ", " "+name+" ") {
				identified = true
				break
			}
		}
		if !identified {
			continue
		}
		lines := []string{}
		for _, line := range page.lines {
			if line.chrome || strings.Contains(line.text, "@") || webPhoneish(line.text) {
				continue
			}
			lines = append(lines, line.text)
		}
		text := bounded(strings.Join(lines, "\n"), 6000)
		if text == "" {
			continue
		}
		h.Evidence = append(h.Evidence, Evidence{SourceID: "web", URLOrFile: page.url.String(), RetrievedAt: page.retrieved, Snippet: text, Kind: EvidencePage, Trust: TrustLow})
		h.Links = append(h.Links, page.url.String())
		for _, link := range page.links {
			if link.chrome {
				continue
			}
			// Profile destinations can be inspected manually even on blocked hosts.
			kind, target := ClassifyLink(link.url.String())
			social := false
			for _, host := range webBlockedHosts {
				social = social || strings.EqualFold(link.url.Hostname(), host) || strings.HasSuffix(strings.ToLower(link.url.Hostname()), "."+host)
			}
			if social || kind == LinkLinkedIn || kind == LinkGitHub || kind == LinkORCID {
				h.Links = append(h.Links, target)
			}
			if link.url.Host != page.url.Host {
				continue
			}
			label := strings.ToLower(link.text + " " + link.url.Path)
			for _, cue := range []string{"about", "bio", "research", "publication", "project", "blog", "education", "experience", "cv"} {
				if strings.Contains(label, cue) {
					frontier = append(frontier, link.url.String())
					break
				}
			}
		}
	}
	if len(h.Evidence) == 0 {
		return nil, lastErr
	}
	return []CandidateDraft{h}, lastErr
}

// Fold punctuation and spacing without fuzzy matching or expanding initials.
func candidatePageName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
