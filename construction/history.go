package construction

// History and comparison (P7). An assembly's history is its own revision
// chain, each revision annotated from the commit receipt that produced it
// (actor, operations, summary) — agent and owner edits are told apart from
// the receipts, never from labels. Comparison is a structured diff of two
// exact revisions by stable component id: parameters, junction, parts,
// materials/products, issues and evidence links.

import (
	"fmt"
	"sort"
	"strings"
)

// AssemblyRevision is one entry of an assembly's history.
type AssemblyRevision struct {
	Revision   string   `json:"revision"`
	Number     int      `json:"number"`
	CreatedAt  string   `json:"createdAt"`
	Actor      Actor    `json:"actor"`
	ModelHash  string   `json:"modelHash"`
	Lifecycle  string   `json:"lifecycle"`
	Operations []string `json:"operations"`
	Summary    string   `json:"summary"`
	RequestID  string   `json:"requestId,omitempty"`
}

// AssemblyHistory walks one assembly's revision chain (newest first).
func (s *Store) AssemblyHistory(sub SubjectRef, problemID, asmID string, limit int) ([]AssemblyRevision, error) {
	st, err := s.Load(sub, problemID)
	if err != nil {
		return nil, err
	}
	tok := st.Revision("assembly:" + asmID)
	if tok == "" {
		return nil, NotFound("no such assembly in this problem")
	}
	receipts, err := s.History(sub, problemID, 0)
	if err != nil {
		return nil, err
	}
	byRev := map[string]*Receipt{}
	for i := range receipts {
		for _, ch := range receipts[i].Changes {
			if ch.Key == "assembly:"+asmID {
				byRev[ch.To] = &receipts[i]
			}
		}
	}
	var out []AssemblyRevision
	for steps := 0; tok != "" && (limit <= 0 || len(out) < limit) && steps < 100000; steps++ {
		raw, err := s.docBytes(DocRef{Kind: DocAssembly, Revision: tok})
		if err != nil {
			return out, err
		}
		var a Assembly
		if err := DecodeStrict(raw, DocAssembly, &a); err != nil {
			return out, err
		}
		e := AssemblyRevision{Revision: tok, Number: a.RevisionNumber, CreatedAt: a.CreatedAt, Actor: a.Actor, ModelHash: a.ModelHash, Lifecycle: a.Lifecycle, Operations: []string{}}
		if rc := byRev[tok]; rc != nil {
			e.Summary, e.RequestID = rc.Summary, rc.RequestID
			for _, op := range rc.Operations {
				e.Operations = append(e.Operations, op.Op)
			}
		}
		out = append(out, e)
		tok = a.ParentRevision
	}
	return out, nil
}

// FieldChange is one changed value.
type FieldChange struct {
	Key  string `json:"key"`
	From string `json:"from"`
	To   string `json:"to"`
}

// ComponentChange is one part's difference (matched by stable id).
type ComponentChange struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Change  string   `json:"change"` // added | removed | changed
	Details []string `json:"details"`
}

// Comparison is the structured difference of two exact revisions.
type Comparison struct {
	A          VersionRef        `json:"a"`
	B          VersionRef        `json:"b"`
	AName      string            `json:"aName"`
	BName      string            `json:"bName"`
	Junction   []FieldChange     `json:"junction"`
	Parameters []FieldChange     `json:"parameters"`
	Components []ComponentChange `json:"components"`
	Issues     struct {
		OnlyA []IssueBrief `json:"onlyA"`
		OnlyB []IssueBrief `json:"onlyB"`
		Both  int          `json:"both"`
	} `json:"issues"`
	Evidence struct {
		OnlyA []string `json:"onlyA"`
		OnlyB []string `json:"onlyB"`
	} `json:"evidence"`
	Geometry FieldChange `json:"geometry"`
	Same     bool        `json:"same"`
}

func qstr(q Quantity) string {
	v, ok := q.Effective()
	if !ok {
		return "unknown"
	}
	s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
	if l := q.Label(); l != "" {
		s += " (" + l + ")"
	}
	return s + " " + q.Unit
}

func pinStr(p *PinRef) string {
	if p == nil {
		return "none"
	}
	return fmt.Sprintf("%s@%d", p.ID, p.Revision)
}

// CompareAssemblies diffs revision a against revision b.
func CompareAssemblies(a, b *Assembly, ra, rb string, repA, repB *ValidationReport) Comparison {
	c := Comparison{A: VersionRef{ID: a.ID, Revision: ra}, B: VersionRef{ID: b.ID, Revision: rb}, AName: a.Name, BName: b.Name,
		Junction: []FieldChange{}, Parameters: []FieldChange{}, Components: []ComponentChange{}}
	c.Issues.OnlyA, c.Issues.OnlyB = []IssueBrief{}, []IssueBrief{}
	c.Evidence.OnlyA, c.Evidence.OnlyB = []string{}, []string{}
	jf := func(k, x, y string) {
		if x != y {
			c.Junction = append(c.Junction, FieldChange{Key: k, From: x, To: y})
		}
	}
	jf("orientation", a.Junction.Orientation, b.Junction.Orientation)
	jf("strategy", a.Junction.Strategy, b.Junction.Strategy)
	jf("wallCondition", a.Junction.WallCondition.Value+" ("+a.Junction.WallCondition.Provenance+")", b.Junction.WallCondition.Value+" ("+b.Junction.WallCondition.Provenance+")")
	keys := map[string]bool{}
	for k := range a.Parameters {
		keys[k] = true
	}
	for k := range b.Parameters {
		keys[k] = true
	}
	for _, k := range sortedKeySet(keys) {
		if x, y := qstr(a.Parameters[k]), qstr(b.Parameters[k]); x != y {
			c.Parameters = append(c.Parameters, FieldChange{Key: k, From: x, To: y})
		}
	}
	am := map[string]Component{}
	for _, x := range a.Components {
		am[x.ID] = x
	}
	seen := map[string]bool{}
	for _, y := range b.Components {
		seen[y.ID] = true
		x, ok := am[y.ID]
		if !ok {
			c.Components = append(c.Components, ComponentChange{ID: y.ID, Name: y.Name, Change: "added", Details: []string{}})
			continue
		}
		var d []string
		if x.Applicability != y.Applicability {
			d = append(d, "applicability "+x.Applicability+" → "+y.Applicability)
		}
		pk := map[string]bool{}
		for k := range x.Shape.Params {
			pk[k] = true
		}
		for k := range y.Shape.Params {
			pk[k] = true
		}
		for _, k := range sortedKeySet(pk) {
			if f, t := qstr(x.Shape.Params[k]), qstr(y.Shape.Params[k]); f != t {
				d = append(d, k+" "+f+" → "+t)
			}
		}
		if pinStr(x.Material) != pinStr(y.Material) {
			d = append(d, "material "+pinStr(x.Material)+" → "+pinStr(y.Material))
		}
		if pinStr(x.Product) != pinStr(y.Product) {
			d = append(d, "product "+pinStr(x.Product)+" → "+pinStr(y.Product))
		}
		if x.Transform != y.Transform {
			d = append(d, "position changed")
		}
		if x.Appearance != y.Appearance {
			d = append(d, "appearance "+x.Appearance+" → "+y.Appearance+" (visual only)")
		}
		if len(d) > 0 {
			c.Components = append(c.Components, ComponentChange{ID: y.ID, Name: y.Name, Change: "changed", Details: d})
		}
	}
	for _, x := range a.Components {
		if !seen[x.ID] {
			c.Components = append(c.Components, ComponentChange{ID: x.ID, Name: x.Name, Change: "removed", Details: []string{}})
		}
	}
	open := func(r *ValidationReport) map[string]IssueBrief {
		m := map[string]IssueBrief{}
		if r == nil {
			return m
		}
		for _, is := range r.Issues {
			if is.Status != "resolved" && is.Severity != SevInfo {
				m[is.RuleKey+"|"+is.Target] = IssueBrief{ID: is.ID, RuleKey: is.RuleKey, Severity: is.Severity, Message: truncate(is.Message, 200)}
			}
		}
		return m
	}
	ia, ib := open(repA), open(repB)
	for _, k := range sortedIssueKeys(ia) {
		if _, ok := ib[k]; ok {
			c.Issues.Both++
		} else {
			c.Issues.OnlyA = append(c.Issues.OnlyA, ia[k])
		}
	}
	for _, k := range sortedIssueKeys(ib) {
		if _, ok := ia[k]; !ok {
			c.Issues.OnlyB = append(c.Issues.OnlyB, ib[k])
		}
	}
	la, lb := map[string]bool{}, map[string]bool{}
	for _, l := range a.EvidenceLinks {
		la[l.Relation+" "+l.EvidenceID+" → "+l.Target] = true
	}
	for _, l := range b.EvidenceLinks {
		lb[l.Relation+" "+l.EvidenceID+" → "+l.Target] = true
	}
	for _, k := range sortedKeySet(la) {
		if !lb[k] {
			c.Evidence.OnlyA = append(c.Evidence.OnlyA, k)
		}
	}
	for _, k := range sortedKeySet(lb) {
		if !la[k] {
			c.Evidence.OnlyB = append(c.Evidence.OnlyB, k)
		}
	}
	if a.ModelHash != b.ModelHash {
		c.Geometry = FieldChange{Key: "modelHash", From: a.ModelHash, To: b.ModelHash}
	}
	c.Same = len(c.Junction) == 0 && len(c.Parameters) == 0 && len(c.Components) == 0 && a.ModelHash == b.ModelHash &&
		len(c.Evidence.OnlyA) == 0 && len(c.Evidence.OnlyB) == 0
	return c
}

func sortedKeySet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedIssueKeys(m map[string]IssueBrief) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
