package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"manifest/fundraising"
	"manifest/fundraisingportal"
	"manifest/teamportal"
)

func TestFundraisingPortalInviteCRUDAndProjectionBoundary(t *testing.T) {
	store := testFundraisingStore(t)
	op, err := store.Create("Acme")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(op.ID, map[string]any{"people": []fundraising.PersonRef{{Key: "person", Display: "Person", NotePath: "private/person.md", Emails: []string{"private@example.com"}}}}); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	invites, _ := fundraisingportal.NewInviteStore(dataDir, "owner@aion.bio")
	if err := invites.Replace([]string{"advisor@example.com"}); err != nil {
		t.Fatal(err)
	}
	auth := teamportal.NewScopedAuth(dataDir, "", "fundraising_test", invites.Allowed, "invited collaborators")
	handler, err := FundraisingPortalHandler(FundraisingPortalOptions{Auth: auth, Invites: invites, Store: store, AdminEmail: "owner@aion.bio"})
	if err != nil {
		t.Fatal(err)
	}

	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/fundraising", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous API status = %d", unauth.Code)
	}
	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	loginRequest.Header.Set("Accept", "text/html")
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), "Sign in with Google") {
		t.Fatalf("anonymous navigation status=%d body=%s", login.Code, login.Body.String())
	}
	stylesheet := httptest.NewRecorder()
	handler.ServeHTTP(stylesheet, httptest.NewRequest(http.MethodGet, "/style.css?v=1", nil))
	if stylesheet.Code != http.StatusOK || !strings.Contains(stylesheet.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("anonymous stylesheet status=%d type=%q body=%s", stylesheet.Code, stylesheet.Header().Get("Content-Type"), stylesheet.Body.String())
	}
	cookie, err := auth.SessionCookie("advisor@example.com", "Advisor", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(cookie)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	listed := request(http.MethodGet, "/api/fundraising", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	for _, secret := range []string{"private@example.com", "private/person.md", "notePath", "sourceRows", `"path"`} {
		if strings.Contains(listed.Body.String(), secret) {
			t.Fatalf("external response leaked %q: %s", secret, listed.Body.String())
		}
	}
	created := request(http.MethodPost, "/api/fundraising/item", `{"firm":"New Fund","people":["Plain Person"]}`)
	if created.Code != http.StatusOK || !strings.Contains(created.Body.String(), "Plain Person") {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	archive := request(http.MethodPatch, "/api/fundraising/"+op.ID, `{"archived":true}`)
	if archive.Code == http.StatusOK {
		t.Fatalf("external archive unexpectedly succeeded: %s", archive.Body.String())
	}
	if got, _ := store.Get(op.ID); got.Archived {
		t.Fatal("external patch archived the record")
	}

	if err := invites.Replace(nil); err != nil {
		t.Fatal(err)
	}
	revoked := request(http.MethodGet, "/api/fundraising", "")
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status = %d", revoked.Code)
	}
}
