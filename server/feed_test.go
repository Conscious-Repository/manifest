package server

import (
	"strings"
	"testing"
)

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
