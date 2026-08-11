package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/portals"
	"manifest/signals"
)

// DeepSeek-local (big-change Phase 5a): the lab's OpenAI-compatible endpoint
// (DeepSeek V4 Flash on the Sparks) as a portal row manifest can actually TEST
// — GET <base>/models needs no key — so its state is honest open/degraded
// rather than "via engine" faith. The row reports the DISCOVERED model id
// (the notes and reality have disagreed before; never hardcode it). A
// degraded row is the "tell RJ" indicator — the serving stack is his; a
// careless restart can OOM the Sparks. No poller, no cards, no LLM in
// manifest's loop (portals doctrine intact); the ENGINE consumes the same
// URL file as its conduit base (Phase 5b).

const deepseekID = "deepseek-local"

// deepseekURLPath is the one source of truth per machine for the endpoint —
// manifest tests it, the engine's conduit reads it. LAB_MODEL_URL env wins.
func deepseekURLPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "excalibur", "lab_model_url")
}

func deepseekBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("LAB_MODEL_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	b, _ := os.ReadFile(deepseekURLPath())
	return strings.TrimRight(strings.TrimSpace(string(b)), "/")
}

// deepseekPortalRow assembles the row from the URL file alone — state beyond
// sealed is decided by an explicit test (no background pings, per doctrine).
func (s *Server) deepseekPortalRow() panelRow {
	row := panelRow{
		ID: deepseekID, Name: "DeepSeek (lab)", Kind: "apikey", Engine: true,
		Fields: []portals.CredField{{
			Key: "baseUrl", Label: "Endpoint base URL", Secret: false,
			Hint: "http://192.168.87.11:8000/v1  (reachable from metis only)",
		}},
	}
	base := deepseekBaseURL()
	if base == "" {
		row.State = "sealed"
		row.Note = "set the lab endpoint URL — the engine's cheap always-on rituals route here (Phase 5b)"
		return row
	}
	row.State = "open" // provisional; the test button decides degraded
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		row.Masked = u.Host
	} else {
		row.Masked = base
	}
	if strings.TrimSpace(os.Getenv("LAB_MODEL_URL")) != "" {
		row.Note = "engine conduit · URL from LAB_MODEL_URL env"
	} else {
		row.Note = "engine conduit · URL at ~/.config/excalibur/lab_model_url"
	}
	return row
}

// handleDeepseekKey writes/replaces the endpoint URL (0600 like its siblings,
// though it is not a secret) and auto-tests it.
func (s *Server) handleDeepseekKey(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(os.Getenv("LAB_MODEL_URL")) != "" {
		httpError(w, errBadRequest("LAB_MODEL_URL is set in the environment — it overrides the file; unset it to manage the URL here"))
		return
	}
	var b struct {
		Fields map[string]string `json:"fields"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	base := strings.TrimSpace(b.Fields["baseUrl"])
	if base == "" {
		httpError(w, errBadRequest("baseUrl is required"))
		return
	}
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		httpError(w, errBadRequest("that doesn't look like a base URL (want http(s)://host:port/v1)"))
		return
	}
	if err := os.MkdirAll(filepath.Dir(deepseekURLPath()), 0o700); err != nil {
		httpError(w, err)
		return
	}
	if err := os.WriteFile(deepseekURLPath(), []byte(base+"\n"), 0o600); err != nil {
		httpError(w, err)
		return
	}
	s.handleDeepseekTest(w, r) // auto-test the freshly saved URL
}

// handleDeepseekTest GETs <base>/models and reports the DISCOVERED model.
// The result also lands in the dataDir state file the DegradedPortal signal
// reads (Phase 7) — a degraded test pages the feed until a later open one.
func (s *Server) handleDeepseekTest(w http.ResponseWriter, r *http.Request) {
	row := s.deepseekPortalRow()
	if row.State == "sealed" {
		writeJSON(w, row)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	id, ctxLen, err := deepseekModels(ctx, deepseekBaseURL())
	if err != nil {
		row.State = "degraded"
		row.Err = err.Error() + " — tell RJ (don't restart the serving stack; a careless restart can OOM the Sparks)"
	} else {
		row.State = "open"
		row.LastCrossing = time.Now().UTC().Format(time.RFC3339)
		note := "model: " + id
		if ctxLen > 0 {
			note += fmt.Sprintf(" · ctx %dk", ctxLen/1024)
		}
		row.Note = note + " · engine conduit"
	}
	s.writeDeepseekState(row.State, row.Err)
	writeJSON(w, row)
}

// writeDeepseekState records the last explicit test for the feed signal.
func (s *Server) writeDeepseekState(state, errMsg string) {
	if s.deepseekStatePath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.deepseekStatePath), 0o755)
	b, _ := json.Marshal(signals.PortalState{Portal: deepseekID, State: state, Err: errMsg, At: time.Now()})
	_ = os.WriteFile(s.deepseekStatePath, b, 0o644)
}

// UseDeepseekState points the test-state file (under dataDir, per-machine).
func (s *Server) UseDeepseekState(path string) { s.deepseekStatePath = path }

// handleDeepseekDisconnect removes the URL file.
func (s *Server) handleDeepseekDisconnect(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(os.Getenv("LAB_MODEL_URL")) != "" {
		httpError(w, errBadRequest("LAB_MODEL_URL is set in the environment — unset it there; nothing to remove here"))
		return
	}
	if err := os.Remove(deepseekURLPath()); err != nil && !os.IsNotExist(err) {
		httpError(w, err)
		return
	}
	writeJSON(w, s.deepseekPortalRow())
}

// deepseekModels lists the endpoint's models: first id + context length.
func deepseekModels(ctx context.Context, base string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("endpoint unreachable: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("HTTP %d from /models", resp.StatusCode)
	}
	var v struct {
		Data []struct {
			ID          string `json:"id"`
			MaxModelLen int    `json:"max_model_len"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &v); err != nil || len(v.Data) == 0 {
		return "", 0, fmt.Errorf("no models in the /models response")
	}
	return v.Data[0].ID, v.Data[0].MaxModelLen, nil
}
