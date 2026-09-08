package recruiting

import (
	"math"
	"sort"
	"strings"

	"manifest/graph"
)

// DOMAIN LEVERAGE — "who are the highest-leverage people in X". A projection
// over two things the graph already holds, computed on every read and never
// stored:
//
//   - KNOWLEDGE: the person → topic `expertise` edges the knowledge overlay
//     derived on accept (knowledge.go), each with the confidence the source
//     justified and the works it rests on;
//   - CONNECTIVITY: the person ↔ person relationship claims on file —
//     network/edges.md (with the calendar / notes derivations the store
//     already merges) and the general graph's person ties (ties.go).
//
// The score is deliberately simple enough to check by hand:
//
//	leverage     = knowledge × (1 + connectivity)
//	knowledge    = confidence of the person's expertise edge for the topic
//	connectivity = Σ over KNOWN people first reached within N hops of
//	               (confidence of the edge that reached them) × decay^(hop−1)
//
// Knowledge is a gate: a person with ties and no expertise in the domain
// scores zero, and is still listed (role `adjacent`) when they are one hop
// from an expert, with their components visible, so the reader sees WHY
// they rank where they do. A person with expertise and no ties scores their
// knowledge alone. Only known people — a candidate, a connector, a vault
// contact, a registered graph person — count toward connectivity; a tie to
// an external key (`ext/…`) is walked (it is a real edge to a real, durably
// named person) but reported separately as reach the board cannot yet act
// on. Every number carries the edges that produced it.

const (
	DefaultLeverageHops  = 2
	MaxLeverageHops      = 3
	DefaultLeverageLimit = 20
	DefaultLeverageDecay = 0.5
	// leverageTiesShown bounds the ties listed per person; the count is
	// always complete.
	leverageTiesShown = 25
	// leverageSuggestions bounds the topics offered when the asked-for one
	// names no expert.
	leverageSuggestions = 10
)

// LeverageOptions bounds a ranking. Zero values take the defaults.
type LeverageOptions struct {
	Hops  int
	Limit int
	Decay float64
}

// LeverageTie is one edge that contributed to a person's connectivity.
type LeverageTie struct {
	Person       string  `json:"person"`
	Name         string  `json:"name,omitempty"`
	Known        bool    `json:"known"`
	Hop          int     `json:"hop"`
	Via          string  `json:"via,omitempty"` // the node the walk came through (hop > 1)
	Kind         string  `json:"kind"`
	Confidence   string  `json:"confidence,omitempty"`
	Inferred     bool    `json:"inferred"`
	Derived      bool    `json:"derived,omitempty"`
	Basis        string  `json:"basis"`
	Evidence     string  `json:"evidence,omitempty"`
	Contribution float64 `json:"contribution"`
}

// LeverageExpertise is the knowledge component's provenance.
type LeverageExpertise struct {
	Confidence string   `json:"confidence"`
	Inferred   bool     `json:"inferred"`
	Source     string   `json:"source,omitempty"`
	Basis      string   `json:"basis"`
	Works      []string `json:"works,omitempty"`
}

// LeveragePerson is one ranked person with every component in the open.
type LeveragePerson struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Role         string             `json:"role"` // expert | adjacent
	Leverage     float64            `json:"leverage"`
	Knowledge    float64            `json:"knowledge"`
	Connectivity float64            `json:"connectivity"`
	Expertise    *LeverageExpertise `json:"expertise,omitempty"`
	Ties         []LeverageTie      `json:"ties"`
	TieCount     int                `json:"tieCount"` // known people reached, all hops
	External     int                `json:"external"` // ext/… people reached — real, not on the board
}

// LeverageResult is the whole answer, honest when empty.
type LeverageResult struct {
	Topic       string           `json:"topic"`
	TopicID     string           `json:"topicId"`
	Known       bool             `json:"known"` // at least one expertise edge names the topic
	Experts     int              `json:"experts"`
	Hops        int              `json:"hops"`
	Decay       float64          `json:"decay"`
	Formula     string           `json:"formula"`
	People      []LeveragePerson `json:"people"`
	Total       int              `json:"total"` // before the limit
	Suggestions []string         `json:"suggestions,omitempty"`
	Note        string           `json:"note,omitempty"`
}

// LeverageFormula is the score, in words, on every reply.
const LeverageFormula = "leverage = knowledge × (1 + connectivity); knowledge = confidence of the person's expertise edge for the topic; connectivity = Σ over known people first reached within N hops of edge confidence × decay^(hop−1)"

type leverageHop struct {
	node string
	edge Edge
}

// RankLeverage ranks people for a topic. knowledge is the general graph's
// edge list (only `expertise` rows are read); ties are the person ↔ person
// rows to walk; display names an id and says whether it is someone known.
func RankLeverage(topic string, knowledge []graph.Edge, ties []Edge, display func(id string) (string, bool), opt LeverageOptions) LeverageResult {
	hops, limit, decay := opt.Hops, opt.Limit, opt.Decay
	if hops <= 0 {
		hops = DefaultLeverageHops
	}
	if hops > MaxLeverageHops {
		hops = MaxLeverageHops
	}
	if limit <= 0 {
		limit = DefaultLeverageLimit
	}
	if decay <= 0 || decay > 1 {
		decay = DefaultLeverageDecay
	}
	if display == nil {
		display = func(id string) (string, bool) { return id, false }
	}
	res := LeverageResult{
		Topic: strings.TrimSpace(topic), TopicID: TopicID(topic), Hops: hops, Decay: decay,
		Formula: LeverageFormula, People: []LeveragePerson{},
	}
	if res.TopicID == "" {
		res.Note = "name a topic"
		return res
	}

	// ---- knowledge: the strongest expertise edge per person for this topic
	experts := map[string]graph.Edge{}
	topics := map[string]bool{}
	for _, e := range knowledge {
		if e.Kind != graph.EdgeExpertise || e.From.Kind != graph.KindPerson || e.To.Kind != graph.KindTopic {
			continue
		}
		topics[e.To.ID] = true
		if e.To.ID != res.TopicID || e.From.ID == "" {
			continue
		}
		if have, ok := experts[e.From.ID]; !ok || e.Weight() > have.Weight() {
			experts[e.From.ID] = e
		}
	}
	res.Experts = len(experts)
	res.Known = len(experts) > 0
	if !res.Known {
		res.Note = "no expertise edge names this topic — nobody on file is a cited expert in it"
		res.Suggestions = suggestTopics(res.TopicID, topics)
		return res
	}

	// ---- adjacency over the ties, undirected, sorted for determinism
	adj := map[string][]leverageHop{}
	for _, e := range ties {
		from, to := strings.TrimSpace(e.From), strings.TrimSpace(e.To)
		if from == "" || to == "" || from == to || ValidateEdge(e) != nil {
			continue
		}
		adj[from] = append(adj[from], leverageHop{node: to, edge: e})
		adj[to] = append(adj[to], leverageHop{node: from, edge: e})
	}
	for k := range adj {
		hs := adj[k]
		sort.SliceStable(hs, func(i, j int) bool {
			if hs[i].node != hs[j].node {
				return hs[i].node < hs[j].node
			}
			if a, b := hs[i].edge.Weight(), hs[j].edge.Weight(); a != b {
				return a > b
			}
			return !hs[i].edge.Inferred && hs[j].edge.Inferred
		})
	}

	// ---- the population: experts, plus the known people one hop from one
	ids := map[string]bool{}
	for id := range experts {
		ids[id] = true
	}
	for id := range experts {
		for _, h := range adj[id] {
			if _, known := display(h.node); known {
				ids[h.node] = true
			}
		}
	}

	for id := range ids {
		name, _ := display(id)
		p := LeveragePerson{ID: id, Name: name, Role: "adjacent", Ties: []LeverageTie{}}
		if e, ok := experts[id]; ok {
			p.Role = "expert"
			p.Knowledge = e.Weight()
			p.Expertise = &LeverageExpertise{Confidence: e.Confidence, Inferred: e.Inferred, Source: e.Source, Basis: e.Basis, Works: splitWorks(e.Evidence)}
		}
		p.Connectivity, p.Ties, p.TieCount, p.External = walkTies(id, adj, hops, decay, display)
		p.Leverage = round3(p.Knowledge * (1 + p.Connectivity))
		p.Knowledge, p.Connectivity = round3(p.Knowledge), round3(p.Connectivity)
		res.People = append(res.People, p)
	}
	sort.SliceStable(res.People, func(i, j int) bool {
		a, b := res.People[i], res.People[j]
		if a.Leverage != b.Leverage {
			return a.Leverage > b.Leverage
		}
		if a.Knowledge != b.Knowledge {
			return a.Knowledge > b.Knowledge
		}
		if a.Connectivity != b.Connectivity {
			return a.Connectivity > b.Connectivity
		}
		return a.ID < b.ID
	})
	res.Total = len(res.People)
	if len(res.People) > limit {
		res.People = res.People[:limit]
	}
	return res
}

// walkTies is the bounded breadth-first walk behind connectivity: every
// node is credited once, at the hop it was first reached, through the
// strongest edge that reached it there (adjacency order makes that a
// function of the input).
func walkTies(start string, adj map[string][]leverageHop, hops int, decay float64, display func(string) (string, bool)) (float64, []LeverageTie, int, int) {
	seen := map[string]bool{start: true}
	frontier := []string{start}
	var all []LeverageTie
	sum, known, external := 0.0, 0, 0
	for hop := 1; hop <= hops && len(frontier) > 0; hop++ {
		var next []string
		for _, cur := range frontier {
			for _, h := range adj[cur] {
				if seen[h.node] {
					continue
				}
				seen[h.node] = true
				next = append(next, h.node)
				name, isKnown := display(h.node)
				tie := LeverageTie{
					Person: h.node, Name: name, Known: isKnown, Hop: hop, Kind: h.edge.Kind,
					Confidence: h.edge.Confidence, Inferred: h.edge.Inferred, Derived: h.edge.Derived,
					Basis: h.edge.Basis, Evidence: h.edge.Evidence,
				}
				if hop > 1 {
					tie.Via = cur
				}
				if isKnown {
					tie.Contribution = round3(h.edge.Weight() * math.Pow(decay, float64(hop-1)))
					sum += h.edge.Weight() * math.Pow(decay, float64(hop-1))
					known++
				} else {
					external++
				}
				all = append(all, tie)
			}
		}
		frontier = next
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Contribution != all[j].Contribution {
			return all[i].Contribution > all[j].Contribution
		}
		if all[i].Hop != all[j].Hop {
			return all[i].Hop < all[j].Hop
		}
		return all[i].Person < all[j].Person
	})
	if len(all) > leverageTiesShown {
		all = all[:leverageTiesShown]
	}
	if all == nil {
		all = []LeverageTie{}
	}
	return sum, all, known, external
}

// suggestTopics lists the topic ids on file that share a word with the one
// asked for — an answer to "did I spell it the way the source did".
func suggestTopics(want string, topics map[string]bool) []string {
	words := strings.Fields(want)
	var out []string
	for id := range topics {
		for _, w := range words {
			if len(w) >= 3 && strings.Contains(" "+id+" ", " "+w+" ") {
				out = append(out, id)
				break
			}
		}
	}
	sort.Strings(out)
	if len(out) > leverageSuggestions {
		out = out[:leverageSuggestions]
	}
	return out
}

// splitWorks reads the comma-joined work URLs an expertise edge carries.
func splitWorks(evidence string) []string {
	var out []string
	for _, w := range strings.Split(evidence, ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
