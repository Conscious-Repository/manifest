package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"manifest/gmailsend"
	"manifest/recruiting"
)

// Approval-gated Gmail outreach (Phase 5, plan §4.8). Every route here is a
// user action, mounted inside server.go's `if s.recruiting != nil` block and
// never on the portal listener. Nothing sends without POST …/send carrying
// approve:true, and that handler is the ONLY caller of the sender: there is
// no poller, no queue, no goroutine, no retry.
//
// Route naming mirrors recruiting_ashby_private.go:
//
//	GET|POST /api/aion/recruiting/outreach/probe           {sendCapable, sender, …} — never the token
//	POST     /api/aion/recruiting/outreach/connect         paste-back OAuth: the consent URL at gmail.send
//	POST     /api/aion/recruiting/outreach/connect/finish  {pasted} → exchange, store the send token 0600
//	GET      /api/aion/recruiting/outreach/{id}            the candidate's log (drafts + sends)
//	POST     /api/aion/recruiting/outreach/draft/{id}      generate or capture a draft; no network
//	POST     /api/aion/recruiting/outreach/prepare/{id}    readiness preflight; writes nothing
//	POST     /api/aion/recruiting/outreach/send/{id}       the approved send: approve:true or 409
//
// Sender posture: without a token the probe answers sendCapable:false and
// send answers 409 — absent config is a state the inspector paints, not a
// failure (the Ashby configured:false posture). The token lives under
// dataDir (gmailsend.TokenPath), never the vault, never config.json, and
// no response here has a slot for it.

// UseGmailSend wires the send-only client (beside UseAshbySync). Nil leaves
// the routes mounted in the unconfigured posture.
func (s *Server) UseGmailSend(c *gmailsend.Client) { s.gmailSend = c }

// gmailOutreachSender adapts gmailsend.Client to recruiting.OutreachSender.
// It is the whole of what the recruiting store can do with Gmail.
type gmailOutreachSender struct{ c *gmailsend.Client }

func (g gmailOutreachSender) Sender() string    { return g.c.Sender() }
func (g gmailOutreachSender) SendCapable() bool { return g.c.SendCapable() }
func (g gmailOutreachSender) Send(ctx context.Context, m recruiting.OutreachMessage) (recruiting.OutreachSendRef, error) {
	ref, err := g.c.Send(ctx, gmailsend.Message{To: m.To, Subject: m.Subject, Body: m.Body})
	if err != nil {
		return recruiting.OutreachSendRef{}, err
	}
	return recruiting.OutreachSendRef{MessageID: ref.ID, ThreadID: ref.ThreadID}, nil
}

// outreachSender is nil in the unconfigured posture (no client wired).
func (s *Server) outreachSender() recruiting.OutreachSender {
	if s.gmailSend == nil {
		return nil
	}
	return gmailOutreachSender{s.gmailSend}
}

func (s *Server) outreachSenderAddr() string {
	if s.gmailSend == nil {
		return gmailsend.DefaultSender
	}
	return s.gmailSend.Sender()
}

// outreachProbe is the probe body: the send state (never the token) plus
// the network people a warm/referral draft may go through.
func (s *Server) outreachProbe() map[string]any {
	st := gmailsend.State{Sender: s.outreachSenderAddr(), Scopes: []string{}, Detail: "sender not wired"}
	if s.gmailSend != nil {
		st = s.gmailSend.Status()
	}
	return map[string]any{
		"configured": st.Configured, "sendCapable": st.SendCapable, "sender": st.Sender,
		"email": st.Email, "hasCreds": st.HasCreds, "detail": st.Detail,
		"people": s.recruiting.OutreachPeople(),
	}
}

// outreachError maps the typed refusals: unconfigured, unapproved and
// not-ready are 409s carrying the readiness (what stopped it); a Gmail
// failure is a 502 (already redacted by the client); the rest 400.
func outreachError(w http.ResponseWriter, err error, readiness recruiting.OutreachReadiness) {
	switch {
	case errors.Is(err, recruiting.ErrOutreachUnconfigured),
		errors.Is(err, recruiting.ErrOutreachNotApproved),
		errors.Is(err, recruiting.ErrOutreachNotReady):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]any{"error": err.Error(), "readiness": readiness})
	case errors.Is(err, gmailsend.ErrUnconfigured), errors.Is(err, gmailsend.ErrNoSendScope), errors.Is(err, gmailsend.ErrSenderMismatch):
		http.Error(w, err.Error(), http.StatusConflict)
	case strings.HasPrefix(err.Error(), "gmail send:"):
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		httpError(w, err)
	}
}

func outreachCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 45*time.Second)
}

// GET|POST …/outreach/probe → always 200.
func (s *Server) handleRecruitingOutreachProbe(w http.ResponseWriter, _ *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	writeJSON(w, s.outreachProbe())
}

// GET …/outreach/{id} → {entries}: the append-only log.
func (s *Server) handleRecruitingOutreach(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	entries, err := s.recruiting.Outreach(strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"entries": entries})
}

// POST …/outreach/draft/{id} {kind, to?, via?, subject?, body?} → {entry,
// view}. Generates when subject/body are empty, captures the owner's edit
// otherwise. Touches no network.
func (s *Server) handleRecruitingOutreachDraft(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b recruiting.OutreachDraftRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decode(r, &b); err != nil {
			httpError(w, errBadRequest("a draft needs a JSON body"))
			return
		}
	}
	entry, _, err := s.recruiting.DraftOutreach(strings.TrimSpace(r.PathValue("id")), b, s.outreachSenderAddr(), time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"entry": entry, "view": s.recruiting.View()})
}

// POST …/outreach/prepare/{id} → {readiness}. Writes nothing.
func (s *Server) handleRecruitingOutreachPrepare(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	rd, err := s.recruiting.PrepareOutreach(strings.TrimSpace(r.PathValue("id")), s.outreachSender())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"readiness": rd})
}

// POST …/outreach/send/{id} {approve:true, subject?, body?, actor?} → the
// approved send. Without approve:true, without readiness, or without a
// connected sender: 409 with the readiness, and nothing sent.
func (s *Server) handleRecruitingOutreachSend(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b recruiting.OutreachSendRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decode(r, &b); err != nil {
			httpError(w, errBadRequest("a send needs a JSON body carrying approve:true"))
			return
		}
	}
	ctx, cancel := outreachCtx(r)
	defer cancel()
	res, err := s.recruiting.SendOutreach(ctx, strings.TrimSpace(r.PathValue("id")), b, s.outreachSender(), time.Now())
	if err != nil {
		outreachError(w, err, res.Readiness)
		return
	}
	writeJSON(w, map[string]any{"send": res, "view": s.recruiting.View()})
}
