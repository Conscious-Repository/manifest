package geocode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSearchPlacesNormalizesCachesAndNeverAutocompletes(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query().Get("q"))
		mu.Unlock()
		if r.URL.Query().Get("format") != "geocodejson" || r.URL.Query().Get("limit") != "5" {
			t.Fatalf("unexpected search params: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"features": []any{map[string]any{
			"geometry": map[string]any{"coordinates": []float64{-90.1994, 38.6270}},
			"properties": map[string]any{"geocoding": map[string]any{
				"type": "city", "name": "St. Louis", "city": "St. Louis", "state": "Missouri",
				"state_code": "US-MO", "country": "United States", "country_code": "us",
			}},
		}}})
	}))
	defer ts.Close()

	s := New(t.TempDir())
	s.base, s.interval = ts.URL, 0
	places, err := s.SearchPlaces(context.Background(), "St. Louis")
	if err != nil || len(places) != 1 {
		t.Fatalf("places=%+v err=%v", places, err)
	}
	if places[0].Label != "St. Louis, MO, US" || places[0].Attribution == "" {
		t.Fatalf("normalized place=%+v", places[0])
	}
	if _, err := s.SearchPlaces(context.Background(), "  st.   louis "); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(queries) != 1 || strings.Contains(strings.ToLower(queries[0]), "main st") {
		t.Fatalf("queries=%v; repeat should be cached and no address submitted", queries)
	}
}

func TestSharedLimiterAndLegacyPointImport(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "realestate", "geocode.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"old property":{"lat":1.25,"lng":-2.5}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	if lat, lng, ok := s.Cached("Old Property"); !ok || lat != 1.25 || lng != -2.5 {
		t.Fatalf("legacy point=(%v,%v,%v)", lat, lng, ok)
	}

	s.interval = 25 * time.Millisecond
	start := time.Now()
	if err := s.waitTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.waitTurn(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Fatalf("shared limiter did not gate consecutive requests: %v", elapsed)
	}
}
