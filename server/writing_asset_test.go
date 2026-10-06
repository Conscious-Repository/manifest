package server

import (
	"encoding/json"
	"manifest/record"
	"manifest/vaultwriter"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritingAssetsListAndServeOnlyVaultEmbeds(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"images/plan.png": "png", "data/table.csv": "a,b\n1,2", "notes/a.md": "# a", "notes/x.exe": "no",
		".hidden/secret.png": "no", "system/writing/r.txt": "no", "pic.svg": "<svg/>",
	} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "leak.png"), []byte("leak"), 0o644)
	os.Symlink(filepath.Join(outside, "leak.png"), filepath.Join(root, "images", "link.png"))
	vw := vaultwriter.New(root).Grant(vaultwriter.Capability{Name: "writing", Zone: record.ZoneSystem, Pattern: "system/writing/**", Actor: vaultwriter.ActorUserAction})
	s := New(nil, nil, nil)
	s.UseVault(vw)
	s.UseWriting("system/writing")
	h := s.Handler()
	get := func(url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		return w
	}
	var list struct{ Assets []struct{ Path, Kind string } }
	json.Unmarshal(get("/api/writing/assets").Body.Bytes(), &list)
	got := []string{}
	for _, a := range list.Assets {
		got = append(got, a.Path+":"+a.Kind)
	}
	if strings.Join(got, " ") != "data/table.csv:csv images/plan.png:image pic.svg:image" {
		t.Fatalf("assets = %v", got)
	}
	if w := get("/api/writing/asset?path=data/table.csv"); w.Code != 200 || w.Body.String() != "a,b\n1,2" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("csv: %d %q %q", w.Code, w.Body.String(), w.Header().Get("Content-Type"))
	}
	if w := get("/api/writing/asset?path=pic.svg"); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("svg: %d %v", w.Code, w.Header())
	}
	for url, code := range map[string]int{
		"/api/writing/asset?path=notes/x.exe":                                415,
		"/api/writing/asset?path=.hidden/secret.png":                         400,
		"/api/writing/asset?path=system/writing/r.txt":                       400,
		"/api/writing/asset?path=../" + filepath.Base(outside) + "/leak.png": 400,
		"/api/writing/asset?path=images/link.png":                            404,
		"/api/writing/asset?path=images/none.png":                            404,
	} {
		if w := get(url); w.Code != code {
			t.Errorf("%s = %d, want %d", url, w.Code, code)
		}
	}
}
