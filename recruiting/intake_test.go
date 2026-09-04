package recruiting

import "testing"

// The resolver is the one part of intake that can be pinned exhaustively: it
// never fetches, so every shape the owner pastes has one right answer.
func TestResolveIntake(t *testing.T) {
	cases := []struct {
		name  string
		paste string
		kind  string
		class string
		dest  string
		want  func(t *testing.T, r Resolution)
	}{
		{name: "bare orcid", paste: "0000-0002-5263-5070", kind: "orcid", class: SeedPerson, dest: DestCandidate,
			want: func(t *testing.T, r Resolution) {
				if r.ORCID != "0000-0002-5263-5070" || r.URL != "https://orcid.org/0000-0002-5263-5070" {
					t.Fatalf("orcid not carried: %+v", r)
				}
				if !r.Provisional {
					t.Fatal("an ORCID iD carries no name — the scaffold must ask")
				}
			}},
		{name: "orcid url", paste: "https://orcid.org/0000-0001-9276-1891", kind: "orcid", class: SeedPerson, dest: DestCandidate},
		{name: "bare doi", paste: "10.1038/s41586-020-2649-2", kind: "doi", class: SeedWork, dest: DestSeed,
			want: func(t *testing.T, r Resolution) {
				if r.DOI != "10.1038/s41586-020-2649-2" {
					t.Fatalf("doi not carried: %+v", r)
				}
				if !hasAdapter(r, "openalex") {
					t.Fatal("a DOI is an OpenAlex works lookup")
				}
			}},
		{name: "doi url", paste: "https://doi.org/10.1038/s41586-020-2649-2", kind: "doi", class: SeedWork, dest: DestSeed},
		{name: "pubmed", paste: "https://pubmed.ncbi.nlm.nih.gov/32939066/", kind: "pubmed", class: SeedWork, dest: DestSeed},
		{name: "openalex work", paste: "https://openalex.org/W3005144120", kind: "openalex", class: SeedWork, dest: DestSeed},
		{name: "openalex author", paste: "https://openalex.org/A5023888391", kind: "openalex", class: SeedPerson, dest: DestCandidate},
		{name: "nih project", paste: "https://reporter.nih.gov/project-details/10461234", kind: "grant", class: SeedWork, dest: DestSeed},
		{name: "github user", paste: "https://github.com/torvalds", kind: "github-user", class: SeedPerson, dest: DestCandidate,
			want: func(t *testing.T, r Resolution) {
				if r.Handle != "torvalds" {
					t.Fatalf("handle: %q", r.Handle)
				}
			}},
		{name: "github repo", paste: "github.com/numpy/numpy", kind: "github-repo", class: SeedRepo, dest: DestSeed,
			want: func(t *testing.T, r Resolution) {
				if r.Name != "numpy/numpy" {
					t.Fatalf("repo name: %q", r.Name)
				}
			}},
		{name: "x profile stays a link", paste: "https://x.com/karpathy", kind: "social", class: SeedPerson, dest: DestCandidate,
			want: func(t *testing.T, r Resolution) {
				if !r.LinkOnly {
					t.Fatal("X is recorded, never crawled (Q2)")
				}
				if len(r.Adapters) != 0 {
					t.Fatalf("no adapter may claim X: %v", r.Adapters)
				}
				if r.Handle != "@karpathy" {
					t.Fatalf("handle: %q", r.Handle)
				}
			}},
		{name: "linkedin stays a link", paste: "https://www.linkedin.com/in/some-person", kind: "social", class: SeedPerson, dest: DestCandidate,
			want: func(t *testing.T, r Resolution) {
				if !r.LinkOnly || len(r.Adapters) != 0 {
					t.Fatal("no adapter reads LinkedIn (D12)")
				}
			}},
		{name: "feed by extension", paste: "https://example.org/podcast.xml", kind: "feed", class: SeedMedia, dest: DestSeed},
		{name: "feed by path", paste: "https://blog.example.org/feed/", kind: "feed", class: SeedMedia, dest: DestSeed},
		{name: "feed by host", paste: "https://feeds.megaphone.fm/showname", kind: "feed", class: SeedMedia, dest: DestSeed},
		{name: "apple podcast page", paste: "https://podcasts.apple.com/us/podcast/x/id123", kind: "feed", class: SeedMedia, dest: DestSeed},
		{name: "plain site is ambiguous", paste: "https://bme.washu.edu/people", kind: "site", class: "", dest: DestSeed,
			want: func(t *testing.T, r Resolution) {
				if len(r.Suggest) == 0 {
					t.Fatal("an ambiguous site must offer the classes, not guess one")
				}
				if !hasAdapter(r, "web") {
					t.Fatal("a page is the web crawler's job")
				}
			}},
		{name: "person name", paste: "Jane Q Smith", kind: "name", class: SeedPerson, dest: DestCandidate,
			want: func(t *testing.T, r Resolution) {
				if r.Provisional {
					t.Fatal("a typed name is not provisional")
				}
				if r.Name != "Jane Q Smith" {
					t.Fatalf("name: %q", r.Name)
				}
			}},
		{name: "lab name", paste: "WashU BME Smith Laboratory", kind: "name", class: SeedLab, dest: DestSeed},
		{name: "company name", paste: "Hyperfine Inc", kind: "name", class: SeedCompany, dest: DestSeed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ResolveIntake(tc.paste)
			if r.Kind != tc.kind {
				t.Fatalf("kind = %q, want %q (%+v)", r.Kind, tc.kind, r)
			}
			if r.Class != tc.class {
				t.Fatalf("class = %q, want %q", r.Class, tc.class)
			}
			if r.Dest != tc.dest {
				t.Fatalf("dest = %q, want %q", r.Dest, tc.dest)
			}
			if r.Why == "" {
				t.Fatal("every resolution says how it decided")
			}
			if tc.want != nil {
				tc.want(t, r)
			}
		})
	}
}

func TestResolveIntakeEmpty(t *testing.T) {
	if r := ResolveIntake("   "); r.Text != "" || r.Kind != "" {
		t.Fatalf("empty paste resolves to nothing: %+v", r)
	}
}

// Prose that merely mentions a DOI is a note, not a paper intake.
func TestResolveIntakeProseIsNotADOI(t *testing.T) {
	r := ResolveIntake("read 10.1038/s41586-020-2649-2 before the call")
	if r.Kind != "name" {
		t.Fatalf("prose should stay a name, got %q", r.Kind)
	}
}

// Every class the resolver can emit must be a class the store will accept —
// the two closed sets cannot drift apart.
func TestResolveIntakeClassesAreValid(t *testing.T) {
	pastes := []string{"0000-0002-5263-5070", "10.1038/x", "https://github.com/a/b",
		"https://github.com/a", "https://x.com/a", "https://feeds.megaphone.fm/s",
		"Jane Smith", "Hyperfine Inc", "WashU Lab", "https://example.org/team"}
	for _, p := range pastes {
		r := ResolveIntake(p)
		if r.Class != "" && !ValidSeedClass(r.Class) {
			t.Fatalf("%q resolved to class %q, which the store refuses", p, r.Class)
		}
		for _, s := range r.Suggest {
			if !ValidSeedClass(s) {
				t.Fatalf("%q suggested class %q, which the store refuses", p, s)
			}
		}
		switch r.Dest {
		case DestCandidate, DestNetwork, DestSeed, "":
		default:
			t.Fatalf("%q resolved to unknown destination %q", p, r.Dest)
		}
	}
}

func hasAdapter(r Resolution, id string) bool {
	for _, a := range r.Adapters {
		if a == id {
			return true
		}
	}
	return false
}
