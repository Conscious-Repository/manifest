package construction

// The problem-local, evidence-pinned catalog (§6, §12.6). It starts with
// GENERIC material families only: typed property slots whose values are
// unknown until a source supplies them — no default catalog fiction, no
// conductivities or corrosion data invented here. Appearance is visual only
// and never changes identity or specification. Actual manufacturer products
// enter only with a checked source (AddProduct); none is assumed.

type genericMaterial struct {
	Key        string
	ID         string
	Family     string
	Name       string
	Properties []string // typed slots, all unknown until sourced
	Unit       map[string]string
	Appearance Appearance
	Limits     []string
}

// Generic material ids are fixed constants (assigned once, not derived), so
// every problem's catalog pins the same generic identity.
var genericMaterials = []genericMaterial{
	{"timber-rafter", "mat-54a2085573b0fddb3d7166c72678fd04", "timber", "Timber rafter (generic, exposed)", []string{"strengthClass", "moistureContent", "density"}, nil, Appearance{"#b98a5a", 0.75, 0, 1}, []string{"exposed: finish and moisture protection required"}},
	{"timber-decking", "mat-cc52455339262921e512d564ed304da0", "timber", "Finish-grade timber decking (generic)", []string{"thermalConductivity", "density", "moistureContent"}, nil, Appearance{"#c9a173", 0.7, 0, 1}, []string{"underside exposed to the interior"}},
	{"timber-batten", "mat-165922dd8c11523e82bb1b30539bebef", "timber", "Timber batten (generic)", []string{"strengthClass", "preservativeTreatment"}, nil, Appearance{"#a8794c", 0.8, 0, 1}, nil},
	{"membrane-avcl", "mat-46418a4524b7e33bc17da08b6f2eaa4b", "membrane", "Air/vapour-control membrane (generic candidate)", []string{"vapourResistance", "airPermeance"}, nil, Appearance{"#5b7fa6", 0.6, 0, 1}, []string{"placement is climate-dependent; no hygrothermal analysis performed"}},
	{"membrane-underlayment", "mat-ad4e640a94748418b55bc0f60b10d32f", "underlayment", "Roof underlayment / waterproofing (generic)", []string{"vapourResistance", "temperatureRating", "uvExposureLimit"}, nil, Appearance{"#3f4a55", 0.7, 0, 1}, nil},
	{"insulation-rigid", "mat-3578699b5afc640eebc010496a000e57", "insulation", "Rigid above-deck insulation (generic)", []string{"thermalConductivity", "compressiveStrength", "vapourResistance"}, nil, Appearance{"#e8d27a", 0.9, 0, 1}, []string{"fastener length and thermal bridging depend on thickness"}},
	{"metal-corrugated", "mat-03437200a18e1357c4ea093d35d40b47", "corrugated-metal-roof", "Corrugated metal roof sheet (generic)", []string{"baseMetal", "coating", "minimumPitch", "thermalExpansion"}, map[string]string{"minimumPitch": "deg"}, Appearance{"#9aa3ab", 0.35, 0.85, 1}, []string{"minimum pitch and lap rules are manufacturer-specific"}},
	{"metal-standing-seam", "mat-fc62e64fe87d2cc035e40c70cd635609", "standing-seam-roof", "Standing-seam metal roof (generic)", []string{"baseMetal", "coating", "minimumPitch"}, map[string]string{"minimumPitch": "deg"}, Appearance{"#8e979e", 0.35, 0.85, 1}, nil},
	{"metal-flashing", "mat-6101962ed1b3bff85298aa981d511002", "sheet-flashing", "Sheet metal flashing (generic)", []string{"baseMetal", "coating", "galvanicCompatibility"}, nil, Appearance{"#7d8890", 0.4, 0.8, 1}, []string{"galvanic compatibility with the roof sheet is unknown until both metals are sourced"}},
	{"closure-foam", "mat-b74f6f1805c315705850c3a0c22aa166", "closure", "Profile closure (generic)", []string{"uvExposureLimit", "compressionSet"}, nil, Appearance{"#2f3337", 0.95, 0, 1}, nil},
	{"sealant-generic", "mat-dee84e5fd4cee4340fb81f70d251b1da", "sealant", "Sealant (generic)", []string{"movementCapability", "substrateCompatibility", "serviceLife"}, nil, Appearance{"#d9d4c7", 0.6, 0, 1}, []string{"a sealant joint is a maintenance item, not a primary water barrier"}},
	{"fastener-steel", "mat-e0db64385261618d01bd24a09856d93e", "fastener", "Fastener (generic)", []string{"pullOutCapacity", "shearCapacity", "corrosionClass"}, nil, Appearance{"#5a5f63", 0.3, 0.9, 1}, []string{"structural capacity unverified: qualified review required"}},
	{"masonry-brick", "mat-bf4a6dedee59a4ce28f6ba64a4ba9a71", "masonry", "Brick masonry (generic)", []string{"compressiveStrength", "waterAbsorption", "condition"}, nil, Appearance{"#a4553d", 0.9, 0, 1}, []string{"condition and permissible cutting unknown"}},
	{"mortar-generic", "mat-7e515034bf406e8b9c7207815b1057a9", "mortar", "Mortar (generic)", []string{"designation", "condition"}, nil, Appearance{"#bdb5a6", 0.95, 0, 1}, nil},
	{"sheathing-generic", "mat-eb2c5086266053355fd3d245ee80e7a5", "sheathing", "Sheathing (generic)", []string{"thickness", "vapourResistance"}, nil, Appearance{"#c2a37a", 0.8, 0, 1}, nil},
	{"cavity-air", "mat-48269e664a799241401bfc194adb5bce", "void", "Masonry cavity (air space)", nil, nil, Appearance{"#7fb2d9", 0.5, 0, 0.18}, nil},
}

var genericByKey = func() map[string]genericMaterial {
	m := map[string]genericMaterial{}
	for _, g := range genericMaterials {
		m[g.Key] = g
	}
	return m
}()

func genericMaterialID(key string) string { return genericByKey[key].ID }

// GenericMaterials returns the generic catalog entries (fresh copies).
func GenericMaterials() []Material {
	out := make([]Material, 0, len(genericMaterials))
	for _, g := range genericMaterials {
		props := map[string]TechnicalValue{}
		for _, p := range g.Properties {
			unit := ""
			if g.Unit != nil {
				unit = g.Unit[p]
			}
			props[p] = TechnicalValue{Value: nil, Unit: unit, Provenance: ProvUnknown, Note: "unknown until a checked source supplies it"}
		}
		limits := append([]string{}, g.Limits...)
		out = append(out, Material{ID: g.ID, Revision: 1, Family: g.Family, Name: g.Name, Generic: true,
			Properties: props, Limits: limits, Compatibility: []CompatibilityNote{}, Appearance: g.Appearance,
			Unknowns: append([]string{}, g.Properties...)})
	}
	return out
}

func init() {
	newCatalog = func(problemID string) *Catalog {
		return &Catalog{Envelope: Envelope{SchemaVersion: SchemaVersion, Kind: DocCatalog, ID: problemID},
			ProblemID: problemID, Materials: GenericMaterials(), Products: []Product{}}
	}
}

// material finds a catalog material pinned at a revision (the current one or
// an earlier version kept in its history).
func (c *Catalog) material(id string, rev int) (Material, bool) {
	if c == nil {
		return Material{}, false
	}
	for _, m := range c.Materials {
		if m.ID != id {
			continue
		}
		if m.Revision == rev {
			return m, true
		}
		for _, h := range m.History {
			if h.Revision == rev {
				return Material{ID: m.ID, Revision: h.Revision, Family: m.Family, Name: h.Name, Generic: m.Generic,
					Properties: h.Properties, Appearance: h.Appearance, Limits: m.Limits, Compatibility: m.Compatibility, Unknowns: m.Unknowns}, true
			}
		}
	}
	return Material{}, false
}

func (c *Catalog) product(id string, rev int) (Product, bool) {
	if c == nil {
		return Product{}, false
	}
	for _, p := range c.Products {
		if p.ID == id && p.Revision == rev {
			return p, true
		}
	}
	return Product{}, false
}

// appearanceFor resolves a component's visual appearance (pinned material,
// else the generic default by key).
func appearanceFor(cat *Catalog, c *Component) Appearance {
	if c.Material != nil {
		if m, ok := cat.material(c.Material.ID, c.Material.Revision); ok {
			return m.Appearance
		}
	}
	if g, ok := genericByKey[c.Appearance]; ok {
		return g.Appearance
	}
	return Appearance{Color: "#9a9a9a", Roughness: 0.8, Opacity: 1}
}
