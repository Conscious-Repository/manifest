package realestate

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The parcel STUDY layer — the full assessor snapshot the public
// ooda.group/parcels page renders (2,448 lots across Fountain Park, Lewis
// Place, and west of Kingshighway north of Page), read straight from the
// re-portal checkout's `public/study-parcels.geojson`.
//
// Why a file and not vault records: the 175 parcels under
// system/realestate/parcels/ are the ones the owner RESEARCHES — they carry a
// `## log` of his own notes and are re-pulled with those notes preserved.
// Turning the other ~2,270 into records would mean ~6,800 new files in the
// vault to hold facts nobody has annotated, and would bloat every index and
// mtime scan that walks the realestate root. Reference geography belongs
// beside bgParcels.json, which is exactly this pattern already.
//
// The projection is deliberately into the SAME Parcel struct the vault records
// use, so one client renders both layers with one popup builder. A study
// parcel has no Slug — that is the whole difference, and it is what tells the
// UI there is no record to append a note to.

// StudyMeta is the geojson's own provenance block (source + snapshot date),
// surfaced so the map can say how old the assessor data is.
type StudyMeta struct {
	Title        string            `json:"title,omitempty"`
	Source       string            `json:"source,omitempty"`
	SourceURL    string            `json:"source_url,omitempty"`
	SnapshotDate string            `json:"snapshot_date,omitempty"`
	Count        int               `json:"count,omitempty"`
	Areas        map[string]string `json:"areas,omitempty"`
}

type studyDoc struct {
	Metadata StudyMeta `json:"metadata"`
	Features []struct {
		Type       string          `json:"type"`
		Geometry   json.RawMessage `json:"geometry"`
		Properties struct {
			Address   string          `json:"address"`
			Owner     string          `json:"owner"`
			OwnerAddr string          `json:"owner_addr"`
			SaleDate  string          `json:"sale_date"`
			RecDate   string          `json:"rec_date"`
			TaxStatus string          `json:"tax_status"`
			TaxBalDue json.RawMessage `json:"tax_bal_due"`
			Assessed  json.RawMessage `json:"assessed"`
			LandUse   string          `json:"land_use"`
			ParcelID  string          `json:"parcel_id"`
			Handle    string          `json:"handle"`
			CityBlock string          `json:"cityblock"`
			Ward      string          `json:"ward"`
			Nbrhd     string          `json:"nbrhd"`
			Lat       float64         `json:"lat"`
			Lng       float64         `json:"lng"`
		} `json:"properties"`
	} `json:"features"`
}

// StudyLayer is one parsed snapshot: the parcels plus where they came from.
type StudyLayer struct {
	Meta    StudyMeta
	Parcels []Parcel
	// Key identifies this snapshot (path+size+mtime). The file lives OUTSIDE
	// the vault, so a caller caching a composed result against the vault's own
	// revision must fold this in or a refreshed study never reaches the page.
	Key string
}

// studyCache memoizes the parse against the file's mtime+size. Re-parsing 2,448
// polygons per request would cost more than composing the entire portfolio.
type studyCache struct {
	mu    sync.Mutex
	key   string
	layer *StudyLayer
}

var study studyCache

// LoadStudyParcels parses the study geojson, cached until the file changes.
// A missing file is NOT an error: the study layer is optional and its absence
// must degrade to "no extra parcels", never to a broken map.
func LoadStudyParcels(path string) (*StudyLayer, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return &StudyLayer{}, nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return &StudyLayer{}, nil
	}
	key := fmt.Sprintf("%s|%d|%d", path, st.Size(), st.ModTime().UnixNano())

	study.mu.Lock()
	defer study.mu.Unlock()
	if study.layer != nil && study.key == key {
		return study.layer, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return &StudyLayer{}, nil
	}
	var doc studyDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("study parcels: %w", err)
	}
	layer := &StudyLayer{Meta: doc.Metadata, Key: key, Parcels: make([]Parcel, 0, len(doc.Features))}
	for i := range doc.Features {
		f := &doc.Features[i]
		p := f.Properties
		// the client wants a GeoJSON Feature, and re-wrapping the geometry is
		// cheaper than carrying the source properties it never reads
		feat, err := json.Marshal(map[string]any{
			"type": "Feature", "geometry": json.RawMessage(f.Geometry),
			"properties": map[string]string{"id": p.ParcelID},
		})
		if err != nil {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(p.TaxStatus))
		if status == "" {
			status = "current"
		}
		layer.Parcels = append(layer.Parcels, Parcel{
			Address: p.Address, Owner: p.Owner, OwnerAddr: p.OwnerAddr,
			SaleDate: p.SaleDate, RecDate: p.RecDate,
			TaxStatus: status, TaxBalDue: studyNum(p.TaxBalDue), Assessed: studyNum(p.Assessed),
			LandUse: p.LandUse, ParcelID: strings.TrimSpace(p.ParcelID), Handle: p.Handle,
			CityBlock: p.CityBlock, Ward: p.Ward, Nbrhd: p.Nbrhd,
			Lat: p.Lat, Lng: p.Lng,
			Features: []json.RawMessage{feat},
		})
	}
	sort.SliceStable(layer.Parcels, func(i, j int) bool {
		si, sj := streetKey(layer.Parcels[i].Address), streetKey(layer.Parcels[j].Address)
		if si != sj {
			return si < sj
		}
		return addrNum(layer.Parcels[i].Address) < addrNum(layer.Parcels[j].Address)
	})
	study.key, study.layer = key, layer
	return layer, nil
}

// studyNum reads a number that the export writes as either a JSON number or a
// quoted string (the ArcGIS pull is not consistent about it, and a silent 0
// would read on the map as "owes nothing").
func studyNum(raw json.RawMessage) float64 {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return 0
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimPrefix(s, "$"), ",", ""), 64)
	if err != nil {
		return 0
	}
	return f
}

// StudyParcelsExcept returns the study layer minus any parcel already drawn by
// a richer layer — the owner's own research records (which carry his notes)
// and the lots we hold. One lot, one polygon.
func StudyParcelsExcept(layer *StudyLayer, drawn map[string]bool) []Parcel {
	if layer == nil {
		return nil
	}
	out := make([]Parcel, 0, len(layer.Parcels))
	for _, p := range layer.Parcels {
		if p.ParcelID != "" && drawn[p.ParcelID] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// StudyAge is a human "how old is this snapshot" for the map's legend.
func (m StudyMeta) StudyAge(now time.Time) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(m.SnapshotDate))
	if err != nil {
		return ""
	}
	days := int(now.Sub(t).Hours() / 24)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "1 day old"
	case days < 60:
		return strconv.Itoa(days) + " days old"
	default:
		return strconv.Itoa(days/30) + " months old"
	}
}
