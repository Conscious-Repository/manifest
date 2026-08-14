// External test package: it imports manifest/server (which imports teamportal)
// to exercise the REAL portal mux — routes, assignee lock, and status codes —
// with sessions minted directly against the auth (no Google round-trip).
package teamportal_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"manifest/server"
	"manifest/teamportal"
)

func newPortal(t *testing.T) (*httptest.Server, *teamportal.Auth) {
	t.Helper()
	auth := teamportal.NewAuth(t.TempDir())
	store, err := teamportal.New(filepath.Join(t.TempDir(), "team"))
	if err != nil {
		t.Fatalf("teamportal.New: %v", err)
	}
	h, err := server.PortalHandler(server.PortalOptions{
		Auth: auth, Store: store, AdminEmail: "benjamin@aion.bio",
	})
	if err != nil {
		t.Fatalf("PortalHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, auth
}

func session(t *testing.T, a *teamportal.Auth, email, name string) *http.Cookie {
	t.Helper()
	c, err := a.SessionCookie(email, name, false, time.Now())
	if err != nil {
		t.Fatalf("SessionCookie(%s): %v", email, err)
	}
	return c
}

// call sends a JSON request with an optional session cookie and decodes the
// response body into out (when out is non-nil and the status is 2xx).
func call(t *testing.T, srv *httptest.Server, method, path string, cookie *http.Cookie, body, out any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequest(method, srv.URL+path, &buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode response: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

// addItem creates a team item as the given member and returns it.
func addItem(t *testing.T, srv *httptest.Server, c *http.Cookie, title string) teamportal.TeamItem {
	t.Helper()
	var it teamportal.TeamItem
	if code := call(t, srv, "POST", "/api/team/items", c, map[string]string{"title": title}, &it); code != 200 {
		t.Fatalf("POST /api/team/items = %d, want 200", code)
	}
	return it
}

// The assignee lock: ONLY the item's owner may PATCH it — everyone else,
// admin included, gets 403 (no override lane, decided 2026-08-13).
func TestPatchAssigneeLock(t *testing.T) {
	srv, auth := newPortal(t)
	hannah := session(t, auth, "hannah@aion.bio", "Hannah Zmuda") // roster: HZ
	benjamin := session(t, auth, "benjamin@aion.bio", "Benjamin Anderson") // roster: BA, admin

	it := addItem(t, srv, hannah, "Calibrate the MRI phantom")
	if it.Owner != "HZ" {
		t.Fatalf("team item owner = %q, want HZ (roster mapping from hannah@)", it.Owner)
	}

	// Non-assignee — even the portal owner/admin — is refused.
	if code := call(t, srv, "PATCH", "/api/team/item/"+it.ID, benjamin,
		map[string]string{"status": "done"}, nil); code != http.StatusForbidden {
		t.Errorf("PATCH by non-assignee (admin) = %d, want 403", code)
	}

	// The assignee succeeds, and the override lands.
	var patched struct {
		Override teamportal.Override `json:"override"`
	}
	if code := call(t, srv, "PATCH", "/api/team/item/"+it.ID, hannah,
		map[string]string{"status": "done"}, &patched); code != 200 {
		t.Fatalf("PATCH by assignee = %d, want 200", code)
	}
	if patched.Override.Fields["status"] != "done" || patched.Override.By != "hannah@aion.bio" {
		t.Errorf("override = %+v, want status=done by hannah@aion.bio", patched.Override)
	}

	// The same lock guards PUBLISHED backlog items (owner from the embedded
	// data/backlog.json, not the team store).
	var backlog struct {
		Items []struct{ ID, Owner string } `json:"items"`
	}
	if code := call(t, srv, "GET", "/data/backlog.json", nil, nil, &backlog); code != 200 {
		t.Fatalf("GET /data/backlog.json = %d", code)
	}
	published := ""
	for _, bi := range backlog.Items {
		if bi.Owner == "BA" {
			published = bi.ID
			break
		}
	}
	if published == "" {
		t.Fatal("embedded backlog.json has no BA-owned item to test against")
	}
	if code := call(t, srv, "PATCH", "/api/team/item/"+published, hannah,
		map[string]string{"status": "in_progress"}, nil); code != http.StatusForbidden {
		t.Errorf("PATCH published BA item by hannah = %d, want 403", code)
	}
	if code := call(t, srv, "PATCH", "/api/team/item/"+published, benjamin,
		map[string]string{"status": "in_progress"}, nil); code != 200 {
		t.Errorf("PATCH published BA item by benjamin = %d, want 200", code)
	}

	// Unknown items are 404, not 403 — the lock never invents items.
	if code := call(t, srv, "PATCH", "/api/team/item/team/no-such-item", hannah,
		map[string]string{"status": "done"}, nil); code != http.StatusNotFound {
		t.Errorf("PATCH unknown item = %d, want 404", code)
	}
}

// Comments are the open write: ANY signed-in @aion.bio member may comment on
// any item — no assignee lock. Anonymous and forged sessions get 401.
func TestAnySignedInMemberCanComment(t *testing.T) {
	srv, auth := newPortal(t)
	hannah := session(t, auth, "hannah@aion.bio", "Hannah Zmuda")
	rj := session(t, auth, "rj@aion.bio", "RJ Tevonian")

	it := addItem(t, srv, hannah, "Draft the phantom study protocol")

	// A member who does NOT own the item comments fine.
	var c teamportal.Comment
	if code := call(t, srv, "POST", "/api/team/comment", rj,
		map[string]string{"item": it.ID, "text": "protocol template is in Drive"}, &c); code != 200 {
		t.Fatalf("comment by non-assignee = %d, want 200", code)
	}
	if c.Author != "rj@aion.bio" || c.Item != it.ID {
		t.Errorf("comment = %+v, want author rj@aion.bio on %s", c, it.ID)
	}

	// So does the assignee.
	if code := call(t, srv, "POST", "/api/team/comment", hannah,
		map[string]string{"item": it.ID, "text": "thanks"}, nil); code != 200 {
		t.Errorf("comment by assignee = %d, want 200", code)
	}

	// Anonymous → 401.
	if code := call(t, srv, "POST", "/api/team/comment", nil,
		map[string]string{"item": it.ID, "text": "drive-by"}, nil); code != http.StatusUnauthorized {
		t.Errorf("anonymous comment = %d, want 401", code)
	}

	// A tampered session is anonymous too.
	forged := *hannah
	forged.Value = "x" + forged.Value[1:]
	if code := call(t, srv, "POST", "/api/team/comment", &forged,
		map[string]string{"item": it.ID, "text": "forged"}, nil); code != http.StatusUnauthorized {
		t.Errorf("forged-session comment = %d, want 401", code)
	}

	// Commenting on a nonexistent item is 404.
	if code := call(t, srv, "POST", "/api/team/comment", rj,
		map[string]string{"item": "team/ghost", "text": "?"}, nil); code != http.StatusNotFound {
		t.Errorf("comment on unknown item = %d, want 404", code)
	}

	// Both comments are visible in the open-read team state.
	var ext teamportal.Ext
	if code := call(t, srv, "GET", "/api/team/state", nil, nil, &ext); code != 200 {
		t.Fatalf("GET /api/team/state = %d", code)
	}
	if got := len(ext.Comments[it.ID]); got != 2 {
		t.Errorf("state has %d comments on %s, want 2", got, it.ID)
	}
}
