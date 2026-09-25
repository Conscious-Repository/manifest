package agentchat

import "testing"

func TestStopTargetsRunningAndRetainsCancelledQueue(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("alfred", "", "", "")
	s.Accept("alfred", id, "first-request", "first")
	s.Claim("alfred", id)
	s.Accept("alfred", id, "queued-request", "retain this text")
	d, err := s.RequestStop("alfred", id, "first-request")
	if err != nil || !d.StopRequested || d.State != DeliveryRunning {
		t.Fatal(d, err)
	}
	s.Accept("alfred", id, "later-request", "new explicit instruction")
	if _, err := s.RequestStop("alfred", id, "first-request"); err != nil {
		t.Fatal(err)
	}
	queued, _ := s.Receipt("alfred", id, "queued-request")
	later, _ := s.Receipt("alfred", id, "later-request")
	if queued.State != DeliveryCancelled || queued.Text != "retain this text" || later.State != DeliveryQueued {
		t.Fatal(queued, later)
	}
	if err := s.Finish("alfred", id, "first-request", "system", "runner returned", DeliveryFailed, "cancel", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish("alfred", id, "first-request", "system", "duplicate", DeliveryFailed, "cancel", 0); err != nil {
		t.Fatal(err)
	}
	fresh := New(s.Root())
	d, _ = fresh.Receipt("alfred", id, "first-request")
	if d.State != DeliveryInterrupted {
		t.Fatal(d)
	}
	next, ok, err := fresh.Claim("alfred", id)
	if err != nil || !ok || next.ID != "later-request" {
		t.Fatal(next, ok, err)
	}
	if _, err := fresh.RequestStop("alfred", id, "queued-request"); err == nil {
		t.Fatal("cancelled request targeted new run")
	}
	if _, err := fresh.RequestStop("alfred", id, "first-request"); err != nil {
		t.Fatal(err)
	}
	next, _ = fresh.Receipt("alfred", id, "later-request")
	if next.StopRequested {
		t.Fatal("old stop hit new run")
	}
}

func TestStopAfterCompletionCannotCancelQueue(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("alfred", "", "", "")
	s.Accept("alfred", id, "first-request", "first")
	s.Claim("alfred", id)
	s.Accept("alfred", id, "queued-request", "queued")
	if err := s.Finish("alfred", id, "first-request", "alfred", "done", DeliveryCompleted, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestStop("alfred", id, "first-request"); err == nil {
		t.Fatal("late stop admitted")
	}
	d, _ := s.Receipt("alfred", id, "queued-request")
	if d.State != DeliveryQueued {
		t.Fatal(d)
	}
}
func TestStopSurvivesCrashBeforeRunnerReturn(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("alfred", "", "", "")
	s.Accept("alfred", id, "first-request", "first")
	s.Claim("alfred", id)
	s.Accept("alfred", id, "queued-request", "retain")
	if _, err := s.RequestStop("alfred", id, "first-request"); err != nil {
		t.Fatal(err)
	}
	fresh := New(s.Root())
	fresh.Recover()
	current, _ := fresh.Receipt("alfred", id, "first-request")
	queued, _ := fresh.Receipt("alfred", id, "queued-request")
	if current.State != DeliveryInterrupted || !current.StopRequested || queued.State != DeliveryCancelled || queued.Text != "retain" {
		t.Fatal(current, queued)
	}
	if _, claimed, err := fresh.Claim("alfred", id); err != nil || claimed {
		t.Fatal("cancelled queue restarted", claimed, err)
	}
}
