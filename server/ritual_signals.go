package server

import (
	"manifest/signals"
	"manifest/spirits"
	"time"
)

type ritualMissedEmitter struct{ s *Server }

func (s *Server) RitualMissedEmitter() signals.Emitter { return ritualMissedEmitter{s} }
func (e ritualMissedEmitter) Emit(now time.Time) ([]signals.Signal, error) {
	out := []signals.Signal{}
	for _, h := range e.s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		plane := h.Spirits.RitualObservations(now)
		if plane.Health == "unknown" {
			out = append(out, signals.Signal{ID: "ritual-registry:" + h.Name, Kind: "ritual-registry", Entity: h.Name, Label: h.Name + " · " + plane.Why, ActHref: "#/agents/runs", Hash: plane.Why})
		}
		for _, r := range plane.Rituals {
			if r.Health != "late" {
				continue
			}
			key := h.Name + "/" + r.Spirit + "/" + r.Ritual
			out = append(out, signals.Signal{ID: "ritual-missed:" + key, Kind: "ritual-missed", Entity: key, Label: key + " · " + snipRunes(r.Why, 160), ActHref: "#/agents/runs", Hash: r.Due + "|" + r.LastError})
		}
	}
	return out, nil
}
func (s *Server) ritualObservations(now time.Time) []spirits.RitualObservation {
	out := []spirits.RitualObservation{}
	for _, h := range s.eachHarness() {
		if h.Spirits == nil {
			continue
		}
		p := h.Spirits.RitualObservations(now)
		out = append(out, p.Rituals...)
		if p.Health == "unknown" {
			out = append(out, spirits.RitualObservation{Health: p.Health, Why: p.Why, Evidence: p.Evidence})
		}
	}
	return out
}
