package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"manifest/aion"
	"manifest/jev"
	"manifest/typesafe"
)

// Jev advisory endpoints (TypeSafe System One; packages typesafe + jev).
//
//	GET  /api/jev/status                     → {enabled, keySource, model}
//	POST /api/aion/transcripts/tier/advise   {name?, text, source?}
//	POST /api/jev/clarify-gate               {request, context?, proposedDefault?}
//	POST /api/jev/evidence-check             {claim, evidence?, context?}
//	POST /api/jev/goal-state                 {goal?, status, context?}
//	POST /api/jev/approval-risk              {action, context?}
//
// Every answer is ADVISORY: these handlers read their request body and call
// Jev; they write nothing — not the tier map, not an approval, not a run.
// The same judgments also run automatically at the existing decision points
// (transcript visibility card default, approval-risk rows, Alfred's work
// order, coding-run sidecars) — see jev_auto.go; advisory there too.
//
// Key: TYPESAFE_API_KEY, else the key held in Settings › Portals › TypeSafe,
// resolved on every request (so setting either takes effect without a
// restart). With neither, every POST answers 503
// {ok:false, disabled:true, error:"TYPESAFE_API_KEY is not set"} — no
// judgment, no fallback guess. The key is never logged or returned.

// jevBodyLimit bounds a request body: the judgment's own text limit plus
// room for JSON framing and the small fields.
const jevBodyLimit = jev.MaxTierText + 32<<10

// jevTimeout bounds one advisory call end to end (retries included).
const jevTimeout = 45 * time.Second

// jevHTTP is shared by every live advisory call (connection reuse).
var jevHTTP = &http.Client{Timeout: typesafe.DefaultTimeout}

// typesafeKey resolves the key and where it came from ("env", "portal", "").
func (s *Server) typesafeKey() (key, source string) {
	if k := typesafe.EnvKeyFunc(); k != "" {
		return k, "env"
	}
	if s.portals != nil {
		if k := strings.TrimSpace(s.portals.Credential("typesafe", "apiKey")); k != "" {
			return k, "portal"
		}
	}
	return "", ""
}

// jevAdvisor is the injected judge (tests) or one over the live client.
func (s *Server) jevAdvisor() *jev.Judge {
	if s.jevJudge != nil {
		return s.jevJudge
	}
	c := typesafe.NewFromEnv()
	c.Key = func() string { k, _ := s.typesafeKey(); return k }
	c.HTTP = jevHTTP
	return &jev.Judge{Eval: c, Model: c.ModelName()}
}

// jevEnabled: a judge was injected, or a key is configured right now.
func (s *Server) jevEnabled() bool {
	if s.jevJudge != nil {
		return true
	}
	k, _ := s.typesafeKey()
	return k != ""
}

func (s *Server) handleJevStatus(w http.ResponseWriter, r *http.Request) {
	_, src := s.typesafeKey()
	model := typesafe.NewFromEnv().ModelName()
	if s.jevJudge != nil && s.jevJudge.Model != "" {
		model = s.jevJudge.Model
	}
	writeJSON(w, map[string]any{"ok": true, "enabled": s.jevEnabled(), "keySource": src, "model": model, "advisory": true})
}

// jevWrite writes a JSON body with a status.
func jevWrite(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func jevDisabled(w http.ResponseWriter) {
	jevWrite(w, http.StatusServiceUnavailable, map[string]any{
		"ok": false, "disabled": true, "error": typesafe.ErrDisabled.Error(),
		"hint": "set TYPESAFE_API_KEY or add the key in Settings › Portals › TypeSafe",
	})
}

// jevFail maps a judgment error to a status: too large 413, disabled 503,
// bad input 400, upstream 502.
func jevFail(w http.ResponseWriter, err error) {
	var tl jev.ErrTooLarge
	var api *typesafe.APIError
	switch {
	case errors.As(err, &tl):
		jevWrite(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": tl.Error(), "limit": tl.Limit})
	case errors.Is(err, typesafe.ErrDisabled), errors.Is(err, jev.ErrNoEvaluator):
		jevDisabled(w)
	case errors.As(err, &api), strings.HasPrefix(err.Error(), "typesafe:"), errors.Is(err, context.DeadlineExceeded):
		jevWrite(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
	default:
		jevWrite(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
	}
}

// jevDecode reads a bounded JSON body into v. false = response already written.
func jevDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, jevBodyLimit))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			jevWrite(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": "body exceeds " + strconv.Itoa(jevBodyLimit) + " bytes", "limit": jevBodyLimit})
			return false
		}
		jevWrite(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		jevWrite(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON: " + err.Error()})
		return false
	}
	return true
}

// flexText accepts a JSON string or any JSON value; a non-string is kept as
// compact JSON text so the model reads it as named fields.
type flexText string

func (f *flexText) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexText(s)
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		*f = ""
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return err
	}
	*f = flexText(buf.String())
	return nil
}

// jevRun runs one judgment under the timeout (the handler has already
// answered the disabled case, before reading the body).
func (s *Server) jevRun(w http.ResponseWriter, r *http.Request, run func(ctx context.Context, j *jev.Judge) (any, error)) {
	ctx, cancel := context.WithTimeout(r.Context(), jevTimeout)
	defer cancel()
	out, err := run(ctx, s.jevAdvisor())
	if err != nil {
		jevFail(w, err)
		return
	}
	jevWrite(w, http.StatusOK, out)
}

// ------------------------------------------------------------ AION tier advice

type tierAdviseBody struct {
	Name   string `json:"name"`
	Text   string `json:"text"`
	Source string `json:"source"`
}

// handleAionTierAdvise: Jev's tier opinion on one transcript, beside what the
// tier map (the only authority) says about the same note today. The map is
// read, never written; an unmapped note is reported held and ineligible no
// matter what Jev advises.
func (s *Server) handleAionTierAdvise(w http.ResponseWriter, r *http.Request) {
	if !s.jevEnabled() {
		jevDisabled(w)
		return
	}
	var b tierAdviseBody
	if !jevDecode(w, r, &b) {
		return
	}
	name := strings.TrimSpace(b.Name)
	if name != "" && !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	s.jevRun(w, r, func(ctx context.Context, j *jev.Judge) (any, error) {
		adv, err := j.AdviseTier(ctx, jev.TierInput{Name: name, Source: strings.TrimSpace(b.Source), Text: b.Text})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "advisory": true, "applied": false, "advice": adv, "map": s.tierMapStanding(name)}, nil
	})
}

// tierStanding is what the tier map — the authority — says about a note now.
type tierStanding struct {
	SourceOfTruth  string    `json:"sourceOfTruth"`
	Note           string    `json:"note,omitempty"`
	Known          bool      `json:"known"`
	Tier           aion.Tier `json:"tier"` // the map's tier; held when unmapped
	PortalEligible bool      `json:"portalEligible"`
	KairosEligible bool      `json:"kairosEligible"`
}

func (s *Server) tierMapStanding(name string) tierStanding {
	st := tierStanding{SourceOfTruth: aion.TierMapPath, Note: name, Tier: aion.TierHeld}
	if name == "" {
		return st
	}
	tm := s.aionTierMap()
	if tm == nil {
		var err error
		if tm, err = aion.LoadTierMap(); err != nil {
			return st // an unreadable map discloses nothing
		}
	}
	if t, ok := tm.Tier(name); ok {
		st.Known, st.Tier = true, t
	}
	st.PortalEligible = tm.PortalEligible(name)
	st.KairosEligible = tm.KairosEligible(name)
	return st
}

// ------------------------------------------------------- agent-workflow gates

func (s *Server) handleJevClarifyGate(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Request         flexText `json:"request"`
		Context         flexText `json:"context"`
		ProposedDefault flexText `json:"proposedDefault"`
	}
	if !s.jevEnabled() {
		jevDisabled(w)
		return
	}
	if !jevDecode(w, r, &b) {
		return
	}
	s.jevRun(w, r, func(ctx context.Context, j *jev.Judge) (any, error) {
		g, err := j.ClarifyGate(ctx, jev.ClarifyInput{Request: string(b.Request), Context: string(b.Context), ProposedDefault: string(b.ProposedDefault)})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "advisory": true, "result": g}, nil
	})
}

func (s *Server) handleJevEvidenceCheck(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Claim    flexText `json:"claim"`
		Evidence flexText `json:"evidence"`
		Context  flexText `json:"context"`
	}
	if !s.jevEnabled() {
		jevDisabled(w)
		return
	}
	if !jevDecode(w, r, &b) {
		return
	}
	s.jevRun(w, r, func(ctx context.Context, j *jev.Judge) (any, error) {
		e, err := j.EvidenceCheck(ctx, jev.EvidenceInput{Claim: string(b.Claim), Evidence: string(b.Evidence), Context: string(b.Context)})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "advisory": true, "result": e}, nil
	})
}

func (s *Server) handleJevGoalState(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Goal    flexText `json:"goal"`
		Status  flexText `json:"status"`
		Context flexText `json:"context"`
	}
	if !s.jevEnabled() {
		jevDisabled(w)
		return
	}
	if !jevDecode(w, r, &b) {
		return
	}
	s.jevRun(w, r, func(ctx context.Context, j *jev.Judge) (any, error) {
		g, err := j.GoalState(ctx, jev.GoalInput{Goal: string(b.Goal), Status: string(b.Status), Context: string(b.Context)})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "advisory": true, "result": g}, nil
	})
}

func (s *Server) handleJevApprovalRisk(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Action  flexText `json:"action"`
		Context flexText `json:"context"`
	}
	if !s.jevEnabled() {
		jevDisabled(w)
		return
	}
	if !jevDecode(w, r, &b) {
		return
	}
	s.jevRun(w, r, func(ctx context.Context, j *jev.Judge) (any, error) {
		a, err := j.ApprovalRisk(ctx, jev.ApprovalInput{Action: string(b.Action), Context: string(b.Context)})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "advisory": true, "result": a}, nil
	})
}
