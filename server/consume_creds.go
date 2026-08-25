package server

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"manifest/consume"
	"manifest/portals"
)

// Site sign-ins — the session cookies that let the reader fetch publications
// the owner pays for.
//
// ⚠ Heavier than every other credential here. An API key is scoped to an API;
// a session cookie IS the owner logged in. Two rules govern this file:
// the value never appears in a response (rows carry a mask), and the value
// never appears in a log line or an error. The consume package enforces the
// rest — never in the vault, never across a host boundary.
//
// Managed from two places by owner decision (2026-08-25): the CONSUME
// subscription row, where the need becomes visible, and the PORTALS panel,
// where every other credential in the app is managed.

const consumeSitePrefix = "site:" // portal-row id namespace, e.g. site:substack.com

// consumeSiteRows renders one PORTALS row per stored sign-in.
func (s *Server) consumeSiteRows() []panelRow {
	if s.consume == nil {
		return nil
	}
	// How many subscriptions each credential actually unlocks — the number
	// that makes "one paste covers all of them" legible.
	feeds := map[string]int{}
	for _, st := range s.consume.Statuses() {
		if st.Site != "" {
			feeds[st.Site]++
		}
	}
	rows := []panelRow{}
	for _, site := range s.consume.Sites().Sites() {
		row := panelRow{
			ID: consumeSitePrefix + site.Host, Name: site.Host + " (reading)", Kind: "apikey",
			State: "open", Masked: site.Masked,
			Fields: []portals.CredField{{
				Key: "cookie", Label: "session cookie", Secret: true,
				Hint: "substack.sid=… from your browser's cookies for this site",
			}},
		}
		row.Note = "unlocks " + plural(feeds[site.Host], "1 feed", " feeds")
		if site.Added != "" {
			row.LastCrossing = site.Added
		}
		if site.FromEnv {
			row.Note = "from " + strings.ToUpper("MANIFEST_CONSUME_COOKIE") + " env"
		}
		if site.Expired {
			row.State = "degraded"
			row.Err = "sign-in expired — paid posts are previews again; paste a fresh cookie"
		}
		rows = append(rows, row)
	}
	return rows
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return strconv.Itoa(n) + many
}

// handleConsumeSiteKey stores a sign-in for one site.
//
// ⚠ The response is re-derived from the store, never reflected from the
// request — the portalRowView discipline. Nothing here logs the body.
func (s *Server) handleConsumeSiteKey(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	var b struct {
		Host   string            `json:"host"`
		Cookie string            `json:"cookie"`
		Fields map[string]string `json:"fields"` // the PORTALS form shape
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	host := strings.TrimSpace(b.Host)
	if host == "" {
		host = strings.TrimPrefix(r.PathValue("id"), consumeSitePrefix)
	}
	cookie := strings.TrimSpace(b.Cookie)
	if cookie == "" {
		cookie = strings.TrimSpace(b.Fields["cookie"])
	}
	if host == "" {
		httpError(w, errBadRequest("which site is this sign-in for?"))
		return
	}
	if cookie == "" {
		httpError(w, errBadRequest("paste the session cookie"))
		return
	}
	if err := s.consume.Sites().Set(host, cookie); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "host": host, "sites": s.consume.Sites().Sites()})
}

// handleConsumeSiteClear removes a sign-in.
func (s *Server) handleConsumeSiteClear(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		http.NotFound(w, r)
		return
	}
	host := strings.TrimPrefix(r.PathValue("id"), consumeSitePrefix)
	if host == "" {
		httpError(w, errBadRequest("which site?"))
		return
	}
	if v := strings.TrimSpace(os.Getenv("MANIFEST_CONSUME_COOKIE_" + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(host)))); v != "" {
		httpError(w, errBadRequest("that sign-in comes from the environment — unset it there"))
		return
	}
	s.consume.Sites().Clear(host)
	writeJSON(w, map[string]any{"ok": true, "sites": s.consume.Sites().Sites()})
}

// handleConsumeSites lists the stored sign-ins (masked).
func (s *Server) handleConsumeSites(w http.ResponseWriter, r *http.Request) {
	if s.consume == nil {
		writeJSON(w, map[string]any{"sites": []any{}})
		return
	}
	writeJSON(w, map[string]any{"sites": s.consume.Sites().Sites()})
}

var _ = consume.SiteKey // the package is the source of the key rule
