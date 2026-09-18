package domainextract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"manifest/approvals"
	"manifest/hermes"
	"manifest/mdfm"
)

type Config struct {
	Aion       bool `json:"aion"`
	RealEstate bool `json:"realEstate"`
	OodaEmail  bool `json:"oodaEmail"`
}

func (c Config) Enabled(ritual string) bool {
	switch ritual {
	case "aion":
		return c.Aion
	case "real-estate":
		return c.RealEstate
	case "ooda-email":
		return c.OodaEmail
	}
	return false
}

type Job struct {
	ParentID          string                      `json:"parentId,omitempty"`
	Execution         *hermes.ExtractionExecution `json:"execution,omitempty"`
	Version           int                         `json:"version"`
	OwnershipRevision uint64                      `json:"ownershipRevision"`
	Replay            bool                        `json:"replay"`
	ID                string                      `json:"id"`
	Input             Input                       `json:"input"`
	State             string                      `json:"state"`
	Reason            string                      `json:"reason,omitempty"`
	Started           time.Time                   `json:"started"`
	Finished          time.Time                   `json:"finished"`
	Model             string                      `json:"model,omitempty"`
	SpentUSD          float64                     `json:"spentUsd"`
	Candidates        []approvals.Proposal        `json:"candidates,omitempty"`
	Published         int                         `json:"published"`
	// Summary is the model's own explanation for the batch. It is retained so an
	// empty (non-publication) outcome can record WHY nothing was proposed.
	Summary string `json:"summary,omitempty"`
}

// replySummary extracts the model's summary from a validated extraction reply.
// It is presentation evidence only: it never participates in candidate checks.
func replySummary(reply string) string {
	var r Response
	if strict([]byte(reply), &r) != nil {
		return ""
	}
	s := strings.TrimSpace(r.Summary)
	if len(s) > 1200 {
		s = s[:1200]
	}
	return s
}

type Service struct {
	dir, vault, harness string
	cfg                 Config
	runner              *hermes.Runner
	ap                  *approvals.Store
	ctx                 context.Context
	mu                  sync.Mutex
	wake                chan struct{}
}

func New(ctx context.Context, dataDir, vault, harness string, cfg Config, r *hermes.Runner, ap *approvals.Store) *Service {
	return &Service{dir: filepath.Join(dataDir, "domain-extraction"), vault: vault, harness: harness, cfg: cfg, runner: r, ap: ap, ctx: ctx, wake: make(chan struct{}, 1)}
}
func atomic(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".extract-*")
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
func (s *Service) save(j Job) error {
	b, e := json.MarshalIndent(j, "", "  ")
	if e != nil {
		return e
	}
	return atomic(filepath.Join(s.dir, j.ID+".json"), b)
}
func (s *Service) Submit(input Input) (string, error) {
	if !s.cfg.Enabled(input.Ritual) || s.runner == nil || s.ap == nil {
		return "", fmt.Errorf("successor duty disabled")
	}
	if e := s.runner.ValidateExtractionDuty(input.Ritual); e != nil {
		return "", e
	}
	if e := input.Validate(); e != nil {
		return "", e
	}
	fence, release, e := s.acquire(input.Ritual)
	if e != nil {
		return "", e
	}
	notify := false
	defer func() {
		release()
		if notify {
			select {
			case s.wake <- struct{}{}:
			default:
			}
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	id := input.ID()
	path := filepath.Join(s.dir, id+".json")
	if e := os.MkdirAll(s.dir, 0700); e != nil {
		return "", e
	}
	claim, e := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return "", e
	}
	defer claim.Close()
	if e = unix.Flock(int(claim.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return "", fmt.Errorf("extraction request busy")
	}
	defer unix.Flock(int(claim.Fd()), unix.LOCK_UN)
	if b, e := os.ReadFile(path); e == nil {
		var j Job
		if json.Unmarshal(b, &j) != nil || j.ID != id {
			return "", fmt.Errorf("extraction job unreadable")
		}
		if j.Version != 1 || j.Replay || j.OwnershipRevision != fence.Revision || j.State == "uncertain" || j.State == "refused" {
			return "", fmt.Errorf("existing extraction held; reconciliation required, replay=false")
		}
		notify = j.State == "queued" || j.State == "verified"
		return id, nil
	} else if !os.IsNotExist(e) {
		return "", e
	}
	j := Job{Version: 1, OwnershipRevision: fence.Revision, ID: id, Input: input, State: "queued", Started: time.Now().UTC()}
	if e := s.save(j); e != nil {
		return "", e
	}
	notify = true
	return id, nil
}
func (s *Service) Start() {
	go func() {
		s.sweep()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
				s.sweep()
			}
		}
	}()
}
func (s *Service) sweep() {
	entries, e := os.ReadDir(s.dir)
	if e != nil {
		return
	}
	for _, entry := range entries {
		if s.ctx.Err() != nil {
			return
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		s.run(filepath.Join(s.dir, entry.Name()))
	}
}
func (s *Service) run(path string) {
	// An OS lock prevents a second app process from claiming the same request.
	f, e := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return
	}
	defer f.Close()
	if unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	s.mu.Lock()
	b, e := os.ReadFile(path)
	var j Job
	if e != nil || json.Unmarshal(b, &j) != nil || !s.validIdentity(j) || !s.cfg.Enabled(j.Input.Ritual) {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if j.State == "completed" || j.State == "refused" || j.State == "uncertain" || j.State == "empty" {
		return
	}
	if j.Version != 1 || j.Replay || j.OwnershipRevision == 0 {
		j.State, j.Reason = "uncertain", "historical or unsupported job; quarantined with replay=false"
		j.Replay = false
		_ = s.save(j)
		return
	}
	fence, release, err := s.acquire(j.Input.Ritual)
	if err != nil {
		return
	}
	defer release()
	if fence.Revision != j.OwnershipRevision {
		j.State, j.Reason = "refused", "ownership revision changed; stale job cannot publish or execute"
		_ = s.save(j)
		return
	}
	if j.State == "running" {
		j.State = "uncertain"
		j.Reason = "interrupted execution; owner review required"
		if s.save(j) != nil {
			return
		}
	}
	if j.State == "uncertain" {
		j.Reason = "interrupted or uncertain execution; owner review required"
		s.report(j)
		return
	}
	if j.State != "queued" && j.State != "verified" {
		return
	}
	finish := func(state, reason string) {
		j.State = state
		j.Reason = reason
		j.Finished = time.Now().UTC()
		if s.save(j) == nil {
			s.report(j)
		}
	}
	if e = s.fresh(j.Input); e != nil {
		finish("refused", "source or domain context changed before publication")
		return
	}
	if j.State == "queued" {
		if s.runner == nil || s.runner.ValidateExtractionDuty(j.Input.Ritual) != nil {
			finish("refused", "extraction authority unavailable")
			return
		}
		prompt, e := j.Input.Prompt()
		if e != nil {
			finish("refused", e.Error())
			return
		}
		j.State = "running"
		j.Reason = "execution in progress; do not replay"
		if s.save(j) != nil {
			return
		}
		if e = s.report(j); e != nil {
			finish("refused", "run report unavailable")
			return
		}
		res, e := s.runner.Run(s.ctx, hermes.Request{MigratedDuty: "extractor/" + j.Input.Ritual, Prompt: prompt})
		j.Execution = res.Extraction
		j.Model = res.Model
		if e != nil || !res.DutyVerified() {
			reason := "bounded execution not verified; owner review required"
			var refusal *hermes.Refusal
			if errors.As(e, &refusal) {
				reason += ": " + refusal.Reason
			}
			finish("uncertain", reason)
			return
		}
		j.Model = res.Model
		j.SpentUSD = res.SpentUSD
		candidates, e := ValidateReply(j.Input, res.Reply)
		if e != nil {
			finish("refused", e.Error())
			return
		}
		j.Candidates = candidates
		j.Summary = replySummary(res.Reply)
		j.State = "verified"
		j.Reason = "execution and candidate contract verified; pending publication"
		if s.save(j) != nil {
			return
		} // durable verified candidates precede any inbox writes
	}
	if e = s.fresh(j.Input); e != nil {
		finish("refused", "source or domain context changed before publication")
		return
	}
	for n, p := range j.Candidates {
		if p.ExtractionSnapshot == "" || p.ExtractionSnapshot != j.Input.snapshot(p) {
			finish("uncertain", "candidate snapshot absent or changed; reconciliation required, replay=false")
			return
		}
		if _, e = s.ap.ProposeOnce(p); e != nil {
			finish("uncertain", "candidate publication incomplete or snapshot conflict; reconciliation required, replay=false")
			return
		}
		j.Published = n + 1
		if s.save(j) != nil {
			return
		}
	}
	// An empty candidate set is a legitimate model judgement, not a filed batch.
	// Reporting it as "candidates filed" would be a false green: nothing was
	// published and the owner has nothing to review. Record it as its own
	// non-publication state and keep the model's own explanation as evidence.
	if len(j.Candidates) == 0 {
		finish("empty", "no candidates proposed: "+j.Summary)
		return
	}
	finish("completed", "candidates filed for owner review; no vault writes")
}
func (s *Service) fresh(i Input) error {
	root, e := os.OpenRoot(s.vault)
	if e != nil {
		return e
	}
	defer root.Close()
	for name, old := range i.Context {
		if strings.HasPrefix(name, "system/") {
			b, e := root.ReadFile(name)
			if e != nil || string(b) != old {
				return errors.New("stale domain context")
			}
		}
	}
	if i.Ritual != "ooda-email" {
		for _, d := range i.Documents {
			b, e := root.ReadFile(d.Name)
			if e != nil || string(b) != d.Text {
				return errors.New("stale source")
			}
		}
	}
	return nil
}
func (s *Service) report(j Job) error {
	if s.harness == "" {
		return nil
	}
	outcome := j.State
	if j.State == "uncertain" {
		outcome = "error"
	}
	if j.State == "refused" {
		outcome = "error"
	}
	if j.State == "empty" {
		outcome = "empty"
	}
	names := []string{}
	for _, d := range j.Input.Documents {
		names = append(names, d.Name)
	}
	finished := ""
	if !j.Finished.IsZero() {
		finished = j.Finished.Format(time.RFC3339)
	}
	execution, _ := json.Marshal(j.Execution)
	report := (&mdfm.Writer{}).Set("run", "manifest-"+j.ID[:20]).Set("spirit", "extractor").Set("ritual", j.Input.Ritual).Set("executor", "manifest").Set("finished", finished).Set("portal", "lab-sparks").Set("request", strings.Join(names, ", ")).Set("started", j.Started.Format(time.RFC3339)).Set("outcome", outcome).Set("model", j.Model).SetRaw("items_written", fmt.Sprint(j.Published)).SetRaw("charge_spent_usd", fmt.Sprint(j.SpentUSD)).String("## Outcome\n\n" + j.Reason + "\n\nExecution receipt: " + string(execution) + "\n")
	return atomic(filepath.Join(s.harness, "artifacts", "runs", j.Started.Format("2006-01-02")+"-extractor-manifest-"+j.ID[:20]+".md"), []byte(report))
}

// ReadInput assembles an explicit bounded snapshot through an os.Root, including
// the domain records that the old ritual read with casts. No arbitrary read tool
// or filesystem path is handed to the model.
func ReadInput(vault, ritual string, documents []Document) (Input, error) {
	i := Input{Ritual: ritual, Documents: documents, Context: map[string]string{}}
	if !validRitual(ritual) {
		return i, fmt.Errorf("unsupported extraction ritual")
	}
	root, e := os.OpenRoot(vault)
	if e != nil {
		return i, e
	}
	defer root.Close()
	domain := "realestate"
	if ritual == "aion" {
		domain = "aion"
	}
	names := []string{"system/" + domain + "/backlog.md", "system/" + domain + "/people.md"}
	if ritual == "aion" {
		names = append(names, "system/aion/heuristics.md")
	}
	for _, name := range names {
		b, e := root.ReadFile(name)
		if e != nil {
			return i, fmt.Errorf("domain context unavailable: %s", name)
		}
		i.Context[name] = string(b)
	}
	if ritual != "aion" {
		for _, dir := range []string{"system/realestate/properties", "system/realestate/contractors", "system/realestate/contracts"} {
			err := fs.WalkDir(root.FS(), dir, func(name string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				if entry.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("uncertain domain symlink")
				}
				if !strings.HasSuffix(name, ".md") {
					return nil
				}
				b, err := root.ReadFile(name)
				if err != nil {
					return err
				}
				i.Context[name] = string(b)
				b, _ = json.Marshal(i)
				if len(b) > maxInputBytes {
					return fmt.Errorf("domain context exceeds bound")
				}
				return nil
			})
			if err != nil {
				return i, fmt.Errorf("canonical domain context unavailable: %w", err)
			}
		}
	}
	if ritual != "ooda-email" {
		for n, d := range i.Documents {
			if filepath.ToSlash(filepath.Clean(d.Name)) != d.Name || !strings.HasSuffix(d.Name, ".md") || strings.Contains(d.Name, "..") || strings.HasPrefix(d.Name, "system/") || strings.HasPrefix(d.Name, "extrinsic/") || filepath.IsAbs(d.Name) {
				return i, fmt.Errorf("explicit transcript log path required")
			}
			b, e := root.ReadFile(d.Name)
			if e != nil {
				return i, fmt.Errorf("source unavailable")
			}
			i.Documents[n].Text = string(b)
		}
	}

	return i, i.Validate()
}
