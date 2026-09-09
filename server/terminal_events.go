package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"sync"
	"time"
)

type terminalEvent struct {
	ManifestID string `json:"manifestId"`
	terminalObservation
	RunID string `json:"runId,omitempty"`
}
type terminalEventSnapshot struct {
	Sessions  []terminalEvent `json:"sessions"`
	Connected bool            `json:"connected"`
}

// One snapshot poller and best-effort daemon stream serve all browser consumers.
// Only List establishes connectivity; events wake observations, never establish
// task completion. The final consumer cancels polling and subscription retries.
type terminalEventHub struct {
	mu          sync.Mutex
	server      *Server
	subscribers map[chan terminalEventSnapshot]struct{}
	latest      map[string]terminalObservation
	connected   bool
	ready       bool
	cancel      context.CancelFunc
}

func (s *Server) terminalHub() *terminalEventHub {
	c := s.terminal
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if c.events == nil {
		c.events = &terminalEventHub{server: s, subscribers: map[chan terminalEventSnapshot]struct{}{}, latest: map[string]terminalObservation{}}
	}
	return c.events
}
func (h *terminalEventHub) snapshotLocked() terminalEventSnapshot {
	out := terminalEventSnapshot{Sessions: []terminalEvent{}, Connected: h.connected}
	for _, se := range h.server.terminal.load() {
		ob := terminalUnknown(se.Runtime)
		ob.Identity.ManifestID = se.ID
		if se.isDraft() {
			ob.AgentState, ob.Connectivity, ob.Process = "not-started", "not-started", "not-started"
		} else if se.backend() == "herdr" && h.connected {
			// Reachability is known even when this saved occupant is absent.
			// Keep its process and agent state unknown rather than adopting another.
			ob.Connectivity = "connected"
			if got, ok := h.latest[se.Runtime.Pane]; ok && got.Identity.Generation == se.Runtime.Generation && got.Identity.Occupant == se.Runtime.Occupant && got.Identity.Session == se.Runtime.Session && got.Identity.Host == se.Runtime.Host && (se.Runtime.AgentSession == "" || se.Runtime.AgentSession == got.Identity.AgentSession) {
				ob = got
				ob.Identity.ManifestID = se.ID
			}
		}
		// Headless detection was inconsistent in live gate A. Keep it unknown.
		if se.BoardBrief != "" {
			ob.AgentState = "unknown"
		}
		ev := terminalEvent{ManifestID: se.ID, terminalObservation: ob}
		if se.BoardBrief != "" {
			ev.RunID = filepath.Base(filepath.Dir(se.BoardBrief))
		}
		out.Sessions = append(out.Sessions, ev)
	}
	return out
}
func (h *terminalEventHub) publishLocked() {
	snapshot := h.snapshotLocked()
	for ch := range h.subscribers {
		select {
		case ch <- snapshot:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snapshot:
			default:
			}
		}
	}
}
func (h *terminalEventHub) join() (chan terminalEventSnapshot, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan terminalEventSnapshot, 1)
	h.subscribers[ch] = struct{}{}
	if h.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		go h.run(ctx)
	}
	if h.ready {
		ch <- h.snapshotLocked()
	}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subscribers, ch)
		if len(h.subscribers) == 0 && h.cancel != nil {
			h.cancel()
			h.cancel = nil
			h.connected = false
			h.ready = false
			h.latest = map[string]terminalObservation{}
		}
	}
}

// wakeOnEvents cannot change hub state or block its authoritative poll loop.
func (h *terminalEventHub) wakeOnEvents(ctx context.Context, wake chan<- struct{}) {
	rt := h.server.terminal.herdr
	if rt == nil {
		return
	}
	for ctx.Err() == nil {
		stream, err := rt.Subscribe(ctx)
		if err == nil {
		reading:
			for {
				select {
				case <-ctx.Done():
					return
				case ob, ok := <-stream:
					if !ok {
						break reading
					}
					select {
					case wake <- struct{}{}:
					default:
					}
					// Preserve removal/stopped hints that may no longer appear in List.
					if ob.Process == "stopped" || ob.AgentState == "done" || ob.AgentState == "blocked" {
						h.server.codingResultSweep()
					}
				}
			}
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (h *terminalEventHub) run(ctx context.Context) {
	wake := make(chan struct{}, 1)
	go h.wakeOnEvents(ctx, wake)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var previous terminalEventSnapshot
	for ctx.Err() == nil {
		var obs []terminalObservation
		err := errTerminalUnsupported
		if rt := h.server.terminal.herdr; rt != nil {
			obs, err = rt.List(ctx)
		}
		latest := make(map[string]terminalObservation, len(obs))
		sweep := false
		if err == nil {
			for _, ob := range obs {
				latest[ob.Identity.Pane] = ob
				sweep = sweep || ob.Process == "stopped" || ob.AgentState == "done" || ob.AgentState == "blocked"
			}
		}
		h.mu.Lock()
		if ctx.Err() != nil {
			h.mu.Unlock()
			return
		}
		h.connected = err == nil
		h.latest = latest
		// Compare the projected state, including registry changes, without letting
		// observation timestamps cause an identical snapshot every two seconds.
		comparable := h.snapshotLocked()
		for i := range comparable.Sessions {
			comparable.Sessions[i].ObservedAt = time.Time{}
		}
		if !h.ready || !reflect.DeepEqual(previous, comparable) {
			h.publishLocked()
		}
		previous = comparable
		h.ready = true
		h.mu.Unlock()
		if sweep {
			h.server.codingResultSweep()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-wake:
		}
	}
}
func (s *Server) handleTermEvents(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil {
		http.Error(w, "terminal disabled", http.StatusServiceUnavailable)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && !sameOrigin(o, r.Host) {
		http.Error(w, "cross-origin refused", http.StatusForbidden)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, leave := s.terminalHub().join()
	defer leave()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case snap := <-ch:
			b, err := json.Marshal(snap)
			if err != nil {
				return
			}
			if _, err = fmt.Fprintf(w, "event: state\ndata: %s\n\n", b); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
