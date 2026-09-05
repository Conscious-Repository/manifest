package manifestmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"manifest/graph"
	"manifest/record"
	"manifest/recruiting"
	"manifest/vaultwriter"
)

type OperationInput struct {
	OperationID string `json:"operationId"`
}

// OperationRecord is the recoverable receipt, independent of source cache expiry.
// Preview and Arguments are immutable after preparation; only the owner entry
// point can decide, and execution never accepts a replacement payload or actor.
type Transition struct {
	Status string    `json:"status"`
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
}

type OperationRecord struct {
	History         []Transition      `json:"history"`
	DecisionActor   string            `json:"decisionActor,omitempty"`
	RequestKey      string            `json:"idempotencyKey,omitempty"`
	RequestRevision string            `json:"requestRevision,omitempty"`
	ID              string            `json:"operationId"`
	SchemaVersion   int               `json:"schemaVersion"`
	ToolVersion     string            `json:"toolVersion"`
	Tool            string            `json:"tool"`
	Agent           string            `json:"requestingAgent"`
	Conversation    string            `json:"conversation"`
	Turn            string            `json:"turn"`
	Arguments       json.RawMessage   `json:"arguments"`
	Payload         json.RawMessage   `json:"payload"`
	Policy          string            `json:"policy"`
	Status          string            `json:"status"`
	ApprovalActor   string            `json:"approvalActor,omitempty"`
	ApprovedAt      time.Time         `json:"approvedAt,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	Expected        map[string]string `json:"expectedRevisions"`
	Files           map[string]string `json:"vaultFiles"`
	Applied         []string          `json:"appliedFiles"`
	Result          Object            `json:"result,omitempty"`
	Error           string            `json:"error,omitempty"`
}

var operationKey = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func (a *Adapter) opPath(id string) (string, error) {
	if !operationKey.MatchString(id) {
		return "", fmt.Errorf("invalid operation ID")
	}
	return filepath.Join(a.Data, "operations", strings.TrimPrefix(id, "sha256:")+".json"), nil
}
func (a *Adapter) lockOperations() (func(), error) {
	dir := filepath.Join(a.Data, "operations")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
func (a *Adapter) loadOperation(id string) (*OperationRecord, error) {
	p, err := a.opPath(id)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var o OperationRecord
	err = json.Unmarshal(b, &o)
	return &o, err
}
func (a *Adapter) saveOperation(o *OperationRecord) error {
	p, err := a.opPath(o.ID)
	if err != nil {
		return err
	}
	if len(o.History) == 0 {
		o.History = append(o.History, Transition{"prepared", o.CreatedAt, o.Agent})
	}
	if o.History[len(o.History)-1].Status != o.Status {
		actor := o.Agent
		if o.Status == "approved" || o.Status == "rejected" || o.Status == "cancelled" {
			actor = o.DecisionActor
		}
		o.History = append(o.History, Transition{o.Status, time.Now().UTC(), actor})
	}
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".receipt-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), p); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(p))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func receipt(o *OperationRecord) Object {
	return Object{"operationId": o.ID, "status": o.Status, "policy": o.Policy, "persisted": true, "executable": o.Status == "approved" || o.Policy == "standing_authorization" && o.Status == "prepared", "record": o, "result": o.Result}
}
func (a *Adapter) Operation(id string) (Object, error) {
	o, err := a.loadOperation(id)
	if err != nil {
		return nil, err
	}
	return receipt(o), nil
}

// snapshot deliberately includes both domain trees: conservative invalidation
// catches candidate identity/slug changes and inputs to knowledge derivation.
func (a *Adapter) snapshot() (map[string]string, error) {
	out := map[string]string{}
	for _, root := range []string{a.Records.Root(), a.Graph.Root()} {
		err := filepath.WalkDir(filepath.Join(a.Vault, root), func(p string, d os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(a.Vault, p)
			out[rel] = revision(string(b))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func decode(v any, target any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}
func (a *Adapter) persist(out Object, args any, preparedSnapshot ...map[string]string) (Object, error) {
	unlock, err := a.lockOperations()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if old, e := a.previousRequest(out["operation"].(Object)["tool"].(string), args); e != nil {
		return nil, e
	} else if old != nil {
		return receipt(old), nil
	}
	var context struct {
		Conversation   string `json:"conversation"`
		Turn           string `json:"turn"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if err = decode(args, &context); err != nil {
		return nil, err
	}
	payload := out["operation"].(Object)
	if context.Conversation != "" || context.Turn != "" || context.IdempotencyKey != "" {
		payload["requestContext"] = context
	}
	id := revision(payload)
	if old, err := a.loadOperation(id); err == nil {
		return receipt(old), nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	p := payload["preview"].(Object)
	expected, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	if len(preparedSnapshot) > 0 && revision(expected) != revision(preparedSnapshot[0]) {
		return nil, fmt.Errorf("targets changed before persistence; prepare again")
	}
	files := map[string]string{}
	if p["vaultFiles"] != nil {
		if err = decode(p["vaultFiles"], &files); err != nil {
			return nil, err
		}
	}
	if file, ok := p["file"].(string); ok {
		files[file] = p["content"].(string)
	}
	argBytes, _ := json.Marshal(p)
	payloadBytes, _ := json.Marshal(payload)
	o := &OperationRecord{ID: id, RequestKey: context.IdempotencyKey, RequestRevision: revision(args), SchemaVersion: 1, ToolVersion: Version, Tool: payload["tool"].(string), Agent: "agent:alfred", Conversation: context.Conversation, Turn: context.Turn, Arguments: argBytes, Payload: payloadBytes, Policy: out["policy"].(string), Status: "pending_approval", CreatedAt: time.Now().UTC(), Expected: expected, Files: files}
	if o.Policy == "standing_authorization" {
		o.Status = "prepared"
	}
	if o.Policy == "no_change" {
		o.Status = "succeeded"
		o.Result = p
	}
	if err = a.saveOperation(o); err != nil {
		return nil, err
	}
	r := receipt(o)
	r["operation"] = payload
	return r, nil
}

// Decide is an owner-only entry point, intentionally absent from the MCP
// catalog. The local owner CLI fixes the actor; an agent cannot submit approval
// fields to operation.execute. OS access to the owner CLI/data requires isolation.
func (a *Adapter) Decide(id, decision, actor string) (Object, error) {
	if actor != "owner:local" {
		return nil, fmt.Errorf("trusted owner actor required")
	}
	unlock, err := a.lockOperations()
	if err != nil {
		return nil, err
	}
	defer unlock()
	o, err := a.loadOperation(id)
	if err != nil {
		return nil, err
	}
	if o.Status != "pending_approval" && o.Status != "prepared" {
		return nil, fmt.Errorf("cannot decide %s operation", o.Status)
	}
	switch decision {
	case "approved":
		if o.Policy != "human_approval" {
			return nil, fmt.Errorf("standing operations need no approval")
		}
		o.ApprovalActor = actor
		o.ApprovedAt = time.Now().UTC()
	case "rejected", "cancelled":
	default:
		return nil, fmt.Errorf("invalid decision")
	}
	o.Status = decision
	o.DecisionActor = actor
	if err = a.saveOperation(o); err != nil {
		return nil, err
	}
	return receipt(o), nil
}
func (a *Adapter) approvedWriter() func(string, []byte) error {
	if a.writeApproved != nil {
		return a.writeApproved
	}
	w := vaultwriter.New(a.Vault).WithZoneRoots(a.System, "extrinsic").WithAudit(a.Data).Grant(
		vaultwriter.Capability{Name: "aion-recruiting-approved", Zone: record.ZoneSystem, Pattern: a.Records.Root() + "/**", Actor: vaultwriter.ActorApprovedProposal},
		vaultwriter.Capability{Name: "graph-approved", Zone: record.ZoneSystem, Pattern: a.Graph.Root() + "/**", Actor: vaultwriter.ActorApprovedProposal})
	return func(rel string, b []byte) error {
		cap := "graph-approved"
		if strings.HasPrefix(rel, a.Records.Root()+"/") {
			cap = "aion-recruiting-approved"
		}
		return w.WriteCap(cap, rel, b)
	}
}
func (a *Adapter) Execute(ctx context.Context, id string) (Object, error) {
	unlock, err := a.lockOperations()
	if err != nil {
		return nil, err
	}
	defer unlock()
	o, err := a.loadOperation(id)
	if err != nil {
		return nil, err
	}
	if o.Status == "executing" { // Crash recovery never reruns an ambiguous external effect.
		o.Error = "execution interrupted; reconciled files below; owner takeover required (no automatic retry)"
		a.reconcile(o)
		o.Status = "failed"
		if len(o.Applied) > 0 {
			o.Status = "partial"
		}
		if err = a.saveOperation(o); err != nil {
			return nil, err
		}
		return receipt(o), nil
	}
	if o.Status != "approved" && !(o.Policy == "standing_authorization" && o.Status == "prepared") {
		if o.Status == "pending_approval" || o.Status == "prepared" {
			return nil, fmt.Errorf("owner approval required")
		}
		return receipt(o), nil
	}
	if o.Policy == "human_approval" && (o.ApprovalActor != "owner:local" || o.ApprovedAt.IsZero()) {
		return nil, fmt.Errorf("missing owner approval")
	}
	current, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	var payload struct {
		Preview json.RawMessage `json:"preview"`
	}
	if err = json.Unmarshal(o.Payload, &payload); err != nil {
		return nil, err
	}
	var p struct {
		Request     recruiting.RunRequest      `json:"request"`
		RunID       string                     `json:"runId"`
		DraftID     string                     `json:"draftId"`
		RunVersion  string                     `json:"runVersion"`
		AsOf        time.Time                  `json:"asOf"`
		Candidate   recruiting.Candidate       `json:"candidate"`
		Person      recruiting.NetworkPerson   `json:"person"`
		Edge        graph.Edge                 `json:"edge"`
		Claims      recruiting.KnowledgeClaims `json:"claims"`
		Suppression recruiting.Passed          `json:"suppression"`
	}
	if err = json.Unmarshal(payload.Preview, &p); err != nil {
		return nil, err
	}
	stale := o.SchemaVersion != 1 || o.ToolVersion != Version || revision(current) != revision(o.Expected)
	if p.RunID != "" {
		run, e := a.Runs.Get(p.RunID)
		stale = stale || e != nil || revision(run) != p.RunVersion
	}
	if stale {
		o.Status = "stale"
		o.Error = "target changed; prepare and approve a new operation"
		if err = a.saveOperation(o); err != nil {
			return nil, err
		}
		return receipt(o), nil
	}
	o.Status = "executing"
	o.Result = Object{"runId": p.RunID, "draftId": p.DraftID, "intendedCandidate": p.Candidate, "intendedPerson": p.Person, "intendedEdge": p.Edge, "knowledgeClaims": p.Claims, "confirmedFiles": []string{}}
	if p.Candidate.ID != "" {
		o.Result["intendedObjectRefs"] = []Ref{{"recruiting", "manifest", "person", p.Candidate.ID}, {"graph", "manifest", "person", p.Candidate.ID}}
	}
	if err = a.saveOperation(o); err != nil {
		return nil, err
	} // intent before any effect
	write := a.approvedWriter()
	guarded := func(abs string, b []byte) error {
		rel, e := filepath.Rel(a.Vault, abs)
		if e != nil {
			return e
		}
		want, ok := o.Files[rel]
		if !ok || want != string(b) {
			return fmt.Errorf("shared service differs from approved bytes: %s", rel)
		}
		old, e := os.ReadFile(abs)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		expected, exists := o.Expected[rel]
		if exists && revision(string(old)) != expected || !exists && !os.IsNotExist(e) {
			return fmt.Errorf("target changed before write: %s", rel)
		}
		if e = write(rel, b); e != nil {
			return e
		}
		o.Applied = append(o.Applied, rel)
		return a.saveOperation(o)
	}
	records := recruiting.NewStore(a.Vault, a.Root, guarded)
	g := graph.NewStore(a.Vault, a.Graph.Root(), guarded)
	runs, e := recruiting.NewRunStore(a.Runs.Root(), records)
	if e != nil {
		err = e
	} else {
		switch o.Tool {
		case "source_run.prepare":
			var run recruiting.Run
			run, err = a.Runs.Execute(ctx, p.Request, o.CreatedAt)
			if run.ID != "" {
				o.Result, _ = a.runGet(RunInput{run.ID})
			}
		case "candidate_accept.prepare":
			_, _, err = runs.Accept(p.RunID, p.DraftID, p.AsOf)
			if err == nil { // Apply the saved claims through the same validators, then flush final previewed documents.
				memory := &knowledgeMemory{entities: g.LoadEntities(), edges: g.LoadEdges(), vocab: g.Vocabulary()}
				var knowledge recruiting.KnowledgeResult
				knowledge, err = recruiting.ApplyKnowledge(memory, p.Claims)
				if err == nil && len(knowledge.AddedEntities) > 0 {
					err = g.SaveEntities(memory.entities)
				}
				if err == nil && len(knowledge.AddedEdges) > 0 {
					err = g.SaveEdges(memory.edges)
				}
				if err == nil {
					o.Result["knowledge"] = knowledge
				}
			}
		case "candidate_reject.prepare":
			_, err = runs.Reject(p.RunID, p.DraftID, p.Suppression.Reason, p.AsOf)
		case "network_person.prepare":
			doc := records.LoadNetworkPeople()
			_, err = doc.Add(p.Person)
			if err == nil {
				err = records.SaveNetworkPeople(doc)
			}
		case "graph_edge.prepare":
			_, _, err = g.AddEdge(p.Edge)
		default:
			err = fmt.Errorf("unsupported operation")
		}
	}
	a.reconcile(o)
	o.Status = "succeeded"
	if err == nil && len(o.Applied) != len(o.Files) {
		err = fmt.Errorf("durable files do not match approved effect; takeover required")
	}
	if err != nil {
		o.Error = err.Error()
		o.Status = "failed"
		if len(o.Applied) > 0 {
			o.Status = "partial"
		}
	}
	if saveErr := a.saveOperation(o); saveErr != nil {
		return nil, saveErr
	}
	return receipt(o), nil
}
func (a *Adapter) reconcile(o *OperationRecord) {
	o.Applied = nil
	for rel, want := range o.Files {
		b, err := os.ReadFile(filepath.Join(a.Vault, rel))
		if err == nil && string(b) == want {
			o.Applied = append(o.Applied, rel)
		}
	}
	sort.Strings(o.Applied)
	if o.Result == nil {
		o.Result = Object{}
	}
	o.Result["confirmedFiles"] = o.Applied
	refs := []Ref{}
	for _, rel := range o.Applied {
		if strings.HasPrefix(rel, a.Root+"/candidates/") {
			slug := strings.TrimSuffix(filepath.Base(rel), ".md")
			refs = append(refs, Ref{"recruiting", "manifest", "person", a.Records.LoadCandidate(slug).Get("id")})
		}
	}
	var payload struct {
		Preview struct {
			Person recruiting.NetworkPerson   `json:"person"`
			Edge   graph.Edge                 `json:"edge"`
			Claims recruiting.KnowledgeClaims `json:"claims"`
		} `json:"preview"`
	}
	_ = json.Unmarshal(o.Payload, &payload)
	for _, rel := range o.Applied {
		if rel == a.Records.Rel("network/people.md") {
			refs = append(refs, Ref{"recruiting", "manifest", "person", payload.Preview.Person.ID})
		}
		if rel == a.Graph.Rel(graph.EntitiesFile) && payload.Preview.Claims.Person.ID != "" {
			refs = append(refs, Ref{"graph", "manifest", "person", payload.Preview.Claims.Person.ID})
		}
		if rel == a.Graph.Rel(graph.EdgesFile) && payload.Preview.Edge.Kind != "" {
			refs = append(refs, Ref{"graph", "manifest", "edge", payload.Preview.Edge.Key()})
		}
	}
	o.Result["objectRefs"] = refs
	o.Result["unconfirmedFiles"] = []string{}
	for rel := range o.Files {
		found := false
		for _, done := range o.Applied {
			if done == rel {
				found = true
			}
		}
		if !found {
			o.Result["unconfirmedFiles"] = append(o.Result["unconfirmedFiles"].([]string), rel)
		}
	}
}

// previousRequest binds an explicit retry key to one input, independent of the
// preview timestamp. Atomic receipts are safe to inspect without a writer lock;
// persist repeats this check while holding the cross-process lock.
func (a *Adapter) previousRequest(tool string, args any) (*OperationRecord, error) {
	var q struct {
		Key string `json:"idempotencyKey"`
	}
	if err := decode(args, &q); err != nil {
		return nil, err
	}
	if q.Key == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.Join(a.Data, "operations"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := "sha256:" + strings.TrimSuffix(entry.Name(), ".json")
		o, err := a.loadOperation(id)
		if err != nil {
			return nil, err
		}
		if o.Tool == tool && o.RequestKey == q.Key {
			if o.RequestRevision != revision(args) {
				return nil, fmt.Errorf("idempotency key already bound to different arguments")
			}
			return o, nil
		}
	}
	return nil, nil
}
