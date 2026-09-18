package domainextract

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Legacy interface {
	SpoolRunNow(string, string, string, string) error
	EngineAlive() (bool, time.Time)
}
type Router struct {
	Config  Config
	Vault   string
	Legacy  Legacy
	mu      sync.RWMutex
	service *Service
}

func (r *Router) Attach(s *Service) { r.mu.Lock(); defer r.mu.Unlock(); r.service = s }
func (r *Router) EngineAlive() (bool, time.Time) {
	if r.Config.Aion || r.Config.RealEstate {
		r.mu.RLock()
		defer r.mu.RUnlock()
		return r.service != nil && r.service.ready("aion", "real-estate"), time.Now()
	}
	if r.Legacy != nil {
		return r.Legacy.EngineAlive()
	}
	return false, time.Time{}
}
func (r *Router) SpoolRunNow(spirit, ritual, request, skill string) error {
	if spirit != "extractor" || !r.Config.Enabled(ritual) {
		if r.Legacy == nil {
			return fmt.Errorf("no legacy executor")
		}
		if alive, _ := r.Legacy.EngineAlive(); !alive {
			return fmt.Errorf("historical executor unavailable; no fallback")
		}
		return r.Legacy.SpoolRunNow(spirit, ritual, request, skill)
	}
	if skill != "" || ritual == "ooda-email" {
		return fmt.Errorf("explicit source submission required")
	}
	var docs []Document
	for _, line := range strings.Split(request, "\n") {
		if strings.HasPrefix(line, "- ") {
			docs = append(docs, Document{Name: strings.TrimSpace(line[2:])})
		}
	}
	input, e := ReadInput(r.Vault, ritual, docs)
	if e != nil {
		return e
	}
	_, e = r.Submit(input)
	return e
}
func (r *Router) Submit(i Input) (string, error) {
	r.mu.RLock()
	s := r.service
	r.mu.RUnlock()
	if s == nil {
		return "", fmt.Errorf("successor unavailable")
	}
	return s.Submit(i)
}

// For keeps the health gate duty-specific during a mixed-runtime cutover.
type Route struct {
	router *Router
	ritual string
}

func (r *Router) For(ritual string) *Route { return &Route{router: r, ritual: ritual} }

// SubmitNote bypasses historical engine liveness for migrated duties. The
// existing worker checks authority and holds the shared ownership fence.
func (r *Route) SubmitNote(path string) error {
	if !r.router.Config.Enabled(r.ritual) {
		return fmt.Errorf("successor duty disabled; historical extraction is not dispatched by the note sink")
	}
	input, err := ReadInput(r.router.Vault, r.ritual, []Document{{Name: path}})
	if err != nil {
		return err
	}
	_, err = r.router.Submit(input)
	return err
}

func (r *Route) EngineAlive() (bool, time.Time) {
	if r.router.Config.Enabled(r.ritual) {
		r.router.mu.RLock()
		defer r.router.mu.RUnlock()
		return r.router.service != nil && r.router.service.ready(r.ritual), time.Now()
	}
	if r.router.Legacy != nil {
		return r.router.Legacy.EngineAlive()
	}
	return false, time.Time{}
}
func (r *Route) SpoolRunNow(spirit, ritual, request, skill string) error {
	if ritual != r.ritual {
		return fmt.Errorf("wrong extraction route")
	}
	return r.router.SpoolRunNow(spirit, ritual, request, skill)
}

func (s *Service) ready(rituals ...string) bool {
	for _, ritual := range rituals {
		if s.cfg.Enabled(ritual) {
			state, _ := Readiness(filepath.Dir(s.dir), s.harness, ritual, true)
			if state != "successor-enabled" {
				return false
			}
		}
	}
	return true
}
