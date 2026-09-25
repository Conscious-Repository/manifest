package agentchat

import (
	"strings"
	"testing"
)

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
	_, body, _, _ := fresh.Get("alfred", id)
	if !strings.Contains(body, "after interruption was requested") {
		t.Fatal(body)
	}
	fresh.Recover()
	_, again, _, _ := fresh.Get("alfred", id)
	if body != again {
		t.Fatal("recovery duplicated explanation")
	}
	current, _ := fresh.Receipt("alfred", id, "first-request")
	queued, _ := fresh.Receipt("alfred", id, "queued-request")
	if current.State != DeliveryInterrupted || !current.StopRequested || queued.State != DeliveryCancelled || queued.Text != "retain" {
		t.Fatal(current, queued)
	}
	if _, claimed, err := fresh.Claim("alfred", id); err != nil || claimed {
		t.Fatal("cancelled queue restarted", claimed, err)
	}
}

func TestCancelQueuedDoesNotInterruptActive(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("alfred", "", "", "")
	s.Accept("alfred", id, "active-request", "active")
	s.Claim("alfred", id)
	s.Accept("alfred", id, "queued-request", "preserve this")
	for range 2 {
		d, err := s.CancelQueued("alfred", id, "queued-request")
		if err != nil || d.State != DeliveryCancelled || d.Text != "preserve this" {
			t.Fatal(d, err)
		}
	}
	active, _ := s.Receipt("alfred", id, "active-request")
	if active.State != DeliveryRunning || active.StopRequested {
		t.Fatal(active)
	}
	if _, err := s.CancelQueued("alfred", id, "active-request"); err == nil {
		t.Fatal("queue cancel interrupted active turn")
	}
}
func TestCancelQueuedRacesClaim(t *testing.T) {
	for range 20 {
		s := New(t.TempDir())
		id, _ := s.Create("alfred", "", "", "")
		s.Accept("alfred", id, "queued-request", "text")
		start := make(chan struct{})
		claimed := make(chan bool, 1)
		cancelled := make(chan error, 1)
		go func() { <-start; _, ok, _ := s.Claim("alfred", id); claimed <- ok }()
		go func() { <-start; _, err := s.CancelQueued("alfred", id, "queued-request"); cancelled <- err }()
		close(start)
		ran, err := <-claimed, <-cancelled
		d, _ := s.Receipt("alfred", id, "queued-request")
		if ran {
			if err == nil || d.State != DeliveryRunning || d.StopRequested {
				t.Fatal(ran, err, d)
			}
		} else if err != nil || d.State != DeliveryCancelled {
			t.Fatal(ran, err, d)
		}
	}
}
