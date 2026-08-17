// Package geocode provides the app's single, rate-limited Nominatim client.
// Coordinates are disposable derived state under dataDir; canonical facts stay
// in their owning vault records.
package geocode

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const attribution = "Place data © OpenStreetMap contributors"

// Point is one cached geocode. A zero point is a remembered miss.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Place is a normalized city/place candidate. Label is the canonical value
// written to a contact note; coordinates remain derived in this cache.
type Place struct {
	Label       string  `json:"label"`
	Locality    string  `json:"locality"`
	Region      string  `json:"region,omitempty"`
	Country     string  `json:"country,omitempty"`
	CountryCode string  `json:"countryCode"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	Attribution string  `json:"attribution"`
}

type diskCache struct {
	Points  map[string]Point   `json:"points"`
	Places  map[string]Place   `json:"places"`
	Queries map[string][]Place `json:"queries"`
}

type job struct {
	query string
	place bool
}

// Service coordinates every property and contact lookup through one request
// limiter, so separate app features cannot collectively exceed the provider's
// one-request-per-second ceiling.
type Service struct {
	cachePath  string
	legacyPath string
	base       string
	hc         *http.Client
	interval   time.Duration

	mu      sync.Mutex
	cache   diskCache
	pending map[string]bool
	queue   chan job
	started bool

	requestMu sync.Mutex
	last      time.Time
}

// New loads the shared cache and imports the former real-estate-only point
// cache when necessary. The legacy file is derived state and is left in place.
func New(dataDir string) *Service {
	s := &Service{
		cachePath:  filepath.Join(dataDir, "geocode.json"),
		legacyPath: filepath.Join(dataDir, "realestate", "geocode.json"),
		base:       "https://nominatim.openstreetmap.org",
		hc:         &http.Client{Timeout: 15 * time.Second},
		interval:   1100 * time.Millisecond,
		cache: diskCache{
			Points: map[string]Point{}, Places: map[string]Place{}, Queries: map[string][]Place{},
		},
		pending: map[string]bool{},
		queue:   make(chan job, 64),
	}
	if b, err := os.ReadFile(s.cachePath); err == nil {
		_ = json.Unmarshal(b, &s.cache)
	} else if b, legacyErr := os.ReadFile(s.legacyPath); legacyErr == nil {
		_ = json.Unmarshal(b, &s.cache.Points)
		if len(s.cache.Points) > 0 {
			s.saveLocked()
		}
	}
	if s.cache.Points == nil {
		s.cache.Points = map[string]Point{}
	}
	if s.cache.Places == nil {
		s.cache.Places = map[string]Place{}
	}
	if s.cache.Queries == nil {
		s.cache.Queries = map[string][]Place{}
	}
	return s
}

// Cached returns a property/address point already resolved by the shared cache.
func (s *Service) Cached(query string) (lat, lng float64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, hit := s.cache.Points[key(query)]
	if !hit || (p.Lat == 0 && p.Lng == 0) {
		return 0, 0, false
	}
	return p.Lat, p.Lng, true
}

// Enqueue schedules an address lookup used by the property map.
func (s *Service) Enqueue(query string) { s.enqueue(job{query: query}) }

// CachedPlace returns a previously selected/resolved locality centroid.
func (s *Service) CachedPlace(label string) (Place, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.cache.Places[key(label)]
	return p, ok && p.Label != ""
}

// PlaceCentroid is the narrow read interface consumed by the contacts layer.
func (s *Service) PlaceCentroid(label string) (lat, lng float64, ok bool) {
	p, ok := s.CachedPlace(label)
	if !ok {
		return 0, 0, false
	}
	return p.Lat, p.Lng, true
}

// EnqueuePlace schedules resolution of a canonical location found through a
// hand edit or after a disposable cache is rebuilt.
func (s *Service) EnqueuePlace(label string) { s.enqueue(job{query: label, place: true}) }

func (s *Service) enqueue(j job) {
	k := jobKey(j)
	if key(j.query) == "" {
		return
	}
	s.mu.Lock()
	if j.place {
		if _, ok := s.cache.Places[key(j.query)]; ok {
			s.mu.Unlock()
			return
		}
	} else if _, ok := s.cache.Points[key(j.query)]; ok {
		s.mu.Unlock()
		return
	}
	if s.pending[k] {
		s.mu.Unlock()
		return
	}
	s.pending[k] = true
	if !s.started {
		s.started = true
		go s.worker()
	}
	select {
	case s.queue <- j:
	default:
		delete(s.pending, k)
	}
	s.mu.Unlock()
}

func (s *Service) worker() {
	for j := range s.queue {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if j.place {
			_, _ = s.searchPlaces(ctx, j.query, false)
		} else {
			p, _ := s.resolvePoint(ctx, j.query)
			s.mu.Lock()
			s.cache.Points[key(j.query)] = p
			s.saveLocked()
			s.mu.Unlock()
		}
		cancel()
		s.mu.Lock()
		delete(s.pending, jobKey(j))
		s.mu.Unlock()
	}
}

// SearchPlaces performs one explicit city/place search. Callers invoke this
// only on form submission, never per keystroke.
func (s *Service) SearchPlaces(ctx context.Context, query string) ([]Place, error) {
	return s.searchPlaces(ctx, query, true)
}

func (s *Service) searchPlaces(ctx context.Context, query string, useQueryCache bool) ([]Place, error) {
	qk := key(query)
	if qk == "" {
		return nil, errors.New("place query is required")
	}
	s.mu.Lock()
	if cached, ok := s.cache.Queries[qk]; useQueryCache && ok {
		out := append([]Place(nil), cached...)
		s.mu.Unlock()
		return out, nil
	}
	s.mu.Unlock()

	if err := s.waitTurn(ctx); err != nil {
		return nil, err
	}
	params := url.Values{"q": {strings.TrimSpace(query)}, "format": {"geocodejson"}, "addressdetails": {"1"}, "limit": {"5"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/search?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "manifest-dashboard/1.0 (private personal contacts)")
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("place lookup failed: " + resp.Status)
	}
	var raw geocodeJSON
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	places := normalizeFeatures(raw.Features)
	s.mu.Lock()
	s.cache.Queries[qk] = append([]Place(nil), places...)
	for _, p := range places {
		s.cache.Places[key(p.Label)] = p
	}
	if len(places) > 0 {
		s.cache.Places[qk] = places[0]
	}
	s.saveLocked()
	s.mu.Unlock()
	return places, nil
}

func (s *Service) resolvePoint(ctx context.Context, query string) (Point, error) {
	if err := s.waitTurn(ctx); err != nil {
		return Point{}, err
	}
	params := url.Values{"q": {strings.TrimSpace(query)}, "format": {"jsonv2"}, "limit": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/search?"+params.Encode(), nil)
	if err != nil {
		return Point{}, err
	}
	req.Header.Set("User-Agent", "manifest-dashboard/1.0 (local personal cockpit)")
	resp, err := s.hc.Do(req)
	if err != nil {
		return Point{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Point{}, errors.New("geocode failed: " + resp.Status)
	}
	var out []struct {
		Lat floatString `json:"lat"`
		Lon floatString `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out) == 0 {
		return Point{}, err
	}
	return Point{Lat: float64(out[0].Lat), Lng: float64(out[0].Lon)}, nil
}

func (s *Service) waitTurn(ctx context.Context) error {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	if wait := s.interval - time.Since(s.last); !s.last.IsZero() && wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	s.last = time.Now()
	return nil
}

func (s *Service) saveLocked() {
	b, err := json.MarshalIndent(s.cache, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.cachePath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(s.cachePath, b, 0o644)
}

func key(v string) string { return strings.ToLower(strings.Join(strings.Fields(v), " ")) }
func jobKey(j job) string {
	kind := "point:"
	if j.place {
		kind = "place:"
	}
	return kind + key(j.query)
}

type floatString float64

func (f *floatString) UnmarshalJSON(b []byte) error {
	var s string
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
	} else {
		s = strings.TrimSpace(string(b))
	}
	n, err := strconv.ParseFloat(s, 64)
	*f = floatString(n)
	return err
}

type geocodeJSON struct {
	Features []geoFeature `json:"features"`
}
type geoFeature struct {
	Geometry struct {
		Coordinates []float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties struct {
		Geocoding geoProperties `json:"geocoding"`
	} `json:"properties"`
}
type geoProperties struct {
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Label       string            `json:"label"`
	City        string            `json:"city"`
	Locality    string            `json:"locality"`
	State       string            `json:"state"`
	StateCode   string            `json:"state_code"`
	Country     string            `json:"country"`
	CountryCode string            `json:"country_code"`
	Admin       map[string]string `json:"admin"`
}

func normalizeFeatures(features []geoFeature) []Place {
	allowed := map[string]bool{
		"city": true, "town": true, "village": true, "municipality": true,
		"administrative": true, "state": true, "region": true, "county": true,
	}
	seen := map[string]bool{}
	out := make([]Place, 0, len(features))
	for _, f := range features {
		g := f.Properties.Geocoding
		if len(f.Geometry.Coordinates) < 2 || (!allowed[strings.ToLower(g.Type)] && g.City == "" && g.Locality == "") {
			continue // never offer houses, streets, POIs, or malformed results
		}
		locality := first(g.City, g.Locality, g.Name)
		if locality == "" {
			continue
		}
		regionCode := g.StateCode
		if regionCode == "" {
			for k, v := range g.Admin {
				if strings.Contains(strings.ToUpper(k), "ISO3166-2") {
					regionCode = v
					break
				}
			}
		}
		countryCode := strings.ToUpper(strings.TrimSpace(g.CountryCode))
		if i := strings.Index(regionCode, "-"); i >= 0 && strings.EqualFold(regionCode[:i], countryCode) {
			regionCode = regionCode[i+1:]
		}
		region := first(regionCode, g.State)
		parts := uniqueParts(locality, region, countryCode)
		label := strings.Join(parts, ", ")
		if label == "" {
			label = strings.TrimSpace(g.Label)
		}
		lk := key(label)
		if lk == "" || seen[lk] {
			continue
		}
		seen[lk] = true
		out = append(out, Place{
			Label: label, Locality: locality, Region: region, Country: g.Country,
			CountryCode: countryCode, Lat: f.Geometry.Coordinates[1], Lng: f.Geometry.Coordinates[0],
			Attribution: attribution,
		})
	}
	return out
}

func first(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

func uniqueParts(xs ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		x = strings.TrimSpace(x)
		k := strings.ToLower(x)
		if x != "" && !seen[k] {
			seen[k] = true
			out = append(out, x)
		}
	}
	return out
}
