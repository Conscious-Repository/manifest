package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"manifest/recruiting"
)

// The PRIVATE Ashby client and the approved write path (Phase 6,
// aion-recruiting-ashby-mirroring.md). Every route here is a user action,
// mounted inside server.go's `if s.recruiting != nil` block and never on the
// portal listener. There is no poller: the sync-back is a route the owner
// hits, and the webhook receiver (recruiting_ashby_webhook.go, Phase 7)
// funnels into the same sync-back.
//
// Route naming mirrors recruiting_ashby.go:
//
//	GET|POST /api/aion/recruiting/ashby/probe          capability probe (200, configured:false without a key)
//	GET      /api/aion/recruiting/ashby/state          sync tokens, base snapshots, audit tail — never the key
//	POST     /api/aion/recruiting/ashby/preflight/{id} read Ashby, dedupe, render the proposal (writes nothing)
//	POST     /api/aion/recruiting/ashby/push/{id}      the approved write: create-or-link → project|application → note → re-fetch → persist
//	POST     /api/aion/recruiting/ashby/stage/{id}     application.changeStage (advance or archive by reason)
//	POST     /api/aion/recruiting/ashby/sync           the user-actioned sync-back trigger (full or incremental)

// UseAshbySync wires the private client + write path (beside UseRecruiting).
func (s *Server) UseAshbySync(a *recruiting.AshbySync) { s.ashbySync = a }

// ashbyReady is recruitingReady plus the sync service.
func (s *Server) ashbyReady(w http.ResponseWriter) bool {
	if !s.recruitingReady(w) {
		return false
	}
	if s.ashbySync == nil {
		http.Error(w, "ashby unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// ashbyError maps the typed failures onto statuses: unconfigured is a 409
// (the action cannot proceed in this state), a conflict is a 409 carrying
// the proposal, an upstream failure is a 502, everything else a 400. No
// path here can echo the key: the client redacts it before an error exists.
func ashbyError(w http.ResponseWriter, err error) {
	var api *recruiting.AshbyError
	switch {
	case errors.Is(err, recruiting.ErrAshbyUnconfigured):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, recruiting.ErrAshbyConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.As(err, &api):
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		httpError(w, err)
	}
}

func ashbyCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 45*time.Second)
}

// GET|POST …/ashby/probe → {"configured":false,"scopes":[]} without a key;
// with one, apiKey.info's scopes. Always 200: an uninstalled key is a state
// the UI paints, not a failure.
func (s *Server) handleRecruitingAshbyProbe(w http.ResponseWriter, r *http.Request) {
	if !s.ashbyReady(w) {
		return
	}
	ctx, cancel := ashbyCtx(r)
	defer cancel()
	writeJSON(w, s.ashbySync.Probe(ctx))
}

// GET …/ashby/state → the derived sync state (dataDir), for the inspector.
func (s *Server) handleRecruitingAshbyState(w http.ResponseWriter, _ *http.Request) {
	if !s.ashbyReady(w) {
		return
	}
	st := s.ashbySync.State()
	writeJSON(w, map[string]any{"configured": s.ashbySync.Configured(), "state": st})
}

// decodePush reads the push/preflight body and pins the candidate from the
// path so a body cannot address a different record than the route names.
func decodePush(r *http.Request) (recruiting.AshbyPushRequest, error) {
	var b recruiting.AshbyPushRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decode(r, &b); err != nil {
			return b, errBadRequest("a push needs a JSON body")
		}
	}
	b.Candidate = strings.TrimSpace(r.PathValue("id"))
	return b, nil
}

// POST …/ashby/preflight/{id} → the proposal. Reads Ashby; writes nothing.
func (s *Server) handleRecruitingAshbyPreflight(w http.ResponseWriter, r *http.Request) {
	if !s.ashbyReady(w) {
		return
	}
	req, err := decodePush(r)
	if err != nil {
		httpError(w, err)
		return
	}
	ctx, cancel := ashbyCtx(r)
	defer cancel()
	prop, err := s.ashbySync.Preflight(ctx, req)
	if err != nil {
		ashbyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"proposal": prop})
}

// POST …/ashby/push/{id} → the approved write. The body must carry
// approve:true, the handoff choice, and — when the preflight found matches —
// the link/create decision. A conflict or a missing choice is a 409 with the
// re-rendered proposal, so the owner sees exactly what stopped it.
func (s *Server) handleRecruitingAshbyPush(w http.ResponseWriter, r *http.Request) {
	if !s.ashbyReady(w) {
		return
	}
	req, err := decodePush(r)
	if err != nil {
		httpError(w, err)
		return
	}
	ctx, cancel := ashbyCtx(r)
	defer cancel()
	res, err := s.ashbySync.Push(ctx, req, time.Now())
	if err != nil {
		if errors.Is(err, recruiting.ErrAshbyConflict) || strings.Contains(err.Error(), "explicit choice") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"error": err.Error(), "proposal": res.Proposal})
			return
		}
		ashbyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"push": res, "view": s.recruiting.View()})
}

// POST …/ashby/stage/{id} {interviewStageId | archiveReasonId} → the
// re-fetched application. Ashby-authoritative: the record only mirrors what
// Ashby answers after the move.
func (s *Server) handleRecruitingAshbyStage(w http.ResponseWriter, r *http.Request) {
	if !s.ashbyReady(w) {
		return
	}
	var b struct {
		InterviewStageID string `json:"interviewStageId"`
		ArchiveReasonID  string `json:"archiveReasonId"`
		Actor            string `json:"actor"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, errBadRequest("a stage change needs interviewStageId or archiveReasonId"))
		return
	}
	ctx, cancel := ashbyCtx(r)
	defer cancel()
	app, err := s.ashbySync.ChangeStage(ctx, strings.TrimSpace(r.PathValue("id")),
		strings.TrimSpace(b.InterviewStageID), strings.TrimSpace(b.ArchiveReasonID), strings.TrimSpace(b.Actor), time.Now())
	if err != nil {
		ashbyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"application": app, "view": s.recruiting.View()})
}

// POST …/ashby/sync {full?:bool} → the sync-back trigger. User-actioned;
// nothing else calls SyncBack.
func (s *Server) handleRecruitingAshbySync(w http.ResponseWriter, r *http.Request) {
	if !s.ashbyReady(w) {
		return
	}
	var b struct {
		Full bool `json:"full"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		_ = decode(r, &b)
	}
	ctx, cancel := ashbyCtx(r)
	defer cancel()
	res, err := s.ashbySync.SyncBack(ctx, b.Full, time.Now())
	if err != nil {
		ashbyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"sync": res, "view": s.recruiting.View()})
}
