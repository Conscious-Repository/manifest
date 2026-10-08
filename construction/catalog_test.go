package construction

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// Every initial family is in a new problem's catalog with explicit unknowns:
// no conductivity, compatibility or capacity is invented.
func TestConstructionCatalogGenericFamiliesUnknown(t *testing.T) {
	_, st, _ := templateProblem(t)
	fams := map[string]bool{}
	for _, m := range st.Catalog.Materials {
		fams[m.Family] = true
		if !m.Generic || m.Revision != 1 {
			t.Fatalf("generic material %s", m.Name)
		}
		for k, tv := range m.Properties {
			if tv.Value != nil || tv.Provenance != ProvUnknown || !contains(m.Unknowns, k) {
				t.Fatalf("%s.%s must start unknown: %+v", m.Name, k, tv)
			}
		}
	}
	for _, f := range []string{"corrugated-metal-roof", "standing-seam-roof", "sheet-flashing", "membrane", "underlayment", "insulation", "timber",
		"sheathing", "masonry", "mortar", "sealant", "closure", "fastener"} {
		if !fams[f] {
			t.Fatalf("family %s missing", f)
		}
	}
	if len(st.Catalog.Products) != 0 {
		t.Fatal("no default catalog fiction: a new problem has no products")
	}
}

func TestConstructionCatalogMaterialProperties(t *testing.T) {
	s, st, asm := templateProblem(t)
	ins := st.Assemblies[asm].firstOf(TypeInsulation)
	mat := ins.Material.ID
	refuse := func(op map[string]any, want string) {
		t.Helper()
		_, _, err := exec(t, s, st, "", OwnerActor(), op)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("expected refusal %q, got %v", want, err)
		}
	}
	refuse(map[string]any{"op": "SetMaterialProperty", "materialId": mat, "property": "thermalConductivity", "value": 0.022, "unit": "W/(m·K)", "testCondition": "10 °C mean", "provenance": "verified-fact"}, "needs verified evidence")
	refuse(map[string]any{"op": "SetMaterialProperty", "materialId": mat, "property": "thermalConductivity", "value": 0.022, "unit": "BTU", "testCondition": "x", "provenance": "user-assumption"}, "unit for thermalConductivity")
	refuse(map[string]any{"op": "SetMaterialProperty", "materialId": mat, "property": "colourMood", "value": 1, "unit": "x", "provenance": "user-assumption"}, "not a typed material property")
	refuse(map[string]any{"op": "SetMaterialProperty", "materialId": mat, "property": "thermalConductivity", "value": 0.022, "unit": "W/(m·K)", "provenance": "user-assumption"}, "test condition")
	st = mustExec(t, s, st, "", map[string]any{"op": "SetMaterialProperty", "materialId": mat, "property": "thermalConductivity", "value": 0.022, "unit": "W/(m·K)",
		"testCondition": "10 °C mean temperature (owner assumption, synthetic)", "provenance": "user-assumption"})
	var m Material
	for _, x := range st.Catalog.Materials {
		if x.ID == mat {
			m = x
		}
	}
	if m.Revision != 2 || len(m.History) != 1 || m.Properties["thermalConductivity"].Value == nil || contains(m.Unknowns, "thermalConductivity") {
		t.Fatalf("a stated property is a new material revision: %+v", m)
	}
	if st.Assemblies[asm].firstOf(TypeInsulation).Material.Revision != 1 {
		t.Fatal("a component keeps the material revision it pinned")
	}
	if old, ok := st.Catalog.material(mat, 1); !ok || old.Properties["thermalConductivity"].Value != nil {
		t.Fatal("the pinned earlier revision stays resolvable exactly")
	}
	// forged verification in the stored document is refused by the validator
	bad := *st.Catalog
	bad.Materials = append([]Material{}, st.Catalog.Materials...)
	for i := range bad.Materials {
		if bad.Materials[i].ID == mat {
			p := copyProps(bad.Materials[i].Properties)
			tv := p["thermalConductivity"]
			tv.Provenance = ProvVerifiedFact
			p["thermalConductivity"] = tv
			bad.Materials[i].Properties = p
		}
	}
	if errs := validateCatalog(&bad, st.Problem.ID, st.Evidence); len(errs) == 0 {
		t.Fatal("verified-fact without verified evidence must be invalid")
	}
}

// researchedProblem: a template problem after the fixture research run, so
// the fixture sources and passages exist for products to cite.
func researchedProblem(t *testing.T) (*Store, *State, string) {
	t.Helper()
	s, st, asm := templateProblem(t)
	r := &Runner{Store: s, Adapters: []SourceAdapter{fixtureAdapter(t)}}
	run := newRun(t, s, st, "local-only", true)
	if err := r.Advance(context.Background(), fixtureProperty, st.Problem.ID, run.ID); err != nil {
		t.Fatal(err)
	}
	st, run = loadRun(t, s, st.Problem.ID, run.ID)
	if run.State != RunCompleted {
		t.Fatalf("research %s", run.State)
	}
	return s, st, asm
}

func TestConstructionCatalogProductsAndSubstitution(t *testing.T) {
	s, st, asm := researchedProblem(t)
	ops, err := CatalogFixtureOps(filepath.Join("testdata", "roof-wall"), st.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := exec(t, s, st, "", AgentActor("alfred", "cap", ""), ops["corrugated"]); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agents cannot add products: %v", err)
	}
	st = mustExec(t, s, st, "", ops["corrugated"], ops["board-120"], ops["board-mismatch"])
	prod := func(key string) Product {
		p, ok := st.Catalog.CurrentProduct(ops[key]["id"].(string))
		if !ok {
			t.Fatalf("product %s missing", key)
		}
		return p
	}
	cor, b120, bmis := prod("corrugated"), prod("board-120"), prod("board-mismatch")
	src, _ := st.Evidence.source(cor.Documents[0].SourceID)
	if !cor.Fictional || cor.Lifecycle != "active" || cor.Documents[0].Revision != src.ContentHash || cor.DocumentRevision != src.ContentHash {
		t.Fatalf("a documented fixture product pins its document revision and is labelled fictional: %+v", cor)
	}
	for _, f := range cor.Facts {
		if !f.Verified {
			t.Fatalf("a fact quoted from the product's own retained document is verified: %+v", f)
		}
	}
	if b120.Lifecycle != "unknown" || b120.Facts[0].Verified || !b120.Fictional {
		t.Fatalf("a fact without evidence never becomes verified: %+v", b120)
	}
	if bmis.Facts[0].Verified || !strings.Contains(bmis.Facts[0].Note, "evidence is for product Synthetic Corrugated 76/18") {
		t.Fatalf("evidence for another model is flagged: %+v", bmis.Facts[0])
	}
	if v := bmis.Dimensions["thickness"]; v.Value == nil || *v.Value != 150 || v.Unit != "mm" || v.Provenance != ProvUserAssumption {
		t.Fatalf("15 cm is stored as 150 mm, unverified: %+v", v)
	}
	// wrong unit / family are refused
	bad := map[string]any{}
	for k, v := range ops["board-120"] {
		bad[k] = v
	}
	bad["id"], bad["dimensions"] = NewID(KindProduct), map[string]any{"thickness": map[string]any{"value": 120, "unit": "deg"}}
	if _, _, err := exec(t, s, st, "", OwnerActor(), bad); StatusOf(err) != 422 {
		t.Fatalf("a length in degrees is refused: %v", err)
	}
	bad["dimensions"], bad["family"] = nil, "unobtainium"
	if _, _, err := exec(t, s, st, "", OwnerActor(), bad); StatusOf(err) != 422 {
		t.Fatalf("an unknown family is refused: %v", err)
	}
	// substitution preview: generic → board 120 changes thickness 100 → 120
	a := st.Assemblies[asm]
	ins := a.firstOf(TypeInsulation)
	rep, err := Substitution(a, st.Catalog, ins.ID, b120.ID)
	if err != nil {
		t.Fatal(err)
	}
	var th *DimensionChange
	for i := range rep.Dimensions {
		if rep.Dimensions[i].Key == "thickness" {
			th = &rep.Dimensions[i]
		}
	}
	if th == nil || !th.Applies || *th.From != 100 || *th.To != 120 || !strings.Contains(strings.Join(rep.Notes, " "), "fictional fixture product") {
		t.Fatalf("substitution report %+v", rep)
	}
	// apply: the same component id, thickness 120, fastener review, product issues
	st = mustExec(t, s, st, asm, map[string]any{"op": "SetProduct", "componentId": ins.ID, "productId": b120.ID, "applyDimensions": true})
	a = st.Assemblies[asm]
	ins2, ok := a.Component(ins.ID)
	if !ok || *ins2.Shape.Params["thickness"].Value != 120 || ins2.Product.ID != b120.ID || ins2.Product.Revision != 1 {
		t.Fatalf("substitution keeps the component id and applies the dimension: %+v", ins2)
	}
	crit, adv := issueKeys(st.Validation[asm], SevCritical), issueKeys(st.Validation[asm], SevAdvisory)
	if !crit["structure.fastener.embedment"] && !adv["structure.fastener.embedment"] {
		t.Fatalf("a thicker board re-runs fastener review: %v %v", crit, adv)
	}
	if !adv["product.region"] || !adv["product.dimension-unverified"] {
		t.Fatalf("product region and unverified dimension are flagged: %v", adv)
	}
	if !crit["product.evidence-missing"] {
		t.Fatal("a product without verified facts is critical")
	}
	// the mismatched board is flagged as citing another model
	st = mustExec(t, s, st, asm, map[string]any{"op": "SetProduct", "componentId": ins.ID, "productId": bmis.ID, "applyDimensions": true})
	if !issueKeys(st.Validation[asm], SevCritical)["product.evidence-mismatch"] {
		t.Fatal("another model's document is flagged")
	}
	// a product family that does not fit the part is refused
	sheet := st.Assemblies[asm].firstOf(TypeCorrugatedSheet)
	if _, _, err := exec(t, s, st, asm, OwnerActor(), map[string]any{"op": "SetProduct", "componentId": sheet.ID, "productId": b120.ID}); StatusOf(err) != 422 {
		t.Fatalf("an insulation board on the roof sheet is refused: %v", err)
	}
	// revision pinning: updating the corrugated product leaves the pin at revision 1
	st = mustExec(t, s, st, asm, map[string]any{"op": "SetProduct", "componentId": sheet.ID, "productId": cor.ID})
	upd := map[string]any{}
	for k, v := range ops["corrugated"] {
		upd[k] = v
	}
	delete(upd, "id")
	upd["op"], upd["productId"], upd["geography"] = "UpdateProduct", cor.ID, "Fictional Region and Elsewhere (synthetic)"
	st = mustExec(t, s, st, "", upd)
	if cur, _ := st.Catalog.CurrentProduct(cor.ID); cur.Revision != 2 {
		t.Fatalf("update makes revision 2: %d", cur.Revision)
	}
	if pin, _ := st.Assemblies[asm].Component(sheet.ID); pin.Product.Revision != 1 {
		t.Fatal("a pinned component never follows the catalog head")
	}
	// staleness reaches the pinned component; withdrawn cannot be newly pinned
	st = mustExec(t, s, st, "", map[string]any{"op": "SetProductLifecycle", "productId": cor.ID, "lifecycle": "stale", "note": "fixture document revised"})
	if !issueKeys(st.Validation[asm], SevCritical)["product.stale"] {
		t.Fatal("a stale product is flagged where it is pinned")
	}
	st = mustExec(t, s, st, "", map[string]any{"op": "SetProductLifecycle", "productId": b120.ID, "lifecycle": "withdrawn"})
	if _, _, err := exec(t, s, st, asm, OwnerActor(), map[string]any{"op": "SetProduct", "componentId": ins.ID, "productId": b120.ID}); StatusOf(err) != 422 {
		t.Fatalf("a withdrawn product cannot be pinned: %v", err)
	}
	// forged product verification is refused by the validator
	forged := *st.Catalog
	forged.Products = append([]Product{}, st.Catalog.Products...)
	for i := range forged.Products {
		if forged.Products[i].ID == b120.ID {
			facts := append([]ProductFact{}, forged.Products[i].Facts...)
			facts[0].Verified = true
			forged.Products[i].Facts = facts
		}
	}
	if errs := validateCatalog(&forged, st.Problem.ID, st.Evidence); len(errs) == 0 {
		t.Fatal("a verified fact without verified evidence must be invalid")
	}
}
