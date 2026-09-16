package personalemail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/connectorhandoff"
	"manifest/gmailauth"
	"manifest/gmailsync"
)

type contacts struct{ paths map[string][]string }

func (c *contacts) Refresh() error { return nil }
func (c *contacts) PersonByEmail(a string) (string, bool) {
	return "Friend", strings.EqualFold(a, "friend@example.com")
}
func (c *contacts) Paths(id string) ([]string, error) { return c.paths[id], nil }

type mailbox struct {
	ids   []string
	msgs  map[string][]gmailsync.Msg
	fail  bool
	calls int
	since time.Time
}

func (m *mailbox) ThreadIDsSince(_ context.Context, t time.Time, _ int) ([]string, error) {
	m.since = t
	if m.fail {
		return nil, fmt.Errorf("failed listing")
	}
	return m.ids, nil
}
func (m *mailbox) ThreadFull(_ context.Context, id string) (string, []gmailsync.Msg, error) {
	m.calls++
	return "Subject", m.msgs[id], nil
}
func put(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func fence(t *testing.T, s *Service, rev uint64, previous, owner, hash string) {
	t.Helper()
	action := "transfer"
	if owner == "excalibur" {
		action = "rollback"
	}
	b, _ := json.Marshal(connectorhandoff.RecordFence{Version: 1, Revision: rev, Duty: "ea-coordinator/email-sync", PreviousOwner: previous, Owner: owner, Action: action, Evidence: hash, At: time.Now().UTC().Format(time.RFC3339Nano)})
	put(t, filepath.Join(s.Config.LegacyRoot, "vessel/state/dispatch-fence/ea-coordinator/email-sync", fmt.Sprintf("%020d.json", rev)), b)
}
func setup(t *testing.T) (*Service, *mailbox) {
	t.Helper()
	root := t.TempDir()
	data := t.TempDir()
	ap := approvals.NewStore(filepath.Join(root, "artifacts"))
	c := Config{Account: "owner@example.com", LegacyRoot: root, DataDir: data, Index: filepath.Join(data, "index.db"), BackfillDays: 30, Extract: true, Workspace: "aion"}
	s := New(c, &contacts{paths: map[string][]string{}}, ap)
	s.extraBindings = func() ([]gmailauth.MailboxBinding, error) { return nil, nil }
	s.configuredAccounts = func() ([]string, error) { return nil, nil }
	s.binding = func(string) (string, bool, bool, string, error) {
		return "/existing/primary-token", true, true, "aion", nil
	}
	put(t, filepath.Join(root, "vessel/state/email/state.json"), []byte(`{"watermark":"2026-09-01T00:00:00Z","threads":{}}`))
	m := &mailbox{msgs: map[string][]gmailsync.Msg{}}
	s.Open = func(context.Context, string) (Mailbox, error) { return m, nil }
	return s, m
}
func activate(t *testing.T, s *Service) {
	t.Helper()
	p, err := s.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	fence(t, s, 1, "excalibur", "manifest", p.Hash())
	if _, err = s.Apply(p.Hash(), 0); err != nil {
		t.Fatal(err)
	}
	s.Config.Enabled = true
}
func msg(id, from string, day int) gmailsync.Msg {
	return gmailsync.Msg{ID: id, From: from, To: "owner@example.com", Body: "hello\n-- \nsignature", Internal: time.Date(2026, 9, day, 12, 0, 0, 0, time.UTC)}
}
func TestWorkerCreateGrowthAndNoReplay(t *testing.T) {
	s, m := setup(t)
	activate(t, s)
	m.ids = []string{"new", "new", "unknown", "calendar"}
	m.msgs["new"] = []gmailsync.Msg{msg("m1", "Friend <friend@example.com>", 1)}
	m.msgs["unknown"] = []gmailsync.Msg{msg("u1", "stranger@example.com", 1)}
	cal := msg("c1", "friend@example.com", 1)
	cal.HasCalendar = true
	m.msgs["calendar"] = []gmailsync.Msg{cal}
	vault := t.TempDir()
	put(t, filepath.Join(vault, "sentinel"), []byte("unchanged"))
	legacyPath := filepath.Join(s.Config.LegacyRoot, "vessel/state/email/state.json")
	before, _ := os.ReadFile(legacyPath)
	if err := s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	ps := s.Approvals.List("pending")
	if len(ps) != 1 {
		t.Fatal(ps)
	}
	p := ps[0]
	if !strings.Contains(p.Proposed, "  - aion") || !strings.Contains(p.Proposed, "[[Friend]]") || strings.Contains(p.Proposed, "signature") {
		t.Fatal(p.Proposed)
	}
	if err := s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.Approvals.List("pending")) != 1 {
		t.Fatal("duplicate")
	}
	// Simulate an existing canonical decision and indexed effect; never Confirm,
	// AutoApply, or vaultwriter during these tests.
	pending := filepath.Join(s.Config.LegacyRoot, "artifacts/approvals/pending", p.ID+".md")
	approved := filepath.Join(s.Config.LegacyRoot, "artifacts/approvals/approved", p.ID+".md")
	if err := os.Rename(pending, approved); err != nil {
		t.Fatal(err)
	}
	s.Contacts.(*contacts).paths["new"] = []string{"log/2026-09-01 owner title.md"}
	m.msgs["new"] = append(m.msgs["new"], msg("m2", "friend@example.com", 2))
	if err := s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	ps = s.Approvals.List("pending")
	if len(ps) != 1 || ps[0].Type != approvals.TypeAppendVaultNote || strings.Contains(ps[0].Proposed, "2026-09-01") || ps[0].RenameTo != "log/2026-09-01 - 2026-09-02 owner title.md" {
		t.Fatal(ps)
	}
	if err := s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.Approvals.List("pending")) != 1 {
		t.Fatal("append replay")
	}
	after, _ := os.ReadFile(legacyPath)
	if string(before) != string(after) {
		t.Fatal("external cursor changed")
	}
	entries, _ := os.ReadDir(vault)
	if len(entries) != 1 {
		t.Fatal("vault modified")
	}
}
func TestDisabledFailureCrashRollbackAndExclusion(t *testing.T) {
	s, m := setup(t)
	if err := s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path()); !os.IsNotExist(err) {
		t.Fatal("disabled state write")
	}
	activate(t, s)
	st, _ := s.read()
	old := st.Watermark
	m.fail = true
	if s.Poll(context.Background()) == nil {
		t.Fatal("failure lost")
	}
	st, _ = s.read()
	if st.Watermark != old {
		t.Fatal("advanced on failed discovery")
	}
	m.fail = false
	// Durable intent can exist with or without a filed artifact after a crash.
	st.Threads["crash"] = Thread{Status: "uncertain", ProposalID: "intent"}
	if err := s.save(st); err != nil {
		t.Fatal(err)
	}
	m.ids = []string{"crash"}
	if err := s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.calls != 0 || len(s.Approvals.List("pending")) != 0 {
		t.Fatal("replayed intent")
	}
	_, release, err := connectorhandoff.AcquireFence(s.Config.LegacyRoot, "email")
	if err != nil {
		t.Fatal(err)
	}
	if s.Poll(context.Background()) == nil {
		t.Fatal("dual writer")
	}
	release()
	fence(t, s, 2, "manifest", "excalibur", strings.Repeat("b", 64))
	if s.Poll(context.Background()) == nil {
		t.Fatal("poll after rollback")
	}
}
func TestApplyCASAndEvidenceDrift(t *testing.T) {
	for _, mode := range []string{"legacy", "hash", "state", "config", "revision", "extra", "twice"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := setup(t)
			p, err := s.Prepare()
			if err != nil {
				t.Fatal(err)
			}
			if mode != "legacy" {
				fence(t, s, 1, "excalibur", "manifest", p.Hash())
			}
			expected := p.Hash()
			rev := uint64(0)
			switch mode {
			case "hash":
				expected = strings.Repeat("c", 64)
			case "state":
				put(t, filepath.Join(s.Config.LegacyRoot, "vessel/state/email/state.json"), []byte(`{"watermark":"2026-08-01T00:00:00Z","threads":{}}`))
			case "config":
				s.Config.BackfillDays++
			case "revision":
				rev = 1
			case "extra":
				put(t, filepath.Join(s.Config.LegacyRoot, "vessel/state/email/state-extra.json"), []byte(`{}`))
			case "twice":
				if _, err := s.Apply(expected, rev); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Apply(expected, rev); err == nil {
				t.Fatal("invalid apply accepted")
			}
		})
	}
}
func TestQuarantineExactMismatchAndCleanThread(t *testing.T) {
	s, m := setup(t)
	// Exact production associations are retained, never repaired or merged.
	raw := `{"watermark":"2026-09-01T00:00:00Z","threads":{"19fdd26132e9cc60":{"status":"proposed","proposal_id":"2a71f54cd7b3"},"19fdd282744d15a0":{"status":"proposed","proposal_id":"d75af2b821e6"}}}`
	put(t, filepath.Join(s.Config.LegacyRoot, "vessel/state/email/state.json"), []byte(raw))
	for _, id := range []string{"2a71f54cd7b3", "d75af2b821e6", "approvalonly"} {
		thread := "19fdd282744d15a0"
		if id == "approvalonly" {
			thread = "absent"
		}
		p := approvals.Proposal{ID: id, Type: approvals.TypeCreateVaultNote, Action: "Create " + id, Agent: "ea-coordinator", Ritual: "email-sync", GmailThreadID: thread, ApplyPath: "2026-09-01 " + id + ".md", Body: "````proposed\n---\ngmail-thread-id: " + thread + "\n---\ntext\n````"}
		if _, err := s.Approvals.Propose(p); err != nil {
			t.Fatal(err)
		}
	}
	activate(t, s)
	st, err := s.read()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Threads) != 3 {
		t.Fatal(st)
	}
	for _, row := range st.Threads {
		if row.Status != approvals.ReconciledUncertain || row.Replay {
			t.Fatal(row)
		}
	}
	if st.Receipt.Threads[0].LegacyProposalID != "2a71f54cd7b3" || st.Receipt.Threads[0].Claims[0].ThreadID != "19fdd282744d15a0" {
		t.Fatal("identity repaired")
	}
	m.ids = []string{"19fdd26132e9cc60", "19fdd282744d15a0", "absent", "clean"}
	m.msgs["clean"] = []gmailsync.Msg{msg("cleanmsg", "friend@example.com", 3)}
	if err = s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.calls != 1 || len(s.Approvals.List("pending")) != 4 {
		t.Fatal("quarantine blocked clean thread or replayed")
	}
	st, _ = s.read()
	st.Threads["absent"] = Thread{Status: "synced"}
	s.save(st)
	if s.Poll(context.Background()) == nil {
		t.Fatal("quarantine became replayable")
	}
}

func TestCrashAfterPublicationAndMissingAnchor(t *testing.T) {
	s, m := setup(t)
	activate(t, s)
	m.ids = []string{"thread"}
	m.msgs["thread"] = []gmailsync.Msg{msg("m1", "friend@example.com", 1)}
	if err := s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := s.read()
	row := st.Threads["thread"]
	row.Status = "uncertain"
	st.Threads["thread"] = row
	if err := s.save(st); err != nil {
		t.Fatal(err)
	}
	calls := m.calls
	if err := s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.calls != calls || len(s.Approvals.List("pending")) != 1 {
		t.Fatal("crash after publication replayed")
	}
	// An approved effect does not permit timestamp-only fallback if its exact
	// Gmail anchor disappears. Preserve the decision and quarantine growth.
	p := s.Approvals.List("pending")[0]
	base := filepath.Join(s.Config.LegacyRoot, "artifacts/approvals")
	if err := os.Rename(filepath.Join(base, "pending", p.ID+".md"), filepath.Join(base, "approved", p.ID+".md")); err != nil {
		t.Fatal(err)
	}
	st, _ = s.read()
	row.Status = "synced"
	st.Threads["thread"] = row
	s.save(st)
	s.Contacts.(*contacts).paths["thread"] = []string{"log/2026-09-01 title.md"}
	m.msgs["thread"] = []gmailsync.Msg{msg("m2", "friend@example.com", 2)}
	if err := s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ = s.read()
	if st.Threads["thread"].Status != "uncertain" || len(s.Approvals.List("pending")) != 0 {
		t.Fatal("missing anchor replayed")
	}
}

func TestExtraMailboxCoverageRoutingAndQuarantine(t *testing.T) {
	for _, sync := range []bool{true, false} {
		t.Run(fmt.Sprint(sync), func(t *testing.T) {
			s, primary := setup(t)
			s.Config.ExtraAccounts = []MailboxConfig{{Account: "extra@example.com", Sync: sync}}
			s.extraBindings = func() ([]gmailauth.MailboxBinding, error) {
				return []gmailauth.MailboxBinding{{Account: "extra@example.com", Path: "/existing/extra-token", Sync: sync}}, nil
			}
			put(t, filepath.Join(s.Config.LegacyRoot, "vessel/state/email", gmailauth.ExtraStateFilename("extra@example.com")), []byte(`{"watermark":"2026-09-01T00:00:00Z","threads":{"historical":{"status":"synced","proposal_id":"extraold","last_msg_id":"oldmsg","last_internal_ms":1}}}`))
			p := approvals.Proposal{ID: "extraold", Type: approvals.TypeCreateVaultNote, Action: "Create historical", Agent: "ea-coordinator", Ritual: "email-sync", GmailThreadID: "historical", ApplyPath: "2026-09-01 historical.md", Body: "````proposed\n---\ngmail-thread-id: historical\n---\ntext\n````"}
			if _, err := s.Approvals.Propose(p); err != nil {
				t.Fatal(err)
			}
			base := filepath.Join(s.Config.LegacyRoot, "artifacts/approvals")
			if err := os.Rename(filepath.Join(base, "pending", "extraold.md"), filepath.Join(base, "approved", "extraold.md")); err != nil {
				t.Fatal(err)
			}
			secondary := &mailbox{ids: []string{"historical", "extra-new"}, msgs: map[string][]gmailsync.Msg{"extra-new": {msg("exmsg", "friend@example.com", 3)}}}
			s.Open = func(_ context.Context, account string) (Mailbox, error) {
				switch account {
				case "owner@example.com":
					return primary, nil
				case "extra@example.com":
					return secondary, nil
				}
				t.Fatal("unbound account")
				return nil, nil
			}
			activate(t, s)
			if err := s.Poll(context.Background()); err != nil {
				t.Fatal(err)
			}
			st, err := s.read()
			if err != nil {
				t.Fatal(err)
			}
			if st.Threads["historical"].Status != approvals.ReconciledUncertain || st.ExtraAccounts["extra@example.com"].Threads["historical"].Status != approvals.ReconciledUncertain {
				t.Fatal("extra state repaired historical quarantine")
			}
			ps := s.Approvals.List("pending")
			if sync {
				if len(ps) != 1 || secondary.calls != 1 || strings.Contains(ps[0].Proposed, "  - aion") {
					t.Fatal("extra routing failed", ps)
				}
			} else if len(ps) != 0 || secondary.calls != 0 {
				t.Fatal("paused extra polled")
			}
			s.Config.ExtraAccounts = nil
			if s.Poll(context.Background()) == nil {
				t.Fatal("dropped extra coverage accepted")
			}
		})
	}
}

func TestOrphanQuarantinePreservedAcrossActivationAndPoll(t *testing.T) {
	s, m := setup(t)
	path := filepath.Join(s.Config.LegacyRoot, "vessel/state/email/state-ben-ooda-group.json")
	raw := []byte(`{"watermark":"2026-09-01T00:00:00Z","threads":{"historical":{"status":"proposed","proposal_id":"original"}}}`)
	put(t, path, raw)
	p, st, err := s.prepare(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Orphans) != 1 || len(st.ExtraAccounts) != 0 || len(st.Threads) != 0 {
		t.Fatal(st)
	}
	orphan := st.Orphans[0]
	if orphan.Path != path || orphan.LegacyHash != approvals.EvidenceHash(string(raw)) || orphan.Disposition != connectorhandoff.LegacyOrphanQuarantined || orphan.Replay || !connectorhandoff.ValidHash(orphan.ThreadHashes["historical"]) {
		t.Fatal(orphan)
	}
	if _, err := os.Stat(s.path()); !os.IsNotExist(err) {
		t.Fatal("prepare wrote state")
	}
	fence(t, s, 1, "excalibur", "manifest", p.Hash())
	if _, err := s.Apply(p.Hash(), 0); err != nil {
		t.Fatal(err)
	}
	s.Config.Enabled = true
	if err := s.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, err := s.read()
	if err != nil {
		t.Fatal(err)
	}
	beforeReceipt, _ := json.Marshal(st.Orphans)
	afterReceipt, _ := json.Marshal(after.Orphans)
	if string(beforeReceipt) != string(afterReceipt) || m.calls != 0 || len(s.Approvals.List("pending")) != 0 {
		t.Fatal("orphan changed or produced work")
	}
	unchanged, _ := os.ReadFile(path)
	if string(unchanged) != string(raw) {
		t.Fatal("orphan source changed")
	}
	after.Orphans = nil
	if err := s.save(after); err != nil {
		t.Fatal(err)
	}
	if _, err := s.read(); err == nil {
		t.Fatal("orphan receipt deletion accepted")
	}
}

func TestOrphanCoverageRefusesConnectedConfiguredAndAmbiguous(t *testing.T) {
	for _, mode := range []string{"live-match", "live-other", "settings-match", "settings-other", "inventory-error", "ambiguous-name", "ambiguous-state", "duplicate-state-key", "symlink", "missing-active-state", "slug-collision"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := setup(t)
			path := filepath.Join(s.Config.LegacyRoot, "vessel/state/email", gmailauth.ExtraStateFilename("extra@example.com"))
			raw := []byte(`{"watermark":"2026-09-01T00:00:00Z","threads":{}}`)
			switch mode {
			case "live-match", "live-other":
				account := "extra@example.com"
				if mode == "live-other" {
					account = "other@example.com"
				}
				s.extraBindings = func() ([]gmailauth.MailboxBinding, error) {
					return []gmailauth.MailboxBinding{{Account: account, Sync: true}}, nil
				}
			case "settings-match", "settings-other":
				account := "extra@example.com"
				if mode == "settings-other" {
					account = "other@example.com"
				}
				s.configuredAccounts = func() ([]string, error) { return []string{account}, nil }
			case "inventory-error":
				s.configuredAccounts = func() ([]string, error) { return nil, fmt.Errorf("unavailable") }
			case "ambiguous-name":
				path = filepath.Join(filepath.Dir(path), "state-UNKNOWN.json")
			case "ambiguous-state":
				raw = []byte(`{"account":"other@example.com","watermark":"2026-09-01T00:00:00Z","threads":{}}`)
			case "duplicate-state-key":
				raw = []byte(`{"watermark":"2026-09-01T00:00:00Z","threads":{},"threads":{}}`)
			case "missing-active-state":
				s.Config.ExtraAccounts = []MailboxConfig{{Account: "other@example.com", Sync: true}}
				s.extraBindings = func() ([]gmailauth.MailboxBinding, error) {
					return []gmailauth.MailboxBinding{{Account: "other@example.com", Sync: true}}, nil
				}
			case "slug-collision":
				s.Config.ExtraAccounts = []MailboxConfig{{Account: "a-b@example.com"}, {Account: "a.b@example.com"}}
				s.extraBindings = func() ([]gmailauth.MailboxBinding, error) {
					return []gmailauth.MailboxBinding{{Account: "a-b@example.com"}, {Account: "a.b@example.com"}}, nil
				}
			}
			put(t, path, raw)
			if mode == "symlink" {
				target := filepath.Join(t.TempDir(), "state.json")
				put(t, target, raw)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Prepare(); err == nil {
				t.Fatal("unsafe orphan exclusion accepted")
			}
		})
	}
}

func TestOrphanEvidenceDriftRefusesApply(t *testing.T) {
	for _, mode := range []string{"changed", "removed", "added", "connected", "configured"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := setup(t)
			path := filepath.Join(s.Config.LegacyRoot, "vessel/state/email/state-old-example-com.json")
			raw := []byte(`{"watermark":"2026-09-01T00:00:00Z","threads":{}}`)
			put(t, path, raw)
			p, err := s.Prepare()
			if err != nil {
				t.Fatal(err)
			}
			fence(t, s, 1, "excalibur", "manifest", p.Hash())
			switch mode {
			case "changed":
				put(t, path, append(raw, '\n'))
			case "removed":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "added":
				put(t, filepath.Join(filepath.Dir(path), "state-another-example-com.json"), raw)
			case "connected":
				s.extraBindings = func() ([]gmailauth.MailboxBinding, error) {
					return []gmailauth.MailboxBinding{{Account: "old@example.com"}}, nil
				}
			case "configured":
				s.configuredAccounts = func() ([]string, error) { return []string{"old@example.com"}, nil }
			}
			if _, err := s.Apply(p.Hash(), 0); err == nil {
				t.Fatal("orphan evidence drift accepted")
			}
		})
	}
}
