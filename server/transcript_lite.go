package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Snappy transcripts (2026-09-27). Measured on the owner's real chats over the
// tailnet: a long coding chat's transcript was 1.8 MB, of which tool results
// were 1.1 MB, inputs 0.27 MB and the replies he reads 0.1 MB; a chat with a
// planning timeline re-sent that ~1 MB timeline on every 750 ms poll. So:
//
//   ?lite=1   step results stay on the server; a step carries resultBytes and
//             sid (the session that holds it) and is fetched on expand via
//             GET /api/terminal/session/{id}/step?id=<tool id>
//   ?tail=N   a first read returns only the latest N turns (and timeline
//             entries), with "older" = how many precede them; earlier ones
//             come from GET /api/terminal/session/{id}/turns?before=<id>
//   ?tl=HASH  the timeline is omitted when it has not changed since HASH
//
// Nothing is dropped from the record; these only shape what one read carries.

// liteTurns copies turns with step results replaced by their size. The input
// may be a cached projection, so blocks are copied, never mutated.
func liteTurns(turns []termTurn, sid string) []termTurn {
	out := make([]termTurn, len(turns))
	for i, t := range turns {
		out[i] = t
		out[i].Blocks = liteBlocks(t.Blocks, sid)
	}
	return out
}

func liteBlocks(blocks []termBlock, sid string) []termBlock {
	if len(blocks) == 0 {
		return blocks
	}
	out := make([]termBlock, len(blocks))
	for i, b := range blocks {
		if b.T == "step" && b.Result != "" {
			b.ResultBytes = len(b.Result)
			b.Result = ""
			b.Done = true
			b.SID = sid
		}
		out[i] = b
	}
	return out
}

func liteTimeline(items []conversationTimelineTurn, sid string) []conversationTimelineTurn {
	out := make([]conversationTimelineTurn, len(items))
	for i, it := range items {
		out[i] = it
		owner := sid
		if it.Native != nil && it.Native.ID != "" {
			owner = it.Native.ID
		}
		out[i].Blocks = liteBlocks(it.Blocks, owner)
	}
	return out
}

func tailOf[T any](items []T, n int) ([]T, int) {
	if n <= 0 || len(items) <= n {
		return items, 0
	}
	return items[len(items)-n:], len(items) - n
}

func timelineHash(items []conversationTimelineTurn) string {
	b, _ := json.Marshal(items)
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum[:12])
}

// transcriptShape reads the snappy query parameters.
type transcriptShape struct {
	lite bool
	tail int
	tl   string
}

func readTranscriptShape(r *http.Request) transcriptShape {
	q := r.URL.Query()
	tail, _ := strconv.Atoi(q.Get("tail"))
	if tail < 0 || tail > 2000 {
		tail = 0
	}
	return transcriptShape{lite: q.Get("lite") == "1", tail: tail, tl: q.Get("tl")}
}

// GET /api/terminal/session/{id}/step?id=<tool id> — one step's full result,
// read from the session's own transcript.
func (s *Server) handleTermStepResult(w http.ResponseWriter, r *http.Request) {
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		http.Error(w, "step id required", http.StatusBadRequest)
		return
	}
	path := s.terminal.transcriptPath(se)
	if path == "" {
		http.Error(w, "transcript unavailable", http.StatusNotFound)
		return
	}
	tr, ok := readTranscript(se.Kind, path, 0)
	if !ok {
		http.Error(w, "transcript unavailable", http.StatusNotFound)
		return
	}
	for ti := len(tr.Turns) - 1; ti >= 0; ti-- {
		for _, b := range tr.Turns[ti].Blocks {
			if b.T == "step" && b.ID == id && b.Result != "" {
				writeJSON(w, map[string]any{"id": id, "result": b.Result, "error": b.Error})
				return
			}
		}
	}
	http.Error(w, "no result recorded for that step", http.StatusNotFound)
}

// GET /api/terminal/session/{id}/turns?before=<turn id>&limit=N[&timeline=1]
// — the turns (or planning-timeline entries) that precede one already shown,
// always lite.
func (s *Server) handleTermOlderTurns(w http.ResponseWriter, r *http.Request) {
	se, ok := s.termRow(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	before := q.Get("before")
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 40
	}
	if q.Get("timeline") == "1" {
		timeline, _ := s.terminalPlanningTimeline(r.Context(), se)
		idx := -1
		for i, it := range timeline {
			if fmt.Sprint(it.N) == before {
				idx = i
				break
			}
		}
		if idx < 0 {
			http.Error(w, "unknown timeline entry", http.StatusNotFound)
			return
		}
		start := max(0, idx-limit)
		writeJSON(w, map[string]any{"planningTimeline": liteTimeline(timeline[start:idx], se.ID), "older": start})
		return
	}
	path := s.terminal.transcriptPath(se)
	full, ok := termTranscript{}, false
	if path != "" {
		full, ok = readTranscript(se.Kind, path, 0)
	}
	if !ok {
		http.Error(w, "transcript unavailable", http.StatusNotFound)
		return
	}
	turns := full.Turns
	if o := se.Origin; o != nil && o.Mode == "continue" && (o.Backend == "" || o.Backend == "portal") {
		turns, _ = s.projectConversationNativeTurns(se, agentConversation("hermes", o.Agent, o.ID, "private", "").Key, turns)
	}
	idx := -1
	for i, t := range turns {
		if t.ID == before {
			idx = i
			break
		}
	}
	if idx < 0 {
		http.Error(w, "unknown turn", http.StatusNotFound)
		return
	}
	start := max(0, idx-limit)
	writeJSON(w, map[string]any{"turns": liteTurns(turns[start:idx], se.ID), "older": start})
}
