package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNoteNameIsLowercaseSpacesFromTitle(t *testing.T) {
	cases := []struct{ title, slug, want string }{
		{"100 Days with Visualize Value's Daily Manifest", "100-days-with-visualize-values-daily-manifest", "100 days with visualize value's daily manifest"},
		{"2 Years of Running a Cleaning Marketplace", "2-years-of-running-a-cleaning-marketplace", "2 years of running a cleaning marketplace"},
		{"#1: Iosif Gershteyn", "1-iosif-gershteyn", "1 iosif gershteyn"},
		{"A boring MRI", "a-boring-mri", "a boring mri"},
		{"", "fallback-to-the-slug", "fallback to the slug"},
		{"???", "punctuation-only-title", "punctuation only title"},
	}
	for _, c := range cases {
		if got := noteName(c.title, c.slug); got != c.want {
			t.Errorf("noteName(%q, %q) = %q, want %q", c.title, c.slug, got, c.want)
		}
	}
}

// A post already mirrored — under the old slug name, or under a title that
// has since changed — is found by its URL and updated in place, never
// duplicated. A fresh post gets its title-derived name.
func TestNoteForFindsAnExistingMirrorByURL(t *testing.T) {
	vault := t.TempDir()
	folder := filepath.Join(vault, "samizdat")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	old := "---\ntitle: \"A boring MRI\"\nurl: https://www.consciousrepository.com/p/a-boring-mri\nsource: substack\ncategories: [samizdat, substack]\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(folder, "a-boring-mri.md"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &vaultStore{vault: vault, folder: "samizdat"}

	name, kept := s.noteFor("a boring mri", "https://consciousrepository.com/p/a-boring-mri/?utm=x")
	if name != "a-boring-mri" || !kept {
		t.Errorf("old-rule mirror not found by URL: %q kept=%v", name, kept)
	}
	name, kept = s.noteFor("a brand new post", "https://www.consciousrepository.com/p/a-brand-new-post")
	if name != "a brand new post" || kept {
		t.Errorf("fresh post: %q kept=%v", name, kept)
	}

	// Once the title-named file exists it wins outright.
	if err := os.WriteFile(filepath.Join(folder, "a boring mri.md"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	name, kept = s.noteFor("a boring mri", "https://www.consciousrepository.com/p/a-boring-mri")
	if name != "a boring mri" || kept {
		t.Errorf("title-named file must win: %q kept=%v", name, kept)
	}
}

func TestCanonURL(t *testing.T) {
	a := canonURL("https://www.consciousrepository.com/p/a-boring-mri/?utm_source=x")
	b := canonURL("http://ConsciousRepository.com/p/a-boring-mri")
	if a != b || a != "consciousrepository.com/p/a-boring-mri" {
		t.Errorf("canonURL: %q vs %q", a, b)
	}
}
