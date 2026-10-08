package construction

// Evidence rules (§3.5, P5): deterministic checks of how evidence is
// APPLIED to an assembly. They flag a code excerpt from a jurisdiction the
// site has not established, manufacturer guidance for a product that is not
// the one specified, a detail for the other junction orientation or another
// wall type, verified contradiction (kept, never averaged), secondary leads,
// fictional fixture sources and unverified owner excerpts. Passing them says
// nothing about engineering correctness.

import (
	"sort"
	"strings"
)

func evidenceFindings(a *Assembly, ev *EvidenceBundle, p *Problem, cat *Catalog) []finding {
	if ev == nil || len(a.EvidenceLinks) == 0 {
		return nil
	}
	src := map[string]Source{}
	for _, s := range ev.Sources {
		src[s.ID] = s
	}
	evm := map[string]Evidence{}
	for _, e := range ev.Evidence {
		evm[e.ID] = e
	}
	claimsOf := map[string][]Claim{}
	for _, c := range ev.Claims {
		for _, id := range c.Supporting {
			claimsOf[id] = append(claimsOf[id], c)
		}
	}
	models := map[string]bool{}
	for _, c := range a.Components {
		if c.Product == nil || c.Applicability == "inapplicable" {
			continue
		}
		if pr, ok := cat.product(c.Product.ID, c.Product.Revision); ok {
			models[strings.ToLower(pr.Model)] = true
		}
	}
	seen := map[string]bool{}
	var out []finding
	add := func(f finding) {
		if !seen[f.key+"|"+f.target] {
			seen[f.key+"|"+f.target] = true
			out = append(out, f)
		}
	}
	jur := "unknown"
	if p != nil && p.Jurisdiction.State != StateUnknown {
		jur = p.Jurisdiction.Text + " (" + p.Jurisdiction.State + ")"
	}
	for _, l := range a.EvidenceLinks {
		e, ok := evm[l.EvidenceID]
		if !ok {
			continue
		}
		s := src[e.SourceID]
		tgt := e.ID + "@" + l.Target
		comps := []string{}
		if _, ok := a.Component(l.Target); ok {
			comps = []string{l.Target}
		}
		if s.Fictional {
			add(finding{key: "source.fictional", target: tgt, severity: SevCritical, category: "sourcing", comps: comps,
				message:  "Evidence from a fictional fixture source (" + s.Title + "): never evidence for a real detail.",
				observed: "fictional source", expected: "a real, checked source", inputs: []string{"evidence"}})
		}
		if l.Relation == "supports" && e.Verification != "verified" {
			add(finding{key: "evidence.unverified", target: tgt, severity: SevAdvisory, category: "sourcing", comps: comps,
				message:  "Linked passage is an owner-supplied excerpt that has not been matched against retained page text.",
				observed: e.QuoteMatch, expected: "quote found on the retained page", inputs: []string{"evidence"}})
		}
		if l.Relation == "supports" && s.Class == "secondary" {
			add(finding{key: "source.secondary", target: tgt, severity: SevAdvisory, category: "sourcing", comps: comps,
				message: "Secondary discussion supports this only as a lead, not as evidence.", observed: "secondary", expected: "primary source", inputs: []string{"evidence"}})
		}
		if l.Relation == "contradicts" && e.Verification == "verified" {
			add(finding{key: "source.contradicted", target: tgt, severity: SevCritical, category: "sourcing", comps: comps,
				message:  "A verified passage contradicts this choice (" + s.Title + " p." + itoa(e.Page) + "); the disagreement is kept, not averaged.",
				observed: "contradicting evidence", expected: "resolved by review", specialist: true, inputs: []string{"evidence"}})
		}
		if l.Relation == "supports" {
			for _, c := range claimsOf[e.ID] {
				if c.Verification == "contradicted" {
					add(finding{key: "source.contradicted", target: c.ID + "@" + l.Target, severity: SevCritical, category: "sourcing", comps: comps,
						message:  "The supporting claim “" + truncate(c.Statement, 120) + "” is contradicted by other verified evidence; the disagreement is kept, not averaged.",
						observed: "contradicted claim", expected: "resolved by review", specialist: true, inputs: []string{"claims"}})
				}
			}
		}
		if l.Relation != "supports" {
			continue
		}
		for _, ap := range e.Applicability {
			kind, val, ok := strings.Cut(ap, ":")
			if !ok {
				continue
			}
			switch kind {
			case "jurisdiction":
				if p == nil || p.Jurisdiction.State != StateKnown || !strings.EqualFold(strings.TrimSpace(p.Jurisdiction.Text), strings.TrimSpace(val)) {
					add(finding{key: "source.applicability.jurisdiction", target: tgt, severity: SevCritical, category: "sourcing", comps: comps,
						message:  "Code text for " + val + " is cited, but the site's jurisdiction and adopted edition are not established (jurisdiction: " + jur + "). An accessible code edition is not proof it is locally adopted.",
						observed: jur, expected: val + ", verified as adopted", inputs: []string{"problem.jurisdiction", "evidence.applicability"}})
				}
			case "product":
				if !models[strings.ToLower(val)] {
					add(finding{key: "source.applicability.product", target: tgt, severity: SevCritical, category: "sourcing", comps: comps,
						message:  "Manufacturer guidance for " + val + " is cited, but that product is not the one specified here; it does not establish this assembly's requirements.",
						observed: strings.Join(sortedKeys(models), ", "), expected: val, inputs: []string{"products", "evidence.applicability"}})
				}
			case "orientation":
				o := a.Junction.Orientation
				switch {
				case o == "unresolved":
					add(finding{key: "source.applicability.detail", target: tgt, severity: SevAdvisory, category: "sourcing", comps: comps,
						message: "A " + val + " detail is cited while the junction orientation is unresolved.", observed: o, expected: val, inputs: []string{"junction.orientation"}})
				case o != val:
					add(finding{key: "source.applicability.detail", target: tgt, severity: SevCritical, category: "sourcing", comps: comps,
						message: "A " + val + " detail is applied to a " + o + " junction; it does not transfer.", observed: o, expected: val, inputs: []string{"junction.orientation"}})
				}
			case "wall":
				w := a.Junction.WallCondition.Value
				switch {
				case w == val:
				case w == "unknown":
					add(finding{key: "source.applicability.wall", target: tgt, severity: SevAdvisory, category: "sourcing", comps: comps,
						message: "Guidance for " + val + " walls is cited while the wall condition is unknown.", observed: w, expected: val, inputs: []string{"junction.wallCondition"}})
				default:
					add(finding{key: "source.applicability.wall", target: tgt, severity: SevCritical, category: "sourcing", comps: comps,
						message: "Guidance for " + val + " walls is applied to a " + w + " wall.", observed: w, expected: val, inputs: []string{"junction.wallCondition"}})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].key != out[j].key {
			return out[i].key < out[j].key
		}
		return out[i].target < out[j].target
	})
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return []string{"none"}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
