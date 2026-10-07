package server

// Benjamin's side of Liber (plan §5.4): Olga's conversations about shared
// Home tasks are saved beside the shared Home plan (system/home/chat). They
// list in his CHAT as task rows — filed under the Home project like every
// Home task row — and the task's stage shows them read-only, with the model
// behind each answer (he sees the machinery; she never does).

import (
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
		Who   string          `json:"who"`
		Text  string          `json:"text"`
		At    time.Time       `json:"at"`
		Model string          `json:"model,omitempty"`
		Cards []olgachat.Card `json:"cards,omitempty"`
	}
	var turns []turn
	for _, tu := range t.Turns {
		if tu.Text == "" && len(tu.Cards) == 0 {
			continue
		}
		row := turn{Who: tu.Who, Text: tu.Text, At: tu.At, Cards: tu.Cards}
		if t.Server != nil {
			row.Model = t.Server.TurnModels[tu.ID]
		}
		turns = append(turns, row)
	}
	return map[string]any{"updated": t.Updated, "turns": turns}
}
