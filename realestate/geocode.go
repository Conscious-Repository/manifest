package realestate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Geocoder pins records that have no parcel polygon: a one-time OSM Nominatim
// lookup per address, cached forever in <dataDir>/realestate/geocode.json —
// NEVER written to the vault. Standalone by design (the portals machinery is
// api-key/poll-shaped; this is keyless request/response). Nominatim policy:
// 1 req/s max + a descriptive User-Agent, both enforced here.
type Geocoder struct {
	cachePath string
	base      string // test override (default nominatim.openstreetmap.org)
	hc        *http.Client

	mu      sync.Mutex
	cache   map[string]geoPoint // lower(address) → point ({0,0} = known miss)
	pending map[string]bool
	queue   chan string
	started bool
}

type geoPoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func NewGeocoder(dataDir string) *Geocoder {
	g := &Geocoder{
		cachePath: filepath.Join(dataDir, "realestate", "geocode.json"),
		base:      "https://nominatim.openstreetmap.org",
		hc:        &http.Client{Timeout: 15 * time.Second},
		cache:     map[string]geoPoint{},
		pending:   map[string]bool{},
		queue:     make(chan string, 64),
	}
	if b, err := os.ReadFile(g.cachePath); err == nil {
		_ = json.Unmarshal(b, &g.cache)
	}
	return g
}

// Cached returns the cached pin for an address ("known miss" returns ok=false).
func (g *Geocoder) Cached(addr string) (lat, lng float64, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, hit := g.cache[geoKey(addr)]
	if !hit || (p.Lat == 0 && p.Lng == 0) {
		return 0, 0, false
	}
	return p.Lat, p.Lng, true
}

// Enqueue schedules a background resolve for an address not yet in the cache.
// The /geo request itself never waits on the network — pins appear next load.
func (g *Geocoder) Enqueue(addr string) {
	key := geoKey(addr)
	if key == "" {
		return
	}
	g.mu.Lock()
	_, cached := g.cache[key]
	already := g.pending[key]
	if !cached && !already {
		g.pending[key] = true
		if !g.started {
			g.started = true
			go g.worker()
		}
		select {
		case g.queue <- addr:
		default: // queue full — drop; it re-enqueues on the next /geo call
			delete(g.pending, key)
		}
	}
	g.mu.Unlock()
}

// worker drains the queue at Nominatim's 1 req/s ceiling.
func (g *Geocoder) worker() {
	tick := time.NewTicker(1100 * time.Millisecond)
	defer tick.Stop()
	for addr := range g.queue {
		<-tick.C
		lat, lng := g.resolve(addr)
		g.mu.Lock()
		g.cache[geoKey(addr)] = geoPoint{Lat: lat, Lng: lng} // {0,0} caches a miss
		delete(g.pending, geoKey(addr))
		g.save()
		g.mu.Unlock()
	}
}

func (g *Geocoder) resolve(addr string) (lat, lng float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	q := url.Values{"q": {addr}, "format": {"json"}, "limit": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.base+"/search?"+q.Encode(), nil)
	if err != nil {
		return 0, 0
	}
	// descriptive UA per Nominatim usage policy
	req.Header.Set("User-Agent", "manifest-dashboard/1.0 (local personal cockpit)")
	resp, err := g.hc.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0
	}
	var out []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out) == 0 {
		return 0, 0
	}
	la, _ := strconv.ParseFloat(out[0].Lat, 64)
	ln, _ := strconv.ParseFloat(out[0].Lon, 64)
	return la, ln
}

func (g *Geocoder) save() {
	b, err := json.MarshalIndent(g.cache, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(g.cachePath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(g.cachePath, b, 0o644)
}

func geoKey(addr string) string { return strings.ToLower(strings.Join(strings.Fields(addr), " ")) }
