package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/contacts"
	"manifest/geocode"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

func TestPrivateContactLocationEndpoints(t *testing.T) {
	vault := t.TempDir()
	note := "---\ncategories: [people]\n---\nfriend\n"
	if err := os.WriteFile(filepath.Join(vault, "alice.md"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	store, err := contacts.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := contacts.New(ix, store, vaultwriter.New(vault), nil, nil)

	dataDir := t.TempDir()
	cache := `{"points":{},"places":{"st. louis, mo, us":{"label":"St. Louis, MO, US","locality":"St. Louis","region":"MO","country":"United States","countryCode":"US","lat":38.627,"lng":-90.1994,"attribution":"Place data © OpenStreetMap contributors"}},"queries":{}}`
	if err := os.WriteFile(filepath.Join(dataDir, "geocode.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &Server{contacts: svc, geocoder: geocode.New(dataDir)}
	h := srv.Handler()

	body := []byte(`{"key":"alice","display":"Alice","location":"St. Louis, MO, US","address":"123 Private St"}`)
	put := httptest.NewRequest(http.MethodPut, "/api/contacts/location", bytes.NewReader(body))
	put.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	h.ServeHTTP(putRec, put)
	if putRec.Code != http.StatusOK || !strings.Contains(putRec.Body.String(), "123 Private St") {
		t.Fatalf("PUT %d: %s", putRec.Code, putRec.Body.String())
	}

	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/contacts", nil))
	if listRec.Code != http.StatusOK || strings.Contains(listRec.Body.String(), "123 Private St") || !strings.Contains(listRec.Body.String(), "St. Louis, MO, US") {
		t.Fatalf("list leaked address or omitted location: %s", listRec.Body.String())
	}

	nearRec := httptest.NewRecorder()
	h.ServeHTTP(nearRec, httptest.NewRequest(http.MethodGet, "/api/contacts/nearby?lat=38.627&lng=-90.1994&radiusMiles=50", nil))
	if nearRec.Code != http.StatusOK || strings.Contains(nearRec.Body.String(), "123 Private St") {
		t.Fatalf("nearby %d leaked address: %s", nearRec.Code, nearRec.Body.String())
	}
	var nearby contacts.NearbyResult
	if err := json.Unmarshal(nearRec.Body.Bytes(), &nearby); err != nil || len(nearby.Contacts) != 1 {
		t.Fatalf("nearby=%+v err=%v", nearby, err)
	}

	delRec := httptest.NewRecorder()
	h.ServeHTTP(delRec, httptest.NewRequest(http.MethodDelete, "/api/contacts/location?key=alice", nil))
	if delRec.Code != http.StatusOK || strings.Contains(delRec.Body.String(), "St. Louis") {
		t.Fatalf("DELETE %d: %s", delRec.Code, delRec.Body.String())
	}
}
