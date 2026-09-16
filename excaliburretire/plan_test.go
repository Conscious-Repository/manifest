package excaliburretire

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/connectorhandoff"
	"manifest/personalemail"
	"manifest/transcriptsync"
)

type manager struct {
	retired  bool
	calls    int
	fail     bool
	mutate   func()
	blockers []string
}

func (m *manager) Snapshot() ([]Service, []string, error) {
	active, enabled := "active", "enabled"
	if m.retired {
		active, enabled = "inactive", "masked"
	}
	return []Service{{Name: engineUnit, Active: active, Enabled: enabled}}, m.blockers, nil
}
func (m *manager) Retire() error {
	m.calls++
	if m.fail {
		return fmt.Errorf("failed")
	}
	m.retired = true
	if m.mutate != nil {
		m.mutate()
	}
	return nil
}
func put(t *testing.T, path string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	write(t, path, b)
}
func write(t *testing.T, path string, b []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func fixture(t *testing.T) (Config, *manager) {
	t.Helper()
	c := Config{t.TempDir(), t.TempDir()}
	m := &manager{}
	token := filepath.Join(c.DataDir, "token")
	t.Setenv("GMAIL_TOKEN", token)
	t.Setenv("EMAIL_ACCOUNTS_FILE", filepath.Join(c.DataDir, "settings"))
	t.Setenv("GMAIL_ACCOUNTS_DIR", filepath.Join(c.DataDir, "accounts"))
	put(t, token, map[string]any{"email": "private@example.com", "token": map[string]string{}})
	put(t, filepath.Join(c.DataDir, "settings"), map[string]any{"accounts": map[string]any{"private@example.com": map[string]any{"sync": true, "extract": false, "workspace": ""}}})
	ec := personalemail.Config{Account: "private@example.com", LegacyRoot: c.Root, DataDir: c.DataDir, Index: filepath.Join(c.DataDir, "index.db"), BackfillDays: 30}
	put(t, filepath.Join(c.Root, "vessel/state/email/state.json"), map[string]any{"watermark": "2026-09-01T00:00:00Z", "threads": map[string]any{}})
	ap := approvals.NewStore(filepath.Join(c.Root, "artifacts"))
	svc := personalemail.New(ec, nil, ap)
	ep, e := svc.Prepare()
	if e != nil {
		t.Fatal(e)
	}
	h := strings.Repeat("a", 64)
	for _, duty := range Duties {
		owner, action, evidence := "manifest", "transfer", h
		if duty == Duties[0] {
			evidence = ep.Hash()
		}
		if strings.HasPrefix(duty, "extractor/") {
			owner, action = "blocked", "pause"
		}
		dir := filepath.Join(c.Root, "vessel/state/dispatch-fence", duty)
		put(t, filepath.Join(dir, "00000000000000000001.json"), connectorhandoff.RecordFence{Version: 1, Revision: 1, Duty: duty, PreviousOwner: "excalibur", Owner: owner, Action: action, Evidence: evidence, At: "2026-09-16T00:00:00Z"})
		write(t, filepath.Join(dir, "lock"), nil)
		parts := strings.Split(duty, "/")
		write(t, filepath.Join(c.Root, "spirits", parts[0], "rituals", parts[1]+".md"), []byte("---\nritual: "+parts[1]+"\nenabled: false\n---\nprivate legacy prompt\n"))
	}
	if _, e = svc.Apply(ep.Hash(), 0); e != nil {
		t.Fatal(e)
	}
	ec.Enabled = true
	put(t, filepath.Join(c.DataDir, "personal-email-worker.json"), ec)
	tc := transcriptsync.Config{LegacyRoot: c.Root, Granola: transcriptsync.SourceConfig{Enabled: true, Account: "private"}, Pocket: transcriptsync.SourceConfig{Enabled: true, Account: "private"}}
	put(t, filepath.Join(c.DataDir, "transcript-worker.json"), map[string]any{"dataDir": c.DataDir, "transcriptSync": tc})
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	for _, source := range []string{"granola", "pocket"} {
		put(t, filepath.Join(c.DataDir, "connector-handoff", source+".json"), connectorhandoff.Record{Version: 1, Source: source, Phase: connectorhandoff.Enabled, Owner: "manifest", RollbackOwner: "excalibur", CheckpointHash: h, ApprovalHash: h, Evidence: map[string]string{"dispatch-exclusion": h, "account-binding": approvals.EvidenceHash("private"), "source-reconciliation": h}})
		put(t, filepath.Join(c.DataDir, "transcript-sync", source, "state.json"), transcriptsync.State{Version: 1, Account: "private", Watermark: now, ImportedFrom: h, Items: map[string]transcriptsync.Outcome{}, LastAttempt: now, LastSuccess: now})
	}
	put(t, filepath.Join(c.Root, "vessel/spool/private-request.json"), map[string]any{"spirit": "extractor", "ritual": "aion", "request": "private source text", "requested_at": now})
	write(t, filepath.Join(c.Root, "artifacts/approvals/pending/private-person.md"), []byte("private approval, not decided"))
	return c, m
}
func TestPlanAndApplyPreserveStateIdempotently(t *testing.T) {
	c, m := fixture(t)
	p := Build(c, m)
	if len(p.Blockers) > 0 {
		t.Fatal(p.Blockers)
	}
	if p.Hash() != Build(c, m).Hash() {
		t.Fatal("plan not idempotent")
	}
	for _, secret := range []string{"private@example.com", "private source text", "private-person", c.Root, c.DataDir} {
		if strings.Contains(string(p.Bytes()), secret) {
			t.Fatal("unredacted receipt")
		}
	}
	before, _ := InventoryTree("all", c.Root)
	if e := Apply(c, m, p.Bytes(), p.Hash()); e != nil {
		t.Fatal(e)
	}
	if !Retired(c.DataDir) || m.calls != 1 {
		t.Fatal("missing retirement")
	}
	if e := Apply(c, m, p.Bytes(), p.Hash()); e != nil || m.calls != 1 {
		t.Fatalf("non-idempotent: %v", e)
	}
	after, _ := InventoryTree("all", c.Root)
	if before.SHA256 != after.SHA256 {
		t.Fatal("legacy state mutated/deleted/replayed")
	}
}
func TestApplyRefusals(t *testing.T) {
	for _, kind := range []string{"wrong-hash", "edited-plan", "queue-drift", "missing-fence", "partial-fence", "enabled-schedule", "unaccounted-chat", "uncertain-spool", "unknown-consumer", "service-failure", "stop-drift", "corrupt-retirement", "missing-lock", "active-chat"} {
		t.Run(kind, func(t *testing.T) {
			c, m := fixture(t)
			p := Build(c, m)
			hash := p.Hash()
			b := p.Bytes()
			switch kind {
			case "corrupt-retirement":
				write(t, filepath.Join(c.DataDir, "excalibur-decommission/retired.json"), []byte("{"))
			case "missing-lock":
				os.Remove(filepath.Join(c.Root, "vessel/state/dispatch-fence/extractor/aion/lock"))
			case "active-chat":
				write(t, filepath.Join(c.Root, "artifacts/chats/live.md"), []byte("---\nstatus: thinking\n---\n"))
			case "wrong-hash":
				hash = strings.Repeat("b", 64)
			case "edited-plan":
				b = append(b, ' ')
				hash = Hash(b)
			case "queue-drift":
				write(t, filepath.Join(c.Root, "vessel/spool/private-request.json"), []byte(`{"spirit":"extractor","ritual":"aion","request":"changed"}`))
			case "missing-fence":
				os.Remove(filepath.Join(c.Root, "vessel/state/dispatch-fence/extractor/aion/00000000000000000001.json"))
			case "partial-fence":
				write(t, filepath.Join(c.Root, "vessel/state/dispatch-fence/extractor/aion/00000000000000000002.json"), []byte("{"))
			case "enabled-schedule":
				write(t, filepath.Join(c.Root, "spirits/extractor/rituals/aion.md"), []byte("---\nenabled: true\n---\n"))
			case "unaccounted-chat":
				put(t, filepath.Join(c.Root, "vessel/spool/chat.json"), map[string]string{"kind": "chat", "spirit": "extractor"})
			case "uncertain-spool":
				write(t, filepath.Join(c.Root, "vessel/spool/partial.json"), []byte("{"))
			case "unknown-consumer":
				m.blockers = []string{"consumer unaccounted"}
			case "service-failure":
				m.fail = true
			case "stop-drift":
				m.mutate = func() {
					write(t, filepath.Join(c.Root, "vessel/spool/new.json"), []byte(`{"spirit":"extractor","ritual":"aion"}`))
				}
			}
			if e := Apply(c, m, b, hash); e == nil {
				t.Fatal("unsafe apply accepted")
			}
			if Retired(c.DataDir) {
				t.Fatal("false retirement")
			}
			if kind != "service-failure" && kind != "stop-drift" && m.calls != 0 {
				t.Fatal("service changed before validation")
			}
		})
	}
}
func TestRepeatedApplyRefusesNewState(t *testing.T) {
	c, m := fixture(t)
	p := Build(c, m)
	if e := Apply(c, m, p.Bytes(), p.Hash()); e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(c.Root, "artifacts/new-history.md"), []byte("new bytes"))
	if e := Apply(c, m, p.Bytes(), p.Hash()); e == nil || m.calls != 1 {
		t.Fatal("repeat ignored drift or changed service")
	}
}

func TestInventoryFailClosed(t *testing.T) {
	c, m := fixture(t)
	if e := os.Symlink(filepath.Join(c.Root, "artifacts"), filepath.Join(c.Root, "vessel/state/link")); e != nil {
		t.Fatal(e)
	}
	p := Build(c, m)
	if len(p.Blockers) == 0 {
		t.Fatal("symlink accepted")
	}
	if e := Apply(c, m, p.Bytes(), p.Hash()); e == nil {
		t.Fatal("blocked plan accepted")
	}
}
