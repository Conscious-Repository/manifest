package realestate

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"manifest/mdfm"
	"manifest/vaultindex"
)

// ParcelIntel is the read-time join from a property to the research-parcel
// layer (cmd/parcel-pull records at the same address): assessor owner + tax
// facts. Derived on read, never stored on the property — the parcel record is
// the source and each re-pull refreshes it.
type ParcelIntel struct {
	ParcelSlug string  `json:"parcelSlug"`
	Owner      string  `json:"owner,omitempty"`
	Owner2     string  `json:"owner2,omitempty"`
	OwnerAddr  string  `json:"ownerAddr,omitempty"`
	SaleDate   string  `json:"saleDate,omitempty"`
	TaxStatus  string  `json:"taxStatus,omitempty"`
	TaxBalDue  float64 `json:"taxBalDue,omitempty"`
	Assessed   float64 `json:"assessed,omitempty"`
}

// addrSuffixes are the street-type tails AddrKey drops so "751 Bayard Ave, St
// Louis…" and the assessor's "751 BAYARD AV" land on the same key.
var addrSuffixes = map[string]bool{
	"AV": true, "AVE": true, "AVENUE": true, "BLVD": true, "BOULEVARD": true,
	"ST": true, "STREET": true, "DR": true, "DRIVE": true, "CT": true,
	"PL": true, "RD": true, "ROAD": true, "TER": true, "LN": true,
}

// AddrKey normalizes an address to "NUMBER STREET" — uppercase, street part
// only (before the first comma), trailing street-type suffix dropped.
func AddrKey(addr string) string {
	street := strings.SplitN(addr, ",", 2)[0]
	f := strings.Fields(strings.ToUpper(street))
	for len(f) > 1 && addrSuffixes[strings.TrimRight(f[len(f)-1], ".")] {
		f = f[:len(f)-1]
	}
	return strings.Join(f, " ")
}

// parcelIntel scans research-parcel FRONTMATTER only (no geo sidecar reads —
// this runs on every properties list) into an address-keyed map for the join.
func (s *Service) parcelIntel() map[string]*ParcelIntel {
	refs, err := s.ix.Category("parcel", vaultindex.SortNameAsc)
	if err != nil {
		return nil
	}
	out := map[string]*ParcelIntel{}
	for _, r := range refs {
		raw, err := os.ReadFile(filepath.Join(s.ix.VaultRoot(), filepath.FromSlash(r.Path)))
		if err != nil {
			continue
		}
		fm, _ := mdfm.Split(string(raw))
		addr := unquote(fm["address"])
		if addr == "" {
			continue
		}
		pi := &ParcelIntel{
			ParcelSlug: r.Name,
			Owner:      unquote(fm["owner"]),
			Owner2:     unquote(fm["owner2"]),
			OwnerAddr:  unquote(fm["owner_addr"]),
			SaleDate:   strings.TrimSpace(fm["sale_date"]),
			TaxStatus:  strings.ToLower(strings.TrimSpace(fm["tax_status"])),
		}
		pi.TaxBalDue, _ = strconv.ParseFloat(strings.TrimSpace(fm["tax_bal_due"]), 64)
		pi.Assessed, _ = strconv.ParseFloat(strings.TrimSpace(fm["assessed"]), 64)
		out[AddrKey(addr)] = pi
	}
	return out
}
