package sources

import "context"

// PREVIEW — the deterministic half of "fill me a scaffold I approve"
// (intake plan §5 stage 2; owner's answer to Q3: extractors first, a model
// only for what is left).
//
// A search returns a queue to triage. A preview answers a different, smaller
// question: the owner pasted ONE named thing — this DOI, this account, this
// repo — so say what it IS, from the source's own record, before anything is
// written. Every value comes back with the field it fills and the URL it came
// from, so the scaffold can show provenance per field instead of asking the
// owner to trust a filled form.
//
// Previewer is an OPTIONAL interface. The Adapter contract stays closed: an
// adapter that cannot resolve a single reference simply does not implement
// this, and the intake falls back to "sweep it and triage the results".
type Previewer interface {
	// Preview describes one reference. It performs at most a couple of
	// bounded GETs and, like everything in this package, cannot write.
	Preview(ctx context.Context, ref string) (PreviewFacts, error)
}

// PreviewFacts is what one reference turned out to be.
type PreviewFacts struct {
	Ref    string          `json:"ref"`
	Kind   string          `json:"kind"` // work | person | repo | media
	Name   string          `json:"name"`
	Org    string          `json:"org,omitempty"`
	Title  string          `json:"title,omitempty"`
	URL    string          `json:"url,omitempty"`
	Feed   string          `json:"feed,omitempty"`
	Links  []string        `json:"links,omitempty"`
	Note   string          `json:"note,omitempty"`
	Facts  []PreviewFact   `json:"facts,omitempty"`
	People []PreviewPerson `json:"people,omitempty"`
	Total  int             `json:"total,omitempty"` // people on the thing, before any cap
}

// PreviewFact is one filled field and where it came from. The `Source` is the
// adapter id, the `URL` the exact record — this is what lets the scaffold
// say "org, from OpenAlex" beside the value instead of just showing a value.
type PreviewFact struct {
	Field  string `json:"field"`
	Value  string `json:"value"`
	Source string `json:"source"`
	URL    string `json:"url,omitempty"`
}

// PreviewPerson is somebody the reference names — an author on the paper, a
// contributor to the repo. Key is their durable graph key ("" when the
// source published nothing to name them by later).
type PreviewPerson struct {
	Name string `json:"name"`
	Org  string `json:"org,omitempty"`
	Key  string `json:"key,omitempty"`
	URL  string `json:"url,omitempty"`
}

// fact appends a filled field with its provenance, skipping empties.
func (p *PreviewFacts) fact(field, value, source, url string) {
	if value == "" {
		return
	}
	p.Facts = append(p.Facts, PreviewFact{Field: field, Value: value, Source: source, URL: url})
}
