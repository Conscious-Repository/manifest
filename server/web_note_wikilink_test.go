package server

import (
	"os/exec"
	"testing"
)

// The [[ typeahead popup stays inside the visible band: it flips above the
// field when the room below (a phone keyboard) is short, caps its height to
// the room it has, follows the visual viewport, and corrects a fixed
// placement that landed off target (testdata/note-wikilink-viewport.cjs).
func TestNoteWikilinkViewportUI(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if out, err := exec.Command(node, "testdata/note-wikilink-viewport.cjs").CombinedOutput(); err != nil {
		t.Fatalf("wikilink popup: %v\n%s", err, out)
	}
}
