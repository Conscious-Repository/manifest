package spirits

// Read-only projections of the engine's own registry and run artifacts.
import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/robfig/cron/v3"
)

const RitualGrace = 15 * time.Minute

type RitualObservation struct {
	Spirit      string `json:"spirit"`
	Ritual      string `json:"ritual"`
	Health      string `json:"health"`
	Why         string `json:"why"`
	Due         string `json:"due,omitempty"`
	LastRun     string `json:"lastRun,omitempty"`
	LastAttempt string `json:"lastAttempt,omitempty"`
	LastError   string `json:"lastError,omitempty"`
	Evidence    string `json:"evidence"`
}
type RitualPlane struct {
	Health   string              `json:"health"`
	Why      string              `json:"why"`
	Evidence string              `json:"evidence"`
	Rituals  []RitualObservation `json:"rituals"`
}

func (p RitualPlane) For(spirit, ritual string) RitualObservation {
	for _, r := range p.Rituals {
		if r.Spirit == spirit && r.Ritual == ritual {
			return r
		}
	}
	health := p.Health
	if health != "unconfigured" {
		health = "unknown"
	}
	return RitualObservation{Health: health, Why: p.Why, Evidence: p.Evidence}
}

// Last expected fire whose bounded grace has expired. Explicit timezone also
// covers DST when Manifest or its browser is running in UTC. Search is bounded
// to 512 days; a rarer cadence is unknown, never silently healthy.
func ritualDue(cadence string, now time.Time) (time.Time, error) {
	sched, err := cron.ParseStandard("CRON_TZ=America/Chicago " + cadence)
	if err != nil {
		return time.Time{}, err
	}
	cutoff := now.Add(-RitualGrace)
	for days := 1; days <= 512; days *= 2 {
		next := sched.Next(cutoff.AddDate(0, 0, -days))
		if next.IsZero() || next.After(cutoff) {
			continue
		}
		last := next
		for next = sched.Next(last); !next.IsZero() && !next.After(cutoff); next = sched.Next(last) {
			last = next
		}
		return last, nil
	}
	return time.Time{}, nil
}

func (s *Store) RitualObservations(now time.Time) RitualPlane {
	evidence := filepath.Join(s.root, "vessel", "state", "ritual-status.json")
	plane := RitualPlane{Health: "unknown", Evidence: evidence, Rituals: []RitualObservation{}}
	b, err := os.ReadFile(evidence)
	if os.IsNotExist(err) {
		plane.Health = "unconfigured"
		plane.Why = "no ritual-status.json"
		return plane
	}
	if err != nil {
		plane.Why = "ritual-status.json unreadable"
		return plane
	}
	var registry struct {
		Rituals *[]struct {
			Spirit      string `json:"spirit"`
			Ritual      string `json:"ritual"`
			Cadence     string `json:"cadence"`
			Valid       bool   `json:"valid"`
			Paused      bool   `json:"paused"`
			Error       string `json:"error"`
			LastAttempt string `json:"last_attempt"`
			LastError   string `json:"last_error"`
		} `json:"rituals"`
	}
	if json.Unmarshal(b, &registry) != nil || registry.Rituals == nil {
		plane.Why = "ritual-status.json malformed"
		return plane
	}
	hb, hbErr := os.Stat(filepath.Join(s.root, "vessel", "state", "engine.heartbeat"))
	alive := hbErr == nil && now.Sub(hb.ModTime()) <= heartbeatFresh
	plane.Health = "ok"
	if !alive {
		plane.Health = "stopped"
		plane.Why = "engine heartbeat missing or stale"
	}
	latest := map[string]RunSummary{}
	for _, run := range s.Runs() {
		k := run.Spirit + "/" + run.Ritual
		at, e := time.Parse(time.RFC3339, run.Started)
		if e != nil || at.After(now) {
			continue
		}
		prev, ok := latest[k]
		pt, _ := time.Parse(time.RFC3339, prev.Started)
		if !ok || at.After(pt) {
			latest[k] = run
		}
	}
	for _, r := range *registry.Rituals {
		o := RitualObservation{Spirit: r.Spirit, Ritual: r.Ritual, Health: "ok", Evidence: evidence, LastAttempt: r.LastAttempt, LastError: r.LastError}
		run, has := latest[r.Spirit+"/"+r.Ritual]
		o.LastRun = run.Started
		switch {
		case r.Paused:
			o.Health = "paused"
			o.Why = "paused in engine registry"
		case !r.Valid:
			o.Health = "unknown"
			o.Why = r.Error
		case r.Cadence == "":
			o.Health = "on-demand"
		default:
			due, e := ritualDue(r.Cadence, now)
			if e != nil || due.IsZero() {
				o.Health = "unknown"
				o.Why = "cadence invalid or no fire within bounded lookback"
				break
			}
			o.Due = due.Format(time.RFC3339)
			at, _ := time.Parse(time.RFC3339, run.Started)
			if !has || at.Before(due) {
				o.Health = "late"
				o.Why = "due but not run · 15m grace expired · America/Chicago"
				if !alive {
					o.Why += " · engine heartbeat missing or stale"
				} else {
					o.Why += " · engine alive"
				}
				if r.LastError != "" {
					o.Why += " · " + r.LastError
				}
			} else if run.Outcome != "completed" && run.Outcome != "running" {
				o.Health = "failed"
				o.Why = run.OutcomeDetail
			}
		}
		if o.Health == "ok" && !alive && r.Cadence != "" {
			o.Health = "stopped"
			o.Why = "engine heartbeat missing or stale"
		}
		if alive && (o.Health == "late" || o.Health == "failed") {
			plane.Health = "failed"
			plane.Why = "engine alive; scheduled duty evidence missing or failed"
		}
		plane.Rituals = append(plane.Rituals, o)
	}
	return plane
}
