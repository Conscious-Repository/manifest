package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const terminalRowVersion = 1

func (se termSession) backend() string {
	if se.Backend == "" {
		return "tmux"
	}
	return se.Backend
}
func (c *termCfg) loadChecked() ([]termSession, error) {
	b, err := os.ReadFile(c.regPath)
	if errors.Is(err, os.ErrNotExist) {
		return []termSession{}, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []termSession
	if err = json.Unmarshal(b, &rows); err != nil {
		return nil, fmt.Errorf("terminal registry invalid; preserved unchanged: %w", err)
	}
	if rows == nil && strings.TrimSpace(string(b)) != "[]" {
		return nil, errors.New("terminal registry must be an array")
	}
	seen := map[string]bool{}
	for _, se := range rows {
		if se.Version > terminalRowVersion || se.ID == "" || seen[se.ID] {
			return nil, errors.New("terminal registry version or identity invalid; preserved unchanged")
		}
		seen[se.ID] = true
	}
	return rows, nil
}
func (c *termCfg) writeRowsLocked(rows []termSession) error {
	if rows == nil {
		rows = []termSession{}
	}
	b, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(c.regPath), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.regPath), ".terminals-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, c.regPath); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(c.regPath))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (c *termCfg) upsertChecked(se termSession) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.loadChecked()
	if err != nil {
		return err
	}
	found := false
	for i := range rows {
		if rows[i].ID == se.ID {
			rows[i] = se
			found = true
			break
		}
	}
	if !found {
		rows = append([]termSession{se}, rows...)
	}
	return c.writeRowsLocked(rows)
}

// importLegacy is a metadata migration only. It never starts, deletes or renames
// a process, and the original file is retained once before changing any row.
func (c *termCfg) importLegacy() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.loadChecked()
	if err != nil {
		return err
	}
	changed := false
	host, _ := os.Hostname()
	for i := range rows {
		se := &rows[i]
		if se.Version != 0 {
			continue
		}
		changed = true
		se.Version = terminalRowVersion
		if se.Backend == "" {
			se.Backend = "tmux"
		}
		if se.Backend == "tmux" {
			se.Runtime = terminalIdentity{Backend: "tmux", Host: host, ManifestID: se.ID, Session: tmuxName(se.ID)}
		}
	}
	if !changed {
		return nil
	}
	original, err := os.ReadFile(c.regPath)
	if err != nil {
		return err
	}
	backup, err := os.OpenFile(c.regPath+".pre-herdr-v1.bak", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		_, err = backup.Write(original)
		if err == nil {
			err = backup.Sync()
		}
		ce := backup.Close()
		if err == nil {
			err = ce
		}
	} else if errors.Is(err, os.ErrExist) {
		err = nil
	}
	if err != nil {
		return fmt.Errorf("terminal import backup: %w", err)
	}
	return c.writeRowsLocked(rows)
}
func (s *Server) runtimeFor(se termSession) (terminalRuntime, error) {
	switch se.backend() {
	case "tmux":
		return &tmuxTerminalRuntime{server: s}, nil
	case "herdr":
		if s.terminal.herdr != nil {
			return s.terminal.herdr, nil
		}
		return nil, errors.New("herdr daemon is not configured")
	default:
		return nil, fmt.Errorf("unknown terminal backend %q", se.Backend)
	}
}
func (s *Server) observeTerm(ctx context.Context, se termSession) (terminalObservation, error) {
	if se.isDraft() {
		return terminalObservation{Identity: se.Runtime, AgentState: "not-started", Connectivity: "not-started", Process: "not-started", ObservedAt: time.Now().UTC()}, nil
	}
	id := se.Runtime
	if se.backend() == "tmux" {
		id.Backend = "tmux"
		id.ManifestID = se.ID
		id.Session = tmuxName(se.ID)
	}
	rt, err := s.runtimeFor(se)
	if err != nil {
		return terminalUnknown(id), err
	}
	return rt.Inspect(ctx, id)
}

// launchHerdr persists every boundary before crossing it. An interrupted intent,
// allocated, or submitted row is inspectable but is never automatically replayed.
// Allocation can itself succeed with a lost reply; a label is not enough evidence
// to recover that identity and therefore cannot authorize a second allocation.
func (s *Server) launchHerdr(ctx context.Context, se termSession) (termSession, error) {
	mu := s.termInputMutex(se.ID)
	mu.Lock()
	defer mu.Unlock()
	return s.launchHerdrLocked(ctx, se)
}

// Caller holds the stable session ID lock across persistence and daemon I/O.
func (s *Server) launchHerdrLocked(ctx context.Context, se termSession) (termSession, error) {
	cwd, err := resolveTerminalCwd(se.Cwd, s.terminal.defaultWd)
	if err != nil {
		return se, err
	}
	if s.terminal.herdr == nil {
		return se, errors.New("herdr daemon is not configured; no fallback launch")
	}
	prior, existed, err := s.terminal.findChecked(se.ID)
	if err != nil {
		return se, &terminalServerError{err}
	}
	se.Cwd = cwd
	se.Version = terminalRowVersion
	se.Backend = "herdr"
	se.LaunchPhase = "intent"
	if err := s.terminal.upsertChecked(se); err != nil {
		return se, &terminalServerError{err}
	}
	id, err := s.terminal.herdr.allocate(ctx, se)
	if err != nil {
		var notAttempted *terminalAllocationNotAttempted
		if errors.As(err, &notAttempted) {
			if se.BoardBrief != "" {
				return se, fmt.Errorf("allocation not attempted; board session %s retained: %w", se.ID, err)
			}
			var rollbackErr error
			if existed {
				rollbackErr = s.terminal.upsertChecked(prior)
			} else {
				rollbackErr = s.terminal.removeChecked(se.ID)
			}
			if rollbackErr != nil {
				return se, &terminalServerError{fmt.Errorf("terminal %s rollback failed (%v); original allocation rejection: %w", se.ID, rollbackErr, err)}
			}
			if existed {
				se = prior
			}
			return se, err
		}
		return se, fmt.Errorf("launch allocation unobserved; do not retry automatically: %w", err)
	}
	se.Runtime = id
	se.LaunchPhase = "allocated"
	if err = s.terminal.upsertChecked(se); err != nil {
		return se, &terminalServerError{fmt.Errorf("allocated pane identity could not be persisted; nothing launched: %w", err)}
	}
	launch := se
	se.LaunchPhase = "submitted"
	se.Started = true
	if err = s.terminal.upsertChecked(se); err != nil {
		return se, &terminalServerError{fmt.Errorf("launch submission posture could not be persisted; nothing launched: %w", err)}
	}
	if err = s.terminal.herdr.launch(ctx, id, launch); err != nil {
		return se, fmt.Errorf("launch outcome unobserved; do not replay: %w", err)
	}
	se.LaunchPhase = "active"
	se.LastUsed = time.Now().Format(time.RFC3339)
	if err = s.terminal.upsertChecked(se); err != nil {
		return se, &terminalServerError{fmt.Errorf("process launched but mapping finalization failed; do not replay: %w", err)}
	}
	return se, nil
}
func (s *Server) ensureHerdrInput(ctx context.Context, se termSession) (termSession, bool, error) {
	mu := s.termInputMutex(se.ID)
	mu.Lock()
	defer mu.Unlock()
	current, ok, err := s.terminal.findChecked(se.ID)
	if err != nil {
		return se, false, &terminalServerError{err}
	}
	if !ok {
		return se, false, errors.New("terminal session no longer exists")
	}
	return s.ensureHerdrInputLocked(ctx, current)
}
func (s *Server) ensureHerdrInputLocked(ctx context.Context, se termSession) (termSession, bool, error) {
	if se.isDraft() {
		started, err := s.launchHerdrLocked(ctx, se)
		return started, err == nil, err
	}
	// The shared ID lock in handleTermInput guards explicit stopped-session resume.
	if se.LaunchPhase != "active" {
		return se, false, fmt.Errorf("launch %s is unresolved; open the exact pane to inspect, no input sent", se.LaunchPhase)
	}
	ob, err := s.observeTerm(ctx, se)
	if err != nil {
		return se, false, err
	}
	if ob.Process == "running" {
		return se, false, nil
	}
	if ob.Process != "stopped" {
		return se, false, errors.New("terminal process state unknown; nothing launched or sent")
	}
	if !resumeIDRe.MatchString(se.ResumeID) || se.Resume {
		return se, false, errors.New("exact conversation identity unavailable; resume from the CLI picker")
	}
	// An explicit owner send may resume an exactly identified, confirmed-absent
	// conversation. Started stays true so a board's first command is never replayed.
	se.Started = true
	resumed, err := s.launchHerdrLocked(ctx, se)
	return resumed, true, err
}
func (s *Server) closeTerm(ctx context.Context, se termSession) error {
	if se.isDraft() {
		return nil
	}
	if se.backend() == "tmux" {
		err := s.terminal.tmux("kill-session", "-t", tmuxName(se.ID))
		s.killRemoteKeep(se)
		_ = err
		return nil
	}
	rt, err := s.runtimeFor(se)
	if err != nil {
		return err
	}
	ob, err := rt.Inspect(ctx, se.Runtime)
	if err != nil {
		return err
	}
	if ob.Process == "stopped" {
		return nil
	}
	return rt.Close(ctx, se.Runtime)
}
func (s *Server) configureHerdr() {
	name := strings.TrimSpace(os.Getenv("MANIFEST_HERDR_SESSION"))
	if name == "" {
		name = "manifest"
	}
	// Match herdr's named-session path; use a fixed local host identity, never a label.
	if strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	host, _ := os.Hostname()
	s.terminal.herdr = &herdrTerminalRuntime{Socket: filepath.Join(base, "herdr", "sessions", name, "herdr.sock"), Session: name, Host: host, server: s}
}

func (s *Server) termInputMutex(id string) *sync.Mutex {
	c := s.terminal
	c.spawnMu.Lock()
	defer c.spawnMu.Unlock()
	if c.spawnIn == nil {
		c.spawnIn = map[string]*sync.Mutex{}
	}
	mu := c.spawnIn[id]
	if mu == nil {
		mu = &sync.Mutex{}
		c.spawnIn[id] = mu
	}
	return mu
}
func (s *Server) herdrPromptReady(ctx context.Context, se termSession) error {
	ctx, cancel := context.WithTimeout(ctx, termPromptWait)
	defer cancel()
	ticker := time.NewTicker(termPromptPoll)
	defer ticker.Stop()
	for {
		// Readiness checks precede the one and only submission. Unknown readiness is
		// never translated into idle and never authorizes a fallback process.
		ob, err := s.observeTerm(ctx, se)
		if err != nil {
			return err
		}
		if ob.Process != "running" {
			return errors.New("agent process is not ready; nothing sent")
		}
		lines, err := s.terminal.herdr.Screen(ctx, se.Runtime)
		if err != nil {
			return err
		}
		if why := termBlockingDialog(lines); why != "" {
			return errors.New(why)
		}
		if ob.AgentState == "blocked" {
			return errors.New("agent needs interactive input; answer the pending questions or open Terminal for approvals; nothing sent")
		}
		if ob.AgentState != "unknown" {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("agent readiness unknown; nothing sent")
		case <-ticker.C:
		}
	}
}
func (s *Server) captureObservedTermIdentity(se termSession, ob terminalObservation) termSession {
	if ob.Process != "running" || ob.Identity.AgentSession == "" {
		return se
	}
	parts := strings.SplitN(ob.Identity.AgentSession, ":", 3)
	if len(parts) != 3 || parts[0] != se.Kind || parts[1] != "id" || !resumeIDRe.MatchString(parts[2]) {
		return se
	}
	if se.ResumeID != "" && se.ResumeID != parts[2] {
		return se
	}
	return s.captureTermMetadata(se, parts[2], ob.Identity.AgentSession)
}
func (s *Server) captureTermResumeID(se termSession, id string) termSession {
	return s.captureTermMetadata(se, id, "")
}
func (s *Server) captureTermMetadata(se termSession, resume, agentSession string) termSession {
	if !resumeIDRe.MatchString(resume) {
		return se
	}
	c := s.terminal
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.loadChecked()
	if err != nil {
		return se
	}
	for i := range rows {
		current := rows[i]
		if current.ID != se.ID {
			continue
		}
		if current.Runtime.Generation != se.Runtime.Generation || current.Runtime.Pane != se.Runtime.Pane || current.Runtime.Occupant != se.Runtime.Occupant {
			return current
		}
		if current.ResumeID != "" && current.ResumeID != resume {
			return current
		}
		if current.ResumeID == resume && (agentSession == "" || current.Runtime.AgentSession == agentSession) {
			return current
		}
		current.ResumeID = resume
		current.Resume = false
		if agentSession != "" {
			current.Runtime.AgentSession = agentSession
		}
		rows[i] = current
		if err = c.writeRowsLocked(rows); err != nil {
			return se
		}
		return current
	}
	return se
}

// updateTermMetadata merges display/touch edits into the latest row so a screen
// read discovering the exact CLI identity cannot be overwritten by a stale tab.
func (c *termCfg) updateTermMetadata(id string, edit func(*termSession)) (termSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.loadChecked()
	if err != nil {
		return termSession{}, err
	}
	for i := range rows {
		if rows[i].ID == id {
			edit(&rows[i])
			if err = c.writeRowsLocked(rows); err != nil {
				return termSession{}, err
			}
			return rows[i], nil
		}
	}
	return termSession{}, errors.New("terminal session no longer exists")
}

func (c *termCfg) findChecked(id string) (termSession, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.loadChecked()
	if err != nil {
		return termSession{}, false, err
	}
	for _, se := range rows {
		if se.ID == id {
			return se, true, nil
		}
	}
	return termSession{}, false, nil
}
