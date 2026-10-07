package approvals

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One extraction files many cards in the same second. Deciding one must not
// reshuffle the rest: the owner works down the list (2026-10-07).
func TestListKeepsTiesInPlaceWhenOneIsDecided(t *testing.T) {
	s, _ := harnessWithCornerstone(t, baseCornerstone)
	var ids []string
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("%012x", (i*7919)%4096+i<<16)
		created := "2026-10-06T16:36:54Z"
		if i%5 == 0 {
			created = fmt.Sprintf("2026-10-0%dT08:00:00Z", 1+i/5)
		}
		content := "---\ntype: approval\nid: " + id + "\naction: track a thing\nagent: domain-scout\ncreated: " + created + "\n---\n\nbody\n"
		if err := os.WriteFile(filepath.Join(s.dir, "pending", id+".md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	order := func() []string {
		var out []string
		for _, p := range s.List("pending") {
			out = append(out, p.ID)
		}
		return out
	}
	before := order()
	for _, gone := range before {
		path := filepath.Join(s.dir, "pending", gone+".md")
		b, _ := os.ReadFile(path)
		os.Remove(path)
		var want []string
		for _, id := range before {
			if id != gone {
				want = append(want, id)
			}
		}
		if got := order(); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("deciding %s reshuffled the rest:\nwant %v\n got %v", gone, want, got)
		}
		os.WriteFile(path, b, 0o644)
	}
}
