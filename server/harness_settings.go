package server

// Harness settings (owner ask 2026-08-11: "a setting like portals to change
// these"). The PORTALS panel already governs the model endpoints; this governs
// the harness↔engine↔portal wiring: for each federated tree — is its engine
// live, what path, what spirits, and which conduit each spirit routes to —
// with an in-place portal switch (rewrites the cornerstone `portal::` line
// through the harness's own allow-listed editor). Starting/stopping an engine
// stays an operator/systemd action (manifest never holds sudo); the panel
// surfaces the state + the one command.

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type harnessSpiritView struct {
	Name   string `json:"name"`
	Portal string `json:"portal"`
}

type harnessSettingRow struct {
	Name        string              `json:"name"`
	Path        string              `json:"path"`
	Primary     bool                `json:"primary"`
	EngineAlive bool                `json:"engineAlive"`
	Heartbeat   string              `json:"heartbeat,omitempty"`
	Queued      int                 `json:"queued"`
	Spirits     []harnessSpiritView `json:"spirits"`
	EngineHint  string              `json:"engineHint,omitempty"` // shown when no engine
	Portals     []string            `json:"portals"`              // switchable conduits (grimoire)
}

var cornerPortalLineRe = regexp.MustCompile(`(?m)^(portal::\s*)(\S+)`)

// handleHarnesses lists every federated harness with engine + spirit state.
func (s *Server) handleHarnesses(w http.ResponseWriter, r *http.Request) {
	rows := []harnessSettingRow{}
	primary := s.primaryHarnessName()
	for _, h := range s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		alive, at := h.Spirits.EngineAlive()
		row := harnessSettingRow{
			Name: h.Name, Path: h.Spirits.Root(), Primary: h.Name == primary,
			EngineAlive: alive, Queued: len(h.Spirits.Queued()),
			Portals: harnessPortals(h.Spirits.Root()),
		}
		if !at.IsZero() {
			row.Heartbeat = at.Format(time.RFC3339)
		}
		if !alive {
			row.EngineHint = "no live engine — delegations here queue until one runs. Start one on the server: sudo systemctl enable --now excalibur-engine@" + h.Name
		}
		row.Spirits = harnessSpirits(h.Spirits.Root())
		rows = append(rows, row)
	}
	writeJSON(w, map[string]any{"harnesses": rows})
}

// harnessSpirits reads each spirit's cornerstone portal (the switchable conduit).
func harnessSpirits(root string) []harnessSpiritView {
	out := []harnessSpiritView{}
	entries, err := os.ReadDir(filepath.Join(root, "spirits"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, "spirits", e.Name(), "cornerstone.md"))
		if err != nil {
			continue
		}
		portal := ""
		if m := cornerPortalLineRe.FindStringSubmatch(string(b)); m != nil {
			portal = m[2]
		}
		out = append(out, harnessSpiritView{Name: e.Name(), Portal: portal})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// harnessPortals lists the conduits a spirit can switch to (grimoire/portals).
func harnessPortals(root string) []string {
	matches, _ := filepath.Glob(filepath.Join(root, "grimoire", "portals", "*.md"))
	var out []string
	for _, m := range matches {
		name := strings.TrimSuffix(filepath.Base(m), ".md")
		if strings.HasSuffix(name, ".example") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// handleHarnessSpiritPortal rewrites a spirit's cornerstone `portal::` line
// through that harness's allow-listed editor (lint-gated, fixpoint-safe). The
// engine hot-reloads the cornerstone on its next run.
func (s *Server) handleHarnessSpiritPortal(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Harness string `json:"harness"`
		Spirit  string `json:"spirit"`
		Portal  string `json:"portal"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	if b.Portal == "" {
		httpError(w, errBadRequest("portal is required"))
		return
	}
	var target *Harness
	hs := s.eachHarness()
	for i := range hs {
		if hs[i].Name == b.Harness {
			target = &hs[i]
			break
		}
	}
	if target == nil || target.Spirits == nil {
		httpError(w, errBadRequest("unknown harness "+b.Harness))
		return
	}
	// the switchable conduit must be a real grimoire portal (fail closed)
	valid := false
	for _, p := range harnessPortals(target.Spirits.Root()) {
		if p == b.Portal {
			valid = true
			break
		}
	}
	if !valid {
		httpError(w, errBadRequest("no grimoire portal named "+b.Portal))
		return
	}
	rel := "spirits/" + b.Spirit + "/cornerstone.md"
	content, allowed, err := target.Spirits.ReadFile(rel)
	if !allowed || err != nil {
		http.Error(w, "spirit cornerstone not found", http.StatusNotFound)
		return
	}
	next := cornerPortalLineRe.ReplaceAllString(content, "${1}"+b.Portal)
	if next == content {
		writeJSON(w, map[string]bool{"ok": true, "unchanged": true})
		return
	}
	res, allowed, err := target.Spirits.WriteFile(rel, next)
	if !allowed {
		http.Error(w, "cornerstone not editable", http.StatusNotFound)
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	if !res.OK {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}
	writeJSON(w, res)
}
