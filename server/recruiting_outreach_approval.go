package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"manifest/artifacts"
	"manifest/manifestmcp"
	"manifest/recruiting"
)

func outreachDraftRevision(entry recruiting.OutreachEntry) string {
	raw, _ := json.Marshal(entry)
	return artifacts.Hash(raw)
}

// Preparing a reviewed saved draft never sends. A stable source revision gives
// retries one canonical operation, including after losing the HTTP response.
func (s *Server) handleRecruitingOutreachPropose(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !s.recruitingReady(w) {
		return
	}
	if s.manifestOperations == nil || s.approvals == nil {
		http.Error(w, "email approvals unavailable", 503)
		return
	}
	var req struct {
		Revision string `json:"revision"`
	}
	if decode(r, &req) != nil || !artifacts.ValidHash(req.Revision) {
		http.Error(w, "reviewed draft revision required", 400)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	// Recover an already frozen operation even if readiness or source changed.
	for _, item := range s.recruitingOutreachOperations(id) {
		o := item["record"].(*manifestmcp.OperationRecord)
		var q manifestmcp.EmailInput
		if json.Unmarshal(o.Input, &q) == nil && q.SourceRecord != nil && q.SourceRecord.Revision == req.Revision {
			writeJSON(w, map[string]any{"operationId": o.ID, "status": o.Status})
			return
		}
	}
	ready, err := s.recruiting.PrepareOutreach(id, s.outreachSender())
	if err != nil {
		httpError(w, err)
		return
	}
	if ready.Draft == nil || outreachDraftRevision(*ready.Draft) != req.Revision {
		http.Error(w, "draft changed; review the saved draft again", 409)
		return
	}
	if !ready.Ready {
		outreachError(w, recruiting.ErrOutreachNotReady, ready)
		return
	}
	d := ready.Draft
	q := manifestmcp.EmailInput{Domain: "aion", To: d.To, Subject: d.Subject, Body: d.Body, IdempotencyKey: "recruiting-outreach:" + id + ":" + req.Revision, SourceRecord: &manifestmcp.EmailSourceRecord{Kind: "recruiting-outreach", ID: id, Revision: req.Revision}}
	out, err := s.manifestOperations.PrepareEmail(q)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, out)
}

// Canonical receipt projection avoids writing a second delivery receipt into the
// append-only legacy log. Exact source metadata is frozen with the operation.
func (s *Server) recruitingOutreachOperations(id string) []map[string]any {
	out := []map[string]any{}
	for _, o := range s.syncManifestOperations() {
		var q manifestmcp.EmailInput
		if o.Tool != "email.prepare" || json.Unmarshal(o.Input, &q) != nil || q.SourceRecord == nil || q.SourceRecord.Kind != "recruiting-outreach" || q.SourceRecord.ID != id {
			continue
		}
		// Only requests made by this source bridge get projected as linked outreach.
		if q.IdempotencyKey != "recruiting-outreach:"+id+":"+q.SourceRecord.Revision || q.Domain != "aion" {
			continue
		}
		out = append(out, map[string]any{"record": o, "proposal": manifestmcp.Proposal(o)})
	}
	return out
}
