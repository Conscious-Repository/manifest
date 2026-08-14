package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The standalone portal listener (AION portal move, phase 1): GET / is the
// portal's own index.html, and its assets resolve at the root of that port.
func TestPortalHandlerServesTheEmbeddedPortal(t *testing.T) {
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatalf("PortalHandler: %v", err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, tc := range []struct{ path, want string }{
		{"/", "AION &middot; portal"},
		{"/index.html", "AION &middot; portal"},
		{"/src/data-load.js", "loadPortalData"},
		{"/data/meta.json", ""},
		{"/content/hiring.md", ""},
		{"/assets/colors_and_type.css", ":root"},
		{"/assets/favicon.png", ""},
	} {
		resp, err := http.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body := readAllString(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", tc.path, resp.StatusCode)
			continue
		}
		if tc.want != "" && !strings.Contains(body, tc.want) {
			t.Errorf("GET %s body missing %q", tc.path, tc.want)
		}
	}

	// The portal index points at its assets by relative path (no /investor
	// prefix from the aionbio root) so it renders standalone on :7778.
	idx := getBody(t, srv.URL+"/")
	if !strings.Contains(idx, `href="./assets/colors_and_type.css"`) {
		t.Errorf("index.html: missing relative colors_and_type.css reference")
	}
	if !strings.Contains(idx, `href="./assets/favicon.png"`) {
		t.Errorf("index.html: missing relative favicon reference")
	}
	if strings.Contains(idx, "/investor/assets") {
		t.Errorf("index.html: still points at aionbio /investor/assets absolute path")
	}

	// The portal mux is mutually exclusive with the dashboard mux: no API
	// surface, and none of the dashboard's own assets, live on this port.
	for _, path := range []string{"/api/day", "/js/app.js"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404 on the portal listener", path, resp.StatusCode)
		}
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}
	return readAllString(t, resp)
}

func readAllString(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}
