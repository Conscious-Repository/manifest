package server

// Benjamin's side of Liber (plan §5.4): Olga's conversations about shared
// Home tasks are saved beside the shared Home plan (system/home/chat). They
// list in his CHAT as task rows — filed under the Home project like every
// Home task row — and the task's stage shows them read-only, with the model
// behind each answer (he sees the machinery; she never does).

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"manifest/olgachat"
)

func (s *Server) liberStore() *olgachat.Store {
	if s.homePlan == nil {
		return nil
	}
	return &olgachat.Store{Shared: filepath.Join(filepath.Dir(s.homePlan.Path), "chat"), Private: os.DevNull}
}

// liberThreads indexes Olga's shared Liber conversations by task id.
func (s *Server) liberThreads() map[string]*olgachat.Thread {
	st := s.liberStore()
	out := map[string]*olgachat.Thread{}
	if st == nil {
		return out
	}
	for _, t := range st.List() {
		if t.Kind == olgachat.KindTask && t.Shared && t.TaskID != "" {
			out[t.TaskID] = t
		}
	}
	return out
}

// liberView is the read-only projection for his task stage.
func liberView(t *olgachat.Thread) map[string]any {
	type turn struct {
		Who    string          `json:"who"`
		Text   string          `json:"text"`
		At     time.Time       `json:"at"`
		Model  string          `json:"model,omitempty"`
		Images []string        `json:"images,omitempty"`
		Cards  []olgachat.Card `json:"cards,omitempty"`
	}
	var turns []turn
	for _, tu := range t.Turns {
		if tu.Text == "" && len(tu.Cards) == 0 && len(tu.Images) == 0 {
			continue
		}
		row := turn{Who: tu.Who, Text: tu.Text, At: tu.At, Cards: tu.Cards, Images: tu.Images}
		if t.Server != nil {
			row.Model = t.Server.TurnModels[tu.ID]
		}
		turns = append(turns, row)
	}
	return map[string]any{"updated": t.Updated, "turns": turns}
}

// handleLiberFile (GET /api/home/liber-file?id=) serves a photo Olga attached
// in a shared Home-task chat, for his read-only view.
func (s *Server) handleLiberFile(w http.ResponseWriter, r *http.Request) {
	st := s.liberStore()
	id := r.URL.Query().Get("id")
	p := ""
	if st != nil {
		p = st.ImagePath(id, true)
	}
	if p == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", olgachat.ImageMime(id))
	http.ServeFile(w, r, p)
}
