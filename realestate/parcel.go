package realestate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"manifest/mdfm"
	"manifest/record"
	"manifest/vaultindex"
)

// Parcel is one research parcel (categories: [parcel]) under
// system/realestate/parcels/ — St. Louis Assessor data pulled by cmd/parcel-pull
// for the acquisition-intel map layer. Facts come from frontmatter (refreshed on
// each re-pull); the `## log` holds the owner's own notes (preserved across
// re-pulls); the parcel polygon lives in the .geo.json sidecar. This layer is
// SEPARATE from owned properties/deals — read-only research, no money rollups.
type Parcel struct {
	Path      string            `json:"path"`
	Slug      string            `json:"slug"`
	Address   string            `json:"address"`
	Owner     string            `json:"owner"`
	Owner2    string            `json:"owner2,omitempty"`
	OwnerAddr string            `json:"ownerAddr"`
	SaleDate  string            `json:"saleDate,omitempty"` // ResSaleDate
	RecDate   string            `json:"recDate,omitempty"`  // RecDailyDate (deed recording)
	TaxBalDue float64           `json:"taxBalDue"`
	TaxStatus string            `json:"taxStatus"` // current | delinquent | lra
	Assessed  float64           `json:"assessed"`
	LandUse   string            `json:"landUse,omitempty"`
	ParcelID  string            `json:"parcelId,omitempty"`
	Handle    string            `json:"handle,omitempty"`
	CityBlock string            `json:"cityBlock,omitempty"`
	Ward      string            `json:"ward,omitempty"`
	Nbrhd     string            `json:"nbrhd,omitempty"`
	Lat       float64           `json:"lat,omitempty"`
	Lng       float64           `json:"lng,omitempty"`
	Log       []string          `json:"log"`
	LastLog   string            `json:"lastLog,omitempty"`
	Features  []json.RawMessage `json:"features,omitempty"` // parcel polygon (.geo.json)
}

// Parcels returns every research-parcel record, sorted by address (street then
// house number) for a stable spreadsheet. Each carries its polygon features.
func (s *Service) Parcels() ([]Parcel, error) {
	refs, err := s.ix.Category("parcel", vaultindex.SortNameAsc)
	if err != nil {
		return nil, err
	}
	out := make([]Parcel, 0, len(refs))
	for _, r := range refs {
		if p, ok := s.parseParcel(r.Path, r.Name); ok {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := streetKey(out[i].Address), streetKey(out[j].Address)
		if si != sj {
			return si < sj
		}
		return addrNum(out[i].Address) < addrNum(out[j].Address)
	})
	return out, nil
}

// GetParcel returns one parcel record, freshly parsed.
func (s *Service) GetParcel(slug string) (Parcel, bool) {
	for _, r := range mustRefs(s.ix.Category("parcel", vaultindex.SortNameAsc)) {
		if strings.EqualFold(r.Name, slug) {
			return s.parseParcel(r.Path, r.Name)
		}
	}
	return Parcel{}, false
}

func (s *Service) parseParcel(rel, name string) (Parcel, bool) {
	full := filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(rel))
	raw, err := os.ReadFile(full)
	if err != nil {
		return Parcel{}, false
	}
	fm, body := mdfm.Split(string(raw))
	p := Parcel{
		Path: rel, Slug: name,
		Address:   unquote(fm["address"]),
		Owner:     unquote(fm["owner"]),
		Owner2:    unquote(fm["owner2"]),
		OwnerAddr: unquote(fm["owner_addr"]),
		SaleDate:  strings.TrimSpace(fm["sale_date"]),
		RecDate:   strings.TrimSpace(fm["rec_date"]),
		TaxStatus: strings.ToLower(strings.TrimSpace(fm["tax_status"])),
		LandUse:   strings.TrimSpace(fm["land_use"]),
		ParcelID:  strings.TrimSpace(fm["parcel_id"]),
		Handle:    strings.TrimSpace(fm["handle"]),
		CityBlock: strings.TrimSpace(fm["cityblock"]),
		Ward:      strings.TrimSpace(fm["ward"]),
		Nbrhd:     strings.TrimSpace(fm["nbrhd"]),
	}
	p.TaxBalDue, _ = strconv.ParseFloat(strings.TrimSpace(fm["tax_bal_due"]), 64)
	p.Assessed, _ = strconv.ParseFloat(strings.TrimSpace(fm["assessed"]), 64)
	p.Lat, _ = strconv.ParseFloat(strings.TrimSpace(fm["lat"]), 64)
	p.Lng, _ = strconv.ParseFloat(strings.TrimSpace(fm["lng"]), 64)
	if p.TaxStatus == "" {
		p.TaxStatus = "current"
	}
	p.Log = parseLog(parseSections(body)["log"])
	if len(p.Log) > 0 {
		p.LastLog = p.Log[0]
	}
	if geo, err := os.ReadFile(record.Sidecar(full, record.SidecarGeo)); err == nil {
		p.Features = normalizeFeatures(geo)
	}
	return p, true
}

// streetKey orders the spreadsheet by street name (the address minus the house
// number), so Bayard / Euclid / Page group together.
func streetKey(addr string) string {
	f := strings.Fields(addr)
	if len(f) > 1 {
		return strings.ToLower(strings.Join(f[1:], " "))
	}
	return strings.ToLower(addr)
}

func addrNum(addr string) int {
	f := strings.Fields(addr)
	if len(f) == 0 {
		return 0
	}
	n := 0
	for _, r := range f[0] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
