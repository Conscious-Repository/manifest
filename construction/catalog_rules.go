package construction

// Product rules (P6): regional availability is unverified unless the site
// location is known and named in the product's geography; a product fact
// quoted from another model's document is flagged; a dimension taken from a
// product without verifying evidence is labelled. These never establish
// suitability: a manufacturer's acceptance needs its own checked document.

import "strings"

func productFindings(a *Assembly, cat *Catalog, p *Problem) []finding {
	var out []finding
	for _, c := range a.Components {
		if c.Product == nil || c.Applicability == "inapplicable" {
			continue
		}
		pr, ok := cat.product(c.Product.ID, c.Product.Revision)
		if !ok {
			continue
		}
		label := pr.Manufacturer + " " + pr.Model
		loc := ""
		if p != nil && p.Location.State != StateUnknown {
			loc = strings.TrimSpace(p.Location.Text)
		}
		switch {
		case loc == "":
			out = append(out, finding{key: "product.region", target: c.ID, severity: SevAdvisory, category: "sourcing", comps: []string{c.ID},
				message:  label + " is listed for " + pr.Geography + "; the site location is unknown, so regional availability and approval are unverified.",
				observed: "location unknown", expected: "site within " + pr.Geography, inputs: []string{"problem.location", "product.geography"}})
		case !strings.Contains(strings.ToLower(pr.Geography), strings.ToLower(loc)):
			out = append(out, finding{key: "product.region", target: c.ID, severity: SevAdvisory, category: "sourcing", comps: []string{c.ID},
				message:  label + " is listed for " + pr.Geography + ", which does not name the site location (" + loc + "); verify regional availability and approval.",
				observed: loc, expected: pr.Geography, inputs: []string{"problem.location", "product.geography"}})
		}
		for _, f := range pr.Facts {
			if strings.HasPrefix(f.Note, "evidence is for product") {
				out = append(out, finding{key: "product.evidence-mismatch", target: c.ID, severity: SevCritical, category: "sourcing", comps: []string{c.ID},
					message:  label + ": “" + truncate(f.Text, 100) + "” cites " + strings.TrimPrefix(f.Note, "evidence is for ") + " — another model's document does not establish this product.",
					observed: f.Note, expected: "this model's own document", inputs: []string{"product.facts"}})
				break
			}
		}
		for k, tv := range pr.Dimensions {
			q, has := c.Shape.Params[k]
			if !has || tv.Value == nil || q.Value == nil || *q.Value != *tv.Value || tv.Provenance == ProvVerifiedFact {
				continue
			}
			out = append(out, finding{key: "product.dimension-unverified", target: c.ID + "." + k, severity: SevAdvisory, category: "sourcing", comps: []string{c.ID},
				message:  c.Name + ": " + k + " comes from " + label + " but no verified evidence states it.",
				observed: tv.Provenance, expected: "verified-fact", inputs: []string{"product.dimensions"}})
		}
	}
	return out
}
