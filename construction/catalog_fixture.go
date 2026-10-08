package construction

// Catalog fixture (testdata/roof-wall/catalog.json): fictional products that
// cite the synthetic fixture sources by locator and their passages by quote,
// resolved against a problem's evidence snapshot into AddProduct operations.
// Tests only; production has no product defaults.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type catalogFixture struct {
	SchemaVersion int              `json:"schemaVersion"`
	Notice        string           `json:"notice"`
	Products      []fixtureProduct `json:"products"`
}

type fixtureProduct struct {
	Key          string                    `json:"key"`
	Manufacturer string                    `json:"manufacturer"`
	Family       string                    `json:"family"`
	Model        string                    `json:"model"`
	Geography    string                    `json:"geography"`
	CheckedAt    string                    `json:"checkedAt"`
	MaterialKey  string                    `json:"materialKey"`
	Documents    []string                  `json:"documents"`
	Dimensions   map[string]DimensionInput `json:"dimensions"`
	Facts        []struct {
		Kind          string `json:"kind"`
		Text          string `json:"text"`
		EvidenceQuote string `json:"evidenceQuote,omitempty"`
	} `json:"facts"`
}

// CatalogFixtureOps reads the fixture and returns one AddProduct operation
// per product (ids minted here), keyed by fixture key.
func CatalogFixtureOps(dir string, b *EvidenceBundle) (map[string]map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		return nil, err
	}
	var f catalogFixture
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return nil, err
	}
	if !strings.Contains(strings.ToUpper(f.Notice), "SYNTHETIC") {
		return nil, fmt.Errorf("catalog fixture must declare a SYNTHETIC notice")
	}
	out := map[string]map[string]any{}
	for _, p := range f.Products {
		if !strings.Contains(strings.ToLower(p.Manufacturer), "fictional") && !strings.Contains(strings.ToLower(p.Manufacturer), "synthetic") {
			return nil, fmt.Errorf("fixture product %s is not visibly fictional", p.Key)
		}
		docs := []string{}
		for _, loc := range p.Documents {
			id := ""
			for _, s := range b.Sources {
				if s.Locator == loc {
					id = s.ID
				}
			}
			if id == "" {
				return nil, fmt.Errorf("fixture product %s: no retained source %s (run the fixture research first)", p.Key, loc)
			}
			docs = append(docs, id)
		}
		var facts []map[string]any
		for _, fc := range p.Facts {
			m := map[string]any{"kind": fc.Kind, "text": fc.Text}
			if fc.EvidenceQuote != "" {
				for _, e := range b.Evidence {
					if NormalizeQuote(e.Quote) == NormalizeQuote(fc.EvidenceQuote) {
						m["evidenceId"] = e.ID
					}
				}
				if m["evidenceId"] == nil {
					return nil, fmt.Errorf("fixture product %s: no evidence quoting %q", p.Key, truncate(fc.EvidenceQuote, 40))
				}
			}
			facts = append(facts, m)
		}
		op := map[string]any{"op": "AddProduct", "id": NewID(KindProduct), "manufacturer": p.Manufacturer, "family": p.Family, "model": p.Model,
			"geography": p.Geography, "checkedAt": p.CheckedAt, "documents": docs, "facts": facts, "dimensions": p.Dimensions, "fictional": true}
		if p.MaterialKey != "" {
			op["material"] = genericMaterialID(p.MaterialKey)
		}
		out[p.Key] = op
	}
	return out, nil
}
