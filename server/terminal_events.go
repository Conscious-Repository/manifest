package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
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

// One daemon stream is shared by all browser consumers. Queues contain only the
// newest projection; events wake observations, never establish task completion.
// The final consumer cancels the daemon socket and any reconnect backoff.
type terminalEventHub struct {
	mu          sync.Mutex
	server      *Server
	subscribers map[chan terminalEventSnapshot]struct{}
	latest      map[string]terminalObservation
	connected   bool
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
		if se.backend() == "herdr" && h.connected {
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
	ch <- h.snapshotLocked()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subscribers, ch)
		if len(h.subscribers) == 0 && h.cancel != nil {
			h.cancel()
			h.cancel = nil
			h.connected = false
			h.latest = map[string]terminalObservation{}
		}
	}
}
func (h *terminalEventHub) run(ctx context.Context) {
	for ctx.Err() == nil {
		rt := h.server.terminal.herdr
		var stream <-chan terminalObservation
		var err error
		if rt != nil {
			stream, err = rt.Subscribe(ctx)
		} else {
			err = errTerminalUnsupported
		}
		if err == nil {
			h.mu.Lock()
			if ctx.Err() != nil {
				h.mu.Unlock()
				return
			}
			h.connected = true
			h.latest = map[string]terminalObservation{}
			h.publishLocked()
			h.mu.Unlock()
			for ob := range stream {
				h.mu.Lock()
				if ctx.Err() != nil {
					h.mu.Unlock()
					return
				}
				h.latest[ob.Identity.Pane] = ob
				h.publishLocked()
				h.mu.Unlock()
				if ob.Process == "stopped" || ob.AgentState == "done" || ob.AgentState == "blocked" {
					h.server.codingResultSweep()
				}
			}
			// The subscription stream ended. Do NOT immediately declare the
			// daemon unreachable: the stream can drop while the daemon stays
			// reachable (a lost pane event / socket hiccup). Verify with an
			// authoritative List() snapshot. If it succeeds, the daemon is up —
			// refresh state from it and stay 'connected'; only report
			// unavailable if List() itself fails.
			if obs, lerr := rt.List(ctx); lerr == nil {
				h.mu.Lock()
				if ctx.Err() != nil {
					h.mu.Unlock()
					return
				}
				h.latest = map[string]terminalObservation{}
				h.connected = true
				for _, ob := range obs {
					h.latest[ob.Identity.Pane] = ob
				}
				h.publishLocked()
				h.mu.Unlock()
				rtimer := time.NewTimer(150 * time.Millisecond)
				select {
				case <-ctx.Done():
					rtimer.Stop()
					return
				case <-rtimer.C:
				}
				continue // daemon reachable — re-subscribe, don't report unavailable
			}
		}
		h.mu.Lock()
		if ctx.Err() != nil {
			h.mu.Unlock()
			return
		}
		h.connected = false
		h.latest = map[string]terminalObservation{}
		h.publishLocked()
		h.mu.Unlock()
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
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
