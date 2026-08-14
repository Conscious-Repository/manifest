package teamportal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The storage path is append-only by contract: every write lands ONE new JSONL
// line at the tail of activity.log, never rewriting what came before, and
// items.ext.json mirrors the latest derived state.
func TestStoreAppendOnlyActivityTrail(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	actor := Identity{Email: "hannah@aion.bio", Name: "Hannah Zmuda"}
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	readLog := func() string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "activity.log"))
		if err != nil {
			t.Fatalf("read activity.log: %v", err)
		}
		return string(b)
	}

	it, err := s.AddItem(actor, "HZ", "task", "Calibrate the MRI phantom", "", "", now)
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	afterAdd := readLog()

	if _, err := s.AddComment(actor, it.ID, "on it", now.Add(time.Minute)); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	afterComment := readLog()
	if !strings.HasPrefix(afterComment, afterAdd) {
		t.Fatal("AddComment rewrote earlier activity.log lines (log must be append-only)")
	}

	if _, err := s.Patch(actor, it.ID, map[string]string{"status": "done"}, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	afterPatch := readLog()
	if !strings.HasPrefix(afterPatch, afterComment) {
		t.Fatal("Patch rewrote earlier activity.log lines (log must be append-only)")
	}

	// One standalone JSON line per write, in write order.
	lines := strings.Split(strings.TrimSpace(afterPatch), "\n")
	if len(lines) != 3 {
		t.Fatalf("activity.log has %d lines after 3 writes, want 3", len(lines))
	}
	wantActions := []string{ActAdd, ActComment, ActPatch}
	for i, line := range lines {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("activity.log line %d is not valid JSON: %v", i+1, err)
		}
		if e.Action != wantActions[i] || e.Actor != actor.Email {
			t.Errorf("line %d = action %q by %q, want %q by %q", i+1, e.Action, e.Actor, wantActions[i], actor.Email)
		}
	}

	// Activity() replays the same trail oldest-first.
	entries := s.Activity(time.Time{})
	if len(entries) != 3 {
		t.Fatalf("Activity() = %d entries, want 3", len(entries))
	}
	for i, e := range entries {
		if e.Action != wantActions[i] {
			t.Errorf("Activity()[%d].Action = %q, want %q", i, e.Action, wantActions[i])
		}
	}

	// items.ext.json holds the derived state the trail describes.
	ext := s.Ext()
	if len(ext.Items) != 1 || ext.Items[0].ID != it.ID || !ext.Items[0].Team {
		t.Errorf("Ext().Items = %+v, want the one team item", ext.Items)
	}
	if got := len(ext.Comments[it.ID]); got != 1 {
		t.Errorf("Ext().Comments[%s] has %d comments, want 1", it.ID, got)
	}
	ov := ext.Overrides[it.ID]
	if ov.Fields["status"] != "done" || ov.Fields["done_on"] == "" {
		t.Errorf("override = %+v, want status=done with done_on stamped", ov.Fields)
	}
	if ov.By != actor.Email {
		t.Errorf("override.By = %q, want %q", ov.By, actor.Email)
	}
}

// Patch accepts only the closed field + status vocabularies.
func TestPatchValidatesFields(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	actor := Identity{Email: "hannah@aion.bio", Name: "Hannah"}
	now := time.Now()

	if _, err := s.Patch(actor, "x", map[string]string{"owner": "BA"}, now); err == nil {
		t.Error("Patch accepted non-team-editable field owner")
	}
	if _, err := s.Patch(actor, "x", map[string]string{"status": "blocked"}, now); err == nil {
		t.Error("Patch accepted status outside open|in_progress|done")
	}
	if _, err := s.Patch(actor, "x", map[string]string{}, now); err == nil {
		t.Error("Patch accepted an empty field set")
	}
}
