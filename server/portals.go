package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"manifest/errands"
	"manifest/portals"
)

// PORTALS — the panel that lists every external realm the app touches and lets
// each be (re)connected right here: an api-key portal takes a pasted key, an
// oauth portal (calendar) runs its sign-in, and the engine's LLM conduits show
// read-only. The polled source portals (clickup, benchling) live in the portals
// service; calendar and the LLM rows are composed in from where they already
// live, so the panel is the one place a connection is seen and repaired.

// panelRow is the generic row the frontend renders — fields drive the connect
// form, actions drive the buttons. One shape spans api-key, oauth, and llm rows.
type panelRow struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Kind         string              `json:"kind"`  // apikey | oauth | llm
	State        string              `json:"state"` // open | degraded | sealed | dormant
	Err          string              `json:"err,omitempty"`
	Masked       string              `json:"masked,omitempty"`       // key column ("····k7q2" | "oauth" | "engine")
	LastCrossing string              `json:"lastCrossing,omitempty"` // RFC3339 (formatted client-side)
	Note         string              `json:"note,omitempty"`         // non-time crossing text ("via engine")
	Fields       []portals.CredField `json:"fields,omitempty"`       // api-key connect/replace form
	Have         []string            `json:"have,omitempty"`         // keys currently set (names only)
	Accounts     []string            `json:"accounts,omitempty"`     // oauth: connected identities
	Engine       bool                `json:"engine,omitempty"`       // engine-managed apikey (heypocket): no manifest poll
}

func (s *Server) UsePortals(p *portals.Service) { s.portals = p }

// handlePortals assembles the full panel: source portals (from the service),
// the calendar row (from s.cal), the discovered LLM rows (from the spirit
// cornerstones), and docusign last (dormant).
func (s *Server) handlePortals(w http.ResponseWriter, r *http.Request) {
	rows := []panelRow{}
	var dormant []panelRow
	if s.portals != nil {
		for _, pr := range s.portals.Rows() {
			row := panelRow{
				ID: pr.ID, Name: pr.Name, Kind: string(pr.Kind), State: string(pr.State),
				Err: pr.Err, Masked: pr.Masked, LastCrossing: pr.LastCrossing,
				Fields: pr.Fields, Have: pr.Have,
			}
			if pr.State == portals.StateDormant {
				row.Note = "not connected — v2"
				dormant = append(dormant, row)
				continue
			}
			rows = append(rows, row)
		}
	}
	rows = append(rows, s.calendarPortalRow())
	rows = append(rows, s.gmailPortalRow())
	rows = append(rows, s.heypocketPortalRow())
	rows = append(rows, s.llmPortalRows()...)
	rows = append(rows, s.asidePortalRow())
	rows = append(rows, dormant...) // docusign at the bottom
	writeJSON(w, map[string]any{"rows": rows})
}

// asidePortalRow surfaces the Aside browser as the first EFFECTOR portal
// (errands-aside §1): no credentials ever (Aside owns its own auth). State:
// sealed = CLI missing (install hint); degraded = the LAST errand failed
// (until the next success); open = CLI present. Last crossing = the last
// finished errand.
func (s *Server) asidePortalRow() panelRow {
	row := panelRow{ID: "aside", Name: "Aside (errands)", Kind: string(portals.KindEffector), Masked: "no creds — aside owns auth"}
	if _, err := exec.LookPath("aside"); err != nil {
		row.State, row.Note = "sealed", "aside CLI not installed — install the Aside app + CLI, then errands run"
		return row
	}
	row.State = "open"
	if accs, err := errands.Accounts(); err == nil { // §1: the row shows the real account list
		for _, a := range accs {
			row.Accounts = append(row.Accounts, a.ID+" — "+a.Label)
		}
	}
	if s.errands != nil {
		for _, r := range s.errands.List() { // newest first: first terminal state decides
			switch r.Status {
			case errands.StatusDone:
				if row.LastCrossing == "" {
					row.LastCrossing = r.Finished
				}
				return row
			case errands.StatusFailed:
				row.State, row.Err = "degraded", "last errand failed — "+r.Outcome
				row.LastCrossing = r.Finished
				return row
			}
		}
	}
	row.Note = "＋ errand in the FEED header composes one"
	return row
}

// calendarPortalRow surfaces the existing Google Calendar connection as a portal
// row without duplicating its store — its connect/disconnect stays the calendar
// API, which the frontend row wires to.
func (s *Server) calendarPortalRow() panelRow {
	row := panelRow{ID: "google-calendar", Name: "Google Calendar", Kind: "oauth", Masked: "oauth"}
	switch {
	case s.cal != nil && s.cal.Enabled():
		row.State = "open"
		row.Accounts = s.cal.Accounts()
		row.LastCrossing = time.Now().UTC().Format(time.RFC3339)
	case s.cal != nil && s.cal.NeedsAuth():
		row.State, row.Note = "sealed", "credentials found — sign in"
	default:
		row.State, row.Note = "sealed", "add credentials, then sign in"
	}
	return row
}

var cornerstonePortalRe = regexp.MustCompile(`(?m)^portal::\s*(\S+)`)
var slugRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// llmPortalRows discovers the engine's LLM conduits from the spirit cornerstones
// (portal:: <name>) — never hardcoded, so a new spirit portal appears without a
// code change. They are read-only here (the engine owns their keys); state is
// informational ("via engine").
func (s *Server) llmPortalRows() []panelRow {
	if s.spirits == nil {
		return nil
	}
	found := map[string]bool{}
	matches, _ := filepath.Glob(filepath.Join(s.spirits.Root(), "spirits", "*", "cornerstone.md"))
	for _, f := range matches {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range cornerstonePortalRe.FindAllStringSubmatch(string(b), -1) {
			id := strings.TrimSpace(m[1])
			if slugRe.MatchString(id) {
				found[id] = true
			}
		}
	}
	var ids []string
	for id := range found {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows := make([]panelRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, panelRow{
			ID: id, Name: id, Kind: "llm", State: "open", Masked: "engine", Note: "via engine",
		})
	}
	return rows
}

// portalService guards handlers that need the service.
func (s *Server) portalService(w http.ResponseWriter) (*portals.Service, bool) {
	if s.portals == nil {
		http.Error(w, "portals disabled", http.StatusServiceUnavailable)
		return nil, false
	}
	return s.portals, true
}

// handlePortalKey sets/replaces an api-key portal's credentials (paste → save →
// auto-test). The key is written 0600 and never echoed back.
func (s *Server) handlePortalKey(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == heypocketID {
		s.handleHeypocketKey(w, r)
		return
	}
	svc, ok := s.portalService(w)
	if !ok {
		return
	}
	var b struct {
		Fields map[string]string `json:"fields"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	row, err := svc.SetCreds(ctx, r.PathValue("id"), b.Fields)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.portalRowView(row))
}

func (s *Server) handlePortalTest(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == heypocketID {
		s.handleHeypocketTest(w, r)
		return
	}
	svc, ok := s.portalService(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	row, err := svc.Test(ctx, r.PathValue("id"))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.portalRowView(row))
}

func (s *Server) handlePortalPoll(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == heypocketID {
		// engine-synced — nothing for manifest to poll; just refresh the row
		writeJSON(w, s.heypocketPortalRow())
		return
	}
	svc, ok := s.portalService(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	row, err := svc.PollNow(ctx, r.PathValue("id"))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.portalRowView(row))
}

func (s *Server) handlePortalDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == heypocketID {
		s.handleHeypocketDisconnect(w, r)
		return
	}
	svc, ok := s.portalService(w)
	if !ok {
		return
	}
	row, err := svc.Disconnect(r.PathValue("id"))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.portalRowView(row))
}

// portalRowView adapts a service Row into the panel shape for single-row replies.
func (s *Server) portalRowView(pr portals.Row) panelRow {
	row := panelRow{
		ID: pr.ID, Name: pr.Name, Kind: string(pr.Kind), State: string(pr.State),
		Err: pr.Err, Masked: pr.Masked, LastCrossing: pr.LastCrossing,
		Fields: pr.Fields, Have: pr.Have,
	}
	if pr.State == portals.StateDormant {
		row.Note = "not connected — v2"
	}
	return row
}

// ---- portal feed items (the third card kind) ----

func (s *Server) handlePortalDismiss(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.portalService(w)
	if !ok {
		return
	}
	var b struct {
		ID string `json:"id"`
	}
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	svc.Dismiss(b.ID)
	writeJSON(w, map[string]bool{"ok": true})
}

// portalCards is the feed's portal-item slice (empty when portals disabled).
func (s *Server) portalCards() []portals.Card {
	if s.portals == nil {
		return []portals.Card{}
	}
	return s.portals.Cards()
}

// portalInboxCount feeds the badge (0 when disabled).
func (s *Server) portalInboxCount() int {
	if s.portals == nil {
		return 0
	}
	return s.portals.InboxCount()
}
