package vaultindex

import (
	"reflect"
	"testing"
)

var aiRegions = []string{"Agents/", "excalibur/"}

func TestBlockStyleCategoriesAndLinks(t *testing.T) {
	// the real "2026-05-19 shoumik sync.md" shape: block dash-list categories,
	// an attendee link row, transcript body.
	src := "---\ncategories:\n  - aion\n  - fundraising\n  - sync\n---\n" +
		"[[shoumik dabir]] [[justin mares]] [[michael levin]]\n\n**microphone:** Hey.\n"
	n := ParseNote("2026-05-19 shoumik sync.md", []byte(src), 100, aiRegions)

	if !reflect.DeepEqual(n.Categories, []string{"aion", "fundraising", "sync"}) {
		t.Fatalf("categories = %v", n.Categories)
	}
	if n.Date != "2026-05-19" || n.DateSource != "filename" {
		t.Fatalf("date = %q (%s), want 2026-05-19/filename", n.Date, n.DateSource)
	}
	keys := linkKeys(n)
	if !reflect.DeepEqual(keys, []string{"shoumik dabir", "justin mares", "michael levin"}) {
		t.Fatalf("link keys = %v", keys)
	}
	if n.AIAuthored {
		t.Fatal("root note must not be AI-authored")
	}
}

func TestInlineCategoriesAndAliases(t *testing.T) {
	src := "---\ncategories: [people]\nalias: [RJ, \"@justinmares\"]\naliases: [Justin]\n---\n- GP at [[Long Journey]]\n"
	n := ParseNote("justin mares.md", []byte(src), 0, aiRegions)
	if !reflect.DeepEqual(n.Categories, []string{"people"}) {
		t.Fatalf("categories = %v", n.Categories)
	}
	if !reflect.DeepEqual(n.Aliases, []string{"RJ", "@justinmares", "Justin"}) {
		t.Fatalf("aliases = %v (want alias + aliases merged, quotes stripped)", n.Aliases)
	}
	if n.Date != "" {
		t.Fatalf("undated note must have no date, got %q", n.Date)
	}
}

func TestDateFromFrontmatterWhenFilenameUndated(t *testing.T) {
	n := ParseNote("some idea.md", []byte("---\ndate: 2026-03-04\ncategories: [essays]\n---\nBody\n"), 0, aiRegions)
	if n.Date != "2026-03-04" || n.DateSource != "frontmatter" {
		t.Fatalf("date = %q (%s)", n.Date, n.DateSource)
	}
}

func TestGranolaIDFromFrontmatter(t *testing.T) {
	n := ParseNote("2026-07-02 Aion sync.md", []byte("---\ncategories:\n  - sync\ngranola-id: not_abc123\n---\n[[jane]]\n\n## Transcript\n\n**Benjamin:** hi\n"), 0, aiRegions)
	if n.GranolaID != "not_abc123" {
		t.Fatalf("granola-id = %q, want not_abc123", n.GranolaID)
	}
	// underscore spelling also works; no field → empty
	if g := ParseNote("x.md", []byte("---\ngranola_id: not_z\n---\nx"), 0, aiRegions).GranolaID; g != "not_z" {
		t.Fatalf("granola_id = %q, want not_z", g)
	}
	if g := ParseNote("y.md", []byte("---\ntitle: z\n---\nx"), 0, aiRegions).GranolaID; g != "" {
		t.Fatalf("granola-id = %q, want empty", g)
	}
}

func TestDailyInlineFieldsAndDisplayLinks(t *testing.T) {
	src := "<!-- manifest:start -->\n## Focus\n- Backyard [goal:: home/backyard] [milestone:: home/backyard/yard-done]\n" +
		"meeting [[shoumik dabir]] and [[olga sobkiv|Olga]] today\n"
	n := ParseNote("intrinsic/2026-07-02.md", []byte(src), 0, aiRegions)
	if n.Date != "2026-07-02" {
		t.Fatalf("daily date = %q", n.Date)
	}
	// inline fields
	got := map[string]string{}
	for _, f := range n.InlineFields {
		got[f.Key] = f.Value
	}
	if got["goal"] != "home/backyard" || got["milestone"] != "home/backyard/yard-done" {
		t.Fatalf("inline fields = %v", n.InlineFields)
	}
	// display link resolves to lowercased target key
	var olga *Link
	for i := range n.Links {
		if n.Links[i].Key == "olga sobkiv" {
			olga = &n.Links[i]
		}
	}
	if olga == nil || olga.Display != "Olga" {
		t.Fatalf("display link = %+v", n.Links)
	}
}

func TestAIAuthoredRegions(t *testing.T) {
	for _, p := range []string{"Agents/brief.md", "excalibur/spirits/x/cornerstone.md"} {
		if !ParseNote(p, []byte("body"), 0, aiRegions).AIAuthored {
			t.Fatalf("%s should be AI-authored", p)
		}
	}
	if ParseNote("Agentsfoo/x.md", []byte("b"), 0, aiRegions).AIAuthored {
		t.Fatal("prefix must match on a path boundary, not a substring")
	}
}

func TestCaseInsensitiveLinkKey(t *testing.T) {
	n := ParseNote("x.md", []byte("[[Shoumik Dabir]] and [[shoumik dabir|Shoumik]]"), 0, aiRegions)
	// both spellings collapse to one entity key
	if len(n.Links) != 1 || n.Links[0].Key != "shoumik dabir" {
		t.Fatalf("links = %+v (want one 'shoumik dabir' key)", n.Links)
	}
}

func linkKeys(n Note) []string {
	var out []string
	for _, l := range n.Links {
		out = append(out, l.Key)
	}
	return out
}
