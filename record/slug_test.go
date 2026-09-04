package record

import "testing"

func TestSlugSpaces(t *testing.T) {
	cases := []struct {
		in   string
		cap  int
		want string
	}{
		{"100 Days with Visualize Value's Daily Manifest", 0, "100 days with visualize value's daily manifest"},
		{"100-days-with-visualize-values-daily-manifest", 0, "100 days with visualize values daily manifest"},
		{"2 Years of Running a Cleaning Marketplace", 0, "2 years of running a cleaning marketplace"},
		{"being a lizard", 0, "being a lizard"},
		{"The Three-Body Problem", 0, "the three body problem"},
		{"snake_case_and--double  spaces ", 0, "snake case and double spaces"},
		{"#729 – The Terahertz Frontier", 0, "729 the terahertz frontier"},
		{"Goblins, Demons & Goddesses: A Conversation at the Edge of AI?", 0, "goblins demons goddesses a conversation at the edge of ai"},
		{"Alice’s Adventures in Wonderland / Through the Looking-Glass", 0, "alice’s adventures in wonderland through the looking glass"},
		{"Café Société — été", 0, "café société été"},
		{"@melissa on X", 0, "melissa on x"},
		{"", 0, ""},
		{"---", 0, ""},
		{"a very long title that gets cut", 12, "a very long"},
		{"cut-right-at a space", 9, "cut right"},
	}
	for _, c := range cases {
		if got := SlugSpaces(c.in, c.cap); got != c.want {
			t.Errorf("SlugSpaces(%q, %d) = %q, want %q", c.in, c.cap, got, c.want)
		}
	}
}

// The hyphen kernel is untouched: ids built on it must not move.
func TestSlugHyphenKernelUnchanged(t *testing.T) {
	if got := Slug("100 Days with Visualize Value's Daily Manifest", 60); got != "100-days-with-visualize-value-s-daily-manifest" {
		t.Errorf("Slug = %q", got)
	}
	if got := Slug("The Three-Body Problem", 9); got != "the-three" {
		t.Errorf("Slug cap = %q", got)
	}
}
