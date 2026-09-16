// Adapted from the existing Excalibur deterministic connector; conversion contract preserved.
package transcriptsync

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// This file is the DETERMINISTIC half of granola-sync: converting a fetched
// transcript to the vault's convention-merged note format, and computing the
// dedupe signal. None of it touches the model (plan §4: transcripts never
// stream through the brain) or the network — it is pure and unit-tested.

// granolaSegment is one raw transcript turn from the Granola API.
type granolaSegment struct {
	Text   string
	Name   string // speaker.name, may be ""
	Source string // speaker.source: "microphone" (Benjamin) | "speaker" (Other) | …
}

// granolaNote is one fetched note with its transcript detail.
type granolaNote struct {
	ID           string
	Title        string
	CreatedAt    time.Time
	Segments     []granolaSegment
	Participants []string // names from the API, if provided
}

// nameResolver resolves a candidate name against the vault index to a canonical
// display form. resolved=false means no entity was found (caller links it bare).
type nameResolver interface {
	ResolveName(name string) (display string, resolved bool)
}

// filenameStrip removes the characters the plan bans from a note filename
// ([ ] < > : " / \ | ? *) so the dated title is a legal, clean basename.
var filenameStrip = regexp.MustCompile(`[\[\]<>:"/\\|?*]`)

// noteFilename builds "YYYY-MM-DD <sanitized title>.md". A title that sanitizes
// to nothing falls back to "untitled".
func noteFilename(created time.Time, title string) string {
	clean := strings.TrimSpace(filenameStrip.ReplaceAllString(title, ""))
	clean = strings.Join(strings.Fields(clean), " ") // collapse whitespace runs
	if clean == "" {
		clean = "untitled"
	}
	return created.Format("2006-01-02") + " " + clean + ".md"
}

// mapSpeaker applies the fixed speaker mapping: an explicit name wins; else
// microphone is Benjamin and everything else is Other.
func mapSpeaker(seg granolaSegment) string {
	if n := strings.TrimSpace(seg.Name); n != "" {
		return n
	}
	if seg.Source == "microphone" {
		return "Benjamin"
	}
	return "Other"
}

// convertTranscript renders the convention-merged note: frontmatter
// (categories: [sync] as a block list, granola-id), an attendee wikilink line
// (resolved to canonical names where the index knows them, Benjamin never
// linked), then "## Transcript" with consecutive same-speaker turns merged.
func convertTranscript(n granolaNote, res nameResolver, extra []string) (content string, segments int) {
	var b strings.Builder
	b.WriteString("---\ncategories:\n  - sync\ngranola-id: ")
	b.WriteString(n.ID)
	b.WriteString("\n---\n")

	if line := attendeeLine(n, res, extra); line != "" {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n## Transcript\n\n")

	// merge consecutive segments from the same mapped speaker into one turn
	var lastSpeaker string
	var turnText []string
	flush := func() {
		if lastSpeaker == "" {
			return
		}
		text := strings.TrimSpace(strings.Join(turnText, " "))
		if text == "" {
			return
		}
		b.WriteString("**")
		b.WriteString(lastSpeaker)
		b.WriteString(":** ")
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	for _, seg := range n.Segments {
		if strings.TrimSpace(seg.Text) == "" {
			continue
		}
		segments++
		sp := mapSpeaker(seg)
		if sp != lastSpeaker {
			flush()
			lastSpeaker = sp
			turnText = turnText[:0]
		}
		turnText = append(turnText, strings.TrimSpace(seg.Text))
	}
	flush()

	return strings.TrimRight(b.String(), "\n") + "\n", segments
}

// attendeeLine gathers candidate attendee names (real speaker names + any
// Granola participants + extra title-resolved people the cast supplies),
// resolves each against the index, and returns a line of [[wikilinks]] —
// canonical form when resolved, bare name otherwise. Benjamin is never linked;
// duplicates and Others are dropped. extra names are already canonical (the
// cast resolved them from the meeting title) and are added verbatim.
func attendeeLine(n granolaNote, res nameResolver, extra []string) string {
	seen := map[string]bool{}
	var links []string
	addDisplay := func(display string) {
		display = strings.TrimSpace(display)
		if display == "" || display == "Benjamin" || display == "Other" {
			return
		}
		key := strings.ToLower(display)
		if seen[key] {
			return
		}
		seen[key] = true
		links = append(links, "[["+display+"]]")
	}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if res != nil {
			if d, ok := res.ResolveName(name); ok && strings.TrimSpace(d) != "" {
				addDisplay(d)
				return
			}
		}
		addDisplay(name)
	}
	for _, seg := range n.Segments {
		if name := strings.TrimSpace(seg.Name); name != "" {
			add(name)
		}
	}
	for _, p := range n.Participants {
		add(p)
	}
	for _, e := range extra { // title-resolved people (already canonical)
		addDisplay(e)
	}
	return strings.Join(links, " ")
}

// --- dedupe -----------------------------------------------------------------

// dupSignal is what dedupe found. Skip means don't propose at all (a definite
// duplicate already in the vault); a non-empty Reason with Skip=false means
// propose but flag it (a possible near-duplicate). Empty = clean.
type dupSignal struct {
	Skip   bool
	Reason string // e.g. "already in the vault: 2026-07-02 Aion sync.md (same filename)"
}

// fillerWords are dropped before fuzzy title comparison; domain words
// (aion/ooda/…) are deliberately KEPT so "Aion sync" and "OODA sync" don't
// collide on the shared "sync".
var fillerWords = map[string]bool{
	"sync": true, "intro": true, "call": true, "meeting": true, "catch": true,
	"up": true, "and": true, "the": true, "with": true, "for": true, "to": true,
	"a": true, "of": true, "on": true,
}

var titleWordRe = regexp.MustCompile(`[A-Za-z0-9]+`)

// significantWords lowercases a title, splits to words, and drops filler.
func significantWords(title string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range titleWordRe.FindAllString(strings.ToLower(title), -1) {
		if fillerWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// titlesOverlap reports whether two titles share ≥2 significant words.
func titlesOverlap(a, b string) bool {
	aw := significantWords(a)
	set := map[string]bool{}
	for _, w := range aw {
		set[w] = true
	}
	overlap := 0
	for _, w := range significantWords(b) {
		if set[w] {
			overlap++
		}
	}
	return overlap >= 2
}
