package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"manifest/goals"
)

// mutate loads goals.md, applies fn, saves, and responds with the full updated
// doc projection so the client always re-renders from server truth.
func (s *Server) mutate(w http.ResponseWriter, fn func(*goals.Doc) bool) {
	doc := s.goals.Load()
	if !fn(doc) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := s.goals.Save(doc); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, doc.View())
}

func (s *Server) handleGoalsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.goalsViewWithRollup())
}

// goalsViewWithRollup builds the goals view and fills each annual's roll-up counts from
// active serving Rocks (goals.md) + this year's archives (§2 — archives are required).
func (s *Server) goalsViewWithRollup() goals.DocView {
	view := s.goals.Load().View()
	fillRollup(&view, s.goals.LoadAllArchives(), time.Now().Format("2006"))
	return view
}

func fillRollup(view *goals.DocView, archives []goals.ArchiveQuarter, year string) {
	activeServ := map[string]int{}
	for _, a := range view.Areas {
		for _, rock := range a.Rocks {
			if rock.Serves != "" {
				activeServ[rock.Serves]++
			}
		}
	}
	won, learn := map[string]int{}, map[string]int{}
	for _, aq := range archives {
		if !strings.HasPrefix(aq.Quarter, year+"-") {
			continue
		}
		for _, e := range aq.Entries {
			switch {
			case e.Serves == "":
			case e.Outcome == "win":
				won[e.Serves]++
			case e.Outcome == "learn":
				learn[e.Serves]++
			}
		}
	}
	for ai := range view.Areas {
		for ni := range view.Areas[ai].Annuals {
			id := view.Areas[ai].Annuals[ni].ID
			view.Areas[ai].Annuals[ni].RollupActive = activeServ[id]
			view.Areas[ai].Annuals[ni].RollupWon = won[id]
			view.Areas[ai].Annuals[ni].RollupLearn = learn[id]
		}
	}
}

// handleGoalClose closes a Rock Win/Learn into the quarter archive (§6). The Rock leaves
// goals.md and is appended to goals <quarter>.md — the user's commit.
func (s *Server) handleGoalClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var b struct {
		ID      string `json:"id"`
		Outcome string `json:"outcome"` // "win" | "learn"
		Note    string `json:"note"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if err := s.goals.CloseGoal(b.ID, b.Outcome, b.Note, time.Now()); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.goalsViewWithRollup())
}

// handleGoalsArchives serves the History view: closed Rocks grouped by quarter with a
// win rate. Read-only.
func (s *Server) handleGoalsArchives(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var quarters []map[string]any
	for _, aq := range s.goals.LoadAllArchives() {
		wins, learns := 0, 0
		for _, e := range aq.Entries {
			switch e.Outcome {
			case "win":
				wins++
			case "learn":
				learns++
			}
		}
		rate := 0.0
		if wins+learns > 0 {
			rate = float64(wins) / float64(wins+learns)
		}
		quarters = append(quarters, map[string]any{
			"quarter": aq.Quarter, "wins": wins, "learns": learns,
			"winRate": rate, "entries": aq.Entries,
		})
	}
	writeJSON(w, map[string]any{"quarters": quarters})
}

func (s *Server) handleMyPlate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{"groups": s.goals.Load().MyPlate()})
}

func (s *Server) handleAreas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var b struct {
			Name string `json:"name"`
		}
		if err := decode(r, &b); err != nil {
			httpError(w, err)
			return
		}
		s.mutate(w, func(d *goals.Doc) bool { d.AddArea(b.Name); return true })
	case http.MethodPatch:
		var b struct {
			Name      string  `json:"name"`
			NewName   *string `json:"newName"`
			NorthStar *string `json:"northStar"`
		}
		if err := decode(r, &b); err != nil {
			httpError(w, err)
			return
		}
		s.mutate(w, func(d *goals.Doc) bool {
			ok := d.FindArea(b.Name) != nil
			if b.NorthStar != nil {
				ok = d.SetNorthStar(b.Name, *b.NorthStar) && ok
			}
			if b.NewName != nil {
				ok = d.RenameArea(b.Name, *b.NewName) && ok
			}
			return ok
		})
	case http.MethodDelete:
		var b struct {
			Name string `json:"name"`
		}
		if err := decode(r, &b); err != nil {
			httpError(w, err)
			return
		}
		s.mutate(w, func(d *goals.Doc) bool { return d.DeleteArea(b.Name) })
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAreasReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var b struct {
		Order []string `json:"order"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	s.mutate(w, func(d *goals.Doc) bool { d.ReorderAreas(b.Order); return true })
}

func (s *Server) handleGoalItem(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var b struct {
			Area     string `json:"area"`     // for a root (Rock or annual)
			ParentID string `json:"parentId"` // for a stage under a Rock, or a task under a stage
			Section  string `json:"section"`  // "annual" | "rock" (root only; default rock)
			Text     string `json:"text"`
			Owner    string `json:"owner"`
		}
		if err := decode(r, &b); err != nil {
			httpError(w, err)
			return
		}
		s.mutate(w, func(d *goals.Doc) bool {
			g, ok := d.AddGoal(b.Area, b.ParentID, b.Section, b.Text, b.Owner)
			if !ok {
				return false
			}
			if b.ParentID == "" && !strings.EqualFold(b.Section, "annual") {
				// A new Rock is stamped with the current quarter at creation (§1).
				g.Quarter = goals.CurrentQuarter(time.Now())
			} else if b.ParentID != "" {
				// Adding a stage/task advances the trail — stamp the Rock's last movement.
				if rock := d.RockOf(g.ID); rock != nil {
					rock.Moved = time.Now().Format("2006-01-02")
				}
			}
			return true
		})
	case http.MethodPatch:
		var b struct {
			ID      string  `json:"id"`
			Text    *string `json:"text"`
			Owner   *string `json:"owner"`
			Quarter *string `json:"quarter"`
			Serves  *string `json:"serves"`
			Status  *string `json:"status"`
		}
		if err := decode(r, &b); err != nil {
			httpError(w, err)
			return
		}
		s.mutate(w, func(d *goals.Doc) bool {
			return d.EditGoal(b.ID, goals.GoalEdit{Text: b.Text, Owner: b.Owner, Quarter: b.Quarter, Serves: b.Serves, Status: b.Status})
		})
	case http.MethodDelete:
		var b struct {
			ID string `json:"id"`
		}
		if err := decode(r, &b); err != nil {
			httpError(w, err)
			return
		}
		s.mutate(w, func(d *goals.Doc) bool { return d.DeleteGoal(b.ID) })
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGoalCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var b struct {
		ID      string `json:"id"`
		Checked bool   `json:"checked"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	s.mutate(w, func(d *goals.Doc) bool {
		if !d.CheckGoal(b.ID, b.Checked) {
			return false
		}
		// Ticking a task/stage is progress — stamp the enclosing Rock's last movement.
		if rock := d.RockOf(b.ID); rock != nil {
			rock.Moved = time.Now().Format("2006-01-02")
		}
		return true
	})
}

func (s *Server) handleGoalsReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var b struct {
		Area     string   `json:"area"`     // when reordering an area's roots
		ParentID string   `json:"parentId"` // when reordering a goal's children
		Section  string   `json:"section"`  // "annual" | "rock" (root reorder only)
		IDs      []string `json:"ids"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	s.mutate(w, func(d *goals.Doc) bool { return d.ReorderGoals(b.Area, b.ParentID, b.Section, b.IDs) })
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
