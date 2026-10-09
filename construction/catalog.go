package construction

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

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
	{"steel-channel", "mat-5c1e7a0f2b9d4e86a3f04c7d1b2e9a61", "structural-steel", "Steel channel (generic, e.g. 2C3×3.5)", []string{"grade", "coating", "fireProtection"}, nil, Appearance{"#6c7379", 0.45, 0.85, 1}, []string{"structural capacity, connections and corrosion protection unverified: qualified review required"}},
	{"steel-beam", "mat-9e3b2d71c4a84f05b6e1d0a27f58c3e4", "structural-steel", "Steel wide-flange beam (generic, e.g. W8×13)", []string{"grade", "coating", "fireProtection"}, nil, Appearance{"#5f666c", 0.45, 0.85, 1}, []string{"structural capacity, bearing and connections unverified: qualified review required"}},
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
	// a generic material added after this problem's catalog was made is
	// still the same generic identity: resolve it from the generic list
	if rev == 1 {
		for _, g := range GenericMaterials() {
			if g.ID == id {
				return g, true
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

// ---- products, material properties and substitution (P6) -------------------------------

// MaterialFamilies are the catalog families every problem starts with.
func MaterialFamilies() []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range genericMaterials {
		if !seen[g.Family] {
			seen[g.Family] = true
			out = append(out, g.Family)
		}
	}
	sort.Strings(out)
	return out
}

func knownFamily(f string) bool {
	for _, g := range genericMaterials {
		if g.Family == f {
			return true
		}
	}
	return false
}

// propertyUnits are the units a typed material property may be stated in.
// A property outside this table cannot be set (no free-form technical data).
var propertyUnits = map[string][]string{
	"thermalConductivity": {"W/(m·K)"}, "density": {"kg/m3"}, "moistureContent": {"%"}, "compressiveStrength": {"kPa", "MPa"},
	"vapourResistance": {"MN·s/g", "m (sd)"}, "airPermeance": {"m3/(m2·h·Pa)"}, "minimumPitch": {"deg"}, "thermalExpansion": {"mm/(m·K)"},
	"temperatureRating": {"°C"}, "uvExposureLimit": {"days"}, "movementCapability": {"%"}, "serviceLife": {"years"},
	"pullOutCapacity": {"kN"}, "shearCapacity": {"kN"}, "waterAbsorption": {"%"}, "thickness": {"mm"}, "compressionSet": {"%"},
	"strengthClass": {"class"}, "preservativeTreatment": {"class"}, "baseMetal": {"designation"}, "coating": {"designation"},
	"galvanicCompatibility": {"rating"}, "corrosionClass": {"class"}, "substrateCompatibility": {"rating"}, "condition": {"rating"}, "designation": {"designation"},
}

// PropertyUnits returns the typed material property units (a copy).
func PropertyUnits() map[string][]string {
	out := make(map[string][]string, len(propertyUnits))
	for k, v := range propertyUnits {
		out[k] = append([]string{}, v...)
	}
	return out
}

// productDimensionUnits: product dimensions are lengths (stored in mm) or a
// pitch in degrees.
var productDimensionKeys = map[string]string{"thickness": "length", "width": "length", "height": "length", "length": "length", "depth": "length",
	"coverWidth": "length", "profileDepth": "length", "pitch": "angle", "minimumPitch": "angle"}

// ProductFactInput is a fact as entered: verification is derived from its
// evidence, never asserted.
type ProductFactInput struct {
	Kind       string `json:"kind"` // dimension | compatibility | installation | limit
	Text       string `json:"text"`
	EvidenceID string `json:"evidenceId,omitempty"`
}

// DimensionInput is a product dimension as entered.
type DimensionInput struct {
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	EvidenceID string  `json:"evidenceId,omitempty"`
}

var productFactKinds = map[string]bool{"dimension": true, "compatibility": true, "installation": true, "limit": true}
var productLifecycles = map[string]bool{"active": true, "stale": true, "withdrawn": true, "unknown": true}

// productSpec is the shared body of AddProduct/UpdateProduct.
type productSpec struct {
	Manufacturer string                    `json:"manufacturer"`
	Family       string                    `json:"family"`
	Model        string                    `json:"model"`
	SKU          string                    `json:"sku,omitempty"`
	Geography    string                    `json:"geography"`
	CheckedAt    string                    `json:"checkedAt"`
	Options      []string                  `json:"options,omitempty"`
	Material     string                    `json:"material,omitempty"`
	Dimensions   map[string]DimensionInput `json:"dimensions,omitempty"`
	Documents    []string                  `json:"documents"`
	Facts        []ProductFactInput        `json:"facts,omitempty"`
	// Fictional marks a synthetic product; a product documented by a
	// fictional source is fictional whatever this says.
	Fictional bool `json:"fictional,omitempty"`
}

func (p *productSpec) check() []string {
	out := checkText("manufacturer", p.Manufacturer, 200, true)
	out = append(out, checkText("model", p.Model, 200, true)...)
	out = append(out, checkText("sku", p.SKU, 100, false)...)
	out = append(out, checkText("geography", p.Geography, 200, true)...)
	if _, err := time.Parse("2006-01-02", p.CheckedAt); err != nil {
		out = append(out, "checkedAt must be the date the documents were checked (YYYY-MM-DD)")
	}
	if !knownFamily(p.Family) {
		out = append(out, "family must be one of "+strings.Join(MaterialFamilies(), ", "))
	}
	if p.Material != "" && !ValidID(KindMaterial, p.Material) {
		out = append(out, "material must be a mat- id")
	}
	if len(p.Options) > 32 || len(p.Facts) > 64 || len(p.Documents) > 16 || len(p.Dimensions) > 16 {
		out = append(out, "too many options/facts/documents/dimensions")
	}
	for _, o := range p.Options {
		out = append(out, checkText("option", o, 200, true)...)
	}
	for _, d := range p.Documents {
		if !ValidID(KindSource, d) {
			out = append(out, "documents are src- ids of retained sources")
		}
	}
	for _, f := range p.Facts {
		if !productFactKinds[f.Kind] {
			out = append(out, "fact kind must be dimension, compatibility, installation or limit")
		}
		out = append(out, checkText("fact", f.Text, 1000, true)...)
		if f.EvidenceID != "" && !ValidID(KindEvidence, f.EvidenceID) {
			out = append(out, "fact evidenceId must be an evd- id")
		}
	}
	for k, d := range p.Dimensions {
		kind, ok := productDimensionKeys[k]
		if !ok {
			out = append(out, "dimension "+k+" is not recognised")
			continue
		}
		if !finite(d.Value) || d.Value <= 0 {
			out = append(out, "dimension "+k+" must be a positive finite number")
		}
		if kind == "angle" && d.Unit != "deg" {
			out = append(out, "dimension "+k+" is stated in deg")
		}
		if kind == "length" {
			if _, err := ToMM(1, d.Unit); err != nil {
				out = append(out, "dimension "+k+": "+err.Error())
			}
		}
		if d.EvidenceID != "" && !ValidID(KindEvidence, d.EvidenceID) {
			out = append(out, "dimension evidenceId must be an evd- id")
		}
	}
	return out
}

// build derives a product revision from the spec against the problem's
// evidence: documents pin their retained source revisions; a fact or
// dimension is verified only by verified evidence quoted from one of the
// product's own documents and not tagged for a different model.
func (p *productSpec) build(id string, rev int, ev *EvidenceBundle, cat *Catalog, now time.Time) (Product, error) {
	pr := Product{ID: id, Revision: rev, Manufacturer: strings.TrimSpace(p.Manufacturer), Family: p.Family, Model: strings.TrimSpace(p.Model), SKU: p.SKU,
		Dimensions: map[string]TechnicalValue{}, Options: nonNilStrings(p.Options), Materials: []PinRef{}, Facts: []ProductFact{}, Documents: []ProductDocument{},
		Geography: p.Geography, CheckedAt: p.CheckedAt, Lifecycle: "active", Fictional: p.Fictional}
	docs := map[string]bool{}
	revs := []string{}
	for _, sid := range p.Documents {
		s, _ := ev.source(sid)
		if s == nil {
			return pr, NotFound("no source " + sid + " in this problem")
		}
		docs[sid] = true
		pr.Documents = append(pr.Documents, ProductDocument{SourceID: sid, Title: s.Title, URL: s.URL, Revision: s.ContentHash})
		if s.Fictional {
			pr.Fictional = true
		}
		if s.ContentHash == "" {
			pr.Lifecycle = "unknown" // a document that was not retained has not been checked
		} else {
			revs = append(revs, s.ContentHash)
		}
	}
	pr.DocumentRevision = strings.Join(revs, ",")
	if len(p.Documents) == 0 {
		pr.Lifecycle = "unknown"
	}
	verify := func(evd string) (bool, string) {
		if evd == "" {
			return false, "no evidence: unverified"
		}
		e, ok := ev.evidence(evd)
		if !ok {
			return false, "names unknown evidence"
		}
		if e.Verification != "verified" {
			return false, "evidence is an unverified excerpt"
		}
		if !docs[e.SourceID] {
			return false, "evidence is not from one of this product's documents"
		}
		for _, a := range e.Applicability {
			if m, ok := strings.CutPrefix(a, "product:"); ok && !strings.EqualFold(strings.TrimSpace(m), pr.Model) {
				return false, "evidence is for product " + m + ", not " + pr.Model
			}
		}
		return true, ""
	}
	for _, f := range p.Facts {
		if f.EvidenceID != "" {
			if _, ok := ev.evidence(f.EvidenceID); !ok {
				return pr, NotFound("no evidence " + f.EvidenceID + " in this problem")
			}
		}
		ok, note := verify(f.EvidenceID)
		pr.Facts = append(pr.Facts, ProductFact{Kind: f.Kind, Text: strings.TrimSpace(f.Text), EvidenceID: f.EvidenceID, Verified: ok, Note: note})
	}
	keys := make([]string, 0, len(p.Dimensions))
	for k := range p.Dimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d := p.Dimensions[k]
		v, unit := d.Value, d.Unit
		if productDimensionKeys[k] == "length" {
			mm, err := ToMM(d.Value, d.Unit)
			if err != nil {
				return pr, err
			}
			v, unit = round3(mm), "mm"
		}
		ok, note := verify(d.EvidenceID)
		prov := ProvUserAssumption
		if ok {
			prov = ProvVerifiedFact
		}
		val := v
		pr.Dimensions[k] = TechnicalValue{Value: &val, Unit: unit, Provenance: prov, EvidenceID: d.EvidenceID, Note: note}
	}
	if p.Material != "" {
		found := false
		for _, m := range cat.Materials {
			if m.ID == p.Material {
				if m.Family != p.Family {
					return pr, Invalid("material " + m.Name + " is a " + m.Family + ", not a " + p.Family)
				}
				pr.Materials = append(pr.Materials, PinRef{ID: m.ID, Revision: m.Revision})
				found = true
			}
		}
		if !found {
			return pr, NotFound("no material " + p.Material + " in this problem's catalog")
		}
	}
	return pr, nil
}

// CurrentProduct is the latest revision of a product id.
func (c *Catalog) CurrentProduct(id string) (Product, bool) {
	var best Product
	ok := false
	if c == nil {
		return best, false
	}
	for _, p := range c.Products {
		if p.ID == id && (!ok || p.Revision > best.Revision) {
			best, ok = p, true
		}
	}
	return best, ok
}

func init() {
	registerOp("AddProduct", TargetCatalog, true, func() Operation { return &AddProduct{} })
	registerOp("UpdateProduct", TargetCatalog, true, func() Operation { return &UpdateProduct{} })
	registerOp("SetProductLifecycle", TargetCatalog, true, func() Operation { return &SetProductLifecycle{} })
	registerOp("SetMaterialProperty", TargetCatalog, true, func() Operation { return &SetMaterialProperty{} })
	docValidators[DocCatalog] = func(st *State, key string, doc any) error {
		pid := ""
		if st.Problem != nil {
			pid = st.Problem.ID
		}
		if errs := validateCatalog(doc.(*Catalog), pid, st.Evidence); len(errs) > 0 {
			return Invalid(errs...)
		}
		return nil
	}
}

// AddProduct registers an actual manufacturer product used by this problem
// (project-local, evidence-pinned; §12.6). There is no default catalog
// fiction: a product exists only when the owner enters it from sources.
type AddProduct struct {
	Op string `json:"op"`
	ID string `json:"id"`
	productSpec
}

func (o *AddProduct) Name() string { return "AddProduct" }
func (o *AddProduct) Check() []string {
	out := o.productSpec.check()
	if !ValidID(KindProduct, o.ID) {
		out = append(out, "id must be a new prd- id chosen by the caller")
	}
	return out
}
func (o *AddProduct) Apply(tx *Tx, c *ApplyContext) error {
	cat := tx.Next.Catalog
	if _, exists := cat.CurrentProduct(o.ID); exists {
		return Invalid("product id already exists")
	}
	if len(cat.Products) >= 400 {
		return Invalid("product limit reached")
	}
	p, err := o.productSpec.build(o.ID, 1, tx.Next.Evidence, cat, tx.Now)
	if err != nil {
		return err
	}
	cat.Products = append(cat.Products, p)
	tx.Record(o.Name(), "product/"+p.ID, nil, p)
	tx.Summary("added product " + p.Manufacturer + " " + p.Model)
	return nil
}

// UpdateProduct records a new revision of a product; components keep the
// revision they pinned until explicitly re-pinned.
type UpdateProduct struct {
	Op        string `json:"op"`
	ProductID string `json:"productId"`
	productSpec
}

func (o *UpdateProduct) Name() string { return "UpdateProduct" }
func (o *UpdateProduct) Check() []string {
	out := o.productSpec.check()
	if !ValidID(KindProduct, o.ProductID) {
		out = append(out, "productId must be a prd- id")
	}
	return out
}
func (o *UpdateProduct) Apply(tx *Tx, c *ApplyContext) error {
	cat := tx.Next.Catalog
	cur, ok := cat.CurrentProduct(o.ProductID)
	if !ok {
		return NotFound("no such product")
	}
	p, err := o.productSpec.build(o.ProductID, cur.Revision+1, tx.Next.Evidence, cat, tx.Now)
	if err != nil {
		return err
	}
	cat.Products = append(cat.Products, p)
	tx.Record(o.Name(), "product/"+p.ID, cur, p)
	tx.Summary("revised product " + p.Model)
	return nil
}

// SetProductLifecycle marks a product active, stale (its source changed or
// went missing) or withdrawn. It applies to every revision: a pinned
// component sees the staleness of the product it pinned.
type SetProductLifecycle struct {
	Op        string `json:"op"`
	ProductID string `json:"productId"`
	Lifecycle string `json:"lifecycle"`
	Note      string `json:"note,omitempty"`
}

func (o *SetProductLifecycle) Name() string { return "SetProductLifecycle" }
func (o *SetProductLifecycle) Check() []string {
	out := checkText("note", o.Note, 500, false)
	if !ValidID(KindProduct, o.ProductID) || !productLifecycles[o.Lifecycle] {
		out = append(out, "productId and lifecycle active|stale|withdrawn|unknown are required")
	}
	return out
}
func (o *SetProductLifecycle) Apply(tx *Tx, c *ApplyContext) error {
	found := false
	for i := range tx.Next.Catalog.Products {
		p := &tx.Next.Catalog.Products[i]
		if p.ID == o.ProductID {
			found = true
			tx.Record(o.Name(), fmt.Sprintf("product/%s@%d", p.ID, p.Revision), p.Lifecycle, o.Lifecycle)
			p.Lifecycle = o.Lifecycle
		}
	}
	if !found {
		return NotFound("no such product")
	}
	return nil
}

// SetMaterialProperty states one typed technical property of a catalog
// material (with unit, test condition and provenance) as a new material
// revision. Unknown stays unknown until a source states it; verified-fact
// provenance needs verified evidence.
type SetMaterialProperty struct {
	Op            string   `json:"op"`
	MaterialID    string   `json:"materialId"`
	Property      string   `json:"property"`
	Value         *float64 `json:"value"`
	Unit          string   `json:"unit"`
	TestCondition string   `json:"testCondition,omitempty"`
	Provenance    string   `json:"provenance"`
	EvidenceID    string   `json:"evidenceId,omitempty"`
	Note          string   `json:"note,omitempty"`
}

func (o *SetMaterialProperty) Name() string { return "SetMaterialProperty" }
func (o *SetMaterialProperty) Check() []string {
	out := checkText("testCondition", o.TestCondition, 300, false)
	out = append(out, checkText("note", o.Note, 500, false)...)
	if !ValidID(KindMaterial, o.MaterialID) {
		out = append(out, "materialId must be a mat- id")
	}
	units, ok := propertyUnits[o.Property]
	if !ok {
		out = append(out, "property "+o.Property+" is not a typed material property")
	} else if o.Value != nil && !contains(units, o.Unit) {
		out = append(out, "unit for "+o.Property+" must be one of "+strings.Join(units, ", "))
	}
	if !provenances[o.Provenance] {
		out = append(out, "provenance is not in the taxonomy")
	}
	if o.Value == nil && o.Provenance != ProvUnknown {
		out = append(out, "an unknown value has provenance unknown")
	}
	if o.Value != nil && (!finite(*o.Value) || o.Provenance == ProvUnknown) {
		out = append(out, "a stated value must be finite and carry its provenance")
	}
	if o.Value != nil && o.TestCondition == "" && o.Property != "strengthClass" && o.Property != "baseMetal" && o.Property != "coating" {
		out = append(out, "a stated technical value needs its test condition")
	}
	if o.EvidenceID != "" && !ValidID(KindEvidence, o.EvidenceID) {
		out = append(out, "evidenceId must be an evd- id")
	}
	return out
}
func (o *SetMaterialProperty) Apply(tx *Tx, c *ApplyContext) error {
	cat := tx.Next.Catalog
	for i := range cat.Materials {
		m := &cat.Materials[i]
		if m.ID != o.MaterialID {
			continue
		}
		if _, ok := m.Properties[o.Property]; !ok {
			return Invalid(m.Name + " has no " + o.Property + " property")
		}
		if o.Provenance == ProvVerifiedFact || o.Provenance == ProvDirectGuidance {
			e, ok := tx.Next.Evidence.evidence(o.EvidenceID)
			if !ok || e.Verification != "verified" {
				return Invalid(o.Provenance + " needs verified evidence for " + o.Property)
			}
		}
		before := m.Properties[o.Property]
		m.History = append(m.History, MaterialVersion{Revision: m.Revision, Name: m.Name, Properties: copyProps(m.Properties), Appearance: m.Appearance})
		m.Revision++
		tv := TechnicalValue{Value: o.Value, Unit: o.Unit, TestCondition: o.TestCondition, Provenance: o.Provenance, EvidenceID: o.EvidenceID, Note: o.Note}
		if o.Value == nil {
			tv.Note = orDefault(o.Note, "unknown until a checked source supplies it")
		}
		m.Properties[o.Property] = tv
		var unknowns []string
		for _, u := range m.Unknowns {
			if u != o.Property {
				unknowns = append(unknowns, u)
			}
		}
		if o.Value == nil {
			unknowns = append(unknowns, o.Property)
		}
		m.Unknowns = sortedUnique(unknowns)
		tx.Record(o.Name(), fmt.Sprintf("material/%s.%s", m.ID, o.Property), before, tv)
		tx.Summary(m.Name + ": " + o.Property)
		return nil
	}
	return NotFound("no such material")
}

func copyProps(p map[string]TechnicalValue) map[string]TechnicalValue {
	out := make(map[string]TechnicalValue, len(p))
	for k, v := range p {
		out[k] = v
	}
	return out
}

// validateCatalog checks the catalog's structure and that verification is
// consistent with the evidence it cites.
func validateCatalog(c *Catalog, problemID string, ev *EvidenceBundle) []string {
	out := checkEnvelope(c.Envelope, DocCatalog, KindProblem)
	if c.ProblemID != problemID || c.ID != problemID {
		out = append(out, "catalog belongs to its problem")
	}
	verified := map[string]bool{}
	if ev != nil {
		for _, e := range ev.Evidence {
			verified[e.ID] = e.Verification == "verified"
		}
	}
	seen := map[string]bool{}
	for _, m := range c.Materials {
		key := m.ID + "@" + fmt.Sprint(m.Revision)
		if !ValidID(KindMaterial, m.ID) || m.Revision < 1 || seen[key] {
			out = append(out, "materials need unique mat- id + revision")
		}
		seen[key] = true
		if !knownFamily(m.Family) {
			out = append(out, "material "+m.ID+" family is not recognised")
		}
		for k, tv := range m.Properties {
			if !provenances[tv.Provenance] {
				out = append(out, "material "+m.ID+"."+k+" provenance is not recognised")
			}
			if tv.Value != nil && (!finite(*tv.Value) || tv.Unit == "") {
				out = append(out, "material "+m.ID+"."+k+" needs a finite value with its unit")
			}
			if tv.Value == nil && tv.Provenance != ProvUnknown {
				out = append(out, "material "+m.ID+"."+k+": unknown is never given a provenance")
			}
			if (tv.Provenance == ProvVerifiedFact || tv.Provenance == ProvDirectGuidance) && !verified[tv.EvidenceID] {
				out = append(out, "material "+m.ID+"."+k+" claims "+tv.Provenance+" without verified evidence")
			}
		}
	}
	for _, p := range c.Products {
		key := p.ID + "@" + fmt.Sprint(p.Revision)
		if !ValidID(KindProduct, p.ID) || p.Revision < 1 || seen[key] {
			out = append(out, "products need unique prd- id + revision")
		}
		seen[key] = true
		if strings.TrimSpace(p.Manufacturer) == "" || strings.TrimSpace(p.Model) == "" || !knownFamily(p.Family) || p.Geography == "" || p.CheckedAt == "" {
			out = append(out, "product "+p.ID+" needs manufacturer, model, family, geography and a check date")
		}
		if !productLifecycles[p.Lifecycle] {
			out = append(out, "product "+p.ID+" lifecycle is not recognised")
		}
		for _, f := range p.Facts {
			if f.Verified && !verified[f.EvidenceID] {
				out = append(out, "product "+p.ID+" fact claims verification without verified evidence")
			}
		}
		for k, tv := range p.Dimensions {
			if tv.Value == nil || !finite(*tv.Value) || tv.Unit == "" {
				out = append(out, "product "+p.ID+" dimension "+k+" needs a finite value with its unit")
			}
			if tv.Provenance == ProvVerifiedFact && !verified[tv.EvidenceID] {
				out = append(out, "product "+p.ID+" dimension "+k+" claims verification without verified evidence")
			}
		}
	}
	return out
}

// SubstitutionReport is what changes when a component's product changes:
// a fact diff, dimension changes and staleness — computed, never assumed.
type SubstitutionReport struct {
	ComponentID string            `json:"componentId"`
	From        *PinRef           `json:"from"`
	To          *PinRef           `json:"to"`
	FromLabel   string            `json:"fromLabel"`
	ToLabel     string            `json:"toLabel"`
	Added       []string          `json:"factsAdded"`
	Removed     []string          `json:"factsRemoved"`
	Dimensions  []DimensionChange `json:"dimensions"`
	Notes       []string          `json:"notes"`
}

type DimensionChange struct {
	Key  string   `json:"key"`
	From *float64 `json:"from"`
	To   *float64 `json:"to"`
	Unit string   `json:"unit"`
	// Applies says the component has this parameter, so applying the
	// product's dimension changes the model (and triggers revalidation).
	Applies    bool   `json:"applies"`
	Provenance string `json:"provenance"`
}

// Substitution compares a component's current product (or generic
// material) with a candidate product.
func Substitution(a *Assembly, cat *Catalog, componentID, productID string) (*SubstitutionReport, error) {
	c, ok := a.Component(componentID)
	if !ok {
		return nil, NotFound("no such component")
	}
	to, ok := cat.CurrentProduct(productID)
	if !ok {
		return nil, NotFound("no such product")
	}
	rep := &SubstitutionReport{ComponentID: c.ID, From: c.Product, To: &PinRef{ID: to.ID, Revision: to.Revision}, FromLabel: "generic material",
		ToLabel: to.Manufacturer + " " + to.Model, Added: []string{}, Removed: []string{}, Dimensions: []DimensionChange{}, Notes: []string{}}
	var from Product
	if c.Product != nil {
		if p, ok := cat.product(c.Product.ID, c.Product.Revision); ok {
			from = p
			rep.FromLabel = p.Manufacturer + " " + p.Model
		}
	}
	fset := map[string]bool{}
	for _, f := range from.Facts {
		fset[f.Kind+": "+f.Text] = true
	}
	tset := map[string]bool{}
	for _, f := range to.Facts {
		k := f.Kind + ": " + f.Text
		tset[k] = true
		if !fset[k] {
			rep.Added = append(rep.Added, k+map[bool]string{true: " (verified)", false: " (unverified)"}[f.Verified])
		}
	}
	for k := range fset {
		if !tset[k] {
			rep.Removed = append(rep.Removed, k)
		}
	}
	sort.Strings(rep.Added)
	sort.Strings(rep.Removed)
	keys := map[string]bool{}
	for k := range from.Dimensions {
		keys[k] = true
	}
	for k := range to.Dimensions {
		keys[k] = true
	}
	ks := make([]string, 0, len(keys))
	for k := range keys {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		dc := DimensionChange{Key: k, Unit: "mm"}
		if v, ok := from.Dimensions[k]; ok {
			dc.From, dc.Unit = v.Value, v.Unit
		} else if q, ok := c.Shape.Params[k]; ok {
			if v, ok := q.Effective(); ok {
				vv := v
				dc.From = &vv
			}
		}
		if v, ok := to.Dimensions[k]; ok {
			dc.To, dc.Unit, dc.Provenance = v.Value, v.Unit, v.Provenance
		}
		_, dc.Applies = c.Shape.Params[k]
		rep.Dimensions = append(rep.Dimensions, dc)
	}
	if to.Lifecycle != "active" {
		rep.Notes = append(rep.Notes, "the candidate product is "+to.Lifecycle+": re-check its source before relying on it")
	}
	if to.Fictional {
		rep.Notes = append(rep.Notes, "fictional fixture product: never a real specification")
	}
	for _, f := range to.Facts {
		if !f.Verified && f.Note != "" {
			rep.Notes = append(rep.Notes, f.Text+": "+f.Note)
		}
	}
	if !knownFamilyForComponent(c, to.Family) {
		rep.Notes = append(rep.Notes, "the product family "+to.Family+" does not match this part's material family")
	}
	rep.Notes = append(rep.Notes, "substitution re-runs fit, fastener, compatibility and installation checks; any decision bound to this assembly becomes stale")
	return rep, nil
}

func knownFamilyForComponent(c Component, family string) bool {
	if c.Material == nil {
		return true
	}
	for _, g := range genericMaterials {
		if g.ID == c.Material.ID {
			return g.Family == family
		}
	}
	return true
}
