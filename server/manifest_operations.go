package server

import (
	"context"
	"fmt"
	"log"
	"manifest/manifestmcp"
	"net/http"
)

// UseManifestOperations uses the same disk receipts as the stdio MCP process.
// It is wired independently of Hermes, so owner inspection and decisions work
// during provider outages. FEED pending files are recoverable projections.
func (s *Server) UseManifestOperations(a *manifestmcp.Adapter) {
	s.manifestOperations = a
	a.Approvals = s.approvals
	if s.approvals != nil {
		s.approvals.WithOperationDecision(func(id, decision string) error {
			s.operationMu.Lock()
			defer s.operationMu.Unlock()
			if _, err := a.Observe(); err != nil {
				return err
			}
			out, err := a.Operation(id)
			if err != nil {
				return err
			}
			status := out["status"]
			if status == "pending_approval" {
				out, err = a.Decide(id, decision, "owner:local")
				if err != nil {
					return err
				}
				status = out["status"]
			}
			if decision == "approved" && (status == "approved" || status == "executing") {
				out, err = a.Execute(context.Background(), id)
				if err != nil {
					return err
				}
				status = out["status"]
			}
			if decision == "rejected" && status == "rejected" {
				return nil
			}
			if decision == "approved" && (status == "succeeded" || status == "partial" || status == "failed") {
				return nil
			}
			return fmt.Errorf("operation is %s; inspect its receipt", status)
		})
	}
	s.syncManifestOperations()
}

func (s *Server) syncManifestOperations() []*manifestmcp.OperationRecord {
	if s.manifestOperations == nil {
		return nil
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	rows, err := s.manifestOperations.Observe()
	if err != nil {
		log.Printf("manifest operations: %v", err)
		return nil
	}
	if s.approvals != nil {
		for _, o := range rows {
			if o.Status == "approved" || o.Status == "executing" {
				if out, e := s.manifestOperations.Execute(context.Background(), o.ID); e != nil {
					log.Printf("manifest execution %s: %v", o.ID, e)
					continue
				} else {
					*o = *out["record"].(*manifestmcp.OperationRecord)
				}
			}
			if o.Status == "pending_approval" {
				err = s.manifestOperations.FileProposal(o)
			} else if o.Policy == "human_approval" {
				id := manifestmcp.ProposalID(o.ID)
				if _, e := s.approvals.LoadPending(id); e == nil {
					status := "rejected"
					if o.ApprovalActor != "" && o.Status != "stale" {
						status = "approved"
					}
					err = s.approvals.Settle(id, status)
				}
			}
			if err != nil {
				log.Printf("manifest proposal %s: %v", o.ID, err)
			}
		}
	}
	return rows
}

func (s *Server) chatOperations(conversation string) []map[string]any {
	out := []map[string]any{}
	for _, o := range s.syncManifestOperations() {
		if o.Conversation == conversation {
			out = append(out, map[string]any{"record": o, "proposal": manifestmcp.Proposal(o)})
		}
	}
	return out
}

func (s *Server) handleOperationRegenerate(w http.ResponseWriter, r *http.Request) {
	if s.manifestOperations == nil {
		http.Error(w, "operations unavailable", 503)
		return
	}
	s.syncManifestOperations()
	out, err := s.manifestOperations.Regenerate(r.PathValue("id"))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, out)
}

// Context carries current receipts, not duplicate copies of every proposed file.
func (s *Server) operationContext(conversation string) []map[string]any {
	out := []map[string]any{}
	for _, o := range s.syncManifestOperations() {
		if o.Conversation != conversation {
			continue
		}
		out = append(out, map[string]any{"operationId": o.ID, "tool": o.Tool, "status": o.Status, "error": o.Error, "result": o.Result})
	}
	return out
}
