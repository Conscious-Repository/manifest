// Package spirits is the dashboard's read side of the sibling excalibur
// harness tree (the summoner's console, plan excalibur-path-plan.md §2): the
// spirit feed, run reports, preserved prompts, engine heartbeat, and the
// run-now spool. The dashboard never invokes the engine — a run request is a
// file the engine picks up (§7.5). Feed status writes (keep/discard/snooze)
// are the user's own actions and reuse the feed store against the excalibur
// surface.
package spirits

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/feed"
	"manifest/mdfm"
)

// heartbeatFresh is how recent the engine.heartbeat mtime must be for the
// engine to count as alive (engine writes every scheduler tick, 30s).
const heartbeatFresh = 90 * time.Second

type Store struct {
	root string
	Feed *feed.Store
}

func NewStore(root string) *Store {
	return &Store{root: root, Feed: feed.NewStoreDir(filepath.Join(root, "artifacts", "feed"))}
}

// RunSummary is one run report's frontmatter, as the runs list renders it.
type RunSummary struct {
	ID           string  `json:"id"` // report filename stem
	Run          string  `json:"run"`
	Spirit       string  `json:"spirit"`
	Ritual       string  `json:"ritual"`
	Request      string  `json:"request"`
	Started      string  `json:"started"`
	Finished     string  `json:"finished"`
	Outcome      string  `json:"outcome"`
	Steps        int     `json:"steps"`
	ItemsWritten int     `json:"itemsWritten"`
	SpentUSD     float64 `json:"spentUsd"`
	CeilingUSD   float64 `json:"ceilingUsd"`
	Portal       string  `json:"portal"`
	Model        string  `json:"model"`
}

// Runs lists run reports newest-first (report filenames sort by date; ties
// broken by the started timestamp).
func (s *Store) Runs() []RunSummary {
	dir := filepath.Join(s.root, "artifacts", "runs")
	entries, _ := os.ReadDir(dir)
	var out []RunSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		sum, _, err := s.parseRun(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started > out[j].Started })
	return out
}

// Run loads one report (summary + markdown body) by filename stem.
func (s *Store) Run(id string) (RunSummary, string, bool) {
	if !validID(id) {
		return RunSummary{}, "", false
	}
	sum, body, err := s.parseRun(filepath.Join(s.root, "artifacts", "runs", id+".md"))
	if err != nil {
		return RunSummary{}, "", false
	}
	return sum, body, true
}

func (s *Store) parseRun(path string) (RunSummary, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return RunSummary{}, "", err
	}
	fm, body := mdfm.Split(string(b))
	num := func(k string) float64 { f, _ := strconv.ParseFloat(fm[k], 64); return f }
	n := func(k string) int { i, _ := strconv.Atoi(fm[k]); return i }
	return RunSummary{
		ID:           strings.TrimSuffix(filepath.Base(path), ".md"),
		Run:          fm["run"],
		Spirit:       fm["spirit"],
		Ritual:       fm["ritual"],
		Request:      fm["request"],
		Started:      fm["started"],
		Finished:     fm["finished"],
		Outcome:      fm["outcome"],
		Steps:        n("steps"),
		ItemsWritten: n("items_written"),
		SpentUSD:     num("charge_spent_usd"),
		CeilingUSD:   num("charge_ceiling_usd"),
		Portal:       fm["portal"],
		Model:        fm["model"],
	}, strings.TrimSpace(body), nil
}

// PromptTurn is one preserved turn of a run's exact assembled prompt.
type PromptTurn struct {
	Turn   int    `json:"turn"`
	System string `json:"system"`
	User   string `json:"user"`
}

// RunPrompts reads vessel/state/<spirit>/prompts/<run>/turn-NN-{system,user}.md.
func (s *Store) RunPrompts(spirit, run string) ([]PromptTurn, error) {
	if !validID(spirit) || !validID(run) {
		return nil, fmt.Errorf("bad id")
	}
	dir := filepath.Join(s.root, "vessel", "state", spirit, "prompts", run)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	byTurn := map[int]*PromptTurn{}
	for _, e := range entries {
		name := e.Name()
		var turn int
		var kind string
		if _, err := fmt.Sscanf(name, "turn-%d-system.md", &turn); err == nil {
			kind = "system"
		} else if _, err := fmt.Sscanf(name, "turn-%d-user.md", &turn); err == nil {
			kind = "user"
		} else {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		pt := byTurn[turn]
		if pt == nil {
			pt = &PromptTurn{Turn: turn}
			byTurn[turn] = pt
		}
		if kind == "system" {
			pt.System = string(b)
		} else {
			pt.User = string(b)
		}
	}
	var out []PromptTurn
	for _, pt := range byTurn {
		out = append(out, *pt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Turn < out[j].Turn })
	return out, nil
}

// maxRequestChars bounds a spooled request (mirrors the engine cap).
const maxRequestChars = 4000

// SpoolRunNow drops a run request for the engine to pick up (never a direct
// invocation). Mirrors the engine's scheduler.SpoolRequest shape. request is
// the summoner's free-form ask for on-demand spirits (options-scout); empty
// for a plain run-now.
func (s *Store) SpoolRunNow(spirit, ritual, request string) error {
	if !validID(spirit) || !validID(ritual) {
		return fmt.Errorf("bad spirit/ritual name")
	}
	if len(request) > maxRequestChars {
		request = request[:maxRequestChars]
	}
	dir := filepath.Join(s.root, "vessel", "spool")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	payload := map[string]string{
		"spirit":       spirit,
		"ritual":       ritual,
		"source":       "dashboard",
		"requested_at": time.Now().Format(time.RFC3339),
	}
	if strings.TrimSpace(request) != "" {
		payload["request"] = request
	}
	req, _ := json.Marshal(payload)
	name := fmt.Sprintf("%d-%s-%s.json", time.Now().UnixNano(), spirit, ritual)
	return os.WriteFile(filepath.Join(dir, name), req, 0o644)
}

// EngineAlive reports whether the engine heartbeat is fresh, and its mtime.
func (s *Store) EngineAlive() (bool, time.Time) {
	fi, err := os.Stat(filepath.Join(s.root, "vessel", "state", "engine.heartbeat"))
	if err != nil {
		return false, time.Time{}
	}
	return time.Since(fi.ModTime()) < heartbeatFresh, fi.ModTime()
}

// Spirits lists spirit names (for the run-now picker); each with its rituals.
func (s *Store) Spirits() map[string][]string {
	out := map[string][]string{}
	entries, _ := os.ReadDir(filepath.Join(s.root, "spirits"))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var rituals []string
		rits, _ := os.ReadDir(filepath.Join(s.root, "spirits", e.Name(), "rituals"))
		for _, r := range rits {
			if !r.IsDir() && strings.HasSuffix(r.Name(), ".md") {
				rituals = append(rituals, strings.TrimSuffix(r.Name(), ".md"))
			}
		}
		out[e.Name()] = rituals
	}
	return out
}

// validID rejects anything that could traverse paths — ids here are filename
// stems and spirit/ritual dir names.
func validID(s string) bool {
	if s == "" || strings.ContainsAny(s, "/\\") || strings.Contains(s, "..") {
		return false
	}
	return true
}
