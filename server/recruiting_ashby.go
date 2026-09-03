package server

import (
	"context"
	"net/http"
	"time"

	"manifest/recruiting"
)

// The PUBLIC Ashby job-board mirror (plan §4.7, Phase 2): one user-actioned
// route that reads the no-key posting API and mirrors it onto the role
// records. It lives inside server.go's `if s.recruiting != nil` block beside
// the other recruiting routes and, like them, is never mounted on the portal
// listener. There is no poller, no key, and no write toward Ashby here; the
// private client is Phase 6.

// UseAshbyPublic swaps the public job-board client (tests bind it to an
// httptest server). Absent, the handler reads the real AION board.
func (s *Server) UseAshbyPublic(c *recruiting.AshbyPublic) { s.ashbyPublic = c }

func (s *Server) ashbyPublicClient() *recruiting.AshbyPublic {
	if s.ashbyPublic == nil {
		s.ashbyPublic = recruiting.NewAshbyPublic("", nil)
	}
	return s.ashbyPublic
}

// POST /api/aion/recruiting/roles/sync — fetch the public board, mirror the
// Ashby-owned fields onto roles/<slug>.md, and answer with what moved plus
// the fresh board view. An upstream failure is a 502 that names the failure;
// nothing is written on that path.
func (s *Server) handleRecruitingRolesSync(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	postings, err := s.ashbyPublicClient().Fetch(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	res, err := s.recruiting.SyncAshbyPostings(postings, time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"sync": res, "view": s.recruiting.View()})
}
