package server

// ledgerSweep (persona plan Phase 0): the background mirror that keeps the
// daily ledger a full continuity — completed RUNS (rituals and delegations
// alike) and engine-authored CHAT turns, neither of which pass through a
// manifest handler. Runs on the AgentLoopTicker after agentLoopSweep.
// Idempotency is a cursor file, not markers: <ledgerDir>/cursor.json.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/ledger"
)

type ledgerCursor struct {
	Runs time.Time      `json:"runs"`           // max run Started already mirrored
	Chat map[string]int `json:"chat,omitempty"` // session id → max event seq mirrored
}

func (s *Server) ledgerCursorPath() string {
	return filepath.Join(s.ledgerStore.Dir(), "cursor.json")
}

func (s *Server) readLedgerCursor() ledgerCursor {
	cur := ledgerCursor{Chat: map[string]int{}}
	if b, err := os.ReadFile(s.ledgerCursorPath()); err == nil {
		_ = json.Unmarshal(b, &cur)
		if cur.Chat == nil {
			cur.Chat = map[string]int{}
		}
	}
	return cur
}

func (s *Server) writeLedgerCursor(cur ledgerCursor) {
	b, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return
	}
	tmp := s.ledgerCursorPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, s.ledgerCursorPath())
	}
}

// ledgerSweep mirrors finished runs and assistant chat turns since the cursor.
func (s *Server) ledgerSweep() {
	if s.ledgerStore == nil {
		return
	}
	cur := s.readLedgerCursor()
	dirty := false

	// Runs: every harness's reports with Finished set, Started after the cursor.
	maxStarted := cur.Runs
	for _, r := range s.mergedRuns() {
		if strings.TrimSpace(r.Finished) == "" {
			continue
		}
		started, err := time.Parse(time.RFC3339, r.Started)
		if err != nil || !started.After(cur.Runs) {
			continue
		}
		kind := "run.completed"
		if r.Outcome != "completed" {
			kind = "run.failed"
		}
		e := ledger.Entry{
			TS: started, Source: "run", Kind: kind,
			Actor: r.Spirit, Object: ledger.Object{Kind: ledger.ObjRun, ID: r.ID}, Run: r.ID,
			Harness: orStr(r.Harness, s.primaryHarnessName()),
			Text:    firstLine(r.Request, 280),
		}
		if m := todoTokenRe.FindStringSubmatch(r.Request); m != nil {
			e.Task = strings.TrimSpace(m[1])
		}
		s.ledger(e)
		dirty = true
		if started.After(maxStarted) {
			maxStarted = started
		}
	}
	cur.Runs = maxStarted

	// Chat: engine-written assistant turns, per-session seq cursor.
	if s.spirits != nil {
		for _, sess := range s.spirits.ChatSessions() {
			after := cur.Chat[sess.ID]
			maxSeq := after
			for _, raw := range s.spirits.ChatEvents(sess.ID, after) {
				var ev struct {
					Seq  int       `json:"seq"`
					TS   time.Time `json:"ts"`
					Type string    `json:"type"`
					Data struct {
						Text string `json:"text"`
					} `json:"data"`
				}
				if json.Unmarshal(raw, &ev) != nil {
					continue
				}
				if ev.Seq > maxSeq {
					maxSeq = ev.Seq
				}
				if ev.Type != "assistant.message" { // deltas/thinking never ledger
					continue
				}
				s.ledger(ledger.Entry{TS: ev.TS, Source: "chat", Kind: "chat.assistant",
					Actor: sess.Spirit, Object: ledger.Object{Kind: ledger.ObjSession, ID: sess.ID}, Session: sess.ID,
					Text: ledger.Snip(ev.Data.Text, 280)})
			}
			if maxSeq != after {
				cur.Chat[sess.ID] = maxSeq
				dirty = true
			}
		}
	}
	if dirty {
		s.writeLedgerCursor(cur)
	}
}
