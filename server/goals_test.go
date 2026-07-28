package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/approvals"
	"manifest/daily"
	"manifest/goals"
	"manifest/todos"
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
	return New(nil, goals.NewStore(idx, dir, "goals.md", testWrite), nil), dir
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

	// task-substrate split: depth-2 lines are frozen — checking a STAGE stamps
	// the Rock's last movement (task ids no longer resolve in goals).
	rec := httptest.NewRecorder()
	s.handleGoalCheck(rec, httptest.NewRequest(http.MethodPost, "/api/goals/check",
		strings.NewReader(`{"id":"aion/series-a-15m/term-sheet","checked":true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("check: %d %s", rec.Code, rec.Body.String())
	}
	if r := findRock(getView(t, s), "Aion", "aion/series-a-15m"); r == nil || r.Moved == "" {
		t.Fatalf("moved not stamped after a stage check: %+v", r)
	}

	// Closing the Rock Won archives it → roll-up flips to won.
	// A win with no evidence is a 400 (§5, UI and API agree).
	rec = httptest.NewRecorder()
	s.handleGoalClose(rec, httptest.NewRequest(http.MethodPost, "/api/goals/close",
		strings.NewReader(`{"id":"aion/series-a-15m","outcome":"win"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("win without evidence should 400, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.handleGoalClose(rec, httptest.NewRequest(http.MethodPost, "/api/goals/close",
		strings.NewReader(`{"id":"aion/series-a-15m","outcome":"win","evidence":"[[term sheet]]"}`)))
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

func TestSyncGoalTasksWriteBackAndMiss(t *testing.T) {
	md := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n- [ ] Rock [goal:: aion/rock]\n" +
		"    - [ ] Stage\n        - [ ] Task [goal:: aion/rock/stage/task]\n"
	s, _ := goalsServer(t, md)
	s.UseApprovals(approvals.NewStore(t.TempDir()))

	s.syncGoalTasks([]daily.Task{
		{Text: "Stage", Done: true, GoalID: "aion/rock/stage"},
		{Text: "Task", Done: true, GoalID: "aion/rock/stage/task"}, // frozen post-split → miss
		{Text: "Ghost task", Done: true, GoalID: "aion/removed"},
	})

	// The stage-linked tick is checked in goals.md; the legacy TASK tick is a
	// no-op (task depth is frozen history now) and lands an inbox note.
	if r := findRock(getView(t, s), "Aion", "aion/rock"); r == nil || !r.Children[0].Checked {
		t.Fatalf("linked stage not checked via write-back: %+v", r)
	}
	pend := s.approvals.List("pending")
	if len(pend) != 2 {
		t.Fatalf("expected 2 miss notes (legacy task + removed), got %+v", pend)
	}
	joined := pend[0].Body + pend[1].Body
	if !strings.Contains(joined, "aion/removed") || !strings.Contains(joined, "aion/rock/stage/task") {
		t.Fatalf("miss notes wrong: %s", joined)
	}
}

func TestHandleGoalCarry(t *testing.T) {
	md := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n- [ ] Rock [goal:: aion/rock] [quarter:: 2026-Q2]\n"
	s, _ := goalsServer(t, md)
	rec := httptest.NewRecorder()
	s.handleGoalCarry(rec, httptest.NewRequest(http.MethodPost, "/api/goals/carry", strings.NewReader(`{"id":"aion/rock"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("carry: %d %s", rec.Code, rec.Body.String())
	}
	r := findRock(getView(t, s), "Aion", "aion/rock")
	if r == nil || r.Quarter == "2026-Q2" {
		t.Fatalf("carry did not update the quarter: %+v", r)
	}
}

// TestDayCapture drives POST /api/day/capture end to end: a free-typed task lands
// under the focus slot's stage in goals.md (durable id) AND on the day, linked;
// a repeat POST is idempotent on both sides (goals-orient plan §1).
func TestDayCapture(t *testing.T) {
	dir := t.TempDir()
	idx, err := vault.NewIndex(vault.Config{Root: dir, GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	seed := "# Goals\n\n## Aion\n\n### Rocks (90-day)\n" +
		"- [ ] Rock [goal:: aion/rock] [quarter:: 2026-Q3]\n" +
		"    - [ ] Stage [goal:: aion/rock/stage]\n"
	if err := os.WriteFile(filepath.Join(dir, "goals.md"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := daily.NewService(daily.Config{VaultPath: dir, ScheduleStart: 8, ScheduleEnd: 18, Write: testWrite}, idx)
	s := New(svc, goals.NewStore(idx, dir, "goals.md", testWrite), nil)
	s.UseTodos(todos.NewStore(dir, "to do.md", testWrite))

	post := func() daily.Day {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/day/capture?date=2026-07-20",
			strings.NewReader(`{"stageId":"aion/rock/stage","text":"Lee sync"}`))
		s.handleDayCapture(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("capture: %d %s", rec.Code, rec.Body.String())
		}
		var d daily.Day
		if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}

	// task-substrate: the typed task lands in to do.md as a ROCK-TETHERED todo
	// (goals.md holds no tasks), seated on the day with [todo:: id].
	day := post()
	linked := 0
	for _, tk := range day.Tasks {
		if tk.TodoID == "aion/lee-sync" && tk.Text == "Lee sync" {
			linked++
		}
	}
	if linked != 1 {
		t.Fatalf("day tasks wrong: %+v", day.Tasks)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "to do.md"))
	if !strings.Contains(string(b), "Lee sync [todo:: aion/lee-sync] [rock:: aion/rock]") {
		t.Fatalf("to do.md missing captured todo:\n%s", b)
	}
	gm, _ := os.ReadFile(filepath.Join(dir, "goals.md"))
	if strings.Contains(string(gm), "Lee sync") {
		t.Fatalf("goals.md must not receive tasks:\n%s", gm)
	}
	if !strings.Contains(string(gm), "[moved:: ") {
		t.Fatalf("capture should stamp the Rock's moved::\n%s", gm)
	}

	// Idempotent: same POST → still one day task, one todo line.
	day = post()
	linked = 0
	for _, tk := range day.Tasks {
		if tk.TodoID == "aion/lee-sync" {
			linked++
		}
	}
	b, _ = os.ReadFile(filepath.Join(dir, "to do.md"))
	if linked != 1 || strings.Count(string(b), "Lee sync") != 1 {
		t.Fatalf("not idempotent: %d day tasks, %d todo lines", linked, strings.Count(string(b), "Lee sync"))
	}

	// Bad stage ids are refused.
	rec := httptest.NewRecorder()
	s.handleDayCapture(rec, httptest.NewRequest(http.MethodPost, "/api/day/capture?date=2026-07-20",
		strings.NewReader(`{"stageId":"aion/rock","text":"x"}`)))
	if rec.Code == http.StatusOK {
		t.Fatal("capture under a rock id must be refused")
	}
}

// testWrite is the tests' plain write path (prod injects a vaultwriter capability).
func testWrite(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
