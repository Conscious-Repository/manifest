package manifestmcp

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"manifest/graph"
	"manifest/recruiting"
)

// BatchAcceptInput selects an explicit, bounded set; a search never auto-accepts.
type BatchAcceptInput struct {
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
	Conversation   string   `json:"conversation,omitempty"`
	Turn           string   `json:"turn,omitempty"`
	RunID          string   `json:"runId"`
	DraftIDs       []string `json:"draftIds"`
}

type acceptStep struct {
	RunVersion string                     `json:"runVersion"`
	DraftID    string                     `json:"draftId"`
	AsOf       time.Time                  `json:"asOf"`
	Candidate  recruiting.Candidate       `json:"candidate"`
	Claims     recruiting.KnowledgeClaims `json:"claims"`
	Files      map[string]string          `json:"vaultFiles"`
}

// applyAccept is shared by single execution, batch rehearsal and batch execution.
func applyAccept(runs *recruiting.RunStore, g *graph.Store, runID string, step acceptStep) (recruiting.KnowledgeResult, error) {
	var result recruiting.KnowledgeResult
	if _, _, err := runs.Accept(runID, step.DraftID, step.AsOf); err != nil {
		return result, err
	}
	memory := &knowledgeMemory{entities: g.LoadEntities(), edges: g.LoadEdges(), vocab: g.Vocabulary()}
	result, err := recruiting.ApplyKnowledge(memory, step.Claims)
	if err == nil && len(result.AddedEntities) > 0 {
		err = g.SaveEntities(memory.entities)
	}
	if err == nil && len(result.AddedEdges) > 0 {
		err = g.SaveEdges(memory.edges)
	}
	return result, err
}

func (a *Adapter) batchAcceptPrepare(q BatchAcceptInput) (Object, error) {
	if len(q.DraftIDs) == 0 || len(q.DraftIDs) > recruiting.MaxRunMax {
		return nil, fmt.Errorf("select 1–%d draft IDs", recruiting.MaxRunMax)
	}
	seen := map[string]bool{}
	for _, id := range q.DraftIDs {
		if id == "" || seen[id] {
			return nil, fmt.Errorf("draft IDs must be nonempty and unique")
		}
		seen[id] = true
	}
	run, err := a.Runs.Get(q.RunID)
	if err != nil {
		return nil, err
	}
	before, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	// Rehearse on a private disk snapshot so every shared service reads the
	// preceding draft's effects, including identity resolution and graph dedupe.
	dir, err := os.MkdirTemp("", "manifest-accept-preview-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	stage, err := New(filepath.Join(dir, "vault"), filepath.Join(dir, "data"), a.System)
	if err != nil {
		return nil, err
	}
	put := func(path string, b []byte) error {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		return os.WriteFile(path, b, 0600)
	}
	for rel, want := range before {
		b, err := os.ReadFile(filepath.Join(a.Vault, rel))
		if err != nil {
			return nil, err
		}
		if revision(string(b)) != want {
			return nil, fmt.Errorf("target changed during batch preview")
		}
		if err := put(filepath.Join(stage.Vault, rel), b); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"run.json", "drafts.json"} {
		b, err := os.ReadFile(filepath.Join(a.Runs.Root(), run.ID, name))
		if err != nil {
			return nil, err
		}
		if err := put(filepath.Join(stage.Runs.Root(), run.ID, name), b); err != nil {
			return nil, err
		}
	}
	copied, err := stage.Runs.Get(run.ID)
	if err != nil || revision(copied) != revision(run) {
		return nil, fmt.Errorf("run changed during batch preview")
	}
	records := recruiting.NewStore(stage.Vault, stage.Root, put)
	runs, err := recruiting.NewRunStore(stage.Runs.Root(), records)
	if err != nil {
		return nil, err
	}
	g := graph.NewStore(stage.Vault, stage.Graph.Root(), put)
	steps := []acceptStep{}
	refs := []Entity{}
	files := map[string]string{}
	var cache map[string]string
	var after any
	for _, id := range q.DraftIDs {
		out, err := stage.draftPrepare(DraftInput{RunID: run.ID, DraftID: id}, true)
		if err != nil {
			return nil, err
		}
		p := out["operation"].(Object)["preview"].(Object)
		var step acceptStep
		if err := decode(p, &step); err != nil {
			return nil, err
		}
		if _, err := applyAccept(runs, g, run.ID, step); err != nil {
			return nil, err
		}
		for rel, want := range step.Files {
			b, err := os.ReadFile(filepath.Join(stage.Vault, rel))
			if err != nil || string(b) != want {
				return nil, fmt.Errorf("batch rehearsal differs from preview: %s", rel)
			}
			files[rel] = want
		}
		steps = append(steps, step)
		refs = append(refs, entity("recruiting", "draft", run.ID+"/"+id, step.Candidate.Name, p["draft"]))
		cache = p["cacheFiles"].(map[string]string)
		after = p["queueAfter"]
	}
	current, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	currentRun, err := a.Runs.Get(run.ID)
	if err != nil || revision(currentRun) != revision(run) || revision(current) != revision(before) {
		return nil, fmt.Errorf("targets changed during batch preview")
	}
	return prepared("candidate_accept_batch.prepare", "human_approval", Object{
		"runId": run.ID, "runVersion": revision(run), "steps": steps,
		"vaultFiles": files, "cacheFiles": cache, "queueAfter": after,
		"effects": []string{"accept selected drafts in order through shared services; each intermediate and final vault write is approved", "one receipt; partial completion is reported without automatic replay; final queue includes all selected decisions"},
	}, refs...), nil
}
