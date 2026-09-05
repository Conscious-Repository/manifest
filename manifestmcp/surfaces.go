package manifestmcp

import (
	"encoding/json"
	"fmt"
	"manifest/approvals"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func ProposalID(id string) string { return "operation-" + strings.TrimPrefix(id, "sha256:") }

// Proposal is a projection of immutable validated bytes, never agent prose.
func Proposal(o *OperationRecord) approvals.Proposal {
	var payload struct {
		Preview Object `json:"preview"`
	}
	_ = json.Unmarshal(o.Payload, &payload)
	preview := payload.Preview
	delete(preview, "vaultFiles")
	delete(preview, "content")
	delete(preview, "cacheFiles")
	b, _ := json.MarshalIndent(preview, "", "  ")
	body := "Classification: " + o.Policy + "\n\n```json\n" + string(b) + "\n```\n"
	keys := make([]string, 0, len(o.Files))
	for k := range o.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		body += "\nProposed final content: " + k + "\n\n"
		for _, line := range strings.Split(o.Files[k], "\n") {
			body += "    + " + line + "\n"
		}
	}
	return approvals.Proposal{ID: ProposalID(o.ID), Type: approvals.TypeManifestOperation, Action: strings.TrimSuffix(o.Tool, ".prepare"), Agent: o.Agent, Created: o.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), ApplyPath: o.ID, Body: body}
}
func (a *Adapter) FileProposal(o *OperationRecord) error {
	if a.Approvals == nil || o.Policy != "human_approval" || o.Status != "pending_approval" {
		return nil
	}
	_, err := a.Approvals.Propose(Proposal(o))
	return err
}

// Observe re-reads targets even while idle. Manual completion invalidates the
// proposal; it never stamps an agent execution onto a manual domain action.
func (a *Adapter) Observe() ([]*OperationRecord, error) {
	unlock, err := a.lockOperations()
	if err != nil {
		return nil, err
	}
	defer unlock()
	entries, err := os.ReadDir(filepath.Join(a.Data, "operations"))
	if err != nil {
		return nil, err
	}
	current, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	out := []*OperationRecord{}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		o, err := a.loadOperation("sha256:" + strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		if o.Status == "pending_approval" || o.Status == "approved" {
			var p struct {
				RunID      string `json:"runId"`
				RunVersion string `json:"runVersion"`
			}
			if err := json.Unmarshal(o.Arguments, &p); err != nil {
				return nil, err
			}
			stale := o.SchemaVersion != 1 || o.ToolVersion != Version || revision(current) != revision(o.Expected)
			if p.RunID != "" {
				run, err := a.Runs.Get(p.RunID)
				stale = stale || err != nil || revision(run) != p.RunVersion
			}
			if stale {
				o.Status = "stale"
				o.Error = "Target changed or was completed manually. Inspect current state and regenerate for fresh approval."
				if err := a.saveOperation(o); err != nil {
					return nil, err
				}
			}
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// Regenerate calls the same prepare services on saved input, with a fresh retry
// identity. The old receipt is immutable; a new payload requires new approval.
func (a *Adapter) Regenerate(id string) (Object, error) {
	o, err := a.loadOperation(id)
	if err != nil {
		return nil, err
	}
	if o.Status != "stale" {
		return nil, fmt.Errorf("only stale operations can regenerate")
	}
	var input Object
	if err = json.Unmarshal(o.Input, &input); err != nil {
		return nil, fmt.Errorf("saved input unavailable: %w", err)
	}
	input["idempotencyKey"] = "regenerate:" + id + ":" + time.Now().UTC().Format(time.RFC3339Nano)
	before, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	var out Object
	switch o.Tool {
	case "candidate_accept.prepare", "candidate_reject.prepare":
		var q DraftInput
		err = decode(input, &q)
		if err == nil {
			out, err = a.draftPrepare(q, o.Tool == "candidate_accept.prepare")
		}
	case "network_person.prepare":
		var q PersonInput
		err = decode(input, &q)
		if err == nil {
			out, err = a.personPrepare(q)
		}
	case "graph_edge.prepare":
		var q EdgeInput
		err = decode(input, &q)
		if err == nil {
			out, err = a.edgePrepare(q)
		}
	default:
		return nil, fmt.Errorf("operation cannot regenerate")
	}
	if err != nil {
		return nil, err
	}
	return a.persist(out, input, before)
}
