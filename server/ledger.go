package server

// The ledger read surface (P0 Phase 1): the daily view, the object-scoped
// event query, and one object's reconstructed history. Reads only — the
// writers stay where the state they mirror is written, and the ledger itself
// stays a log (ledger package doc).

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"manifest/ledger"
)

// ledgerEventsMax bounds an unbounded scan on the wire; limit= overrides,
// limit=0 lifts it.
const ledgerEventsMax = 500

// handleLedger serves one day's entries (default: today in the owner's tz).
func (s *Server) handleLedger(w http.ResponseWriter, r *http.Request) {
	if s.ledgerStore == nil {
		writeJSON(w, map[string]any{"date": "", "entries": []any{}, "days": []any{}})
		return
	}
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	if date == "" {
		date = s.ledgerStore.Today()
	}
	entries, err := s.ledgerStore.Day(date)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"date": date, "entries": entries, "days": s.ledgerStore.Days()})
}

// handleLedgerEvents — GET /api/ledger/events?kind=&source=&actor=&object=
// &objectKind=&since=&until=&limit=. Entries in file order; kind takes a
// comma list; since/until take RFC3339 or a bare date (since inclusive, until
// exclusive; a bare until date means "through that day" in the owner's tz).
func (s *Server) handleLedgerEvents(w http.ResponseWriter, r *http.Request) {
	if s.ledgerStore == nil {
		writeJSON(w, map[string]any{"entries": []any{}, "count": 0})
		return
	}
	q, err := ledgerQueryFromURL(r, s.ledgerStore.Loc())
	if err != nil {
		httpError(w, err)
		return
	}
	entries, err := s.ledgerStore.Events(q)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"entries": entries, "count": len(entries), "query": q})
}

// handleLedgerHistory — GET /api/ledger/history?object=<id>&objectKind=<kind>.
// One object's ordered history plus its resume shape (first/last/actors/kinds).
func (s *Server) handleLedgerHistory(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("object"))
	if id == "" {
		httpError(w, errBadRequest("object is required"))
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("objectKind"))
	if s.ledgerStore == nil {
		writeJSON(w, ledger.History{Object: ledger.Object{Kind: kind, ID: id}, Entries: []ledger.Entry{}, Actors: []string{}, Kinds: []string{}})
		return
	}
	h, err := s.ledgerStore.History(kind, id)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	writeJSON(w, h)
}

// ledgerQueryFromURL maps the wire params onto a ledger.Query; bare dates
// read in loc, the owner's calendar.
func ledgerQueryFromURL(r *http.Request, loc *time.Location) (ledger.Query, error) {
	v := r.URL.Query()
	q := ledger.Query{
		Source:     strings.TrimSpace(v.Get("source")),
		Actor:      strings.TrimSpace(v.Get("actor")),
		Object:     strings.TrimSpace(v.Get("object")),
		ObjectKind: strings.TrimSpace(v.Get("objectKind")),
		Limit:      ledgerEventsMax,
	}
	for _, k := range strings.Split(v.Get("kind"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			q.Kinds = append(q.Kinds, k)
		}
	}
	var err error
	if q.Since, err = parseLedgerTime(v.Get("since"), loc, false); err != nil {
		return q, errBadRequest("since: " + err.Error())
	}
	if q.Until, err = parseLedgerTime(v.Get("until"), loc, true); err != nil {
		return q, errBadRequest("until: " + err.Error())
	}
	if raw := strings.TrimSpace(v.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return q, errBadRequest("limit must be a non-negative integer")
		}
		q.Limit = n
	}
	return q, nil
}

// parseLedgerTime accepts RFC3339 or YYYY-MM-DD. A bare date is midnight in
// loc (the owner's tz); endOfDay bumps it to the next midnight so an until=
// date is inclusive of that day.
func parseLedgerTime(raw string, loc *time.Location, endOfDay bool) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	if loc == nil {
		loc = time.Local
	}
	t, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}
