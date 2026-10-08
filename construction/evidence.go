package construction

// Evidence graph (§3.5): Source → Evidence → Claim → (DesignDecision) →
// Component/Junction → AssemblyVersion, with supports / contradicts /
// informs / references edges. The canonical edges live inside the problem's
// private evidence snapshot and are validated and traversed with the
// platform graph package over a construction vocabulary; nothing here calls
// the vault-backed graph writer.
//
// A quote is verified only when it is found on the stated page of the
// retained source text (with any normalisation recorded). A URL alone is
// never verified evidence: nothing in this package fetches anything, and a
// source without retained bytes can only carry unverified, owner-supplied
// passages. Confidence never overrules contrary evidence; disagreement is
// stored, never averaged.

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"manifest/graph"
)

// Graph entity kinds for construction relations.
const (
	EntSource    = "construction-source"
	EntClaim     = "construction-claim"
	EntEvidence  = "construction-evidence"
	EntDecision  = "construction-decision"
	EntComponent = "construction-component"
	EntJunction  = "construction-junction"
	EntAssembly  = "construction-assembly"
	EntParameter = "construction-parameter"
)

// ConstructionVocabulary extends the platform vocabulary; edges reuse the
// platform's supports / contradicts / informs / references kinds.
func ConstructionVocabulary() graph.Vocabulary {
	return graph.Default().Extend([]string{EntSource, EntClaim, EntEvidence, EntDecision, EntComponent, EntJunction, EntAssembly, EntParameter}, nil)
}

// SourceClasses in the plan's default priority (1 = strongest). Priority is
// question-specific (PlanQuestions); this order is only the fallback.
var SourceClasses = []string{"code", "code-interpretation", "manufacturer", "trade-association", "engineering", "detail-library", "secondary", "owner-document"}

var sourceClasses = map[string]int{"code": 1, "code-interpretation": 1, "manufacturer": 2, "trade-association": 3,
	"engineering": 4, "detail-library": 5, "secondary": 6, "owner-document": 7}
var accessStates = map[string]bool{"open": true, "licensed": true, "restricted": true, "unknown": true}
var pageMaps = map[string]bool{"exact": true, "approximate": true, "unavailable": true}
var quoteMatches = map[string]bool{"exact": true, "normalized": true, "not-found": true, "unavailable": true, "owner-supplied": true}
var evidenceMethods = map[string]bool{"page-text-match": true, "owner-supplied-excerpt": true}
var claimVerifications = map[string]bool{"unverified": true, "quote-verified": true, "owner-reviewed": true, "contradicted": true}

// SourcePriority is the class's default priority (lower = stronger).
func SourcePriority(class string) int {
	if p, ok := sourceClasses[class]; ok {
		return p
	}
	return 99
}

// NormalizeQuote collapses whitespace and maps typographic punctuation to
// ASCII — the only normalisation a quote match may use.
func NormalizeQuote(s string) string {
	repl := strings.NewReplacer("“", "\"", "”", "\"", "‘", "'", "’", "'", "–", "-", "—", "-", " ", " ", "­", "")
	s = repl.Replace(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// QuoteNormalization names the normalisation a "normalized" match used.
const QuoteNormalization = "whitespace collapsed; typographic quotes/dashes mapped to ASCII"

// VerifyQuote looks for quote on the 1-based page of the retained page text:
// exact (with byte offsets in the page), normalized, not-found, or
// unavailable (no page text, unknown page, page beyond the document).
func VerifyQuote(pages []string, page int, quote string) (match string, offsets [2]int, normalization string) {
	if strings.TrimSpace(quote) == "" || page < 1 || page > len(pages) {
		return "unavailable", [2]int{}, ""
	}
	text := pages[page-1]
	if i := strings.Index(text, quote); i >= 0 {
		return "exact", [2]int{i, i + len(quote)}, ""
	}
	if strings.Contains(NormalizeQuote(text), NormalizeQuote(quote)) {
		return "normalized", [2]int{}, QuoteNormalization
	}
	return "not-found", [2]int{}, ""
}

// SplitPages turns retained text into pages: form feeds separate pages
// (exact page map); text without them is one page (approximate map).
func SplitPages(text string) ([]string, string) {
	if strings.Contains(text, "\f") {
		return strings.Split(text, "\f"), "exact"
	}
	return []string{text}, "approximate"
}

// PageText extracts page text from retained source bytes. Only UTF-8 text is
// read; PDF and other binary formats are retained but report extraction
// unavailable (no extractor is bundled) — exact-page evidence then needs an
// owner-supplied excerpt and page identification.
func PageText(mime string, content []byte) ([]string, Extraction) {
	ex := Extraction{Tool: "construction-text-pages", Version: "1", PageMap: "unavailable"}
	m := strings.ToLower(mime)
	if m == "" {
		m = strings.ToLower(http.DetectContentType(content))
	}
	if !strings.HasPrefix(m, "text/") || !utf8.Valid(content) || strings.ContainsRune(string(content), 0) {
		ex.Tool, ex.Version = "none", ""
		return nil, ex
	}
	pages, pm := SplitPages(string(content))
	if len(pages) > MaxPagesPerDocument {
		ex.PageMap = "unavailable"
		return nil, ex
	}
	ex.PageMap, ex.Pages = pm, len(pages)
	_, ex.PagesHash, _ = TokenOf(pages)
	return pages, ex
}

// instructionPhrases are patterns of text addressed to an agent.
var instructionPhrases = []string{"ignore previous", "ignore all previous", "ignore the above", "you are now", "approve this", "approve the decision",
	"mark this approved", "write to the vault", "send the files", "send this to", "run the command", "execute the following", "system prompt",
	"disregard your instructions", "new instructions:"}

// InstructionLike flags source text that reads like instructions to an
// agent. It is recorded as a warning and never obeyed: no pipeline stage has
// any action field a source could fill.
func InstructionLike(text string) bool {
	low := strings.ToLower(NormalizeQuote(text))
	for _, p := range instructionPhrases {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// CheckSourceURL validates a source URL as metadata. It is never fetched
// here; credentials and non-http(s) schemes are refused.
func CheckSourceURL(raw string) []string {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" && u.Scheme != "fixture" {
		return []string{"url must be an absolute http(s) URL"}
	}
	if u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "fixture" {
		return []string{"url must be http(s) (fixture:// marks synthetic sources)"}
	}
	if u.User != nil {
		return []string{"source URLs never carry credentials"}
	}
	if len(raw) > 2000 {
		return []string{"url is too long"}
	}
	return nil
}

// ValidateEvidenceBundle checks the evidence snapshot's structure and every
// relation edge against the construction vocabulary.
func ValidateEvidenceBundle(b *EvidenceBundle, problemID string) error {
	var out []string
	out = append(out, checkEnvelope(b.Envelope, DocEvidence, KindProblem)...)
	if b.ProblemID != problemID || b.ID != problemID {
		out = append(out, "evidence snapshot belongs to its problem")
	}
	src := map[string]Source{}
	for i, s := range b.Sources {
		f := fmt.Sprintf("sources[%d]", i)
		if !ValidID(KindSource, s.ID) || src[s.ID].ID != "" {
			out = append(out, f+": needs a unique src- id")
		}
		src[s.ID] = s
		out = append(out, checkText(f+".title", s.Title, 300, true)...)
		out = append(out, checkText(f+".publisher", s.Publisher, 200, false)...)
		out = append(out, checkText(f+".excerpt", s.Excerpt, MaxText, false)...)
		if _, ok := sourceClasses[s.Class]; !ok {
			out = append(out, f+": class is not recognised")
		}
		if !accessStates[s.Access] {
			out = append(out, f+": access must be open, licensed, restricted or unknown")
		}
		if !pageMaps[s.Extraction.PageMap] {
			out = append(out, f+": extraction page map must be exact, approximate or unavailable")
		}
		if s.ContentHash != "" && !ValidToken(s.ContentHash) {
			out = append(out, f+": content hash must be sha256 hex")
		}
		if s.ContentHash != "" && (s.Snapshot == nil || s.Snapshot.Revision != s.ContentHash) {
			out = append(out, f+": a content hash names the retained snapshot it hashes")
		}
		for _, p := range CheckSourceURL(s.URL) {
			out = append(out, f+": "+p)
		}
	}
	ev := map[string]Evidence{}
	for i, e := range b.Evidence {
		f := fmt.Sprintf("evidence[%d]", i)
		if !ValidID(KindEvidence, e.ID) || ev[e.ID].ID != "" {
			out = append(out, f+": needs a unique evd- id")
		}
		ev[e.ID] = e
		s, ok := src[e.SourceID]
		if !ok {
			out = append(out, f+": names an unknown source")
		}
		out = append(out, checkText(f+".quote", e.Quote, 4000, true)...)
		if e.Page < 0 || e.Page > 100000 {
			out = append(out, f+": page out of range")
		}
		if !quoteMatches[e.QuoteMatch] || !evidenceMethods[e.Method] {
			out = append(out, f+": quoteMatch/method is not recognised")
		}
		if e.Verification != "verified" && e.Verification != "unverified" {
			out = append(out, f+": verification must be verified or unverified")
		}
		if e.Verification == "verified" && ((e.QuoteMatch != "exact" && e.QuoteMatch != "normalized") || !ok || s.ContentHash == "" || e.SourceRevision != s.ContentHash) {
			out = append(out, f+": verified evidence needs a quote found in the retained source revision")
		}
		if e.QuoteMatch == "normalized" && e.Normalization == "" {
			out = append(out, f+": a normalized match records its normalisation")
		}
		if e.Confidence < 0 || e.Confidence > 1 || !finite(e.Confidence) {
			out = append(out, f+": confidence must be 0–1")
		}
	}
	cl := map[string]bool{}
	for i, c := range b.Claims {
		f := fmt.Sprintf("claims[%d]", i)
		if !ValidID(KindClaim, c.ID) || cl[c.ID] {
			out = append(out, f+": needs a unique clm- id")
		}
		cl[c.ID] = true
		out = append(out, checkText(f+".statement", c.Statement, 2000, true)...)
		if !provenances[c.Provenance] {
			out = append(out, f+": provenance is not in the taxonomy")
		}
		if !claimVerifications[c.Verification] {
			out = append(out, f+": verification is not recognised")
		}
		for _, id := range append(append([]string{}, c.Supporting...), c.Contradicting...) {
			if ev[id].ID == "" {
				out = append(out, f+": cites unknown evidence "+id)
			}
		}
		if c.Provenance == ProvVerifiedFact || c.Provenance == ProvDirectGuidance {
			verified := false
			for _, id := range c.Supporting {
				verified = verified || ev[id].Verification == "verified"
			}
			if !verified {
				out = append(out, f+": "+c.Provenance+" needs at least one verified supporting quote")
			}
		}
		if c.Confidence < 0 || c.Confidence > 1 || !finite(c.Confidence) {
			out = append(out, f+": confidence must be 0–1")
		}
	}
	vocab := ConstructionVocabulary()
	for i, r := range b.Relations {
		if err := graph.Validate(r.Edge(), vocab); err != nil {
			out = append(out, fmt.Sprintf("relations[%d]: %v", i, err))
		}
		if r.Kind != graph.EdgeSupports && r.Kind != graph.EdgeContradicts && r.Kind != graph.EdgeInforms && r.Kind != graph.EdgeReferences {
			out = append(out, fmt.Sprintf("relations[%d]: kind must be supports, contradicts, informs or references", i))
		}
	}
	if len(out) > 0 {
		return Invalid(out...)
	}
	return nil
}

// Edge is the relation as a platform graph edge.
func (r Relation) Edge() graph.Edge {
	e := graph.Edge{From: graph.ParseRef(r.From), To: graph.ParseRef(r.To), Kind: r.Kind, Basis: r.Basis, Source: r.Source, Inferred: r.Inferred}
	if r.Confidence > 0 {
		e.Confidence = graph.FormatConfidence(r.Confidence)
	}
	return e
}

func init() {
	docValidators[DocEvidence] = func(st *State, key string, doc any) error {
		pid := ""
		if st.Problem != nil {
			pid = st.Problem.ID
		}
		return ValidateEvidenceBundle(doc.(*EvidenceBundle), pid)
	}
	registerOp("AddSource", TargetEvidence, true, func() Operation { return &AddSource{} })
	registerOp("AddEvidence", TargetEvidence, true, func() Operation { return &AddEvidence{} })
	registerOp("LinkEvidence", TargetAssembly, false, func() Operation { return &LinkEvidence{} })
}

// ---- graph view --------------------------------------------------------------------------

func gref(kind, id string) string { return kind + ":" + id }

// EvidenceEdges is the construction graph for one assembly: the snapshot's
// canonical relations plus the edges its evidence links and decisions imply.
func EvidenceEdges(b *EvidenceBundle, a *Assembly, decisions map[string]*Decision) []graph.Edge {
	var out []graph.Edge
	if b == nil {
		return out
	}
	for _, r := range b.Relations {
		out = append(out, r.Edge())
	}
	if a == nil {
		return out
	}
	claimOf := map[string][]string{} // evidence id → claims citing it
	for _, c := range b.Claims {
		for _, id := range append(append([]string{}, c.Supporting...), c.Contradicting...) {
			claimOf[id] = append(claimOf[id], c.ID)
		}
	}
	targetRef := func(t string) string {
		switch {
		case strings.HasPrefix(t, "param:"):
			return gref(EntParameter, a.ID+"/"+strings.TrimPrefix(t, "param:"))
		case t == a.ID:
			return gref(EntAssembly, a.ID)
		case t == a.Junction.ID:
			return gref(EntJunction, t)
		}
		return gref(EntComponent, t)
	}
	asmRef := gref(EntAssembly, a.ID)
	seen := map[string]bool{}
	add := func(r Relation) {
		e := r.Edge()
		if !seen[e.Key()] {
			seen[e.Key()] = true
			out = append(out, e)
		}
	}
	for _, l := range a.EvidenceLinks {
		kind := l.Relation
		for _, cid := range claimOf[l.EvidenceID] {
			add(Relation{From: gref(EntClaim, cid), To: targetRef(l.Target), Kind: kind, Basis: "evidence " + l.EvidenceID + " linked to " + l.Target, Source: "assembly " + a.ID})
		}
		if len(claimOf[l.EvidenceID]) == 0 {
			add(Relation{From: gref(EntEvidence, l.EvidenceID), To: targetRef(l.Target), Kind: kind, Basis: "evidence linked to " + l.Target, Source: "assembly " + a.ID})
		}
		if t := targetRef(l.Target); t != asmRef {
			add(Relation{From: t, To: asmRef, Kind: graph.EdgeReferences, Basis: "part of " + a.Name, Source: "assembly " + a.ID})
		}
	}
	ids := make([]string, 0, len(decisions))
	for id := range decisions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := decisions[id]
		if d.Assembly.ID != a.ID {
			continue
		}
		for _, eid := range d.Supporting {
			for _, cid := range claimOf[eid] {
				add(Relation{From: gref(EntClaim, cid), To: gref(EntDecision, d.ID), Kind: graph.EdgeSupports, Basis: "cited by decision " + d.Title, Source: "decision " + d.ID})
			}
		}
		add(Relation{From: gref(EntDecision, d.ID), To: asmRef, Kind: graph.EdgeReferences, Basis: "decision binds " + a.Name, Source: "decision " + d.ID})
	}
	return out
}

// PathStep is one node of an evidence path shown to the owner.
type PathStep struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
	Edge  string `json:"edge,omitempty"` // kind of the edge that reached this node
}

// EvidencePaths returns the directed Source → … → Assembly paths through one
// target (component, junction, "param:<name>" or the assembly), found with
// graph.Paths over the validated construction edges.
func EvidencePaths(b *EvidenceBundle, a *Assembly, decisions map[string]*Decision, target string) [][]PathStep {
	out := [][]PathStep{}
	if b == nil || a == nil {
		return out
	}
	g := graph.Build(EvidenceEdges(b, a, decisions), ConstructionVocabulary())
	var starts []graph.Ref
	for _, s := range b.Sources {
		starts = append(starts, graph.R(EntSource, s.ID))
	}
	labels := pathLabels(b, a)
	for _, p := range g.Paths(starts, graph.R(EntAssembly, a.ID), graph.PathOptions{Directed: true, MaxHops: 6, TopN: 64}) {
		through := false
		for _, n := range p.Nodes {
			if n.ID == target || n.ID == a.ID+"/"+strings.TrimPrefix(target, "param:") {
				through = true
			}
		}
		if target != a.ID && !through {
			continue
		}
		steps := make([]PathStep, 0, len(p.Nodes))
		for i, n := range p.Nodes {
			st := PathStep{Kind: n.Kind, ID: n.ID, Label: labels[n.String()]}
			if i > 0 {
				st.Edge = p.Edges[i-1].Kind
			}
			if st.Label == "" {
				st.Label = n.ID
			}
			steps = append(steps, st)
		}
		out = append(out, steps)
	}
	return out
}

func pathLabels(b *EvidenceBundle, a *Assembly) map[string]string {
	m := map[string]string{gref(EntAssembly, a.ID): a.Name, gref(EntJunction, a.Junction.ID): "Junction " + a.Junction.Type}
	for _, s := range b.Sources {
		l := s.Title
		if s.Fictional {
			l += " (fictional fixture)"
		}
		m[gref(EntSource, s.ID)] = l
	}
	for _, e := range b.Evidence {
		m[gref(EntEvidence, e.ID)] = fmt.Sprintf("p.%d %q (%s)", e.Page, truncate(e.Quote, 80), e.Verification)
	}
	for _, c := range b.Claims {
		m[gref(EntClaim, c.ID)] = c.Statement
	}
	for _, c := range a.Components {
		m[gref(EntComponent, c.ID)] = c.Name
	}
	for k := range a.Parameters {
		m[gref(EntParameter, a.ID+"/"+k)] = "parameter " + k
	}
	return m
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// ---- evidence snapshot helpers ----------------------------------------------------------

func (b *EvidenceBundle) source(id string) (*Source, int) {
	for i := range b.Sources {
		if b.Sources[i].ID == id {
			return &b.Sources[i], i
		}
	}
	return nil, -1
}

func (b *EvidenceBundle) evidence(id string) (*Evidence, bool) {
	for i := range b.Evidence {
		if b.Evidence[i].ID == id {
			return &b.Evidence[i], true
		}
	}
	return nil, false
}

func (b *EvidenceBundle) claim(id string) *Claim {
	for i := range b.Claims {
		if b.Claims[i].ID == id {
			return &b.Claims[i]
		}
	}
	return nil
}

// claimByStatement finds a claim with the same statement (normalised) so two
// passages of the same proposition land on one claim.
func (b *EvidenceBundle) claimByStatement(s string) *Claim {
	n := strings.ToLower(NormalizeQuote(s))
	for i := range b.Claims {
		if strings.ToLower(NormalizeQuote(b.Claims[i].Statement)) == n {
			return &b.Claims[i]
		}
	}
	return nil
}

// sameEvidence finds an existing evidence row for the same passage.
func (b *EvidenceBundle) sameEvidence(sourceID string, page int, quote string) *Evidence {
	n := NormalizeQuote(quote)
	for i := range b.Evidence {
		e := &b.Evidence[i]
		if e.SourceID == sourceID && e.Page == page && NormalizeQuote(e.Quote) == n {
			return e
		}
	}
	return nil
}

func (b *EvidenceBundle) addRelation(r Relation) {
	key := r.Edge().Key()
	for _, x := range b.Relations {
		if x.Edge().Key() == key {
			return
		}
	}
	b.Relations = append(b.Relations, r)
}

// refreshClaims recomputes each claim's verification from its evidence:
// contradicted when any verified passage contradicts it, quote-verified when
// a verified passage supports it, else unverified (owner review is kept).
func (b *EvidenceBundle) refreshClaims() {
	ver := map[string]bool{}
	for _, e := range b.Evidence {
		ver[e.ID] = e.Verification == "verified"
	}
	for i := range b.Claims {
		c := &b.Claims[i]
		sup, con := false, false
		for _, id := range c.Supporting {
			sup = sup || ver[id]
		}
		for _, id := range c.Contradicting {
			con = con || ver[id]
		}
		switch {
		case con:
			c.Verification = "contradicted"
		case sup:
			if c.Verification != "owner-reviewed" {
				c.Verification = "quote-verified"
			}
		default:
			if c.Verification != "owner-reviewed" {
				c.Verification = "unverified"
			}
		}
	}
}

// Passage is a proposed quote from one retained source, before verification.
type Passage struct {
	Quote         string       `json:"quote"`
	Page          int          `json:"page"`
	PageLabel     string       `json:"pageLabel,omitempty"`
	Section       string       `json:"section,omitempty"`
	Figure        string       `json:"figure,omitempty"`
	Table         string       `json:"table,omitempty"`
	Claim         string       `json:"claim"`
	Values        []ClaimValue `json:"values,omitempty"`
	Relation      string       `json:"relation"` // supports | contradicts
	Topics        []string     `json:"topics,omitempty"`
	Applicability []string     `json:"applicability,omitempty"`
	Confidence    float64      `json:"confidence"`
}

func (p Passage) check() []string {
	var out []string
	out = append(out, checkText("quote", p.Quote, 4000, true)...)
	out = append(out, checkText("claim", p.Claim, 2000, true)...)
	for _, f := range []string{p.PageLabel, p.Section, p.Figure, p.Table} {
		out = append(out, checkText("locator", f, 100, false)...)
	}
	if p.Relation != "supports" && p.Relation != "contradicts" {
		out = append(out, "relation must be supports or contradicts")
	}
	if p.Page < 0 || p.Page > 100000 {
		out = append(out, "page out of range")
	}
	if !finite(p.Confidence) || p.Confidence < 0 || p.Confidence > 1 {
		out = append(out, "confidence must be 0–1")
	}
	if len(p.Topics) > 16 || len(p.Applicability) > 16 || len(p.Values) > 16 {
		out = append(out, "too many topics/applicability/values")
	}
	for _, t := range append(append([]string{}, p.Topics...), p.Applicability...) {
		out = append(out, checkText("tag", t, 120, true)...)
	}
	for _, v := range p.Values {
		if !finite(v.Value) {
			out = append(out, "values must be finite")
		}
	}
	return out
}

// claimProvenance is the provenance a verified passage may establish from a
// source class under the problem's known context. Code text never becomes
// directly-applicable guidance while the jurisdiction/adoption is not
// established; secondary discussion is a lead only.
func claimProvenance(s Source, p *Problem, verified bool, applic []string) string {
	if !verified {
		return ProvUnknown
	}
	switch s.Class {
	case "secondary":
		return ProvUnknown
	case "code", "code-interpretation":
		if p != nil && p.Jurisdiction.State == StateKnown && s.Jurisdiction != "" && strings.EqualFold(strings.TrimSpace(p.Jurisdiction.Text), strings.TrimSpace(s.Jurisdiction)) {
			return ProvDirectGuidance
		}
		return ProvAdaptedPrecedent
	}
	return ProvAdaptedPrecedent
}

// RecordPassage verifies one proposed passage against the retained pages and
// merges it into the snapshot: a verified (or owner-supplied, unverified)
// evidence row, its claim, and the Source→Evidence→Claim edges. A quote that
// is not on that page of the retained text is rejected and nothing is added.
func (b *EvidenceBundle) RecordPassage(s *Source, pages []string, p Passage, newEvidenceID, newClaimID, proposer string, actor Actor, prob *Problem) (*Evidence, *Claim, string) {
	method, match, normalization := "page-text-match", "unavailable", ""
	var offsets [2]int
	verified := false
	switch {
	case s.ContentHash != "" && pages != nil:
		match, offsets, normalization = VerifyQuote(pages, p.Page, p.Quote)
		switch match {
		case "exact", "normalized":
			verified = true
		case "not-found":
			return nil, nil, "quote not found on page " + fmt.Sprint(p.Page) + " of the retained source"
		default:
			return nil, nil, fmt.Sprintf("page %d does not exist in the retained source (%d pages)", p.Page, len(pages))
		}
	case proposer == "owner":
		method, match = "owner-supplied-excerpt", "owner-supplied"
	default:
		return nil, nil, "no page text is available for this source; the owner must supply the excerpt and page"
	}
	e := b.sameEvidence(s.ID, p.Page, p.Quote)
	if e == nil {
		b.Evidence = append(b.Evidence, Evidence{ID: newEvidenceID, SourceID: s.ID, SourceRevision: s.ContentHash, Quote: p.Quote, Page: p.Page,
			PageLabel: p.PageLabel, Figure: p.Figure, Table: p.Table, Section: p.Section, Offsets: offsets, Method: method, QuoteMatch: match,
			Normalization: normalization, Supports: []string{}, Refutes: []string{}, Applicability: nonNilStrings(p.Applicability),
			Verification: map[bool]string{true: "verified", false: "unverified"}[verified], Confidence: p.Confidence, ProposedBy: proposer, Run: actor.Run})
		e = &b.Evidence[len(b.Evidence)-1]
	}
	c := b.claimByStatement(p.Claim)
	if c == nil {
		b.Claims = append(b.Claims, Claim{ID: newClaimID, Statement: strings.TrimSpace(p.Claim), Values: nonNilValues(p.Values),
			Provenance: claimProvenance(*s, prob, verified, p.Applicability), Applicability: nonNilStrings(p.Applicability), Confidence: p.Confidence,
			Verification: "unverified", Supporting: []string{}, Contradicting: []string{}, Author: actor, Topics: sortedUnique(p.Topics)})
		c = &b.Claims[len(b.Claims)-1]
	} else {
		c.Topics = sortedUnique(append(c.Topics, p.Topics...))
		c.Applicability = sortedUnique(append(c.Applicability, p.Applicability...))
		if verified && c.Provenance == ProvUnknown && s.Class != "secondary" {
			c.Provenance = claimProvenance(*s, prob, true, p.Applicability)
		}
	}
	if p.Relation == "contradicts" {
		if !contains(c.Contradicting, e.ID) {
			c.Contradicting = append(c.Contradicting, e.ID)
		}
		if !contains(e.Refutes, gref(EntClaim, c.ID)) {
			e.Refutes = append(e.Refutes, gref(EntClaim, c.ID))
		}
	} else {
		if !contains(c.Supporting, e.ID) {
			c.Supporting = append(c.Supporting, e.ID)
		}
		if !contains(e.Supports, gref(EntClaim, c.ID)) {
			e.Supports = append(e.Supports, gref(EntClaim, c.ID))
		}
	}
	basis := fmt.Sprintf("quoted from page %d", p.Page)
	if !verified {
		basis = fmt.Sprintf("owner-supplied excerpt, page %d (not verified against retained text)", p.Page)
	}
	b.addRelation(Relation{From: gref(EntSource, s.ID), To: gref(EntEvidence, e.ID), Kind: graph.EdgeInforms, Basis: basis, Source: orDefault(s.ContentHash, "source "+s.ID), Inferred: false})
	kind := graph.EdgeSupports
	if p.Relation == "contradicts" {
		kind = graph.EdgeContradicts
	}
	b.addRelation(Relation{From: gref(EntEvidence, e.ID), To: gref(EntClaim, c.ID), Kind: kind, Basis: "passage " + p.Relation + " the claim", Source: "evidence " + e.ID, Inferred: proposer != "owner", Confidence: p.Confidence})
	b.refreshClaims()
	return e, c, ""
}

func nonNilStrings(x []string) []string {
	if x == nil {
		return []string{}
	}
	return x
}

func nonNilValues(x []ClaimValue) []ClaimValue {
	if x == nil {
		return []ClaimValue{}
	}
	return x
}

// ---- owner evidence operations -----------------------------------------------------------

// AddSource registers a source in the problem's evidence snapshot. Its
// content, when given, is an already-retained problem input (exact artifact
// revision); a URL is metadata only and is never fetched.
type AddSource struct {
	Op             string      `json:"op"`
	ID             string      `json:"id"`
	Title          string      `json:"title"`
	Publisher      string      `json:"publisher,omitempty"`
	Class          string      `json:"class"`
	URL            string      `json:"url,omitempty"`
	Access         string      `json:"access,omitempty"`
	Jurisdiction   string      `json:"jurisdiction,omitempty"`
	Edition        string      `json:"edition,omitempty"`
	PublishedAt    string      `json:"publishedAt,omitempty"`
	Input          *VersionRef `json:"input,omitempty"`
	Excerpt        string      `json:"excerpt,omitempty"`
	OmissionReason string      `json:"omissionReason,omitempty"`
}

func (o *AddSource) Name() string { return "AddSource" }
func (o *AddSource) Check() []string {
	out := checkText("title", o.Title, 300, true)
	out = append(out, checkText("publisher", o.Publisher, 200, false)...)
	out = append(out, checkText("jurisdiction", o.Jurisdiction, 200, false)...)
	out = append(out, checkText("edition", o.Edition, 100, false)...)
	out = append(out, checkText("publishedAt", o.PublishedAt, 40, false)...)
	out = append(out, checkText("excerpt", o.Excerpt, MaxText, false)...)
	out = append(out, checkText("omissionReason", o.OmissionReason, 500, false)...)
	if !ValidID(KindSource, o.ID) {
		out = append(out, "id must be a new src- id chosen by the caller")
	}
	if _, ok := sourceClasses[o.Class]; !ok {
		out = append(out, "class must be one of "+strings.Join(SourceClasses, ", "))
	}
	if o.Access != "" && !accessStates[o.Access] {
		out = append(out, "access must be open, licensed, restricted or unknown")
	}
	out = append(out, CheckSourceURL(o.URL)...)
	if strings.HasPrefix(o.URL, "fixture:") {
		out = append(out, "fixture:// sources come only from the fixture adapter")
	}
	if o.Input != nil && (len(o.Input.ID) != 16 || !ValidToken(o.Input.Revision)) {
		out = append(out, "input must name a retained input's artifact id and revision")
	}
	return out
}
func (o *AddSource) Apply(tx *Tx, c *ApplyContext) error {
	b := tx.Next.Evidence
	if s, _ := b.source(o.ID); s != nil {
		return Invalid("source id already exists")
	}
	if len(b.Sources) >= 500 {
		return Invalid("source limit reached")
	}
	s := Source{ID: o.ID, Title: strings.TrimSpace(o.Title), Publisher: o.Publisher, Class: o.Class, URL: o.URL, Access: orDefault(o.Access, "unknown"),
		Jurisdiction: o.Jurisdiction, Edition: o.Edition, PublishedAt: o.PublishedAt, Excerpt: o.Excerpt, OmissionReason: o.OmissionReason,
		Extraction: Extraction{Tool: "none", PageMap: "unavailable"}}
	if o.Input != nil {
		var in *InputRef
		for i := range tx.Next.Problem.Inputs {
			if tx.Next.Problem.Inputs[i].ArtifactID == o.Input.ID && tx.Next.Problem.Inputs[i].Revision == o.Input.Revision {
				in = &tx.Next.Problem.Inputs[i]
			}
		}
		if in == nil {
			return NotFound("no such input in this problem")
		}
		content, err := tx.store.Content(tx.subject, tx.Next.Problem.ID, in.ArtifactID, in.Revision)
		if err != nil {
			return err
		}
		pages, ex := PageText(in.Mime, content)
		s.Locator, s.Mime, s.Bytes = "input:"+in.ArtifactID+"@"+in.Revision, in.Mime, int64(len(content))
		s.ContentHash, s.Snapshot, s.Extraction = in.Revision, &VersionRef{ID: in.ArtifactID, Revision: in.Revision}, ex
		s.RetrievedAt = in.RetainedAt
		for _, p := range pages {
			if InstructionLike(p) {
				s.Warnings = append(s.Warnings, "contains text addressed to an agent; recorded, never followed")
				break
			}
		}
		if pages == nil {
			s.Warnings = append(s.Warnings, "page text unavailable ("+orDefault(in.Mime, "unknown type")+"): exact-page evidence needs an owner-supplied excerpt and page")
		}
		tx.Input(in.Revision)
	} else if o.URL != "" {
		s.Warnings = append(s.Warnings, "URL recorded as metadata only; nothing was fetched or retained, so no quote from it can be verified")
	}
	b.Sources = append(b.Sources, s)
	tx.Record(o.Name(), "source/"+s.ID, nil, s)
	tx.Summary("added source " + s.Title)
	return nil
}

// AddEvidence records one passage from a source. With retained page text the
// quote must be found on that page (else nothing is added); without it the
// passage is kept as an owner-supplied, unverified excerpt.
type AddEvidence struct {
	Op       string `json:"op"`
	ID       string `json:"id"`
	ClaimID  string `json:"claimId"`
	SourceID string `json:"sourceId"`
	Passage
}

func (o *AddEvidence) Name() string { return "AddEvidence" }
func (o *AddEvidence) Check() []string {
	out := o.Passage.check()
	if !ValidID(KindEvidence, o.ID) || !ValidID(KindClaim, o.ClaimID) || !ValidID(KindSource, o.SourceID) {
		out = append(out, "id (evd-), claimId (clm-, new or existing) and sourceId (src-) are required")
	}
	return out
}
func (o *AddEvidence) Apply(tx *Tx, c *ApplyContext) error {
	b := tx.Next.Evidence
	s, _ := b.source(o.SourceID)
	if s == nil {
		return NotFound("no such source in this problem")
	}
	if _, dup := b.evidence(o.ID); dup {
		return Invalid("evidence id already exists")
	}
	if len(b.Evidence) >= 2000 {
		return Invalid("evidence limit reached")
	}
	var pages []string
	if s.Snapshot != nil && s.Extraction.PageMap != "unavailable" {
		content, err := tx.store.Content(tx.subject, tx.Next.Problem.ID, s.Snapshot.ID, s.Snapshot.Revision)
		if err != nil {
			return err
		}
		pages, _ = PageText(s.Mime, content)
	}
	if existing := b.claim(o.ClaimID); existing != nil && NormalizeQuote(strings.ToLower(existing.Statement)) != NormalizeQuote(strings.ToLower(o.Claim)) {
		return Invalid("claimId names a claim with a different statement")
	}
	e, cl, reason := b.RecordPassage(s, pages, o.Passage, o.ID, o.ClaimID, "owner", tx.Actor, tx.Next.Problem)
	if e == nil {
		return Invalid(reason + "; nothing was added (no manufactured citations)")
	}
	tx.Record(o.Name(), "evidence/"+e.ID, nil, map[string]any{"evidence": e, "claim": cl.ID})
	tx.Summary("added evidence from " + s.Title)
	return nil
}

// LinkEvidence ties (or unties) evidence to a component, the junction, a
// parameter or the assembly itself. Agents may do this on draft/proposed
// assemblies; the link never changes the evidence.
type LinkEvidence struct {
	Op         string `json:"op"`
	Target     string `json:"target"`
	EvidenceID string `json:"evidenceId"`
	Relation   string `json:"relation"`
	Remove     bool   `json:"remove,omitempty"`
}

func (o *LinkEvidence) Name() string { return "LinkEvidence" }
func (o *LinkEvidence) Check() []string {
	var out []string
	if !ValidID(KindEvidence, o.EvidenceID) {
		out = append(out, "evidenceId must be an evd- id")
	}
	if !evidenceRelations[o.Relation] {
		out = append(out, "relation must be supports, contradicts or informs")
	}
	if !(strings.HasPrefix(o.Target, "param:") && len(o.Target) < 80) && !ValidID(KindComponent, o.Target) && !ValidID(KindJunction, o.Target) && !ValidID(KindAssembly, o.Target) {
		out = append(out, "target must be a component, junction, assembly or param:<name>")
	}
	return out
}
func (o *LinkEvidence) Apply(tx *Tx, c *ApplyContext) error {
	a, err := asm(tx, c)
	if err != nil {
		return err
	}
	if _, ok := tx.Next.Evidence.evidence(o.EvidenceID); !ok {
		return NotFound("no such evidence in this problem")
	}
	switch {
	case strings.HasPrefix(o.Target, "param:"):
		if _, ok := a.Parameters[strings.TrimPrefix(o.Target, "param:")]; !ok {
			return NotFound("no such parameter")
		}
	case o.Target == a.ID || o.Target == a.Junction.ID:
	default:
		if _, ok := a.Component(o.Target); !ok {
			return NotFound("no such component")
		}
	}
	link := EvidenceLink{Target: o.Target, EvidenceID: o.EvidenceID, Relation: o.Relation}
	for i, l := range a.EvidenceLinks {
		if l.Target == o.Target && l.EvidenceID == o.EvidenceID {
			if o.Remove {
				a.EvidenceLinks = append(a.EvidenceLinks[:i], a.EvidenceLinks[i+1:]...)
				tx.Record(o.Name(), "evidenceLink/"+o.Target+"/"+o.EvidenceID, l, nil)
				return nil
			}
			if l.Relation == o.Relation {
				return nil
			}
			a.EvidenceLinks[i] = link
			tx.Record(o.Name(), "evidenceLink/"+o.Target+"/"+o.EvidenceID, l, link)
			return nil
		}
	}
	if o.Remove {
		return NotFound("no such evidence link")
	}
	if len(a.EvidenceLinks) >= 1000 {
		return Invalid("evidence link limit reached")
	}
	a.EvidenceLinks = append(a.EvidenceLinks, link)
	tx.Record(o.Name(), "evidenceLink/"+o.Target+"/"+o.EvidenceID, nil, link)
	return nil
}
