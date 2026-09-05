package manifestmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"manifest/graph"
	"manifest/recruiting"
)

type PinInput struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Conversation   string `json:"conversation,omitempty"`
	Turn           string `json:"turn,omitempty"`
	RunID          string `json:"runId"`
	Pinned         bool   `json:"pinned"`
}

func (a *Adapter) pinPrepare(q PinInput) (Object, error) {
	run, err := a.Runs.Get(q.RunID)
	if err != nil {
		return nil, err
	}
	p := Object{"runId": q.RunID, "runVersion": revision(run), "pinned": q.Pinned, "effects": []string{"set cache retention pin; no vault writes"}}
	after := run
	after.Pinned = q.Pinned
	cache := queueFiles(after)
	delete(cache, filepath.Join("recruiting/runs", run.ID, "drafts.json"))
	p["cacheFiles"] = cache
	policy := "standing_authorization"
	if run.Pinned == q.Pinned {
		p["no_change"] = true
		policy = "no_change"
	}
	return prepared("source_run.pin.prepare", policy, p, entity("recruiting", "run", run.ID, run.ID, run)), nil
}
func (a *Adapter) unrejectPrepare(q DraftInput) (Object, error) {
	run, err := a.Runs.Get(q.RunID)
	if err != nil {
		return nil, err
	}
	after, err := a.Runs.PreviewUnreject(q.RunID, q.DraftID)
	if err != nil {
		return nil, err
	}
	for _, d := range run.Drafts {
		if d.ID == q.DraftID {
			p := Object{"runId": q.RunID, "draftId": q.DraftID, "runVersion": revision(run), "asOf": time.Now().UTC(), "queueAfter": after}
			if d.Status == recruiting.DraftNew {
				p["no_change"] = true
				return prepared("candidate_unreject.prepare", "no_change", p), nil
			}
			writes, capture := a.capture()
			if err := capture.RemovePassed(recruiting.PassedKey(d.Draft.SourceID, d.Draft.ExternalID, d.Draft.Name)); err != nil {
				return nil, err
			}
			p["cacheFiles"] = queueFiles(after)
			p["vaultFiles"] = writes
			p["effects"] = []string{"reverse pass, remove passed.md suppression, return draft to new and clear expiry"}
			return prepared("candidate_unreject.prepare", "human_approval", p, entity("recruiting", "draft", q.RunID+"/"+q.DraftID, d.Draft.Name, d)), nil
		}
	}
	return nil, fmt.Errorf("draft not found")
}
func (a *Adapter) lookupPrepare(q DraftInput) (Object, error) {
	run, err := a.Runs.Get(q.RunID)
	if err != nil {
		return nil, err
	}
	for _, d := range run.Drafts {
		if d.ID == q.DraftID {
			return prepared("candidate_lookup.prepare", "standing_authorization", Object{"runId": q.RunID, "draftId": q.DraftID, "runVersion": revision(run), "asOf": time.Now().UTC(), "effects": []string{"bounded cross-source lookup; enrich cached draft evidence; no vault writes; network results unknown until execution"}}, entity("recruiting", "draft", q.RunID+"/"+q.DraftID, d.Draft.Name, d)), nil
		}
	}
	return nil, fmt.Errorf("draft not found")
}

type GraphEntityInput struct {
	IdempotencyKey string       `json:"idempotencyKey,omitempty"`
	Conversation   string       `json:"conversation,omitempty"`
	Turn           string       `json:"turn,omitempty"`
	Entity         graph.Entity `json:"entity"`
}

func (a *Adapter) graphEntityPrepare(q GraphEntityInput) (Object, error) {
	writes := map[string]string{}
	g := graph.NewStore(a.Vault, a.Graph.Root(), func(path string, b []byte) error {
		rel, err := filepath.Rel(a.Vault, path)
		if err != nil {
			return err
		}
		writes[rel] = string(b)
		return nil
	})
	e, added, err := g.AddEntity(q.Entity)
	if err != nil {
		return nil, err
	}
	policy := "human_approval"
	if !added {
		policy = "no_change"
	}
	return prepared("graph_entity.add.prepare", policy, Object{"graphEntity": e, "vaultFiles": writes, "no_change": !added}), nil
}

type SeedInput struct {
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	Conversation   string          `json:"conversation,omitempty"`
	Turn           string          `json:"turn,omitempty"`
	Seed           recruiting.Seed `json:"seed"`
}

func (a *Adapter) seedPrepare(q SeedInput) (Object, error) {
	now := time.Now().UTC()
	// Source and consent describe the actual origin, never impersonate the owner.
	q.Seed.Source = "agent:alfred"
	if q.Seed.Class == recruiting.SeedPerson && q.Seed.Consent == "" {
		q.Seed.Consent = "proposed; owner approval required"
	}
	writes, capture := a.capture()
	seed, err := capture.AddSeed(q.Seed, now)
	if err != nil {
		return nil, err
	}
	return prepared("seed.prepare", "human_approval", Object{"seed": seed, "asOf": now, "vaultFiles": writes}), nil
}
func (a *Adapter) executeTool(ctx context.Context, q OperationInput, tool string) (Object, error) {
	o, err := a.loadOperation(q.OperationID)
	if err != nil {
		return nil, err
	}
	if o.Tool != tool {
		return nil, fmt.Errorf("operation belongs to %s", o.Tool)
	}
	return a.Execute(ctx, q.OperationID)
}

func queueFiles(run recruiting.Run) map[string]string {
	out := map[string]string{}
	for name, value := range map[string]any{"run.json": run.RunState, "drafts.json": run.Drafts} {
		b, _ := json.MarshalIndent(value, "", "  ")
		out[filepath.Join("recruiting/runs", run.ID, name)] = string(b)
	}
	return out
}
