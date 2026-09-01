package consume

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// CURATE A PASTED LINK — the second entrance to the one curate verb.
//
// The lane's own button curates something a subscription delivered.
// external.go's bridge curates something a FEED card pointed at. This file
// curates something the owner simply read somewhere and wants subscribers to
// read too: no subscription, no card, nothing on this side but an address.
//
// It produces an ExternalRef and hands it to CurateExternal. THAT IS ALL IT
// DOES, and it is the property worth protecting: the set of functions that can
// create a curated note does not grow, writeCurated stays the only vault
// write, and public.go's isolation argument needs no second proof. If this
// file ever grows a call to writeVault, something has gone wrong.

// ExternalURLID is the curated identity of a piece curated from a pasted link.
//
// The key is curateKey's normalized URL, not the raw paste: the owner curated
// the writing, not the query string, and pasting the same essay twice from two
// share sheets must refresh one note rather than write two. ExternalItemID
// keeps the ext- prefix, so a pasted link can never collide with a polled
// item's id in the snapshot cache.
func ExternalURLID(raw string) string { return ExternalItemID(urlRefID(raw)) }

func urlRefID(raw string) string {
	sum := sha256.Sum256([]byte(curateKey(raw)))
	return "url-" + hex.EncodeToString(sum[:])[:16]
}

// CurateURL curates the piece at a pasted address and publishes it.
//
// The fetch is guarded (fetchguard.go) because this is the one place a URL
// chosen at request time becomes an outbound request. Everything after the
// guard is the path a FEED card already takes.
func (s *Service) CurateURL(ctx context.Context, rawURL, note string) (CuratedEntry, error) {
	clean, err := s.guardPasted(rawURL)
	if err != nil {
		return CuratedEntry{}, err
	}
	if s.writeVault == nil {
		return CuratedEntry{}, errNoCurateCapability
	}

	ref := ExternalRef{ID: urlRefID(clean), URL: clean, Source: hostOf(clean)}

	// Already curated? Then this is a REFRESH of that note, and it has to land
	// on it. Two things carry forward: the note's item id, which is what
	// notePath compares, and its title, which is what notePath SLUGS — a
	// publisher who re-titles a post between two pastes would otherwise fork
	// the note silently, which is the one bug this entrance could ship that
	// the owner would never see.
	if prior, ok := s.CuratedFor(clean); ok {
		ref.itemID = firstNonEmpty(prior.ItemID, ExternalURLID(clean))
		ref.Title = prior.Title
		ref.Source = firstNonEmpty(prior.Source, ref.Source)
		ref.Author = prior.Author
	}

	// A PAPER is looked up, not scraped — crossref.go's rule, and the reason
	// a DOI link never reaches the metadata ladder. Resolving the record here
	// rather than leaving it to CurateExternal buys the note its title: a
	// journal URL's own page never states one we would keep, and notePath
	// slugs the title into the filename.
	if DOIFromURL(clean) != "" {
		if paper, ok := s.paperFor(ctx, clean); ok {
			ref.paper = &paper
			ref.Title = firstNonEmpty(ref.Title, paper.Title)
			ref.Source = firstNonEmpty(paper.Journal, ref.Source)
			if len(paper.Authors) > 0 {
				ref.Author = firstNonEmpty(ref.Author, strings.Join(paper.Authors, ", "))
			}
		}
		return s.CurateExternal(ctx, ref, note)
	}

	m := s.resolveLink(ctx, clean)
	ref.Title = firstNonEmpty(ref.Title, m.Title)
	ref.Source = firstNonEmpty(m.Source, ref.Source)
	ref.Author = firstNonEmpty(ref.Author, m.Author)
	// The resolved description is the FALLBACK, in the same role a feed card's
	// `why` plays: the excerpt published when the page would not give up its
	// text. It is never the body.
	ref.Fallback = m.Description
	ref.PublishedAt = m.Published
	ref.Audio, ref.AudioType, ref.AudioBytes = m.Audio, m.AudioType, m.AudioBytes
	ref.Duration, ref.Episode, ref.Season = m.Duration, m.Episode, m.Season
	ref.Image, ref.Embed = m.Image, m.Embed
	ref.body = m.body
	return s.CurateExternal(ctx, ref, note)
}

// LinkKind names what a pasted URL turned out to be, for the toast that tells
// the owner which of the five he just published. It re-walks nothing: the kind
// is read off the entry the write produced.
func LinkKind(e CuratedEntry) string {
	switch {
	case strings.TrimSpace(e.DOI) != "":
		return "paper"
	case strings.TrimSpace(e.Audio) != "":
		return linkEpisode
	case strings.TrimSpace(e.Embed) != "":
		return linkPlatform
	}
	return linkArticle
}
