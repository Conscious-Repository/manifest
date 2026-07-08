package server

import (
	"strings"
	"testing"
)

func TestPromoteText(t *testing.T) {
	cases := []struct{ title, link, want string }{
		{"Cool paper", "https://x.com/a", "Cool paper — https://x.com/a"},
		{"No link here", "", "No link here"},
		// inline-field-shaped substrings would be eaten by the day parser → stripped
		{"Sneaky [goal:: aion/x] title", "l", "Sneaky title — l"},
		{"[owner:: me]  spaced", "", "spaced"},
	}
	for _, c := range cases {
		if got := promoteText(c.title, c.link); got != c.want {
			t.Errorf("promoteText(%q,%q) = %q, want %q", c.title, c.link, got, c.want)
		}
	}
	// long titles are capped before the link suffix
	long := ""
	for i := 0; i < 300; i++ {
		long += "x"
	}
	got := promoteText(long, "http://l")
	// 200 (cap) + "…" + " — http://l" = 212 runes; the link suffix must survive
	if len([]rune(got)) != 212 || !strings.HasSuffix(got, "… — http://l") {
		t.Fatalf("long title cap wrong: len=%d got=%q", len([]rune(got)), got[len(got)-20:])
	}
}

func TestProposedFenceStripped(t *testing.T) {
	// evidence is kept; everything from the proposed fence onward is dropped
	// (feedProposals TrimSpace-s the remainder, so trailing whitespace is fine).
	body := "evidence line\n\n```proposed\n---\nfoo: bar\n---\nnew content\n```\n"
	if got := strings.TrimSpace(proposedFenceRe.ReplaceAllString(body, "")); got != "evidence line" {
		t.Fatalf("fence strip = %q, want %q", got, "evidence line")
	}
	// 4-backtick fence wrapping nested 3-backtick code (the real format)
	body4 := "why\n\n````proposed\n```go\nx\n```\n````\n"
	if got := strings.TrimSpace(proposedFenceRe.ReplaceAllString(body4, "")); got != "why" {
		t.Fatalf("4-backtick fence strip = %q", got)
	}
}
