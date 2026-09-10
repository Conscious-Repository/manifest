package chatthreads

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSharedImportRecoversCommittedHistoryAfterLogFailure(t *testing.T) {
	root := t.TempDir()
	s, e := New(root)
	if e != nil {
		t.Fatal(e)
	}
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	thread := Thread{ID: "shared-fixture", Title: "Reviewed chat", By: "owner@example.invalid", Created: at, ImportSource: "private/source", ImportRevision: "reviewed-hash"}
	messages := []Message{{ID: "source-turn-1", Thread: thread.ID, Kind: "ask", Author: thread.By, Text: strings.Repeat("history ", 2000), At: at}}
	// State is committed first. Force the subsequent activity append to fail.
	if e = os.Mkdir(filepath.Join(root, "activity.log"), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ImportSharedThread(thread, messages, at); e == nil {
		t.Fatal("expected log failure")
	}
	s, e = New(root)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ImportSharedThread(thread, messages, at.Add(time.Hour)); e != nil {
		t.Fatal("retry did not recover committed import", e)
	}
	got := s.Messages(thread.ID)
	if len(got) != 1 || got[0].Text != messages[0].Text {
		t.Fatal("history duplicated or truncated")
	}
	messages[0].Text = "changed after approval"
	if _, e = s.ImportSharedThread(thread, messages, at); !errors.Is(e, ErrImportConflict) {
		t.Fatal("changed import accepted", e)
	}
}

func TestShareImportDoesNotOverwriteUnreadableTeamHistory(t *testing.T) {
	root := t.TempDir()
	s, _ := New(root)
	file := filepath.Join(root, "chat.json")
	prior := []byte("{invalid prior history")
	if e := os.WriteFile(file, prior, 0600); e != nil {
		t.Fatal(e)
	}
	_, e := s.ImportSharedThread(Thread{ID: "new", Created: time.Now(), ImportSource: "private/source", ImportRevision: "version"}, nil, time.Now())
	if e == nil {
		t.Fatal("unreadable history overwritten")
	}
	after, _ := os.ReadFile(file)
	if string(after) != string(prior) {
		t.Fatal("prior bytes changed")
	}
}
