package sources

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestCandidateWebBoundedIdentityAndSocialLinks(t *testing.T) {
	net := newWebNet().site("ada.example", map[string]string{
		"/robots.txt": "User-agent: *\nDisallow: /private",
		"/bio":        "<h1>Ada Example</h1><p>PhD at Example University in 2018.</p><a href='/projects'>Projects</a><a href='/private'>Research</a><a href='https://linkedin.com/in/ada'>LinkedIn</a><a href='http://127.0.0.1/admin'>Bio</a><footer>Other people</footer>",
		"/projects":   "<h1>Ada Example — projects</h1><p>Built an open MRI reconstruction toolkit.</p>",
		"/roster":     "<h1>Our team</h1><p>Ada Example and Other Person, PhD elsewhere.</p>",
	})
	d := CandidateDraft{Name: "Ada Example", Links: []string{"https://ada.example/roster", "https://ada.example/bio", "https://linkedin.com/in/ada", "http://127.0.0.1/admin"}}
	got, err := net.adapter().LookupCandidate(context.Background(), d, Scope{})
	if err != nil || len(got) != 1 || len(got[0].Evidence) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, e := range got[0].Evidence {
		if strings.Contains(e.Snippet, "Other") {
			t.Fatalf("roster/chrome leaked: %+v", e)
		}
	}
	if len(net.leaked()) != 0 || net.requested("https://ada.example/private") {
		t.Fatalf("guard bypass: %+v", net.requests())
	}
	if !reflect.DeepEqual(net.pages(), []string{"https://ada.example/bio", "https://ada.example/roster", "https://ada.example/projects"}) {
		t.Fatalf("order: %+v", net.pages())
	}
	if !strings.Contains(strings.Join(got[0].Links, ","), "linkedin.com/in/ada") {
		t.Fatal("supplied social link lost")
	}
}

func TestCandidateWebInitialsAndNamesakes(t *testing.T) {
	net := newWebNet().site("ada.example", map[string]string{"/": "<h1>Different Person</h1><p>Ada Example is a collaborator.</p>"})
	for _, name := range []string{"A. Example", "Ada Example"} {
		got, err := net.adapter().LookupCandidate(context.Background(), CandidateDraft{Name: name, Homepage: "https://ada.example/"}, Scope{})
		if err != nil || len(got) != 0 {
			t.Fatalf("wrong identity: %+v %v", got, err)
		}
	}
}
