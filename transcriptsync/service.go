// Package transcriptsync owns deterministic transcript ingestion. It never
// writes the vault or invokes a model; candidates enter the canonical inbox.
package transcriptsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/sys/unix"
	"manifest/approvals"
)

type SourceConfig struct {
	ContinuityOnly bool   `json:"continuityOnly,omitempty"`
	Enabled        bool   `json:"enabled"`
	Account        string `json:"account"`
}
type Config struct {
	LegacyRoot string       `json:"legacyRoot"`
	Granola    SourceConfig `json:"granola"`
	Pocket     SourceConfig `json:"pocket"`
}
type Outcome struct {
	Replay      bool   `json:"replay"`
	ProposalID  string `json:"proposalId,omitempty"`
	Disposition string `json:"disposition"`
}
type State struct {
	Version      int                `json:"version"`
	Account      string             `json:"account"`
	Watermark    time.Time          `json:"watermark"`
	ImportedFrom string             `json:"importedFrom"`
	Items        map[string]Outcome `json:"items"`
	LastAttempt  time.Time          `json:"lastAttempt"`
	LastSuccess  time.Time          `json:"lastSuccess"`
	Error        string             `json:"error,omitempty"`
	Fetched      int                `json:"fetched"`
	Filed        int                `json:"filed"`
	Skipped      int                `json:"skipped"`
	Waiting      int                `json:"waiting"`
}
type Service struct {
	dir            string
	handoffDataDir string
	cfg            Config
	idx            *Index
	approvals      *approvals.Store
	locks          map[string]*sync.Mutex
	now            func() time.Time
	granola        func(string) *GranolaClient
	pocket         func(string) *PocketClient
}

func New(dataDir string, cfg Config, idx *Index, ap *approvals.Store) *Service {
	return &Service{dir: filepath.Join(dataDir, "transcript-sync"), cfg: cfg, idx: idx, approvals: ap, locks: map[string]*sync.Mutex{"granola": {}, "pocket": {}}, now: time.Now, granola: NewGranolaClient, pocket: NewPocketClient}
}

// WithHandoffGuard requires durable migration evidence in production. Tests of
// the isolated polling mechanism can use synthetic state without a live harness.
func (s *Service) WithHandoffGuard(dataDir string) *Service { s.handoffDataDir = dataDir; return s }
func (s *Service) config(source string) (SourceConfig, error) {
	switch source {
	case "granola":
		return s.cfg.Granola, nil
	case "pocket":
		return s.cfg.Pocket, nil
	}
	return SourceConfig{}, fmt.Errorf("unknown transcript source")
}
func (s *Service) Enabled(source string) bool     { c, e := s.config(source); return e == nil && c.Enabled }
func (s *Service) statePath(source string) string { return filepath.Join(s.dir, source, "state.json") }
func (s *Service) keyPath(source string) string   { return filepath.Join(s.dir, source, "key") }
func (s *Service) read(source string) (State, error) {
	c, e := s.config(source)
	if e != nil {
		return State{}, e
	}
	b, e := os.ReadFile(s.statePath(source))
	if e != nil {
		return State{}, fmt.Errorf("transcript state unavailable; import required")
	}
	var st State
	if json.Unmarshal(b, &st) != nil || st.Version != 1 || st.Items == nil || st.Account == "" || st.Account != c.Account || st.Watermark.IsZero() {
		return State{}, fmt.Errorf("invalid transcript state or account mismatch")
	}
	return st, nil
}
func (s *Service) Status(source string) (State, error) {
	if _, e := s.config(source); e != nil {
		return State{}, e
	}
	s.locks[source].Lock()
	defer s.locks[source].Unlock()
	return s.read(source)
}
func atomicWrite(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".sync-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func (s *Service) save(source string, st State) error {
	b, e := json.MarshalIndent(st, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(s.statePath(source), b)
}
func (s *Service) key(source string) (string, error) {
	if v := strings.TrimSpace(os.Getenv(strings.ToUpper(source) + "_API_KEY")); v != "" {
		return v, nil
	}
	b, e := os.ReadFile(s.keyPath(source))
	if e != nil || strings.TrimSpace(string(b)) == "" {
		return "", fmt.Errorf("source credential missing")
	}
	return strings.TrimSpace(string(b)), nil
}
func (s *Service) HasKey(source string) bool {
	if _, e := s.config(source); e != nil {
		return false
	}
	_, e := s.key(source)
	return e == nil
}
func (s *Service) SetKey(source, key string) error {
	if _, e := s.config(source); e != nil {
		return e
	}
	s.locks[source].Lock()
	defer s.locks[source].Unlock()
	if os.Getenv(strings.ToUpper(source)+"_API_KEY") != "" {
		return fmt.Errorf("credential managed by environment")
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("empty key")
	}
	return atomicWrite(s.keyPath(source), []byte(strings.TrimSpace(key)+"\n"))
}
func (s *Service) Disconnect(source string) error {
	if _, e := s.config(source); e != nil {
		return e
	}
	s.locks[source].Lock()
	defer s.locks[source].Unlock()
	if os.Getenv(strings.ToUpper(source)+"_API_KEY") != "" {
		return fmt.Errorf("credential managed by environment")
	}
	e := os.Remove(s.keyPath(source))
	if os.IsNotExist(e) {
		return nil
	}
	return e
}

// Import previews the legacy cursor without writing. Applying the former
// watermark-only import is unsafe: Excalibur does not share a dispatch fence.
// Use ReconcileCheckpoint for approval/source continuity before a future handoff.
func (s *Service) Import(source, legacyRoot, key string, apply bool) (State, error) {
	c, e := s.config(source)
	if e != nil {
		return State{}, e
	}
	if apply {
		return State{}, fmt.Errorf("handoff blocked: enforceable legacy dispatch exclusion, drained work, account binding and source reconciliation required; no state or credential written")
	}
	if c.Account == "" || c.Enabled {
		return State{}, fmt.Errorf("import requires named account and disabled source")
	}
	s.locks[source].Lock()
	defer s.locks[source].Unlock()
	if _, e := os.Lstat(s.statePath(source)); e == nil {
		return State{}, fmt.Errorf("state already imported")
	} else if !os.IsNotExist(e) {
		return State{}, fmt.Errorf("successor state unavailable")
	}
	b, e := os.ReadFile(filepath.Join(legacyRoot, "vessel", "state", source, "watermark"))
	if e != nil {
		return State{}, fmt.Errorf("legacy checkpoint unavailable")
	}
	at, e := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	if e != nil {
		return State{}, fmt.Errorf("invalid legacy checkpoint")
	}
	hash := sha256.Sum256(b)
	return State{Version: 1, Account: c.Account, Watermark: at, ImportedFrom: hex.EncodeToString(hash[:]), Items: map[string]Outcome{}}, nil
}

func validID(id string) bool {
	return id != "" && len(id) < 512 && strings.IndexFunc(id, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("/\\`:\"'[]{}", r)
	}) < 0
}
func (s *Service) duplicate(source, id, name, title, date string) (dupSignal, error) {
	if s.idx == nil || s.idx.db == nil {
		return dupSignal{}, fmt.Errorf("vault index unavailable")
	}
	var paths []string
	var e error
	if source == "granola" {
		paths, e = s.idx.PathsByGranolaID(id)
	} else {
		paths, e = s.idx.PathsByPocketID(id)
	}
	if e != nil {
		return dupSignal{}, fmt.Errorf("source identity lookup failed")
	}
	if len(paths) > 1 {
		return dupSignal{}, fmt.Errorf("duplicate vault source identity")
	}
	if len(paths) == 1 {
		return dupSignal{Skip: true}, nil
	}
	// Name comparisons are case-insensitive, matching Confirm's lowercase path.
	paths, e = s.idx.queryStrings("SELECT path FROM notes WHERE lower(name)=lower(?)", name)
	if e != nil {
		return dupSignal{}, fmt.Errorf("filename lookup failed")
	}
	if len(paths) > 0 {
		return dupSignal{}, fmt.Errorf("transcript filename already exists without matching source identity")
	}
	names, e := s.idx.NamesByDate(date)
	if e != nil {
		return dupSignal{}, fmt.Errorf("date lookup failed")
	}
	for _, n := range names {
		if titlesOverlap(n, title) {
			return dupSignal{Reason: "Possible duplicate: same date and overlapping title; compare existing notes before confirming."}, nil
		}
	}
	return dupSignal{}, nil
}

type candidate struct {
	id, title, filename, content, warning string
	at                                    time.Time
	ready                                 bool
}

func (s *Service) candidates(ctx context.Context, source, key string, st State) ([]candidate, error) {
	since := st.Watermark.Add(-24 * time.Hour)
	var out []candidate
	if source == "granola" {
		c := s.granola(key)
		items, e := c.ListNotesSince(ctx, since)
		if e != nil {
			return nil, e
		}
		for _, it := range items {
			if !validID(it.ID) {
				return nil, fmt.Errorf("invalid Granola identity")
			}
			at := parseGranolaTime(it.CreatedAt)
			if at.IsZero() {
				return nil, fmt.Errorf("invalid Granola timestamp")
			}
			if _, ok := st.Items[it.ID]; ok {
				out = append(out, candidate{id: it.ID, at: at, ready: true})
				continue
			}
			d, e := c.FetchDetail(ctx, it.ID)
			if e != nil {
				return nil, e
			}
			if d.ID != "" && d.ID != it.ID {
				return nil, fmt.Errorf("Granola detail identity mismatch")
			}
			d.ID = it.ID
			if d.Title == "" {
				d.Title = it.Title
			}
			if d.CreatedAt.IsZero() {
				d.CreatedAt = at
			}
			content, n := convertTranscript(d, s.idx, titleAttendees(s.idx, d.Title))
			out = append(out, candidate{id: it.ID, title: d.Title, filename: noteFilename(d.CreatedAt, d.Title), content: content, at: at, ready: n > 0})
		}
	} else {
		c := s.pocket(key)
		items, e := c.ListRecordings(ctx, since.UTC().Format("2006-01-02"))
		if e != nil {
			return nil, e
		}
		for _, it := range items {
			if !validID(it.ID) {
				return nil, fmt.Errorf("invalid Pocket identity")
			}
			at, e := time.Parse(time.RFC3339, it.RecordingAt)
			if e != nil {
				return nil, fmt.Errorf("invalid Pocket timestamp")
			}
			if _, ok := st.Items[it.ID]; ok {
				out = append(out, candidate{id: it.ID, at: at, ready: true})
				continue
			}
			n := candidate{id: it.ID, at: at}
			if it.State != "completed" || s.now().Sub(at) < time.Hour {
				out = append(out, n)
				continue
			}
			d, e := c.FetchDetail(ctx, it.ID)
			if e != nil {
				return nil, e
			}
			if d.ID != "" && d.ID != it.ID {
				return nil, fmt.Errorf("Pocket detail identity mismatch")
			}
			d.ID = it.ID
			if d.Title == "" {
				d.Title = it.Title
			}
			n.title = d.Title
			n.filename = pocketNoteFilename(at.In(chicago), d.Title)
			content, segs, unresolved := convertPocketTranscript(d, s.idx, titleAttendees(s.idx, d.Title))
			n.content = content
			n.ready = segs > 0
			if unresolved {
				n.warning = "Unresolved speaker labels: review names before confirming."
			}
			out = append(out, n)
		}
	}
	// Validate the complete batch before publication: first-wins would silently
	// discard conflicting copies of a source ID returned across pages/details.
	seen := make(map[string]candidate, len(out))
	unique := out[:0]
	for _, it := range out {
		if old, ok := seen[it.id]; ok {
			if old.title != it.title || old.filename != it.filename || old.content != it.content || old.warning != it.warning || old.ready != it.ready || !old.at.Equal(it.at) {
				return nil, fmt.Errorf("conflicting duplicate transcript identity")
			}
			continue
		}
		seen[it.id] = it
		unique = append(unique, it)
	}
	out = unique
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out, nil
}

// Poll serializes all callers and processes only a complete fetch. Individual
// outcomes persist before checkpoint movement. An unfinished source item holds
// the checkpoint, so even a transcription delayed by days cannot be skipped.
func (s *Service) Poll(ctx context.Context, source string) (st State, err error) {
	c, e := s.config(source)
	if e != nil {
		return st, e
	}
	if !c.Enabled {
		return st, fmt.Errorf("transcript source disabled")
	}
	if c.ContinuityOnly {
		return st, fmt.Errorf("source is in read-only continuity mode")
	}
	if s.handoffDataDir != "" {
		release, err := s.enterSuccessor(source)
		if err != nil {
			return st, err
		}
		defer release()
	}

	s.locks[source].Lock()
	defer s.locks[source].Unlock()
	if e = os.MkdirAll(filepath.Join(s.dir, source), 0700); e != nil {
		return st, e
	}
	f, e := os.OpenFile(filepath.Join(s.dir, source, "poll.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return st, e
	}
	defer f.Close()
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return st, fmt.Errorf("source poll already active")
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	st, err = s.read(source)
	if err != nil {
		return st, err
	}
	st.LastAttempt = s.now().UTC()
	st.Error = ""
	st.Fetched = 0
	st.Filed = 0
	st.Skipped = 0
	st.Waiting = 0
	if err = s.save(source, st); err != nil {
		return st, err
	}
	defer func() {
		if err != nil {
			st.Error = err.Error()
		}
		if e := s.save(source, st); e != nil {
			err = errors.Join(err, e)
		}
	}()
	if s.idx == nil || s.idx.db == nil || s.approvals == nil {
		return st, fmt.Errorf("index or approvals unavailable")
	}
	inv, owner, e := s.approvals.TranscriptSnapshot(source, s.handoffDataDir)
	if e != nil {
		return st, e
	}
	for id, prior := range st.Items {
		if e := s.checkReconciledOutcome(source, id, &prior, inv, owner); e != nil {
			return st, e
		}
	}
	key, e := s.key(source)
	if e != nil {
		return st, e
	}
	items, e := s.candidates(ctx, source, key, st)
	if e != nil {
		return st, e
	}
	st.Fetched = len(items)
	maxSeen := st.Watermark
	for _, it := range items {
		if it.at.After(maxSeen) {
			maxSeen = it.at
		}
		if !it.ready {
			st.Waiting++
			continue
		}
		if _, ok := st.Items[it.id]; ok {
			st.Skipped++
			continue
		}
		if e := s.checkReconciledOutcome(source, it.id, nil, inv, owner); e != nil {
			return st, e
		}
		dup, e := s.duplicate(source, it.id, strings.TrimSuffix(it.filename, ".md"), it.title, it.filename[:10])
		if e != nil {
			return st, e
		}
		if dup.Skip {
			st.Items[it.id] = Outcome{Disposition: "existing-note"}
			st.Skipped++
		} else {
			p, created, e := s.approvals.ProposeTranscript(source, it.id, approvals.Proposal{Type: approvals.TypeCreateVaultNote, Agent: "ea-coordinator", Ritual: source + "-sync", Action: "Create vault note: " + it.filename, ApplyPath: it.filename, Proposed: it.content, Body: "New " + source + " transcript. Confirm to write under log/.\n\n" + it.warning + "\n" + dup.Reason})
			if e != nil {
				return st, e
			}
			if p.Status == "approved" {
				return st, fmt.Errorf("approval changed during poll; reconcile source note before retry")
			}
			st.Items[it.id] = Outcome{ProposalID: p.ID, Disposition: p.Status}
			if created {
				st.Filed++
			} else {
				st.Skipped++
			}
		}
		if e = s.save(source, st); e != nil {
			return st, e
		}
	}
	if st.Waiting == 0 {
		st.Watermark = maxSeen
	}
	st.LastSuccess = s.now().UTC()
	return st, nil
}
func (s *Service) Test(ctx context.Context, source string) error {
	if _, e := s.config(source); e != nil {
		return e
	}
	s.locks[source].Lock()
	defer s.locks[source].Unlock()
	key, e := s.key(source)
	if e != nil {
		return e
	}
	var response map[string]any
	if source == "granola" {
		if e := s.granola(key).get(ctx, "/notes?limit=1", &response); e != nil {
			return e
		}
		if _, ok := response["notes"].([]any); !ok {
			return fmt.Errorf("invalid Granola test response")
		}
		return nil
	}
	if e := s.pocket(key).get(ctx, "/public/recordings", url.Values{"limit": {"1"}}, &response); e != nil {
		return e
	}
	if response["success"] != true {
		return fmt.Errorf("unsuccessful Pocket test response")
	}
	return nil
}

// Start uses Manifest's poller lifecycle and the original Chicago wall-clock
// slots. A persisted attempt suppresses repeated dispatch within the same slot.
func (s *Service) Start(ctx context.Context) {
	for _, source := range []string{"granola", "pocket"} {
		if !s.Enabled(source) {
			continue
		}
		go func(source string) {
			tick := time.NewTicker(time.Minute)
			defer tick.Stop()
			for {
				st, e := s.Status(source)
				now := s.now()
				if e == nil && due(source, now, st.LastAttempt) {
					run, cancel := context.WithTimeout(ctx, 15*time.Minute)
					if cfg, _ := s.config(source); cfg.ContinuityOnly {
						s.observe(run, source)
					} else {
						s.Poll(run, source)
					}
					cancel()
				}
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
			}
		}(source)
	}
}
func due(source string, now, last time.Time) bool {
	hours := []int{8, 13, 18}
	if source == "pocket" {
		hours = []int{9, 18}
	}
	local := now.In(chicago)
	for i := len(hours) - 1; i >= 0; i-- {
		slot := time.Date(local.Year(), local.Month(), local.Day(), hours[i], 0, 0, 0, chicago)
		if !now.Before(slot) {
			return last.Before(slot)
		}
	}
	return false
}
