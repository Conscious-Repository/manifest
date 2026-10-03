package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"manifest/aion"
	"manifest/jev"
	"manifest/typesafe"
)

const jevTestKey = "ts-server-test-key-not-real"

// jevServer is a Server whose Jev calls go to a Fake (no network), with the
// environment key cleared so nothing can fall through to the live client.
func jevServer(t *testing.T, f *typesafe.Fake) (*Server, http.Handler) {
	t.Helper()
	t.Setenv(typesafe.EnvKey, "")
	s := &Server{}
	if f != nil {
		s.jevJudge = &jev.Judge{Eval: f}
	}
	return s, s.Handler()
}

func jevPost(t *testing.T, h http.Handler, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, rd))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func tierAnswers(choice string, conf float64, probs map[string]float64) map[string]typesafe.Answer {
	ans := map[string]typesafe.Answer{"tier": typesafe.ChoiceAnswer(choice, conf, probs)}
	for _, s := range jev.TierSignals {
		ans["signal_"+s.ID] = typesafe.NoulAnswer(0.01)
	}
	return ans
}

var jevPaths = []string{
	"/api/aion/transcripts/tier/advise",
	"/api/jev/clarify-gate",
	"/api/jev/evidence-check",
	"/api/jev/goal-state",
	"/api/jev/approval-risk",
}

func TestJevEndpointsDisabledWithoutKey(t *testing.T) {
	_, h := jevServer(t, nil)
	for _, p := range jevPaths {
		w, out := jevPost(t, h, p, map[string]any{"text": "x", "request": "x", "claim": "x", "status": "x", "action": "x"})
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: %d, want 503", p, w.Code)
		}
		if out["ok"] != false || out["disabled"] != true || out["error"] != "TYPESAFE_API_KEY is not set" {
			t.Errorf("%s: body %v", p, out)
		}
		if _, has := out["advice"]; has {
			t.Errorf("%s: a disabled endpoint returned advice", p)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/jev/status", nil))
	var st map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &st)
	if st["enabled"] != false || st["keySource"] != "" || st["model"] != typesafe.DefaultModel {
		t.Errorf("status = %v", st)
	}
}

// With the env key set and TYPESAFE_API_URL pointed at a local stub, the
// production client path runs end to end — and the key reaches only the
// Authorization header, never a response body.
func TestJevEnvKeyLiveClientPathNeverEchoesKey(t *testing.T) {
	var auth atomic.Value
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"detail":"bad key"}`)
	}))
	defer stub.Close()
	t.Setenv(typesafe.EnvKey, jevTestKey)
	t.Setenv(typesafe.EnvURL, stub.URL)
	h := (&Server{}).Handler()

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/jev/status", nil))
	if strings.Contains(w.Body.String(), jevTestKey) || !strings.Contains(w.Body.String(), `"keySource":"env"`) || !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Errorf("status = %s", w.Body.String())
	}
	w, out := jevPost(t, h, "/api/jev/clarify-gate", map[string]any{"request": "do it"})
	if w.Code != http.StatusBadGateway || out["ok"] != false || !strings.Contains(out["error"].(string), "401") {
		t.Errorf("upstream 401 → %d %v", w.Code, out)
	}
	if strings.Contains(w.Body.String(), jevTestKey) {
		t.Error("response echoes the key")
	}
	if auth.Load() != "Bearer "+jevTestKey {
		t.Errorf("Authorization = %v", auth.Load())
	}
}

func TestJevAionTierAdviseEndpoint(t *testing.T) {
	probs := map[string]float64{"open": 0.07, "internal": 0.88, "held": 0.05}
	f := &typesafe.Fake{Answers: tierAnswers("internal", 0.79, probs), Model: "jev-1.13.0"}
	_, h := jevServer(t, f)
	w, out := jevPost(t, h, "/api/aion/transcripts/tier/advise", map[string]any{"name": "2099-01-01 never mapped", "text": "Coil roadmap and Q3 plan.", "source": "granola"})
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if out["ok"] != true || out["advisory"] != true || out["applied"] != false {
		t.Errorf("envelope = %v", out)
	}
	adv := out["advice"].(map[string]any)
	if adv["tier"] != "internal" || adv["jevTier"] != "internal" || adv["confidence"] != 0.79 || adv["advisory"] != true ||
		adv["sourceOfTruth"] != "aion/tier-map.json" || adv["model"] != "jev-1.13.0" || adv["note"] != "2099-01-01 never mapped.md" {
		t.Errorf("advice = %v", adv)
	}
	if p := adv["probabilities"].(map[string]any); p["internal"] != 0.88 || p["held"] != 0.05 {
		t.Errorf("probabilities = %v", p)
	}
	m := out["map"].(map[string]any)
	if m["known"] != false || m["tier"] != "held" || m["portalEligible"] != false || m["kairosEligible"] != false || m["sourceOfTruth"] != "aion/tier-map.json" {
		t.Errorf("an unmapped note must stand held and ineligible: %v", m)
	}
	if st := f.Last().State.(map[string]any); st["transcript"] != "Coil roadmap and Q3 plan." || st["source"] != "granola" {
		t.Errorf("state = %v", st)
	}
}

// The advice is an opinion: confident "open" advice on an unmapped note and
// on a held note changes neither the data file nor any eligibility.
func TestJevAionTierAdviceNeverChangesTheMap(t *testing.T) {
	path := filepath.Join("..", aion.TierMapPath)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tm, err := aion.LoadTierMap()
	if err != nil {
		t.Fatal(err)
	}
	var heldName string
	for _, n := range tm.Names() {
		if e := tm[n]; e.Tier == aion.TierHeld {
			heldName = n
			break
		}
	}
	if heldName == "" {
		t.Fatal("tier map has no held note to test against")
	}
	f := &typesafe.Fake{Answers: tierAnswers("open", 0.99, map[string]float64{"open": 0.99, "internal": 0.01, "held": 0})}
	_, h := jevServer(t, f)
	const unmapped = "2099-12-31 not in the map.md"
	for _, name := range []string{unmapped, heldName} {
		w, out := jevPost(t, h, "/api/aion/transcripts/tier/advise", map[string]any{"name": name, "text": "general science"})
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
		if out["advice"].(map[string]any)["tier"] != "open" {
			t.Fatalf("fixture should advise open: %v", out["advice"])
		}
		m := out["map"].(map[string]any)
		if m["tier"] != "held" || m["portalEligible"] != false || m["kairosEligible"] != false {
			t.Errorf("%s: map standing moved with the advice: %v", name, m)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("aion/tier-map.json changed")
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Fatal("a tier-map write was attempted")
	}
	reloaded, err := aion.LoadTierMap()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.PortalEligible(unmapped) || reloaded.KairosEligible(unmapped) || reloaded.PortalEligible(heldName) || reloaded.KairosEligible(heldName) {
		t.Fatal("advice made a note eligible")
	}
	if len(reloaded) != len(tm) {
		t.Fatalf("map grew from %d to %d entries", len(tm), len(reloaded))
	}
}

func TestJevAionTierAdviseRefusesOversize(t *testing.T) {
	f := &typesafe.Fake{Answers: tierAnswers("held", 1, nil)}
	_, h := jevServer(t, f)
	// text over the judgment's limit but inside the body limit
	w, out := jevPost(t, h, "/api/aion/transcripts/tier/advise", map[string]any{"text": strings.Repeat("a", jev.MaxTierText+1)})
	if w.Code != http.StatusRequestEntityTooLarge || out["limit"] != float64(jev.MaxTierText) || !strings.Contains(out["error"].(string), "text exceeds 90000 bytes") {
		t.Errorf("oversize text → %d %v", w.Code, out)
	}
	// a body over the body limit
	w, out = jevPost(t, h, "/api/aion/transcripts/tier/advise", `{"text":"`+strings.Repeat("a", jevBodyLimit+10)+`"}`)
	if w.Code != http.StatusRequestEntityTooLarge || out["ok"] != false || !strings.Contains(out["error"].(string), "body exceeds") {
		t.Errorf("oversize body → %d %v", w.Code, out)
	}
	w, _ = jevPost(t, h, "/api/aion/transcripts/tier/advise", map[string]any{"text": ""})
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty text → %d", w.Code)
	}
	w, _ = jevPost(t, h, "/api/aion/transcripts/tier/advise", `{not json`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad JSON → %d", w.Code)
	}
	if len(f.Requests) != 0 {
		t.Errorf("refused requests reached Jev %d times", len(f.Requests))
	}
}

func TestJevClarifyGateEndpointInspectSourceFirst(t *testing.T) {
	f := &typesafe.Fake{Answers: map[string]typesafe.Answer{
		"gate":               typesafe.ChoiceAnswer("inspect_source_first", 0.7, map[string]float64{"act_now": 0.1, "ask_user": 0.1, "inspect_source_first": 0.78, "stop_due_to_risk": 0.02}),
		"risk":               typesafe.ScoreAnswer(1.1, 0.6, map[string]float64{"1": 0.9}, nil),
		"source_retrievable": typesafe.NoulAnswer(0.93),
		"continuation":       typesafe.NoulAnswer(0.4),
	}}
	_, h := jevServer(t, f)
	w, out := jevPost(t, h, "/api/jev/clarify-gate", map[string]any{
		"request": "fix the failing test", "context": map[string]any{"repo": "manifest", "branch": "main"}, "proposedDefault": "run the tests",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	r := out["result"].(map[string]any)
	if out["advisory"] != true || r["decision"] != "inspect_source_first" || r["confidence"] != 0.7 || r["sourceRetrievable"] != 0.93 {
		t.Errorf("result = %v", r)
	}
	if !strings.Contains(r["recommendation"].(string), "Look the missing detail up first") {
		t.Errorf("recommendation = %v", r["recommendation"])
	}
	if p := r["probabilities"].(map[string]any); p["inspect_source_first"] != 0.78 {
		t.Errorf("probabilities = %v", p)
	}
	if ctx := f.Last().State.(map[string]any)["context"]; ctx != `{"branch":"main","repo":"manifest"}` {
		t.Errorf("object context flattened to %v", ctx)
	}
}

func TestJevEvidenceGoalApprovalEndpoints(t *testing.T) {
	f := &typesafe.Fake{Answers: map[string]typesafe.Answer{
		"verdict":              typesafe.ChoiceAnswer("contradicted", 0.8, map[string]float64{"contradicted": 0.85}),
		"overclaims":           typesafe.NoulAnswer(0.9),
		"missing_verification": typesafe.NoulAnswer(0.2),
		"state":                typesafe.ChoiceAnswer("blocked_by_error", 0.75, map[string]float64{"blocked_by_error": 0.8}),
		"category":             typesafe.ChoiceAnswer("destructive_action", 0.88, map[string]float64{"destructive_action": 0.9}),
		"risk":                 typesafe.ScoreAnswer(3.8, 0.7, nil, nil),
		"irreversible":         typesafe.NoulAnswer(0.95),
	}}
	_, h := jevServer(t, f)
	w, out := jevPost(t, h, "/api/jev/evidence-check", map[string]any{"claim": "tests pass", "evidence": map[string]any{"exit": 1, "log": "FAIL TestX"}})
	if r := out["result"].(map[string]any); w.Code != 200 || r["verdict"] != "contradicted" || r["overclaims"] != 0.9 || r["advisory"] != true {
		t.Errorf("evidence → %d %v", w.Code, out)
	}
	w, out = jevPost(t, h, "/api/jev/goal-state", map[string]any{"goal": "ship jev", "status": "go vet failed"})
	if r := out["result"].(map[string]any); w.Code != 200 || r["state"] != "blocked_by_error" || r["kind"] != "goal.state" {
		t.Errorf("goal → %d %v", w.Code, out)
	}
	w, out = jevPost(t, h, "/api/jev/approval-risk", map[string]any{"action": "rm -rf data/"})
	r := out["result"].(map[string]any)
	if w.Code != 200 || r["category"] != "destructive_action" || r["irreversible"] != 0.95 || r["risk"].(map[string]any)["level"] != float64(4) {
		t.Errorf("approval → %d %v", w.Code, out)
	}
	for p, body := range map[string]string{
		"/api/jev/clarify-gate":   `{}`,
		"/api/jev/evidence-check": `{"evidence":"x"}`,
		"/api/jev/goal-state":     `{"goal":"x"}`,
		"/api/jev/approval-risk":  `{"context":"x"}`,
	} {
		if w, _ := jevPost(t, h, p, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s with no subject → %d", p, w.Code)
		}
	}
	if w, _ := jevPost(t, h, "/api/jev/approval-risk", map[string]any{"action": strings.Repeat("z", jev.MaxAgentInput+1)}); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize action → %d", w.Code)
	}
}

// The public team-portal listener builds its own mux: no Jev route — and so no
// way to send a transcript to an outside service — exists there.
func TestJevRoutesAbsentFromPortalMux(t *testing.T) {
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range append([]string{"/api/jev/status"}, jevPaths...) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{"text":"x"}`)))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s answered %d on the portal listener, want 404", p, w.Code)
		}
	}
}
