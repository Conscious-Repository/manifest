package errands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubCLI writes a fake `aside` that echoes, honors sleep, and exits per its
// task text — the executor is tested against controlled process reality.
func stubCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "aside")
	script := `#!/bin/sh
# args: --account <id> <text>
text="$3"
case "$text" in
  fail*) echo "boom: $text"; exit 1 ;;
  sleep*) sleep 30; echo never ;;
  *) printf 'Thinking...\n\033[32mdone\033[0m\nPage title: OK — %s\n' "$text"; exit 0 ;;
esac
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func waitStatus(t *testing.T, s *Store, id string, want Status, within time.Duration) *Record {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if r, err := s.Get(id); err == nil && r.Status == want {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	r, _ := s.Get(id)
	t.Fatalf("errand %s: status %v, wanted %v", id, r.Status, want)
	return nil
}

func TestExecutorRunsSeriallyAndRecords(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := NewExecutor(store, 1)
	e.SetBinary(stubCLI(t))
	e.Start()

	a, err := e.Enqueue("do thing one", "u0", "user", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := e.Enqueue("do thing two", "u0", "user", "", "goal/x")

	ra := waitStatus(t, store, a.ID, StatusDone, 5*time.Second)
	rb := waitStatus(t, store, b.ID, StatusDone, 5*time.Second)
	if ra.Finished > rb.Started {
		t.Fatal("serial queue violated: b started before a finished")
	}
	if *ra.ExitCode != 0 || ra.Outcome != "Page title: OK — do thing one" {
		t.Fatalf("record a: exit=%v outcome=%q", ra.ExitCode, ra.Outcome)
	}
	if rb.GoalID != "goal/x" {
		t.Fatal("goal link lost")
	}
	// transcript is the raw bytes, ANSI included
	raw, err := os.ReadFile(store.TranscriptPath(a.ID))
	if err != nil || !strings.Contains(string(raw), "\x1b[32mdone\x1b[0m") {
		t.Fatalf("transcript raw ANSI missing: %q err=%v", raw, err)
	}
}

func TestExecutorFailure(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	e := NewExecutor(store, 1)
	e.SetBinary(stubCLI(t))
	e.Start()
	r, _ := e.Enqueue("fail loudly", "u0", "user", "", "")
	got := waitStatus(t, store, r.ID, StatusFailed, 5*time.Second)
	if *got.ExitCode != 1 || !strings.Contains(got.Outcome, "boom") {
		t.Fatalf("exit=%v outcome=%q", got.ExitCode, got.Outcome)
	}
}

func TestExecutorCancelRunning(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	e := NewExecutor(store, 1)
	e.SetBinary(stubCLI(t))
	e.Start()
	r, _ := e.Enqueue("sleep forever", "u0", "user", "", "")
	waitStatus(t, store, r.ID, StatusRunning, 5*time.Second)
	if err := e.Cancel(r.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, r.ID, StatusCancelled, 5*time.Second)
}

func TestCancelQueuedAndPositions(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	e := NewExecutor(store, 1)
	e.SetBinary(stubCLI(t))
	e.Start()
	running, _ := e.Enqueue("sleep a while", "u0", "user", "", "")
	queued, _ := e.Enqueue("queued task", "u0", "user", "", "")
	waitStatus(t, store, running.ID, StatusRunning, 5*time.Second)
	if pos := e.QueuePosition(queued.ID); pos != 1 {
		t.Fatalf("queue position: %d", pos)
	}
	if err := e.Cancel(queued.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, store, queued.ID, StatusCancelled, 2*time.Second)
	_ = e.Cancel(running.ID) // cleanup
}

func TestExecutorTimeout(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	e := NewExecutor(store, 1)
	e.timeout = 500 * time.Millisecond // test-scale
	e.SetBinary(stubCLI(t))
	e.Start()
	r, _ := e.Enqueue("sleep too long", "u0", "user", "", "")
	got := waitStatus(t, store, r.ID, StatusFailed, 10*time.Second)
	if !strings.Contains(got.Outcome, "timed out") {
		t.Fatalf("outcome: %q", got.Outcome)
	}
	if _, err := os.Stat(store.TranscriptPath(r.ID)); err != nil {
		t.Fatal("transcript must survive a timeout")
	}
}

func TestEnqueueValidation(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	e := NewExecutor(store, 1)
	if _, err := e.Enqueue("   ", "u0", "user", "", ""); err == nil {
		t.Fatal("empty text must refuse")
	}
	if _, err := e.Enqueue("x", "", "user", "", ""); err == nil {
		t.Fatal("missing account must refuse")
	}
}

func TestOutcomeLineAndAccountParse(t *testing.T) {
	if got := OutcomeLine([]byte("a\n\x1b[32mgreen line\x1b[0m\n\n")); got != "green line" {
		t.Fatalf("outcome: %q", got)
	}
	accs := parseAccounts("* u0  a@b.com  signed in  profiles: Profile 0\n  provider: google\n  u1  c@d.com  signed in\n")
	if len(accs) != 2 || !accs[0].Current || accs[0].ID != "u0" || accs[1].ID != "u1" || accs[1].Current {
		t.Fatalf("accounts: %+v", accs)
	}
}

func TestAckOnlyTerminal(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	r := &Record{ID: "x1", Text: "t", Account: "u0", Created: "2026-07-30T00:00:00Z", Status: StatusQueued}
	_ = store.Save(r)
	if err := store.Ack("x1"); err == nil {
		t.Fatal("queued must not ack")
	}
	r.Status = StatusFailed
	_ = store.Save(r)
	if err := store.Ack("x1"); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get("x1")
	if !got.Acknowledged || got.Status != StatusFailed {
		t.Fatalf("ack lost state: %+v", got)
	}
}

func TestSendInputAnswersAskGate(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	e := NewExecutor(store, 1)
	// stub that ASKS on stdout then blocks on stdin — exits 0 when answered
	dir := t.TempDir()
	bin := filepath.Join(dir, "aside")
	os.WriteFile(bin, []byte("#!/bin/sh\necho 'which account?'\nread answer\necho \"got: $answer\"\nexit 0\n"), 0o755)
	e.SetBinary(bin)
	e.Start()
	r, _ := e.Enqueue("interactive task", "u0", "user", "", "")
	waitStatus(t, store, r.ID, StatusRunning, 5*time.Second)
	time.Sleep(200 * time.Millisecond) // let it print + block on read
	if err := e.SendInput(r.ID, "yahoo one"); err != nil {
		t.Fatal(err)
	}
	got := waitStatus(t, store, r.ID, StatusDone, 5*time.Second)
	if !strings.Contains(got.Outcome, "got: yahoo one") {
		t.Fatalf("outcome: %q", got.Outcome)
	}
	// echo puts BOTH the question and the answer on the audit trail
	raw, _ := os.ReadFile(store.TranscriptPath(r.ID))
	if !strings.Contains(string(raw), "which account?") || !strings.Contains(string(raw), "yahoo one") {
		t.Fatalf("transcript missing exchange: %q", raw)
	}
	if err := e.SendInput(r.ID, "late"); err == nil {
		t.Fatal("input after exit must refuse")
	}
}

func TestStartRecoversOrphans(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	_ = store.Save(&Record{ID: "orph", Text: "t", Account: "u0", Created: "2026-07-30T00:00:00Z", Status: StatusRunning})
	e := NewExecutor(store, 1)
	e.Start()
	got := waitStatus(t, store, "orph", StatusFailed, 2*time.Second)
	if !strings.Contains(got.Outcome, "restart") {
		t.Fatalf("outcome: %q", got.Outcome)
	}
}
