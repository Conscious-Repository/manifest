// Package manifestmcp exposes versioned domain operations with durable approval receipts.
package manifestmcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"manifest/graph"
	"manifest/recruiting"
)

const Version = "2.0.0"

type Object map[string]any
type Adapter struct {
	Vault, Root   string
	Data, System  string
	writeApproved func(string, []byte) error
	Records       *recruiting.Store
	Runs          *recruiting.RunStore
	Graph         *graph.Store
	Tools         []*mcp.Tool
}

func New(vault, data, system string) (*Adapter, error) {
	r := recruiting.NewStore(vault, filepath.Join(system, "aion/recruiting"), nil)
	runs, err := recruiting.NewRunStore(filepath.Join(data, "recruiting/runs"), r)
	if err != nil {
		return nil, err
	}
	runs.RegisterDefaults()
	return &Adapter{Vault: vault, Root: r.Root(), Data: data, System: system, Records: r, Runs: runs, Graph: graph.NewStore(vault, filepath.Join(system, "graph"), nil)}, nil
}
func revision(v any) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}

type Ref struct {
	Domain    string `json:"domain"`
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
}
type Entity struct {
	Ref     Ref    `json:"ref"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Value   any    `json:"value"`
}

func entity(domain, kind, id, name string, v any) Entity {
	return Entity{Ref: Ref{domain, "manifest", kind, id}, Name: name, Version: revision(v), Value: v}
}
func (a *Adapter) entities() []Entity {
	out := []Entity{}
	v := a.Records.View()
	for _, c := range v.Candidates {
		out = append(out, entity("recruiting", "person", c.ID, c.Name, c))
	}
	for _, p := range v.Network.People {
		out = append(out, entity("recruiting", "person", p.ID, p.Name, p))
	}
	for _, p := range v.Seeds {
		out = append(out, entity("recruiting", "seed", p.ID, p.Name, p))
	}
	for _, r := range v.Roles {
		out = append(out, entity("recruiting", "role", r.ID, r.Title, r))
	}
	for _, e := range a.Graph.LoadEntities().Entities() {
		out = append(out, entity("graph", e.Kind, e.ID, e.Title, e))
	}
	return out
}

type ResolveInput struct {
	Query  string `json:"query"`
	Domain string `json:"domain,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

func (a *Adapter) resolve(q ResolveInput) []Entity {
	out := []Entity{}
	for _, e := range a.entities() {
		if (q.Domain == "" || q.Domain == e.Ref.Domain) && (q.Kind == "" || q.Kind == e.Ref.Kind) && (strings.EqualFold(strings.TrimSpace(q.Query), e.Name) || q.Query == e.Ref.ID || q.Query == e.Ref.Kind+":"+e.Ref.ID) {
			out = append(out, e)
		}
	}
	return out
}
func (a *Adapter) get(r Ref) (Entity, error) {
	if r.Namespace != "manifest" {
		return Entity{}, fmt.Errorf("namespace must be manifest")
	}
	for _, e := range a.entities() {
		if e.Ref == r {
			return e, nil
		}
	}
	return Entity{}, fmt.Errorf("canonical ref not found: %+v", r)
}
func (a *Adapter) Server() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "manifest", Version: Version}, nil)
	a.Tools = nil
	add(a, s, "capabilities.list", "Read the versioned tools, generated input schemas, domain vocabulary and source scopes.", func(_ struct{}) (Object, error) {
		return Object{"tools": a.Tools, "version": Version, "sources": a.Runs.Sources(), "vocabulary": a.Graph.Vocabulary()}, nil
	})
	add(a, s, "entity.resolve", "Resolve exact name or ID across recruiting people, seeds/labs, roles and registered graph entities. Multiple matches require an explicit choice; never guess.", func(q ResolveInput) (Object, error) {
		if strings.TrimSpace(q.Query) == "" {
			return nil, fmt.Errorf("query required")
		}
		matches := a.resolve(q)
		status := "not_found"
		if len(matches) == 1 {
			status = "resolved"
		} else if len(matches) > 1 {
			status = "ambiguous"
		}
		return Object{"status": status, "matches": matches}, nil
	})
	add(a, s, "entity.get", "Read a canonical entity, evidence/provenance and content revision.", func(q Ref) (Object, error) { e, err := a.get(q); return Object{"entity": e}, err })
	add(a, s, "sources.list", "Read the application's shared adapter registry and scope fields; no fetch or cache sweep.", func(_ struct{}) (Object, error) {
		return Object{"sources": a.Runs.Sources(), "defaultMax": recruiting.DefaultRunMax, "maxMax": recruiting.MaxRunMax}, nil
	})
	add(a, s, "source_run.get", "Read one run and review queue using RunStore.Get; counts include previously passed drafts separately.", a.runGet)
	add(a, s, "graph.neighbors", "Bounded stored general-graph neighbors and optional paths (at most 3 hops, 10 paths). Server-only task/calendar derivations are not included.", a.neighbors)
	add(a, s, "source_run.prepare", "Normalize a source scope using Execute's shared PrepareScope. Resolve optional seed and role refs. No fetch or source-cache write; persists an operation. Standing authorization applies; network/robots validation remains execution-time.", a.sourcePrepare)
	add(a, s, "candidate_accept.prepare", "Preview exactly one new draft through AcceptDraft with an in-memory capture writer, plus derived knowledge and decision effects. Persists an operation outside the vault.", func(q DraftInput) (Object, error) { return a.draftPrepare(q, true) })
	add(a, s, "candidate_reject.prepare", "Resolve one new draft and preview durable passed.md suppression plus queue and audit effects.", func(q DraftInput) (Object, error) { return a.draftPrepare(q, false) })
	add(a, s, "network_person.prepare", "Resolve a canonical person; check existing network identity and preview PeopleDoc.Add with the shared domain payload and validation.", a.personPrepare)
	add(a, s, "graph_edge.prepare", "Resolve both registered general-graph endpoints and preview a typed claim with shared graph validation and duplicate detection.", a.edgePrepare)
	add(a, s, "operation.get", "Read a durable operation, approval and execution receipt.", func(q OperationInput) (Object, error) { return a.Operation(q.OperationID) })
	addContext(a, s, "operation.execute", "Execute the saved payload. Source runs have standing authorization; world changes require an owner decision outside MCP. Terminal receipts are idempotent; incomplete effects require takeover.", func(ctx context.Context, q OperationInput) (Object, error) { return a.Execute(ctx, q.OperationID) })
	return s
}
func add[I any](a *Adapter, s *mcp.Server, name, description string, f func(I) (Object, error)) {
	addContext(a, s, name, description, func(_ context.Context, in I) (Object, error) { return f(in) })
}
func addContext[I any](a *Adapter, s *mcp.Server, name, description string, f func(context.Context, I) (Object, error)) {
	schema, err := jsonschema.For[I](nil)
	if err != nil {
		panic(err)
	}
	t := &mcp.Tool{Name: name, Description: description, InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: !strings.HasSuffix(name, ".prepare") && name != "operation.execute"}}
	a.Tools = append(a.Tools, t)
	mcp.AddTool(s, t, func(ctx context.Context, req *mcp.CallToolRequest, in I) (*mcp.CallToolResult, any, error) {
		var before map[string]string
		if strings.HasSuffix(name, ".prepare") {
			if previous, err := a.previousRequest(name, in); err != nil {
				return nil, nil, err
			} else if previous != nil {
				return nil, receipt(previous), nil
			}
			var err error
			before, err = a.snapshot()
			if err != nil {
				return nil, nil, err
			}
		}
		out, err := f(ctx, in)
		if err == nil && strings.HasSuffix(name, ".prepare") {
			after, e := a.snapshot()
			if e != nil {
				return nil, nil, e
			}
			if revision(before) != revision(after) {
				return nil, nil, fmt.Errorf("targets changed during preparation; retry")
			}
			out, err = a.persist(out, in, before)
		}
		return nil, out, err
	})
}
func prepared(tool, policy string, preview any, refs ...Entity) Object {
	payload := Object{"tool": tool, "toolVersion": Version, "schemaVersion": 1, "preview": preview, "targets": refs}
	return Object{"operationId": revision(payload), "status": "pending_approval", "policy": policy, "persisted": false, "executable": false, "operation": payload}
}

type RunInput struct {
	RunID string `json:"runId"`
}

func (a *Adapter) runGet(q RunInput) (Object, error) {
	r, err := a.Runs.Get(q.RunID)
	passed := 0
	for _, d := range r.Drafts {
		if d.Status == recruiting.DraftRejected && strings.HasPrefix(d.Reason, "passed ") {
			passed++
		}
	}
	counts := map[string]int{"new": 0, "duplicate": 0, "accepted": 0, "rejected": 0, "previouslyPassed": passed}
	for _, d := range r.Drafts {
		counts[d.Status]++
	}
	return Object{"runId": q.RunID, "run": r, "version": revision(r), "previouslyPassed": passed, "queueCounts": counts}, err
}

type NeighborsInput struct {
	Ref   Ref  `json:"ref"`
	To    *Ref `json:"to,omitempty"`
	Limit int  `json:"limit,omitempty"`
}

func (a *Adapter) neighbors(q NeighborsInput) (Object, error) {
	e, err := a.get(q.Ref)
	if err != nil {
		return nil, err
	}
	if e.Ref.Domain != "graph" {
		return nil, fmt.Errorf("graph context requires graph-domain ref")
	}
	n := q.Limit
	if n <= 0 {
		n = 25
	}
	if n > 100 {
		n = 100
	}
	g := a.Graph.Graph()
	r := graph.R(e.Ref.Kind, e.Ref.ID)
	h := g.Neighbors(r, graph.NeighborFilter{})
	total := len(h)
	if len(h) > n {
		h = h[:n]
	}
	out := Object{"ref": e.Ref, "neighbors": h, "total": total, "truncated": total > n, "projection": "stored_general_graph"}
	if q.To != nil {
		to, err := a.get(*q.To)
		if err != nil {
			return nil, err
		}
		if to.Ref.Domain != "graph" {
			return nil, fmt.Errorf("target must be graph-domain")
		}
		out["paths"] = g.Paths([]graph.Ref{r}, graph.R(to.Ref.Kind, to.Ref.ID), graph.PathOptions{MaxHops: 3, TopN: 10})
	}
	return out, nil
}

type SourceInput struct {
	IdempotencyKey string                `json:"idempotencyKey,omitempty"`
	Conversation   string                `json:"conversation,omitempty"`
	Turn           string                `json:"turn,omitempty"`
	Request        recruiting.RunRequest `json:"request"`
	Seed           *Ref                  `json:"seed,omitempty"`
	Role           *Ref                  `json:"role,omitempty"`
}

func (a *Adapter) sourcePrepare(q SourceInput) (Object, error) {
	refs := []Entity{}
	if q.Seed != nil {
		e, err := a.get(*q.Seed)
		if err != nil {
			return nil, err
		}
		var seedURL string
		switch v := e.Value.(type) {
		case recruiting.Seed:
			seedURL = v.URL
		case graph.Entity:
			seedURL = v.Ref
		default:
			return nil, fmt.Errorf("seed must be a recruiting seed or graph entity with URL")
		}
		adapter, ok := a.Runs.Adapter(q.Request.Source)
		if !ok {
			return nil, fmt.Errorf("unknown source")
		}
		field := ""
		for _, f := range adapter.Scope() {
			if f.Key == "seed_url" || f.Key == "feed_url" {
				field = f.Key
			}
		}
		if seedURL == "" || field == "" {
			return nil, fmt.Errorf("resolved seed has no URL or adapter has no URL scope field")
		}
		if q.Request.Fields == nil {
			q.Request.Fields = map[string]string{}
		}
		if have := q.Request.Fields[field]; have != "" && have != seedURL {
			return nil, fmt.Errorf("scope URL differs from resolved seed URL")
		}
		q.Request.Fields[field] = seedURL
		refs = append(refs, e)
	}
	if q.Role != nil {
		e, err := a.get(*q.Role)
		if err != nil {
			return nil, err
		}
		if e.Ref.Domain != "recruiting" || e.Ref.Kind != "role" {
			return nil, fmt.Errorf("role ref required")
		}
		q.Request.Role = e.Ref.ID
		refs = append(refs, e)
	}
	if q.Role == nil && q.Request.Role != "" {
		matches := a.resolve(ResolveInput{Query: q.Request.Role, Domain: "recruiting", Kind: "role"})
		if len(matches) != 1 {
			return nil, fmt.Errorf("role must resolve uniquely")
		}
		q.Request.Role = matches[0].Ref.ID
		refs = append(refs, matches[0])
	}
	adapter, scope, err := a.Runs.PrepareScope(q.Request)
	if err != nil {
		return nil, err
	}
	refs = append(refs, entity("recruiting", "source", adapter.ID(), adapter.ID(), adapter.Scope()))
	return prepared("source_run.prepare", "standing_authorization", Object{"source": adapter.ID(), "scope": scope, "request": recruiting.RunRequest{Source: adapter.ID(), Role: scope.Role, Query: scope.Query, Max: scope.Max, DryRun: scope.DryRun, Fields: scope.Fields}, "scopeFields": adapter.Scope(), "network": adapter.ID() != "manual", "cacheEffects": []string{"create run.json, response.json, drafts.json under dataDir/recruiting/runs", "dedupe candidates and apply passed.md suppression"}, "vaultEffects": []string{}, "validation": "shared scope and adapter preparation validated; network/robots/response validation deferred"}, refs...), nil
}

type DraftInput struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Conversation   string `json:"conversation,omitempty"`
	Turn           string `json:"turn,omitempty"`
	RunID          string `json:"runId"`
	DraftID        string `json:"draftId"`
	Reason         string `json:"reason,omitempty"`
}

func (a *Adapter) draftPrepare(q DraftInput, accept bool) (Object, error) {
	run, err := a.Runs.Get(q.RunID)
	if err != nil {
		return nil, err
	}
	var d *recruiting.Draft
	for i := range run.Drafts {
		if run.Drafts[i].ID == q.DraftID {
			d = &run.Drafts[i]
		}
	}
	if d == nil {
		return nil, fmt.Errorf("draft not found")
	}
	if d.Status != recruiting.DraftNew {
		return nil, fmt.Errorf("draft is %s, not new", d.Status)
	}
	ref := entity("recruiting", "draft", q.RunID+"/"+q.DraftID, d.Draft.Name, *d)
	now := time.Now().UTC()
	preview := Object{"runId": q.RunID, "draftId": q.DraftID, "draft": d, "asOf": now, "runVersion": revision(run)}
	tool := "candidate_reject.prepare"
	decision, candidateID := recruiting.DraftRejected, ""
	if accept {
		tool = "candidate_accept.prepare"
		writes, capture := a.capture()

		c, err := capture.AcceptDraft(d.Draft, now)
		if err != nil {
			return nil, err
		}
		preview["candidate"] = c
		decision, candidateID = recruiting.DraftAccepted, c.ID
		preview["vaultFiles"] = writes
		claims := recruiting.DeriveKnowledge(d.Draft, c.ID, a.Records.Rel("candidates/"+c.Slug+".md"), now)
		memory := &knowledgeMemory{entities: a.Graph.LoadEntities(), edges: a.Graph.LoadEdges(), vocab: a.Graph.Vocabulary()}
		knowledge, err := recruiting.ApplyKnowledge(memory, claims)
		if err != nil {
			return nil, err
		}
		preview["knowledge"] = knowledge
		preview["claims"] = claims
		if len(knowledge.AddedEntities) > 0 {
			writes[a.Graph.Rel(graph.EntitiesFile)] = graph.SerializeEntities(memory.entities)
		}
		if len(knowledge.AddedEdges) > 0 {
			writes[a.Graph.Rel(graph.EdgesFile)] = graph.SerializeEdges(memory.edges)
		}

		preview["effects"] = []string{"mark this draft accepted and set candidateId/decidedAt; increment accepted count; triage/expiry if complete", "apply saved knowledge through shared graph validators: existing keys retained; receipt identifies confirmed claims", "operation receipt for recruiting.draft.accepted; approved-proposal vault audit; receipt records approving owner", "passed.md unchanged"}
	} else {
		p := recruiting.Passed{Key: recruiting.PassedKey(d.Draft.SourceID, d.Draft.ExternalID, d.Draft.Name), Name: d.Draft.Name, Reason: strings.TrimSpace(q.Reason), Source: d.Draft.SourceID}
		p.At = now.Format("2006-01-02")
		writes, capture := a.capture()
		if err := capture.AddPassed(p, now); err != nil {
			return nil, err
		}
		preview["vaultFiles"] = writes
		preview["suppression"] = p
		preview["effects"] = []string{"AddPassed in passed.md; suppress same source/externalId/name in future runs", "mark this draft rejected; set decidedAt; increment rejected count; triage/expiry if complete", "operation receipt for recruiting.draft.passed with approved-proposal actor"}
	}
	after, err := a.Runs.PreviewDecision(q.RunID, q.DraftID, decision, candidateID, now)
	if err != nil {
		return nil, err
	}
	preview["queueAfter"] = after
	cache := map[string]string{}
	for name, value := range map[string]any{"run.json": after.RunState, "drafts.json": after.Drafts} {
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, err
		}
		cache[filepath.Join("recruiting/runs", q.RunID, name)] = string(b)
	}
	preview["cacheFiles"] = cache
	out := prepared(tool, "human_approval", preview, ref)
	out["runId"] = q.RunID
	out["draftId"] = q.DraftID
	return out, nil
}

type PersonInput struct {
	IdempotencyKey string                   `json:"idempotencyKey,omitempty"`
	Conversation   string                   `json:"conversation,omitempty"`
	Turn           string                   `json:"turn,omitempty"`
	Ref            Ref                      `json:"ref"`
	Person         recruiting.NetworkPerson `json:"person"`
}

func (a *Adapter) personPrepare(q PersonInput) (Object, error) {
	e, err := a.get(q.Ref)
	if err != nil {
		return nil, err
	}
	if e.Ref.Kind != "person" {
		return nil, fmt.Errorf("person ref required")
	}
	if q.Person.Name != "" && q.Person.Name != e.Name {
		return nil, fmt.Errorf("person name differs from resolved ref")
	}
	q.Person.Name = e.Name
	for _, p := range a.Records.LoadNetworkPeople().People() {
		if p.ID == e.Ref.ID || strings.EqualFold(p.Name, e.Name) || (p.Ref != "" && p.Ref == q.Person.Ref) {
			return prepared("network_person.prepare", "no_change", Object{"alreadyExists": true, "entity": entity("recruiting", "person", p.ID, p.Name, p), "effects": []string{}}, e), nil
		}
	}
	doc := a.Records.LoadNetworkPeople()
	before := revision(doc.People())
	p, err := doc.Add(q.Person)
	if err != nil {
		return nil, err
	}
	return prepared("network_person.prepare", "human_approval", Object{"person": p, "file": a.Records.Rel("network/people.md"), "content": recruiting.SerializeNetworkPeople(doc), "expectedVersion": before}, e), nil
}

type EdgeInput struct {
	IdempotencyKey string     `json:"idempotencyKey,omitempty"`
	Conversation   string     `json:"conversation,omitempty"`
	Turn           string     `json:"turn,omitempty"`
	From           Ref        `json:"from"`
	To             Ref        `json:"to"`
	Edge           graph.Edge `json:"edge"`
}

func (a *Adapter) edgePrepare(q EdgeInput) (Object, error) {
	from, err := a.get(q.From)
	if err != nil {
		return nil, err
	}
	to, err := a.get(q.To)
	if err != nil {
		return nil, err
	}
	if from.Ref.Domain != "graph" || to.Ref.Domain != "graph" {
		return nil, fmt.Errorf("both endpoints must resolve in graph domain; no implicit cross-domain mapping")
	}
	e := q.Edge
	e.From = graph.R(from.Ref.Kind, from.Ref.ID)
	e.To = graph.R(to.Ref.Kind, to.Ref.ID)
	if err := graph.Validate(e, a.Graph.Vocabulary()); err != nil {
		return nil, err
	}
	doc := a.Graph.LoadEdges()
	before := revision(doc.Edges())
	if have, ok := doc.Find(e.Key()); ok {
		return prepared("graph_edge.prepare", "no_change", Object{"alreadyExists": true, "edge": have, "effects": []string{}}, from, to), nil
	}
	if _, err := doc.Add(e, a.Graph.Vocabulary()); err != nil {
		return nil, err
	}
	return prepared("graph_edge.prepare", "human_approval", Object{"edge": e, "file": a.Graph.Rel(graph.EdgesFile), "content": graph.SerializeEdges(doc), "expectedVersion": before, "auditEffect": "graph.edge.added; approved-proposal actor"}, from, to), nil
}

func (a *Adapter) capture() (map[string]string, *recruiting.Store) {
	writes := map[string]string{}
	return writes, recruiting.NewStore(a.Vault, a.Root, func(path string, b []byte) error {
		rel, err := filepath.Rel(a.Vault, path)
		if err != nil {
			return err
		}
		writes[rel] = string(b)
		return nil
	})
}

// knowledgeMemory applies the same document validators and dedupe rules as
// graph.Store, retaining successive changes only in memory.
type knowledgeMemory struct {
	entities *graph.EntitiesDoc
	edges    *graph.EdgesDoc
	vocab    graph.Vocabulary
}

func (k *knowledgeMemory) AddEntity(e graph.Entity) (graph.Entity, bool, error) {
	if err := graph.ValidateEntity(e, k.vocab); err != nil {
		return e, false, err
	}
	if have, ok := k.entities.Find(e.AsRef()); ok {
		return have, false, nil
	}
	_, err := k.entities.Add(e, k.vocab)
	return e, err == nil, err
}
func (k *knowledgeMemory) AddEdge(e graph.Edge) (graph.Edge, bool, error) {
	if err := graph.Validate(e, k.vocab); err != nil {
		return e, false, err
	}
	if have, ok := k.edges.Find(e.Key()); ok {
		return have, false, nil
	}
	_, err := k.edges.Add(e, k.vocab)
	return e, err == nil, err
}
