package server

import (
	"encoding/json"
	"net/http"

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
	writeJSON(w, s.goals.Load().View())
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
			Area     string `json:"area"`     // for a 90-day root
			ParentID string `json:"parentId"` // for a 30-day under a 90-day, or a task under a 30-day
			Text     string `json:"text"`
			Owner    string `json:"owner"`
			Due      string `json:"due"`
		}
		if err := decode(r, &b); err != nil {
			httpError(w, err)
			return
		}
		s.mutate(w, func(d *goals.Doc) bool {
			_, ok := d.AddGoal(b.Area, b.ParentID, b.Text, b.Owner, b.Due)
			return ok
		})
	case http.MethodPatch:
		var b struct {
			ID    string  `json:"id"`
			Text  *string `json:"text"`
			Owner *string `json:"owner"`
			Due   *string `json:"due"`
		}
		if err := decode(r, &b); err != nil {
			httpError(w, err)
			return
		}
		s.mutate(w, func(d *goals.Doc) bool {
			return d.EditGoal(b.ID, goals.GoalEdit{Text: b.Text, Owner: b.Owner, Due: b.Due})
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
	s.mutate(w, func(d *goals.Doc) bool { return d.CheckGoal(b.ID, b.Checked) })
}

func (s *Server) handleGoalsReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var b struct {
		Area     string   `json:"area"`     // when reordering an area's 90-day roots
		ParentID string   `json:"parentId"` // when reordering a goal's children
		IDs      []string `json:"ids"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	s.mutate(w, func(d *goals.Doc) bool { return d.ReorderGoals(b.Area, b.ParentID, b.IDs) })
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
