// Adapted from the existing Excalibur deterministic connector; conversion contract preserved.
package transcriptsync

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// loadPocketFixture parses the synthetic detail response using the observed legacy wire shape.
func loadPocketFixture(t *testing.T, name string) pocketDetail {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Data pocketDetail `json:"data"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data
}

func TestPocketFixtureShape(t *testing.T) {
	d := loadPocketFixture(t, "pocket_detail.json")
	if d.State != "completed" || d.ID == "" || d.Transcript == nil || len(d.Transcript.Segments) == 0 {
		t.Fatalf("fixture shape: %+v", d)
	}
	if s := d.Transcript.Segments[0]; s.Speaker != "SPEAKER_00" || s.Text == "" {
		t.Fatalf("segment shape: %+v", s)
	}
	// pending recordings carry NO transcript field at all (the qualify gate)
	p := loadPocketFixture(t, "pocket_detail_pending.json")
	if p.State != "pending" || p.Transcript != nil {
		t.Fatalf("pending shape: state=%s transcript=%v", p.State, p.Transcript)
	}
}

func TestConvertPocketTranscriptByteStable(t *testing.T) {
	d := loadPocketFixture(t, "pocket_detail.json")
	a, segsA, unA := convertPocketTranscript(d, nil, nil)
	b, segsB, unB := convertPocketTranscript(d, nil, nil)
	if a != b || segsA != segsB || unA != unB {
		t.Fatal("conversion is not byte-stable")
	}
	if !unA {
		t.Fatal("SPEAKER_NN fixture must flag unresolved speakers")
	}
	if !strings.HasPrefix(a, "---\ncategories:\n  - sync\npocket-id: "+d.ID+"\n---\n") {
		t.Fatalf("frontmatter convention: %q", a[:80])
	}
	if !strings.Contains(a, "\n## Transcript\n\n**Speaker 1:** ") {
		t.Fatalf("speaker mapping/heading: %q", a)
	}
	if strings.Contains(a, "SPEAKER_0") {
		t.Fatal("raw diarization labels must not leak into the note")
	}
	// consecutive same-speaker turns merge (fixture has adjacent SPEAKER_00 runs)
	if strings.Count(a, "**Speaker 1:**") >= segsA {
		t.Fatal("same-speaker turns did not merge")
	}
}

func TestPocketSpeakerMapping(t *testing.T) {
	for in, want := range map[string]string{
		"SPEAKER_00": "Speaker 1", "SPEAKER_01": "Speaker 2", "SPEAKER_11": "Speaker 12",
		"Jane Doe": "Jane Doe",
	} {
		got, un := mapPocketSpeaker(in)
		if got != want {
			t.Fatalf("%s → %s, want %s", in, got, want)
		}
		if wantUn := strings.HasPrefix(in, "SPEAKER_"); un != wantUn {
			t.Fatalf("%s unresolved=%v", in, un)
		}
	}
}

func TestPocketNoteDateChicagoEdge(t *testing.T) {
	// 9:43pm Chicago on Jul 29 = 02:43Z Jul 30 — must file under the 29th.
	local, err := pocketNoteDate("2026-07-30T02:43:33Z")
	if err != nil {
		t.Fatal(err)
	}
	if got := local.Format("2006-01-02"); got != "2026-07-29" {
		t.Fatalf("UTC→Chicago date: %s", got)
	}
	if fn := pocketNoteFilename(local, "Call With Jane Smith"); fn != "2026-07-29 call with jane smith.md" {
		t.Fatalf("filename (lowercase + date): %q", fn)
	}
}

func TestPocketLabeledSpeakersAttendeeLine(t *testing.T) {
	d := pocketDetail{ID: "rec_x", Transcript: &struct {
		Segments []pocketSegment `json:"segments"`
	}{Segments: []pocketSegment{
		{Speaker: "Benjamin", Text: "hi"},
		{Speaker: "Jane Doe", Text: "hello"},
		{Speaker: "Jane Doe", Text: "again"},
	}}}
	content, segs, un := convertPocketTranscript(d, nil, nil)
	if un || segs != 3 {
		t.Fatalf("labeled fixture: un=%v segs=%d", un, segs)
	}
	if !strings.Contains(content, "[[Jane Doe]]") {
		t.Fatalf("attendee line missing: %q", content)
	}
	if strings.Contains(content, "[[Benjamin]]") {
		t.Fatal("Benjamin must never be linked")
	}
	if !strings.Contains(content, "**Jane Doe:** hello again") {
		t.Fatalf("same-speaker merge: %q", content)
	}
}
