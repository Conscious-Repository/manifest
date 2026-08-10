package todos

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/record"
)

// Store owns the vault-root `to do.md` (name configurable — todosFileName).
// Same contract as goals.Store: whole-file Load→mutate→Save, with every write
// going through the injected write func (§A3 boundary — main binds it to a
// vaultwriter knowledge-zone capability; this package never opens a file to
// write).
type Store struct {
	vaultRoot string
	name      string
	write     func(path string, data []byte) error
}

func NewStore(vaultRoot, name string, write func(path string, data []byte) error) *Store {
	if strings.TrimSpace(name) == "" {
		name = "to do.md"
	}
	if write == nil {
		write = func(string, []byte) error {
			return errors.New("todos: no vault writer injected (§A3 write boundary)")
		}
	}
	return &Store{vaultRoot: vaultRoot, name: name, write: write}
}

func (s *Store) Path() string { return filepath.Join(s.vaultRoot, s.name) }

// ArchivePath is the append-only history peer ("to do archive.md").
func (s *Store) ArchivePath() string {
	base := strings.TrimSuffix(s.name, ".md")
	return filepath.Join(s.vaultRoot, base+" archive.md")
}

// Load parses the live file (missing file → empty doc with an Inbox).
func (s *Store) Load() (*Doc, error) {
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			d := &Doc{preamble: []string{"# To Do"}}
			d.EnsureDomain(InboxName)
			return d, nil
		}
		return nil, err
	}
	return Parse(string(raw)), nil
}

func (s *Store) Save(d *Doc) error {
	d.assignIDs()
	return s.write(s.Path(), []byte(Serialize(d)))
}

// BackupOnce writes Path()+suffix with the live bytes if no such backup exists
// yet (the .pre-split pattern) — through the same §A3 boundary as every write.
func (s *Store) BackupOnce(suffix string) error {
	dst := s.Path() + suffix
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return s.write(dst, raw)
}

// SyncChecks marks todos done from daily-note ticks (id → done). Returns ids
// with no match — the caller files an approvals note per miss and the write
// is a no-op for those (never a guess).
func (s *Store) SyncChecks(updates map[string]bool, now time.Time) (missed []string, err error) {
	if len(updates) == 0 {
		return nil, nil
	}
	d, err := s.Load()
	if err != nil {
		return nil, err
	}
	changed := false
	for id, done := range updates {
		_, t := d.Find(id)
		if t == nil {
			missed = append(missed, id)
			continue
		}
		if t.Checked != done {
			t.Checked = done
			if done {
				t.Done = now.Format("2006-01-02")
			} else {
				t.Done = ""
			}
			changed = true
		}
	}
	if changed {
		err = s.Save(d)
	}
	return missed, err
}

// Sweep moves done items older than 48h (and archives nothing else) into the
// dated archive — the live file stays lean, history is preserved, nothing is
// ever deleted. Returns how many moved.
func (s *Store) Sweep(now time.Time) (int, error) {
	d, err := s.Load()
	if err != nil {
		return 0, err
	}
	cutoff := now.Add(-48 * time.Hour).Format("2006-01-02")
	var archived []string
	sweepList := func(list []*Todo, where string) []*Todo {
		var keep []*Todo
		for _, t := range list {
			if t.Checked && t.Done != "" && t.Done < cutoff {
				archived = append(archived, emitTodo(t)+" [domain:: "+where+"]")
				continue
			}
			keep = append(keep, t)
		}
		return keep
	}
	for _, dom := range d.Domains {
		dom.Todos = sweepList(dom.Todos, dom.Name)
		for _, b := range dom.Buckets {
			b.Todos = sweepList(b.Todos, dom.Name+" / "+b.Name)
		}
		// resolved issues sweep too (with their resolution note riding along)
		var keepIssues []*Issue
		for _, is := range dom.Issues {
			if is.Checked && is.Done != "" && is.Done < cutoff {
				archived = append(archived, emitIssue(is)+" [domain:: "+dom.Name+"]")
				continue
			}
			keepIssues = append(keepIssues, is)
		}
		dom.Issues = keepIssues
	}
	if len(archived) == 0 {
		return 0, nil
	}
	if err := s.appendArchive(now.Format("2006-01"), archived); err != nil {
		return 0, err
	}
	return len(archived), s.Save(d)
}

// Drop removes a todo from the live file and records it in the archive with a
// [dropped:: date] stamp (the stale-nudge's "drop" — reversible by hand).
// Find locates loose AND bucket todos, so removal must cover both — dropping
// a bucket todo used to archive a copy while the live line survived.
func (s *Store) Drop(id string, now time.Time) error {
	d, err := s.Load()
	if err != nil {
		return err
	}
	dom, t := d.Find(id)
	if t == nil {
		return os.ErrNotExist
	}
	where := dom.Name
	removed := false
	var keep []*Todo
	for _, o := range dom.Todos {
		if o != t {
			keep = append(keep, o)
		} else {
			removed = true
		}
	}
	dom.Todos = keep
	if !removed {
		for _, b := range dom.Buckets {
			var kb []*Todo
			for _, o := range b.Todos {
				if o != t {
					kb = append(kb, o)
				} else {
					removed = true
					where = dom.Name + " / " + b.Name // the Sweep's archive convention
				}
			}
			b.Todos = kb
		}
	}
	if !removed {
		return os.ErrNotExist // never archive a line the live file keeps
	}
	line := emitTodo(t) + " [dropped:: " + now.Format("2006-01-02") + "] [domain:: " + where + "]"
	if err := s.appendArchive(now.Format("2006-01"), []string{line}); err != nil {
		return err
	}
	return s.Save(d)
}

// ArchiveLines appends raw lines under a `## <section>` archive heading —
// the goals-triage sweep records dropped tasks here (nothing silently dropped).
func (s *Store) ArchiveLines(section string, lines []string) error {
	return s.appendArchive(section, lines)
}

// appendArchive appends lines under a `## <section>` heading in the archive
// file (created on first use) — the kernel's append-only archive policy.
func (s *Store) appendArchive(section string, lines []string) error {
	path := s.ArchivePath()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := record.AppendSection(string(raw), "# To Do — archive", section, lines)
	return s.write(path, []byte(content))
}
