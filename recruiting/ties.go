package recruiting

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"manifest/graph"
	"manifest/recruiting/sources"
)

// SOCIAL TIES (recruiting graph, social-tie discovery). A draft arrives with
// what a registry said about the person's works: who else signed the paper,
// which institution each of them printed under it. The adapters already turn
// that into relationship CLAIMS keyed by durable external id, and the store
// already writes those claims into network/edges.md on accept, resolving any
// key the vault has matched to a record (edges_identity.go). This file is the
// step after that: projecting what is now KNOWN into the general graph, the
// same way knowledge.go projects the draft's topics.
//
// The never-guess rule, stated once:
//
//   - a person ↔ person tie reaches the general graph ONLY when both
//     endpoints resolve to people this vault knows — a candidate record or a
//     connector row — through PersonResolver. A stranger's ORCID stays where
//     it is (network/edges.md, as the durable key it is) until the day they
//     become a record, at which point their own accept carries the tie here.
//     A display name resolves to nobody, ever.
//   - every tie keeps the basis, source, confidence, inferred bit and
//     evidence of the row it came from. Nothing is re-derived and nothing is
//     upgraded: the general graph mirrors the claim, it does not restate it.
//   - a paper is registered only from a citation that IDENTIFIES a work — a
//     DOI, an OpenAlex work id, a PubMed id — never from a title string.
//
// Pure: it writes nothing. ApplyKnowledge is the one writer (through the
// injected graph store), and KnowledgeClaims.WithTies folds these claims in
// so an accept is still one application, one receipt.

const (
	// TieAuthoredConfidence is the weight of "this person is an author on
	// this work" read from a registry's own author list. A stated fact from a
	// primary index; below the owner's word because indexes mis-attribute.
	TieAuthoredConfidence = "0.90"

	// paperTitleChars bounds the title an entity row carries.
	paperTitleChars = 160
)

// TieSkip is one claim the gate refused, with the reason — so the accept
// payload can say "these coauthors are not on the board" rather than
// silently writing fewer edges than the paper named.
type TieSkip struct {
	Endpoint string `json:"endpoint"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
}

// TieClaims is what one accepted person lets the general graph state about
// their works and the known people they share them with.
type TieClaims struct {
	Papers  []graph.Entity `json:"papers"`
	Edges   []graph.Edge   `json:"edges"`
	Skipped []TieSkip      `json:"skipped,omitempty"`
}

// symmetricKinds read the same either way; a tie of one of these kinds is
// stored with its endpoints in a canonical order so the same pair claimed
// from either side is one row.
var symmetricKinds = map[string]bool{
	graph.EdgeCoauthor: true, graph.EdgeCoinventor: true, graph.EdgeCoworker: true,
	graph.EdgeSameLab: true, graph.EdgeSameGrant: true, graph.EdgeSameRepo: true,
	graph.EdgeSameConference: true, graph.EdgeSameCompany: true,
	string(sources.EdgeSameMeeting): true, string(sources.EdgeCoMentioned): true,
}

// doiRe (intake.go) matches the DOI itself; these two match the other two
// registries a citation URL can point at.
var (
	openAlexW  = regexp.MustCompile(`(?i)openalex\.org/(W\d{4,})\b`)
	pubmedIDRe = regexp.MustCompile(`(?i)pubmed\.ncbi\.nlm\.nih\.gov/(\d{4,9})`)
)

// PaperRef identifies a work from the URL a citation points at: a DOI
// (canonical, lower-cased — DOIs are case-insensitive), an OpenAlex work id,
// or a PubMed id. Anything else — an author page, a lab site, a repo — is
// not a work, and reports false.
func PaperRef(rawURL string) (graph.Ref, bool) {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return graph.Ref{}, false
	}
	if strings.Contains(strings.ToLower(u), "doi.org/") {
		if m := doiRe.FindStringSubmatch(u); m != nil {
			return graph.R(graph.KindPaper, "doi/"+strings.ToLower(strings.TrimRight(m[1], ".)"))), true
		}
	}
	if m := openAlexW.FindStringSubmatch(u); m != nil {
		return graph.R(graph.KindPaper, sources.ExtNodePrefix+"openalex/"+strings.ToUpper(m[1])), true
	}
	if m := pubmedIDRe.FindStringSubmatch(u); m != nil {
		return graph.R(graph.KindPaper, sources.ExtNodePrefix+"pubmed/"+m[1]), true
	}
	return graph.Ref{}, false
}

// paperOf is PaperRef over an evidence row, preferring the DOI the snippet
// names (a PubMed summary prints `doi: …`) so the same work reached through
// PubMed and through OpenAlex is one node.
func paperOf(ev sources.Evidence) (graph.Ref, bool) {
	for _, part := range strings.Split(ev.Snippet, " · ") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(part), "doi: "); ok {
			if ref, ok := PaperRef("https://doi.org/" + strings.TrimSpace(v)); ok {
				return ref, true
			}
		}
	}
	return PaperRef(ev.URLOrFile)
}

// paperTitle is the citation a row shows for the work: the `title:` segment
// of a PubMed summary, else the first segment of the snippet (the work
// sweep's citation line), bounded.
func paperTitle(snippet string) string {
	parts := strings.Split(snippet, " · ")
	title := ""
	for _, part := range parts {
		if v, ok := strings.CutPrefix(strings.TrimSpace(part), "title: "); ok {
			title = v
			break
		}
	}
	if title == "" && len(parts) > 0 {
		title = strings.TrimSpace(parts[0])
	}
	if r := []rune(title); len(r) > paperTitleChars {
		title = string(r[:paperTitleChars]) + "…"
	}
	return title
}

// DeriveTies projects one accepted person's ties onto the general graph.
//
//   - d is the draft as accepted (its publication evidence names the works;
//     its edge claims name the far endpoints the gate will be asked about);
//   - candidateID is the record the person became;
//   - network is network/edges.md AS WRITTEN after the accept — the store
//     has already added the draft's claims and repointed any key that names
//     this person onto candidateID. Rows not touching candidateID are
//     ignored; derived (calendar, notes) edges are not on file and are never
//     mirrored, by design;
//   - resolve is PersonResolver: the only judge of "known".
//
// Output is deterministic: papers by id, authored edges by paper, ties by
// key. A pair claimed twice keeps the stronger claim — a stated one over an
// inferred one, then the higher confidence — so a low-confidence overlap can
// never displace a cited coauthorship.
func DeriveTies(d sources.CandidateDraft, candidateID string, network []Edge, resolve func(string) (string, bool), now time.Time) TieClaims {
	date := now.UTC().Format("2006-01-02")
	me := strings.TrimSpace(candidateID)
	t := TieClaims{Papers: []graph.Entity{}, Edges: []graph.Edge{}}
	if me == "" {
		return t
	}
	if resolve == nil {
		resolve = func(string) (string, bool) { return "", false }
	}
	person := PersonRef(me)

	// ---- the works, and the person's authorship of each
	seenPaper := map[string]bool{}
	var authored []graph.Edge
	for _, ev := range d.Evidence {
		if ev.Kind != sources.EvidencePublication {
			continue
		}
		ref, ok := paperOf(ev)
		if !ok || seenPaper[ref.ID] {
			continue
		}
		seenPaper[ref.ID] = true
		src := orSource(strings.TrimSpace(ev.SourceID))
		t.Papers = append(t.Papers, graph.Entity{
			ID: ref.ID, Kind: graph.KindPaper, Title: paperTitle(ev.Snippet),
			Ref: strings.TrimSpace(ev.URLOrFile), Source: src, Added: date,
		})
		authored = append(authored, graph.Edge{
			From: person, To: ref, Kind: graph.EdgeAuthored,
			Basis: "listed as author by " + src + ": " + strings.TrimSpace(ev.Snippet), Confidence: TieAuthoredConfidence,
			Inferred: false, Source: src, Evidence: strings.TrimSpace(ev.URLOrFile), Observed: date,
		})
	}
	sort.Slice(t.Papers, func(i, j int) bool { return t.Papers[i].ID < t.Papers[j].ID })
	sort.Slice(authored, func(i, j int) bool { return authored[i].To.ID < authored[j].To.ID })

	// ---- the people: every row on file that touches this person
	vocab := graph.Default()
	best := map[string]graph.Edge{}
	skipped := map[string]TieSkip{}
	skip := func(endpoint, kind, reason string) {
		k := endpoint + "\x00" + kind
		if _, have := skipped[k]; !have {
			skipped[k] = TieSkip{Endpoint: endpoint, Kind: kind, Reason: reason}
		}
	}
	for _, row := range network {
		from, to := strings.TrimSpace(row.From), strings.TrimSpace(row.To)
		if from != me && to != me {
			continue
		}
		other := to
		if other == me {
			other = from
		}
		if other == me || other == "" {
			continue
		}
		kind := strings.TrimSpace(row.Kind)
		id, ok := resolve(other)
		if !ok {
			skip(other, kind, "no known person answers to this id — the tie stays in network/edges.md by its external key")
			continue
		}
		if id == me {
			continue
		}
		if !vocab.ValidEdgeKind(kind) {
			skip(other, kind, "kind is outside the platform graph vocabulary")
			continue
		}
		e := row.Graph()
		e.From, e.To = PersonRef(from), PersonRef(to)
		if from == me {
			e.To = PersonRef(id)
		} else {
			e.From = PersonRef(id)
		}
		if symmetricKinds[kind] && e.From.ID > e.To.ID {
			e.From, e.To = e.To, e.From
		}
		if e.Observed == "" {
			e.Observed = date
		}
		e.Unknown = nil
		if graph.Validate(e, vocab) != nil {
			skip(other, kind, "the row on file does not validate against the platform graph")
			continue
		}
		if have, ok := best[e.Key()]; !ok || stronger(e, have) {
			best[e.Key()] = e
		}
	}
	// the draft's own claims that resolved to nobody are named too, so the
	// accept payload accounts for every coauthor the paper listed
	for _, c := range d.Edges {
		far := strings.TrimSpace(c.From)
		if far == "" || far == me {
			continue
		}
		if _, ok := resolve(far); !ok {
			skip(far, string(c.Type), "no known person answers to this id — the tie stays in network/edges.md by its external key")
		}
	}

	keys := make([]string, 0, len(best))
	for k := range best {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Edges = append(t.Edges, authored...)
	for _, k := range keys {
		t.Edges = append(t.Edges, best[k])
	}
	for _, s := range skipped {
		t.Skipped = append(t.Skipped, s)
	}
	sort.Slice(t.Skipped, func(i, j int) bool {
		if t.Skipped[i].Endpoint != t.Skipped[j].Endpoint {
			return t.Skipped[i].Endpoint < t.Skipped[j].Endpoint
		}
		return t.Skipped[i].Kind < t.Skipped[j].Kind
	})
	return t
}

// stronger decides a conflict between two claims of the same key: a stated
// claim beats an inferred one whatever the numbers say; between equals the
// higher confidence wins; a full tie keeps the lexically smaller basis so
// the result is a function of the input.
func stronger(a, b graph.Edge) bool {
	if a.Inferred != b.Inferred {
		return !a.Inferred
	}
	if wa, wb := a.Weight(), b.Weight(); wa != wb {
		return wa > wb
	}
	return a.Basis < b.Basis
}

// WithTies folds tie claims into the knowledge claims of the same accept:
// papers after topics (entities), tie edges after expertise edges, each
// deduped by id / key so applying the union is still one idempotent pass.
func (k KnowledgeClaims) WithTies(t TieClaims) KnowledgeClaims {
	havePaper := map[string]bool{}
	for _, p := range k.Papers {
		havePaper[p.ID] = true
	}
	for _, p := range t.Papers {
		if p.ID == "" || havePaper[p.ID] {
			continue
		}
		havePaper[p.ID] = true
		k.Papers = append(k.Papers, p)
	}
	haveEdge := map[string]bool{}
	for _, e := range k.Edges {
		haveEdge[e.Key()] = true
	}
	for _, e := range t.Edges {
		if haveEdge[e.Key()] {
			continue
		}
		haveEdge[e.Key()] = true
		k.Edges = append(k.Edges, e)
	}
	k.Skipped = append(k.Skipped, t.Skipped...)
	return k
}
