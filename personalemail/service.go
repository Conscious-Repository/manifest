package personalemail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"manifest/approvals"
	"manifest/connectorhandoff"
	"manifest/gmailauth"
	"manifest/gmailsync"
)

type MailboxConfig struct {
	Account   string `json:"account"`
	Sync      bool   `json:"sync"`
	Extract   bool   `json:"extract"`
	Workspace string `json:"workspace"`
}
type Config struct {
	Sync          *bool           `json:"sync,omitempty"`
	ExtraAccounts []MailboxConfig `json:"extraAccounts,omitempty"`
	Enabled       bool            `json:"enabled"`
	Account       string          `json:"account"`
	LegacyRoot    string          `json:"legacyRoot"`
	DataDir       string          `json:"dataDir"`
	Index         string          `json:"index"`
	Extract       bool            `json:"extract"`
	Workspace     string          `json:"workspace"`
	BackfillDays  int             `json:"backfillDays"`
}
type Thread struct {
	Status     string `json:"status"`
	ProposalID string `json:"proposalId,omitempty"`
	LastID     string `json:"lastId,omitempty"`
	LastMS     int64  `json:"lastMS,omitempty"`
	Replay     bool   `json:"replay"`
}
type MailboxState struct {
	Watermark time.Time                            `json:"watermark"`
	Receipt   connectorhandoff.EmailIdentityReport `json:"receipt"`
	Threads   map[string]Thread                    `json:"threads"`
}
type State struct {
	ExtraAccounts map[string]MailboxState              `json:"extraAccounts,omitempty"`
	Activation    Plan                                 `json:"activation"`
	Version       int                                  `json:"version"`
	PlanHash      string                               `json:"planHash"`
	Revision      uint64                               `json:"revision"`
	ConfigHash    string                               `json:"configHash"`
	BindingHash   string                               `json:"bindingHash"`
	Watermark     time.Time                            `json:"watermark"`
	Receipt       connectorhandoff.EmailIdentityReport `json:"receipt"`
	Threads       map[string]Thread                    `json:"threads"`
}
type Mailbox interface {
	ThreadIDsSince(context.Context, time.Time, int) ([]string, error)
	ThreadFull(context.Context, string) (string, []gmailsync.Msg, error)
}
type Contacts interface {
	gmailsync.Resolver
	Refresh() error
	Paths(string) ([]string, error)
}
type Service struct {
	Config        Config
	Contacts      Contacts
	Approvals     *approvals.Store
	Open          func(context.Context, string) (Mailbox, error)
	extraBindings func() ([]gmailauth.MailboxBinding, error)
	persist       func(State) error
	binding       func(string) (string, bool, bool, string, error)
}

func New(c Config, idx Contacts, ap *approvals.Store) *Service {
	return &Service{Config: c, Contacts: idx, Approvals: ap, Open: func(ctx context.Context, account string) (Mailbox, error) {
		src, err := gmailauth.New().ReadSource(ctx, account)
		if err != nil {
			return nil, err
		}
		return gmailsync.NewMailboxClient(src, account), nil
	}, binding: gmailauth.PrimaryBinding, extraBindings: gmailauth.ExtraBindings}
}
func (c Config) hash() string {
	c.Enabled = false
	b, _ := json.Marshal(c)
	return approvals.EvidenceHash(string(b))
}
func (s *Service) path() string {
	return filepath.Join(s.Config.DataDir, "personal-email", "active.json")
}
func (s *Service) validate() (string, error) {
	c := s.Config
	if c.Account == "" || c.Account != strings.ToLower(strings.TrimSpace(c.Account)) || !filepath.IsAbs(c.LegacyRoot) || !filepath.IsAbs(c.DataDir) || !filepath.IsAbs(c.Index) || c.BackfillDays < 1 || (c.Workspace != "" && c.Workspace != "aion" && c.Workspace != "real-estate") {
		return "", fmt.Errorf("explicit primary account, absolute paths, backfill and workspace required")
	}
	// Never permit the successor cursor inside the legacy harness or index.
	rel, e := filepath.Rel(c.LegacyRoot, c.DataDir)
	if e != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return "", fmt.Errorf("successor data must be outside legacy root")
	}
	path, sync, extract, workspace, err := s.binding(c.Account)
	if err != nil {
		return "", err
	}
	if sync != (c.Sync == nil || *c.Sync) || extract != c.Extract || workspace != c.Workspace {
		return "", fmt.Errorf("primary account settings differ from worker config")
	}
	bindings, err := s.extraBindings()
	if err != nil {
		return "", err
	}
	configured := map[string]MailboxConfig{}
	for _, c := range s.Config.ExtraAccounts {
		if c.Account == s.Config.Account || c.Account == "" || c.Account != strings.ToLower(strings.TrimSpace(c.Account)) {
			return "", fmt.Errorf("invalid extra mailbox identity")
		}
		if _, ok := configured[c.Account]; ok {
			return "", fmt.Errorf("duplicate extra mailbox")
		}
		configured[c.Account] = c
	}
	if len(bindings) != len(configured) {
		return "", fmt.Errorf("every extra mailbox requires explicit successor configuration")
	}
	for _, b := range bindings {
		c, ok := configured[b.Account]
		if !ok || c.Sync != b.Sync || c.Extract != b.Extract || c.Workspace != b.Workspace {
			return "", fmt.Errorf("extra mailbox settings mismatch")
		}
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].Account < bindings[j].Account })
	b, _ := json.Marshal(bindings)
	return path + "\x00" + string(b), nil
}
func (s *Service) read() (State, error) {
	var st State
	b, err := os.ReadFile(s.path())
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(b, &st)
	if err != nil {
		return st, err
	}
	if st.Version != 1 || st.Threads == nil || st.ConfigHash != s.Config.hash() {
		return st, fmt.Errorf("invalid successor state/config")
	}
	receiptHash := quarantineHash(st)
	if st.Activation.Hash() != st.PlanHash || st.Activation.Revision+1 != st.Revision || st.Activation.ConfigHash != st.ConfigHash || st.Activation.BindingHash != st.BindingHash || st.Activation.QuarantineHash != receiptHash {
		return st, fmt.Errorf("activation receipt mismatch")
	}
	if err := validateMailbox(st.Threads, st.Receipt); err != nil {
		return st, err
	}
	if len(st.ExtraAccounts) != len(s.Config.ExtraAccounts) {
		return st, fmt.Errorf("extra mailbox state coverage mismatch")
	}
	for _, c := range s.Config.ExtraAccounts {
		m, ok := st.ExtraAccounts[c.Account]
		if !ok {
			return st, fmt.Errorf("extra mailbox missing")
		}
		if err := validateMailbox(m.Threads, m.Receipt); err != nil {
			return st, err
		}
	}
	return st, nil
}
func validateMailbox(threads map[string]Thread, receipt connectorhandoff.EmailIdentityReport) error {
	if threads == nil {
		return fmt.Errorf("missing mailbox ledger")
	}
	for _, t := range threads {
		if t.Status != "proposed" && t.Status != "synced" && t.Status != "append-pending" && t.Status != "muted" && t.Status != "uncertain" && t.Status != approvals.ReconciledUncertain {
			return fmt.Errorf("unknown lifecycle")
		}
		if t.Replay {
			return fmt.Errorf("replay forbidden")
		}
	}
	for _, r := range receipt.Threads {
		t, ok := threads[r.ThreadID]
		if !ok || r.Replay || (len(r.StopReasons) > 0 && t.Status != approvals.ReconciledUncertain) {
			return fmt.Errorf("quarantine changed")
		}
	}
	return nil
}
func write(path string, v any, exclusive bool) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".email-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	if exclusive {
		err = os.Link(f.Name(), path)
	} else {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (s *Service) save(st State) error {
	if s.persist != nil {
		return s.persist(st)
	}
	return write(s.path(), st, false)
}

// Poll holds the shared legacy dispatch lock through every effect and cursor save.
// A durable uncertain intent precedes each proposal; interruption cannot replay it.
func (s *Service) Poll(ctx context.Context) error {
	if !s.Config.Enabled {
		return nil
	}
	binding, err := s.validate()
	if err != nil {
		return err
	}
	f, release, err := connectorhandoff.AcquireFence(s.Config.LegacyRoot, "email")
	if err != nil {
		return err
	}
	defer release()
	st, err := s.read()
	if err != nil {
		return err
	}
	if f.Owner != "manifest" || f.Revision != st.Revision || f.Evidence != st.PlanHash || st.BindingHash != approvals.EvidenceHash(binding+"\x00"+s.Config.Account) {
		return fmt.Errorf("email ownership/activation mismatch")
	}
	if s.Config.Sync == nil || *s.Config.Sync {
		if err = s.pollMailbox(ctx, st); err != nil {
			return err
		}
	}
	st, err = s.read()
	if err != nil {
		return err
	}
	for _, c := range s.Config.ExtraAccounts {
		if !c.Sync {
			continue
		}
		m := st.ExtraAccounts[c.Account]
		child := *s
		child.Config.Account = c.Account
		child.Config.Extract = c.Extract
		child.Config.Workspace = c.Workspace
		child.persist = func(updated State) error {
			st.ExtraAccounts[c.Account] = MailboxState{updated.Watermark, updated.Receipt, updated.Threads}
			return s.save(st)
		}
		if err = child.pollMailbox(ctx, State{Watermark: m.Watermark, Receipt: m.Receipt, Threads: m.Threads}); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) pollMailbox(ctx context.Context, st State) error {
	if s.Contacts == nil || s.Approvals == nil {
		return fmt.Errorf("canonical contacts and approvals required")
	}
	if err := s.Contacts.Refresh(); err != nil {
		return err
	}
	inv, err := approvals.ReadEmailContinuityInventory(filepath.Join(s.Config.LegacyRoot, "artifacts"))
	if err != nil {
		return err
	}
	byID := map[string]approvals.ConnectorApproval{}
	byThread := map[string]bool{}
	for _, p := range inv.Items {
		byID[p.ID] = p
		byThread[p.SourceID] = true
	}
	reader, err := s.Open(ctx, s.Config.Account)
	if err != nil {
		return err
	}
	started := time.Now().UTC()
	since := st.Watermark.Add(-time.Hour)
	backfill := started.Add(-time.Duration(s.Config.BackfillDays) * 24 * time.Hour)
	if backfill.Before(since) {
		since = backfill
	}
	ids, err := reader.ThreadIDsSince(ctx, since, 100)
	if err != nil {
		return err
	}
	// Pending threads must be revisited even after the discovery window expires.
	for id, t := range st.Threads {
		if t.Status == "proposed" || t.Status == "append-pending" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if !gmailID.MatchString(id) {
			return fmt.Errorf("invalid Gmail identity")
		}
		t, exists := st.Threads[id]
		switch t.Status {
		case approvals.ReconciledUncertain, "uncertain", "muted":
			continue
		}
		if t.Status == "proposed" || t.Status == "append-pending" {
			p, ok := byID[t.ProposalID]
			want := approvals.TypeCreateVaultNote
			if t.Status == "append-pending" {
				want = approvals.TypeAppendVaultNote
			}
			if !ok || p.SourceID != id || p.Type != want {
				t.Status = "uncertain"
				st.Threads[id] = t
				if err = s.save(st); err != nil {
					return err
				}
				continue
			}
			if p.Status == "pending" {
				continue
			}
			if p.Status == "rejected" {
				t.Status = "muted"
				st.Threads[id] = t
				if err = s.save(st); err != nil {
					return err
				}
				continue
			}
			t.Status = "synced"
		}
		if t.Status == "synced" {
			p, ok := byID[t.ProposalID]
			if !ok || p.SourceID != id || p.Status != "approved" {
				t.Status = "uncertain"
				st.Threads[id] = t
				if err = s.save(st); err != nil {
					return err
				}
				continue
			}
		}
		subject, msgs, err := reader.ThreadFull(ctx, id)
		if err != nil {
			return fmt.Errorf("Gmail thread read failed; cursor retained")
		}
		if len(msgs) == 0 {
			return fmt.Errorf("empty Gmail thread")
		}
		for _, m := range msgs {
			if !gmailID.MatchString(m.ID) || m.Internal.IsZero() {
				return fmt.Errorf("invalid message anchor")
			}
		}
		sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Internal.Before(msgs[j].Internal) })
		if gmailsync.IsCalendarThread(msgs) || !known(msgs, s.Contacts, s.Config.Account) {
			continue
		}
		paths, err := s.Contacts.Paths(id)
		if err != nil {
			return err
		}
		if !exists && (byThread[id] || len(paths) > 0) {
			st.Threads[id] = Thread{Status: "uncertain"}
			if err = s.save(st); err != nil {
				return err
			}
			continue
		}
		fresh := msgs
		if exists {
			if t.Status != "synced" {
				return fmt.Errorf("unknown thread lifecycle")
			}
			anchor := -1
			for i, m := range msgs {
				if m.ID == t.LastID && m.Internal.UnixMilli() == t.LastMS {
					anchor = i
				}
			}
			if anchor < 0 || len(paths) != 1 {
				t.Status = "uncertain"
				st.Threads[id] = t
				if err = s.save(st); err != nil {
					return err
				}
				continue
			}
			fresh = msgs[anchor+1:]
			if len(fresh) == 0 {
				st.Threads[id] = t
				continue
			}
		}
		last := msgs[len(msgs)-1]
		p := approvals.Proposal{ID: approvals.EvidenceHash(s.Config.Account + "\x00" + id + "\x00" + last.ID), Agent: "ea-coordinator", Ritual: approvals.PersonalEmailRitual, GmailThreadID: id, Body: "Gmail thread `" + id + "`, through message `" + last.ID + "`. Mechanically cleaned sender text."}
		next := "proposed"
		if exists {
			p.Type = approvals.TypeAppendVaultNote
			p.ApplyPath = paths[0]
			p.Proposed = gmailsync.MessageSections(fresh, s.Contacts)
			p.Action = "Append email messages to " + p.ApplyPath
			p.RenameTo = renamePath(p.ApplyPath, last.Internal)
			next = "append-pending"
		} else {
			p.Type = approvals.TypeCreateVaultNote
			p.ApplyPath = gmailsync.NoteFilename(msgs, subject)
			// Full mailbox-local ID suffix avoids same-subject/date collisions.
			p.ApplyPath = strings.TrimSuffix(p.ApplyPath, ".md") + " " + id + ".md"
			p.Action = "Create vault note: " + p.ApplyPath
			tag := ""
			if s.Config.Extract && s.Config.Workspace != "" {
				tag = "  - " + s.Config.Workspace + "\n"
			}
			p.Proposed = "---\ncategories:\n  - sync\n" + tag + "gmail-thread-id: " + id + "\n---\n" + participants(msgs, s.Contacts, s.Config.Account) + "\n\n" + gmailsync.MessageSections(msgs, s.Contacts) + "\n"
		}
		t = Thread{Status: "uncertain", ProposalID: p.ID, LastID: last.ID, LastMS: last.Internal.UnixMilli()}
		st.Threads[id] = t
		if err = s.save(st); err != nil {
			return err
		}
		if _, err = s.Approvals.ProposeEmail(p); err != nil {
			return err
		}
		t.Status = next
		st.Threads[id] = t
		if err = s.save(st); err != nil {
			return err
		}
	}
	st.Watermark = started
	return s.save(st)
}

var gmailID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
var address = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

func known(msgs []gmailsync.Msg, r gmailsync.Resolver, own string) bool {
	for _, m := range msgs {
		for _, h := range []string{m.From, m.To, m.Cc} {
			for _, a := range address.FindAllString(h, -1) {
				if !strings.EqualFold(a, own) {
					if _, ok := r.PersonByEmail(a); ok {
						return true
					}
				}
			}
		}
	}
	return false
}
func participants(msgs []gmailsync.Msg, r gmailsync.Resolver, own string) string {
	var links []string
	seen := map[string]bool{}
	for _, m := range msgs {
		for _, h := range []string{m.From, m.To, m.Cc} {
			for _, a := range address.FindAllString(h, -1) {
				if strings.EqualFold(a, own) {
					continue
				}
				if n, ok := r.PersonByEmail(a); ok && !seen[n] {
					seen[n] = true
					links = append(links, "[["+n+"]]")
				}
			}
		}
	}
	return strings.Join(links, " ")
}

var rangeName = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})(?: - (\d{4}-\d{2}-\d{2}))? (.+)\.md$`)

func renamePath(path string, last time.Time) string {
	m := rangeName.FindStringSubmatch(filepath.Base(path))
	if m == nil {
		return ""
	}
	end := last.Format("2006-01-02")
	if m[2] > end {
		end = m[2]
	}
	prefix := m[1]
	if end > m[1] {
		prefix += " - " + end
	}
	out := filepath.ToSlash(filepath.Join(filepath.Dir(path), prefix+" "+m[3]+".md"))
	if out == path {
		return ""
	}
	return out
}
