package recruiting

import (
	"sort"
	"strings"
	"time"

	"manifest/graph"
	"manifest/recruiting/sources"
)

// THE KNOWLEDGE OVERLAY (source-card enrichment Phase 3). A draft's Topics
// are what a source said about the person's OWN works (O4). Accepting the
// draft is the moment those chips stop being a card decoration and become
// relations in the general graph: one `person → topic` expertise edge per
// topic, Inferred, with the works as basis and their URLs as evidence — so
// "who knows X" is a graph query rather than a scan of candidate files.
//
// Derived, not stored twice (Path B): the record keeps its topics field and
// evidence rows untouched; the graph holds the RELATION, with provenance.
// This file is pure — it computes claims and applies them through whatever
// writer it is handed (the graph store, whose only write path is the
// injected vault writer). It never opens a file: runs.go stays the one place
// this package writes directly.

const (
	// MaxKnowledgeTopics caps the edges one accept derives (O2: 2–4 chips).
	// A source that names ten fields is still one person; the first four the
	// source ranked are the claim, the rest stay on the record as text.
	MaxKnowledgeTopics = 4

	// KnowledgeConfidence is "medium" as a number: a source's topic list for
	// an author record is a real signal but not a confirmed skill. Above the
	// unstated default (0.50) because the source did state it; well below an
	// owner assertion.
	KnowledgeConfidence = "0.60"
)

// TopicID is the controlled normalization that makes two mentions of the
// same concept the same node: lookup.go's topicKey (record.SlugSpaces — the
// vault's note-name rule), so the chip a lookup merged and the node an
// accept derives agree on identity. Exact after normalization means the same
// topic; anything else stays a different node (near-miss dropped, never
// merged — O1).
func TopicID(topic string) string { return topicKey(topic) }

// TopicRef is the graph endpoint for a topic.
func TopicRef(topic string) graph.Ref { return graph.R(graph.KindTopic, TopicID(topic)) }

// KnowledgeClaims is what one accepted draft asserts into the graph: the
// person (registered rich — title + record path + classified links), the
// topic nodes, and the expertise edges between them.
type KnowledgeClaims struct {
	Person graph.Entity   `json:"person"`
	Topics []graph.Entity `json:"topics"`
	Edges  []graph.Edge   `json:"edges"`
}

// DeriveKnowledge projects a draft onto graph claims for the candidate it
// became. candidateID is the record id (cand/…); recordRef is where the
// record lives (vault-relative). It writes nothing.
//
//   - person: Kind person, Title = name, Ref = recordRef, Links = the
//     classified homepage/linkedin/github/orcid (verbatim; Site is not a
//     homepage and is kept under its own key);
//   - topic: Kind topic, ID = TopicID, Title = the term as the source said it;
//   - edge: person → topic, Kind expertise, Inferred = true, Confidence =
//     KnowledgeConfidence, Basis = the attributed works, Evidence = their
//     URLs, Source = the source that named the topics, Observed = now.
//
// Topics dedupe by TopicID (first wording wins) and cap at
// MaxKnowledgeTopics. A draft with no topics yields the person alone.
func DeriveKnowledge(d sources.CandidateDraft, candidateID, recordRef string, now time.Time) KnowledgeClaims {
	date := now.UTC().Format("2006-01-02")
	source := strings.TrimSpace(d.SourceID)
	k := KnowledgeClaims{
		Person: graph.Entity{
			ID: strings.TrimSpace(candidateID), Kind: graph.KindPerson, Title: strings.TrimSpace(d.Name),
			Ref: strings.TrimSpace(recordRef), Source: orSource(source), Added: date,
			Links: personLinks(d),
		},
		Topics: []graph.Entity{},
		Edges:  []graph.Edge{},
	}
	if k.Person.ID == "" {
		return k
	}
	works, topicSource := attributedWorks(d)
	if topicSource == "" {
		topicSource = orSource(source)
	}
	basis := "attributed works named by " + topicSource
	if len(works) > 0 {
		basis = "attributed works " + strings.Join(works, " ")
	}
	seen := map[string]bool{}
	for _, t := range d.Topics {
		if len(k.Edges) >= MaxKnowledgeTopics {
			break
		}
		title := strings.TrimSpace(t)
		id := TopicID(title)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		k.Topics = append(k.Topics, graph.Entity{
			ID: id, Kind: graph.KindTopic, Title: title, Source: topicSource, Added: date,
		})
		k.Edges = append(k.Edges, graph.Edge{
			From: k.Person.AsRef(), To: graph.R(graph.KindTopic, id), Kind: graph.EdgeExpertise,
			Basis: basis, Confidence: KnowledgeConfidence, Inferred: true, Source: topicSource,
			Evidence: strings.Join(works, ", "), Observed: date,
		})
	}
	return k
}

// personLinks is the classified presence as entity links — only the fields
// the classifier filled, never the raw Links union.
func personLinks(d sources.CandidateDraft) map[string]string {
	out := map[string]string{}
	for k, v := range map[string]string{
		"homepage": d.Homepage, "linkedin": d.LinkedIn, "github": d.Github, "orcid": d.Orcid, "site": d.Site,
	} {
		if v = strings.TrimSpace(v); v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// attributedWorks lists the URLs of the draft's publication evidence — the
// works the topics were read from — deduped, sorted, and the source that
// carried the topics (the row whose snippet names them, else the first
// publication row's source).
func attributedWorks(d sources.CandidateDraft) ([]string, string) {
	seen := map[string]bool{}
	var urls []string
	source := ""
	for _, ev := range d.Evidence {
		if ev.Kind != sources.EvidencePublication {
			continue
		}
		if strings.Contains(strings.ToLower(ev.Snippet), "topics:") && source == "" {
			source = strings.TrimSpace(ev.SourceID)
		}
		u := strings.TrimSpace(ev.URLOrFile)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		urls = append(urls, u)
	}
	if source == "" {
		for _, ev := range d.Evidence {
			if ev.Kind == sources.EvidencePublication && strings.TrimSpace(ev.SourceID) != "" {
				source = strings.TrimSpace(ev.SourceID)
				break
			}
		}
	}
	sort.Strings(urls)
	return urls, source
}

func orSource(s string) string {
	if s == "" {
		return "recruiting"
	}
	return s
}

// KnowledgeWriter is the slice of the graph store a derivation writes
// through. Both methods are idempotent by key, which is what makes applying
// the same claims twice add nothing.
type KnowledgeWriter interface {
	AddEntity(graph.Entity) (graph.Entity, bool, error)
	AddEdge(graph.Edge) (graph.Edge, bool, error)
}

// KnowledgeResult is what ApplyKnowledge did: the claims, and the subset it
// actually added (the caller ledgers those — a replay that added nothing
// leaves no event).
type KnowledgeResult struct {
	Claims        KnowledgeClaims `json:"claims"`
	AddedEntities []graph.Entity  `json:"addedEntities"`
	AddedEdges    []graph.Edge    `json:"addedEdges"`
}

// ApplyKnowledge writes the claims: entities first (a person or topic
// already registered is left as-is), then the edges (a claim already on
// file is not a second claim). It stops at the first write error and
// reports what landed before it.
func ApplyKnowledge(w KnowledgeWriter, k KnowledgeClaims) (KnowledgeResult, error) {
	res := KnowledgeResult{Claims: k, AddedEntities: []graph.Entity{}, AddedEdges: []graph.Edge{}}
	if w == nil || strings.TrimSpace(k.Person.ID) == "" {
		return res, nil
	}
	for _, e := range append([]graph.Entity{k.Person}, k.Topics...) {
		got, added, err := w.AddEntity(e)
		if err != nil {
			return res, err
		}
		if added {
			res.AddedEntities = append(res.AddedEntities, got)
		}
	}
	for _, e := range k.Edges {
		got, added, err := w.AddEdge(e)
		if err != nil {
			return res, err
		}
		if added {
			res.AddedEdges = append(res.AddedEdges, got)
		}
	}
	return res, nil
}
