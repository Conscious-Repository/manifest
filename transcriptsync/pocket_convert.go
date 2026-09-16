// Adapted from the existing Excalibur deterministic connector; conversion contract preserved.
package transcriptsync

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The deterministic half of pocket-sync (plan §2), mirroring granola_convert:
// converting a fetched Pocket recording to the vault's convention-merged note
// format. Pure — no model, no network. Differences from Granola, all
// owner-decided 2026-07-30: filenames LOWERCASE the title; the note date is
// recording_at converted UTC → America/Chicago (a 9pm call must not file
// under tomorrow); unresolved diarization labels ("SPEAKER_NN") render as
// "Speaker N+1" and flag the proposal.

// chicago is the owner's home zone for filing dates. Resolved once; a machine
// without tzdata falls back to UTC (never crashes a sync run).
var chicago = func() *time.Location {
	if loc, err := time.LoadLocation("America/Chicago"); err == nil {
		return loc
	}
	return time.UTC
}()

// pocketSpeakerRe matches Pocket's unresolved diarization labels.
var pocketSpeakerRe = regexp.MustCompile(`^SPEAKER_(\d+)$`)

// mapPocketSpeaker renders a segment's speaker: a real (labeled) name passes
// through; SPEAKER_00/01/… become Speaker 1/2/… (1-based, human-readable).
func mapPocketSpeaker(label string) (name string, unresolved bool) {
	label = strings.TrimSpace(label)
	if m := pocketSpeakerRe.FindStringSubmatch(label); m != nil {
		n := 0
		fmt.Sscanf(m[1], "%d", &n)
		return fmt.Sprintf("Speaker %d", n+1), true
	}
	if label == "" {
		return "Speaker", true
	}
	return label, false
}

// pocketNoteDate converts recording_at (UTC RFC3339) to the Chicago-local
// date the note files under.
func pocketNoteDate(recordingAt string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, recordingAt)
	if err != nil {
		return time.Time{}, err
	}
	return t.In(chicago), nil
}

// pocketNoteFilename builds "YYYY-MM-DD <lowercased sanitized title>.md" —
// granola's noteFilename plus the lowercase normalization (Pocket auto-titles
// arrive Title-Cased; the vault convention reads lowercase).
func pocketNoteFilename(local time.Time, title string) string {
	return noteFilename(local, strings.ToLower(title))
}

// convertPocketTranscript renders the convention-merged note: frontmatter
// (categories: [sync] block list — identical to granola, owner decision —
// plus pocket-id), an attendee wikilink line when any speaker resolved to a
// real name, then "## Transcript" with consecutive same-speaker turns merged.
// Returns the content, the segment count, and whether any unresolved
// SPEAKER_NN labels remain (the proposal flags it).
func convertPocketTranscript(d pocketDetail, res nameResolver, extra []string) (content string, segments int, unresolved bool) {
	var b strings.Builder
	b.WriteString("---\ncategories:\n  - sync\npocket-id: ")
	b.WriteString(d.ID)
	b.WriteString("\n---\n")

	// attendee line: labeled speaker names + title-resolved people (extra,
	// already canonical). Reuses granola's attendeeLine via a shim note.
	shim := granolaNote{}
	segs := segmentsOf(d)
	for _, s := range segs {
		if name, un := mapPocketSpeaker(s.Speaker); !un {
			shim.Segments = append(shim.Segments, granolaSegment{Name: name})
		}
	}
	if line := attendeeLine(shim, res, extra); line != "" {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n## Transcript\n\n")

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
	for _, seg := range segs {
		if strings.TrimSpace(seg.Text) == "" {
			continue
		}
		segments++
		sp, un := mapPocketSpeaker(seg.Speaker)
		if un {
			unresolved = true
		}
		if sp != lastSpeaker {
			flush()
			lastSpeaker = sp
			turnText = turnText[:0]
		}
		turnText = append(turnText, strings.TrimSpace(seg.Text))
	}
	flush()

	return strings.TrimRight(b.String(), "\n") + "\n", segments, unresolved
}

func segmentsOf(d pocketDetail) []pocketSegment {
	if d.Transcript == nil {
		return nil
	}
	return d.Transcript.Segments
}
