// Adapted from the existing Excalibur deterministic connector; conversion contract preserved.
package transcriptsync

import (
	"strings"
	"testing"
	"time"
)

type fakeResolver map[string]string // lower(name) → canonical display

func (f fakeResolver) ResolveName(name string) (string, bool) {
	d, ok := f[strings.ToLower(strings.TrimSpace(name))]
	return d, ok
}

func TestNoteFilename(t *testing.T) {
	at := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"Aion sync":                    "2026-07-02 Aion sync.md",
		`Weird: title/with\bad*chars?`: "2026-07-02 Weird titlewithbadchars.md",
		"   ":                          "2026-07-02 untitled.md",
	}
	for in, want := range cases {
		if got := noteFilename(at, in); got != want {
			t.Errorf("noteFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapSpeaker(t *testing.T) {
	if s := mapSpeaker(granolaSegment{Source: "microphone"}); s != "Benjamin" {
		t.Errorf("microphone → %q, want Benjamin", s)
	}
	if s := mapSpeaker(granolaSegment{Source: "speaker"}); s != "Other" {
		t.Errorf("speaker → %q, want Other", s)
	}
	if s := mapSpeaker(granolaSegment{Name: "Jane Doe", Source: "speaker"}); s != "Jane Doe" {
		t.Errorf("named → %q, want Jane Doe", s)
	}
}

func TestConvertTranscript(t *testing.T) {
	n := granolaNote{
		ID:        "not_abc",
		Title:     "Aion sync",
		CreatedAt: time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
		Segments: []granolaSegment{
			{Text: "Hi there.", Source: "microphone"},
			{Text: "How are you?", Source: "microphone"}, // merges with previous Benjamin turn
			{Text: "Good, thanks.", Name: "Jane Doe", Source: "speaker"},
			{Text: "", Source: "speaker"}, // empty dropped
			{Text: "Let's begin.", Source: "microphone"},
		},
	}
	res := fakeResolver{"jane doe": "Jane Doe"}
	content, segs := convertTranscript(n, res, nil)

	if segs != 4 { // 4 non-empty segments; the empty one is dropped, two Benjamin turns merge
		t.Errorf("segments = %d, want 4", segs)
	}
	wantParts := []string{
		"---\ncategories:\n  - sync\ngranola-id: not_abc\n---",
		"[[Jane Doe]]",
		"## Transcript",
		"**Benjamin:** Hi there. How are you?",
		"**Jane Doe:** Good, thanks.",
		"**Benjamin:** Let's begin.",
	}
	for _, p := range wantParts {
		if !strings.Contains(content, p) {
			t.Errorf("content missing %q:\n%s", p, content)
		}
	}
	// Benjamin is never an attendee link.
	if strings.Contains(strings.SplitN(content, "## Transcript", 2)[0], "[[Benjamin]]") {
		t.Error("Benjamin must not be linked as an attendee")
	}
}

func TestConvertTranscriptBareAttendee(t *testing.T) {
	n := granolaNote{
		ID: "not_x", Title: "Intro", CreatedAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		Segments: []granolaSegment{{Text: "hi", Name: "Ada Lovelace", Source: "speaker"}},
	}
	// no resolver match → bare link with the raw name
	content, _ := convertTranscript(n, fakeResolver{}, nil)
	if !strings.Contains(content, "[[Ada Lovelace]]") {
		t.Errorf("unresolved human name should still be linked bare:\n%s", content)
	}
}

func TestConvertTranscriptTitleExtras(t *testing.T) {
	// A transcript whose speakers are all "Other" (Granola gave no names) still
	// gets attendees from the title-resolved extras the cast supplies.
	n := granolaNote{
		ID: "not_a", Title: "austin", CreatedAt: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		Segments: []granolaSegment{{Text: "hey", Source: "speaker"}, {Text: "hi", Source: "microphone"}},
	}
	content, _ := convertTranscript(n, nil, []string{"Austin Tunnell"})
	if !strings.Contains(content, "[[Austin Tunnell]]") {
		t.Errorf("title-resolved attendee missing:\n%s", content)
	}
	// extras dedupe against speaker names and never link Benjamin
	if strings.Contains(strings.SplitN(content, "## Transcript", 2)[0], "[[Benjamin]]") {
		t.Error("Benjamin must not be linked")
	}
}

func TestTitlesOverlap(t *testing.T) {
	// "sync" is filler and dropped; "aion" + "roadmap" carry the match.
	if !titlesOverlap("Aion roadmap sync", "Aion roadmap catch-up") {
		t.Error("expected overlap on aion+roadmap")
	}
	// only the filler word "sync" in common → no overlap
	if titlesOverlap("Aion sync", "OODA sync") {
		t.Error("filler-only overlap must not count (aion vs ooda differ)")
	}
	// single shared significant word is not enough
	if titlesOverlap("Aion planning", "Aion") {
		t.Error("single-word overlap must not count as duplicate")
	}
}
