package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/goals"
	"manifest/vault"
)

func goalsServer(t *testing.T, goalsMD string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	idx, err := vault.NewIndex(vault.Config{Root: dir, GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "goals.md"), []byte(goalsMD), 0o644); err != nil {
		t.Fatal(err)
	}
	return New(nil, goals.NewStore(idx, dir, "goals.md"), nil), dir
}

// getView drives handleGoalsGet and returns the parsed DocView.
func getView(t *testing.T, s *Server) goals.DocView {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleGoalsGet(rec, httptest.NewRequest(http.MethodGet, "/api/goals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("goals GET: %d %s", rec.Code, rec.Body.String())
	}
	var v goals.DocView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func findRock(v goals.DocView, area, id string) *goals.GoalView {
	for ai := range v.Areas {
		if v.Areas[ai].Name != area {
			continue
		}
		for ri := range v.Areas[ai].Rocks {
			if v.Areas[ai].Rocks[ri].ID == id {
				return &v.Areas[ai].Rocks[ri]
			}
		}
	}
	return nil
}

func TestGoalsRollupMovedAndClose(t *testing.T) {
	md := "# Goals\n\n## Aion\n\n### 1-year — 2026\n- [ ] Series A closed [goal:: aion/2026]\n\n### Rocks (90-day)\n" +
		"- [ ] Series A 15M [goal:: aion/series-a-15m] [quarter:: 2026-Q3] [serves:: aion/2026]\n" +
		"    - [ ] Term sheet\n" +
		"        - [ ] Send deck\n"
	s, _ := goalsServer(t, md)

	// Roll-up: one active Rock serves the annual.
	if a := getView(t, s).Areas[0].Annuals[0]; a.RollupActive != 1 || a.RollupWon != 0 {
		t.Fatalf("initial rollup wrong: active=%d won=%d", a.RollupActive, a.RollupWon)
	}

	// Ticking a task stamps the Rock's last movement.
	rec := httptest.NewRecorder()
	s.handleGoalCheck(rec, httptest.NewRequest(http.MethodPost, "/api/goals/check",
		strings.NewReader(`{"id":"aion/series-a-15m/term-sheet/send-deck","checked":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("check: %d %s", rec.Code, rec.Body.String())
	}
	if r := findRock(getView(t, s), "Aion", "aion/series-a-15m"); r == nil || r.Moved == "" {
		t.Fatalf("moved not stamped after a task check: %+v", r)
	}

	// Closing the Rock Won archives it → roll-up flips to won.
	rec = httptest.NewRecorder()
	s.handleGoalClose(rec, httptest.NewRequest(http.MethodPost, "/api/goals/close",
		strings.NewReader(`{"id":"aion/series-a-15m","outcome":"win"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("close: %d %s", rec.Code, rec.Body.String())
	}
	v := getView(t, s)
	if findRock(v, "Aion", "aion/series-a-15m") != nil {
		t.Fatal("closed Rock still in goals.md view")
	}
	if a := v.Areas[0].Annuals[0]; a.RollupActive != 0 || a.RollupWon != 1 {
		t.Fatalf("post-close rollup wrong: active=%d won=%d", a.RollupActive, a.RollupWon)
	}

	// The History endpoint reports the closed Rock with a win rate.
	rec = httptest.NewRecorder()
	s.handleGoalsArchives(rec, httptest.NewRequest(http.MethodGet, "/api/goals/archives", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Series A 15M") || !strings.Contains(rec.Body.String(), `"winRate":1`) {
		t.Fatalf("archives endpoint wrong: %d %s", rec.Code, rec.Body.String())
	}
}
