package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"manifest/artifacts"
	"manifest/ledger"
	"manifest/manifestmcp"
	"manifest/recruiting"
)

// REJECTION WITH A REAL EMAIL (social graph plan phase 5). Three steps, and
// the order is the point:
//
//  1. GET  …/reject/{id}          the rendered email + "seen before" flags
//  2. POST …/reject/prepare/{id}  freezes that exact email as an owner-
//     approval operation. Nothing is sent: approval is owner-only by
//     construction (manifestmcp), and happens on the approval card.
//  3. POST …/reject/complete/{id} only once the receipt says SENT: writes
//     the Ashby rejection with the chosen reason (or, for a record with no
//     Ashby application, archives it here).
//
// So a rejection is never recorded for an email that was not sent, and a
// failure between mail and Ashby says exactly that and retries.

const rejectionKind = "recruiting-rejection"

func (s *Server) recruitingRejectionOperations(id string) []map[string]any {
	out := []map[string]any{}
	for _, o := range s.syncManifestOperations() {
		var q manifestmcp.EmailInput
		if o.Tool != "email.prepare" || json.Unmarshal(o.Input, &q) != nil || q.SourceRecord == nil ||
			q.SourceRecord.Kind != rejectionKind || q.SourceRecord.ID != id {
			continue
		}
		if q.IdempotencyKey != rejectionKind+":"+id+":"+q.SourceRecord.Revision || q.Domain != "aion" {
			continue
		}
		out = append(out, map[string]any{"record": o, "proposal": manifestmcp.Proposal(o)})
	}
	return out
}

// GET /api/aion/recruiting/reject/{id...}
func (s *Server) handleRecruitingRejection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !s.recruitingReady(w) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	d, err := s.recruiting.RejectionFor(id)
	out := map[string]any{"draft": d, "operations": []map[string]any{},
		"template":  "system/aion/recruiting/" + recruiting.RejectionTemplateFile,
		"approvals": s.manifestOperations != nil && s.approvals != nil}
	if err != nil {
		out["error"] = err.Error()
	}
	if s.manifestOperations != nil {
		out["operations"] = s.recruitingRejectionOperations(id)
	}
	writeJSON(w, out)
}

// POST /api/aion/recruiting/reject/prepare/{id...} {revision}
func (s *Server) handleRecruitingRejectionPrepare(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !s.recruitingReady(w) {
		return
	}
	if s.manifestOperations == nil || s.approvals == nil {
		http.Error(w, "email approvals unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Revision string `json:"revision"`
	}
	if decode(r, &req) != nil || !artifacts.ValidHash(req.Revision) {
		http.Error(w, "the reviewed email's revision is required", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	for _, item := range s.recruitingRejectionOperations(id) {
		o := item["record"].(*manifestmcp.OperationRecord)
		var q manifestmcp.EmailInput
		if json.Unmarshal(o.Input, &q) == nil && q.SourceRecord != nil && q.SourceRecord.Revision == req.Revision {
			writeJSON(w, map[string]any{"operationId": o.ID, "status": o.Status})
			return
		}
	}
	d, err := s.recruiting.RejectionFor(id)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	if d.Revision != req.Revision {
		http.Error(w, "the email changed (template or record) — review it again", http.StatusConflict)
		return
	}
	out, err := s.manifestOperations.PrepareEmail(manifestmcp.EmailInput{
		Domain: "aion", To: d.To, Subject: d.Subject, Body: d.Body,
		IdempotencyKey: rejectionKind + ":" + id + ":" + d.Revision,
		SourceRecord:   &manifestmcp.EmailSourceRecord{Kind: rejectionKind, ID: id, Revision: d.Revision},
	})
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, out)
}

// POST /api/aion/recruiting/reject/complete/{id...} {operationId, archiveReasonId}
func (s *Server) handleRecruitingRejectionComplete(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	if s.manifestOperations == nil {
		http.Error(w, "email receipts unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		OperationID     string `json:"operationId"`
		ArchiveReasonID string `json:"archiveReasonId"`
		ApplicationID   string `json:"applicationId"`
	}
	if decode(r, &req) != nil || strings.TrimSpace(req.OperationID) == "" {
		http.Error(w, "which approved email?", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	outcome, err := s.manifestOperations.ConfirmedEmail(req.OperationID)
	if err != nil {
		http.Error(w, "the rejection email has not been sent yet — approve it first ("+err.Error()+")", http.StatusConflict)
		return
	}
	if outcome.Source == nil || outcome.Source.Kind != rejectionKind || outcome.Source.ID != id ||
		outcome.Message.From != "ben@aion.bio" || len(outcome.Message.Cc) > 0 || len(outcome.Message.Attachments) > 0 {
		http.Error(w, "that receipt is not this applicant's rejection", http.StatusConflict)
		return
	}
	if strings.TrimSpace(req.ArchiveReasonID) != "" {
		if s.ashbySync == nil || !s.ashbySync.Configured() {
			http.Error(w, "the email was sent, but no Ashby key is installed — the rejection was not written there", http.StatusConflict)
			return
		}
		ctx, cancel := ashbyCtx(r)
		defer cancel()
		// the archived stage of THIS application's plan is resolved from
		// the reason; the application's own status then carries the verdict
		// (the stage control's rule — the record is not archived wholesale,
		// because the same person may hold another live application)
		if _, err := s.ashbySync.ChangeStage(ctx, id, "", strings.TrimSpace(req.ArchiveReasonID), "owner triage — rejection emailed",
			time.Now(), strings.TrimSpace(req.ApplicationID)); err != nil {
			http.Error(w, "the email was sent, but Ashby was not updated — retry ("+err.Error()+")", http.StatusBadGateway)
			return
		}
	} else if _, err := s.recruiting.Archive(id, true, time.Now()); err != nil {
		http.Error(w, "the email was sent, but the record did not archive — retry ("+err.Error()+")", http.StatusInternalServerError)
		return
	}
	s.ledger(ledger.Entry{Source: "recruiting", Kind: "recruiting.candidate.rejected", Actor: "owner",
		Object: ledger.Object{Kind: "candidate", ID: id},
		Text:   ledger.Snip("rejection emailed and archived: "+id, 280),
		Meta:   map[string]any{"operation": req.OperationID, "messageId": outcome.Ref.ID, "ashby": req.ArchiveReasonID != ""}})
	writeJSON(w, s.recruiting.View())
}
