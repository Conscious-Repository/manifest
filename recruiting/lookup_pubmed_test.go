package recruiting

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"manifest/recruiting/sources"
)

func TestPubMedLookupAttributedAuthorTopics(t *testing.T) {
	for _, tc := range []struct {
		name, byline, topics, authorID string
		wantTopics, wantAuthorCalls    int
		fail                           bool
	}{
		{"attributed initials", `{"author_position":"first","raw_author_name":"Yu G","author":{"id":"https://openalex.org/A1234","display_name":"Guang Yu"}}`, `[{"display_name":"Diffusion MRI"},{"display_name":"Diffusion MR"}]`, "A1234", 2, 2, false},
		{"different raw name", `{"author_position":"first","raw_author_name":"Yu H","author":{"id":"https://openalex.org/A1234"}}`, `[]`, "A1234", 0, 0, false},
		{"no raw name", `{"author_position":"first","author":{"id":"https://openalex.org/A1234","display_name":"Yu G"}}`, `[]`, "A1234", 0, 0, false},
		{"middle author", `{"author_position":"middle","raw_author_name":"Yu G","author":{"id":"https://openalex.org/A1234"}}`, `[]`, "A1234", 0, 0, false},
		{"ambiguous first authors", `{"author_position":"first","raw_author_name":"Yu G","author":{"id":"https://openalex.org/A1234"}},{"author_position":"first","raw_author_name":"Yu G","author":{"id":"https://openalex.org/A5678"}}`, `[]`, "A1234", 0, 0, false},
		{"missing author id", `{"author_position":"first","raw_author_name":"Yu G","author":{}}`, `[]`, "A1234", 0, 0, false},
		{"author has no topics", `{"author_position":"first","raw_author_name":"Yu G","author":{"id":"https://openalex.org/A1234"}}`, `[]`, "A1234", 0, 2, false},
		{"wrong author response", `{"author_position":"first","raw_author_name":"Yu G","author":{"id":"https://openalex.org/A1234"}}`, `[{"display_name":"Wrong topic"}]`, "A5678", 0, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authorCalls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/entrez/eutils/esearch.fcgi":
					fmt.Fprint(w, `{"esearchresult":{"idlist":["39000001"]}}`)
				case "/entrez/eutils/efetch.fcgi":
					fmt.Fprint(w, `<PubmedArticleSet><PubmedArticle><MedlineCitation><PMID>39000001</PMID><Article><ArticleTitle>Diffusion MRI reconstruction.</ArticleTitle><AuthorList><Author><LastName>Yu</LastName><ForeName>G</ForeName><Initials>G</Initials></Author></AuthorList></Article></MedlineCitation></PubmedArticle></PubmedArticleSet>`)
				case "/works/pmid:39000001":
					fmt.Fprintf(w, `{"id":"https://openalex.org/W1234","title":"Diffusion MRI reconstruction.","authorships":[%s]}`, tc.byline)
				case "/authors/A1234":
					authorCalls++
					fmt.Fprintf(w, `{"id":"https://openalex.org/%s","display_name":"Guang Yu","last_known_institution":{"display_name":"Example University"},"topics":%s}`, tc.authorID, tc.topics)
				default:
					t.Errorf("unexpected request: %s", r.URL)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			rs, _, _ := testRunStore(t, nil)
			rs.Register(sources.PubMed{BaseURL: srv.URL, Client: *srv.Client()})
			rs.Register(sources.OpenAlex{BaseURL: srv.URL, Client: *srv.Client()})
			run := mustRun(t, rs, RunRequest{Source: "pubmed", Query: "diffusion MRI"})
			if len(run.Drafts[0].Draft.Topics) != 0 {
				t.Fatal("PubMed invented title topics")
			}
			for pass := 0; pass < 2; pass++ {
				var res LookupResult
				var err error
				run, res, err = rs.Lookup(context.Background(), run.ID, "d1", testNow)
				if err != nil {
					t.Fatal(err)
				}
				if (len(res.Failed) > 0) != tc.fail {
					t.Fatalf("lookup status: %+v", res)
				}
				if pass == 1 && (res.Cites != 0 || len(res.Filled) != 0) {
					t.Fatalf("non-idempotent lookup: %+v", res)
				}
			}
			if authorCalls != tc.wantAuthorCalls {
				t.Fatalf("author calls: %d", authorCalls)
			}
			reloaded, err := rs.Get(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			d := reloaded.Drafts[0].Draft
			if d.SourceID != "pubmed" || d.Name != "Yu G" || len(d.Topics) != tc.wantTopics {
				t.Fatalf("draft: %+v", d)
			}
			k := DeriveKnowledge(d, "cand/yu-g", "", testNow)
			if len(k.Edges) != tc.wantTopics {
				t.Fatalf("edges: %+v", k)
			}
			for _, e := range k.Edges {
				if !e.Inferred || e.Source != "openalex" || !strings.Contains(e.Evidence, "https://openalex.org/A1234") || !strings.Contains(e.Basis, "https://openalex.org/W1234") {
					t.Fatalf("lost provenance: %+v", e)
				}
			}
			if tc.wantTopics > 0 && k.Edges[0].To == k.Edges[1].To {
				t.Fatal("near-miss topics merged")
			}
			_, candidate, err := rs.Accept(run.ID, "d1", testNow)
			if err != nil {
				t.Fatal(err)
			}
			gs, _ := testGraphStore(t)
			if _, err := ApplyKnowledge(gs, DeriveKnowledge(d, candidate.ID, "", testNow)); err != nil {
				t.Fatal(err)
			}
			if got := len(gs.LoadEdges().Edges()); got != tc.wantTopics {
				t.Fatalf("persisted expertise edges: %d, want %d", got, tc.wantTopics)
			}
		})
	}
}
