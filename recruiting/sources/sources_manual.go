package sources

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Manual is the Phase 1 adapter: the four D11 seed classes and the first
// candidates, typed in by Benjamin or RJ. It is the first adapter on purpose
// — it proves the contract (DTOs in, no writer, provenance attached) before
// any network adapter exists.
//
// The edges it emits are direct_known / owner_said at OwnerConfidence with
// the owner as the source. They are the highest-trust edges in the system and
// the only ones asserting a real human relationship. Nothing here reaches the
// network; the owner IS the source.
type Manual struct {
	// Owner is the person asserting the entry ("benjamin", "rj"). It becomes
	// the evidence source and the edge's `from` node when a relationship is
	// asserted.
	Owner string
}

// compile-time proof that the manual source satisfies the adapter contract.
var _ Adapter = Manual{}

func (Manual) ID() string { return "manual" }

func (Manual) Kind() Kind { return KindManual }

func (Manual) Scope() []ScopeField {
	return []ScopeField{
		{Key: "role", Label: "role", Required: true},
		{Key: "query", Label: "candidate url, name, or note", Required: true},
		{Key: "org", Label: "org"},
		// ⚠ `known` is NOT offered here, and was never usable: it is a boolean
		// the form rendered as a text box, and the field that gives it meaning
		// — knownVia, the person asserting the acquaintance — was read by
		// Search but never declared, so every run that set it failed with "a
		// known-person entry needs the network node asserting it". The working
		// path is the intake's "I know them · via …", which names the asserter
		// because an owner edge is worth exactly as much as who is making it.
	}
}

// Entry is one owner-typed line. Text is what the owner actually wrote — a
// URL, a name, or a note — and is preserved verbatim as the citation.
type Entry struct {
	Text     string
	Name     string
	Role     string
	Org      string
	Title    string
	Location string
	Owner    string
	// Known marks a person the owner says they know: it is what turns a
	// manual entry into a direct_known edge rather than a bare note.
	Known bool
	// KnownVia is the network node id asserting the relationship
	// ("aion-net/ben-anderson"). Required when Known is set.
	KnownVia string
	Now      time.Time
}

// Draft converts one owner-typed entry into a CandidateDraft carrying its own
// provenance. It is the single place the manual source's shape is decided;
// Search wraps it from a Scope.
//
// A URL in the text becomes the draft's link AND its citation. A bare name or
// note is still cited — by the owner's own words, kind owner_note — because a
// draft with no evidence is refused downstream, and "Benjamin said so" is a
// real, auditable provenance rather than an absent one.
func (m Manual) Draft(e Entry) (CandidateDraft, error) {
	text := strings.TrimSpace(e.Text)
	name := strings.TrimSpace(e.Name)
	link := ""
	if u := firstURL(text); u != "" {
		link = u
		if name == "" {
			name = nameFromURL(u)
		}
	}
	if name == "" {
		name = text
	}
	if strings.TrimSpace(name) == "" {
		return CandidateDraft{}, errors.New("a candidate needs a url, a name, or a note")
	}
	owner := strings.TrimSpace(e.Owner)
	if owner == "" {
		owner = strings.TrimSpace(m.Owner)
	}
	if owner == "" {
		owner = "owner"
	}
	now := e.Now
	if now.IsZero() {
		now = time.Now()
	}

	d := CandidateDraft{
		SourceID: m.ID(),
		Name:     strings.TrimSpace(name),
		Org:      strings.TrimSpace(e.Org),
		Title:    strings.TrimSpace(e.Title),
		Location: strings.TrimSpace(e.Location),
		Role:     strings.TrimSpace(e.Role),
		Note:     text,
	}
	if link != "" {
		d.Links = append(d.Links, link)
	}
	ev := Evidence{
		SourceID:    m.ID(),
		URLOrFile:   link,
		RetrievedAt: now.UTC(),
		Snippet:     text,
		Kind:        EvidenceOwnerNote,
		Trust:       TrustMedium,
	}
	if link != "" {
		// a page the owner pointed at is a page, and its trust is the page's
		ev.Kind = EvidencePage
	}
	if !ev.Cited() {
		// unreachable while Snippet carries the owner's words; kept because
		// the invariant is the point, not the code path
		return CandidateDraft{}, errors.New("manual: evidence needs a url or the owner's own words")
	}
	d.Evidence = append(d.Evidence, ev)

	if e.Known {
		via := strings.TrimSpace(e.KnownVia)
		if via == "" {
			return CandidateDraft{}, errors.New("a known-person entry needs the network node asserting it")
		}
		d.Edges = append(d.Edges, EdgeClaim{
			From:       via,
			Type:       EdgeDirectKnown,
			SourceID:   "owner",
			Basis:      fmt.Sprintf("%s says they know %s", owner, d.Name),
			Confidence: OwnerConfidence,
			Inferred:   false,
		})
	}
	return d, nil
}

// Search runs the adapter over a Scope. For the manual source the "search" is
// the owner's own line: exactly one draft, no network call, no pagination.
func (m Manual) Search(_ context.Context, s Scope) ([]CandidateDraft, error) {
	d, err := m.Draft(Entry{
		Text:  s.Query,
		Role:  s.Role,
		Org:   s.Fields["org"],
		Owner: m.Owner,
		Known: s.Fields["known"] == "true",
		// the asserting node is explicit — never guessed from the owner name
		KnownVia: s.Fields["knownVia"],
	})
	if err != nil {
		return nil, err
	}
	if s.Max > 0 {
		return []CandidateDraft{d}[:min(1, s.Max)], nil
	}
	return []CandidateDraft{d}, nil
}

// Enrich is a no-op for the manual source: there is nothing to look up that
// the owner did not already type, and inventing a field here would be exactly
// the enrichment D15 refuses.
func (Manual) Enrich(_ context.Context, d CandidateDraft) (CandidateDraft, error) { return d, nil }

// GraphEdges returns the edges the entry already asserted. The manual source
// never derives an edge it was not told about.
func (Manual) GraphEdges(_ context.Context, d CandidateDraft) ([]EdgeClaim, error) {
	return d.Edges, nil
}

// firstURL returns the first http(s) token in text ("" when there is none).
func firstURL(text string) string {
	for _, tok := range strings.Fields(text) {
		tok = strings.Trim(tok, "<>()[],;")
		if !strings.HasPrefix(tok, "http://") && !strings.HasPrefix(tok, "https://") {
			continue
		}
		if u, err := url.Parse(tok); err == nil && u.Host != "" {
			return tok
		}
	}
	return ""
}

// nameFromURL derives a provisional display name from a profile URL's last
// meaningful path segment. It is a PLACEHOLDER the owner renames — never an
// identity claim.
func nameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if p == "" || p == "in" || p == "profile" || p == "people" {
			continue
		}
		return strings.Join(strings.FieldsFunc(p, func(r rune) bool { return r == '-' || r == '_' }), " ")
	}
	return u.Host
}
