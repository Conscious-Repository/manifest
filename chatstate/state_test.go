package chatstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const testKey = "conversation-0123456789abcdef0123456789abcdef"

func TestDeliveryRecoverySurvivesStoreReopen(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{testKey, "landing-0123456789abcdef0123456789abcdef"} {
		value := json.RawMessage(`{"items":{"request-123":{"payload":{"requestId":"request-123","text":"pending"}}}}`)
		written, err := New(root).Write(key, "deliveries", 0, value)
		if err != nil {
			t.Fatal(err)
		}
		read, err := New(root).Read(key, "deliveries")
		if err != nil || !bytes.Equal(read.Value, written.Value) {
			t.Fatal(read, err)
		}
		if _, err := New(root).Write(key, "deliveries", 0, json.RawMessage(`{"items":{}}`)); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
}

func TestInboxPinsPersistIndependently(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	saved, err := s.Write("inbox", "pins", 0, json.RawMessage(`{"pins":{"terminal:codex/native":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	read, err := New(root).Read("inbox", "pins")
	if err != nil || !bytes.Equal(read.Value, saved.Value) {
		t.Fatal(read, err)
	}
	if _, err = s.Write("inbox", "pins", 0, json.RawMessage(`{"pins":{}}`)); !errors.Is(err, ErrConflict) {
		t.Fatal("stale pin write accepted", err)
	}
	if _, err = s.Read("inbox", "draft"); !errors.Is(err, ErrInvalid) {
		t.Fatal("pins created a draft slot", err)
	}
}

func TestLandingDraftIsPrivateStateWithoutConversation(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	key := "landing-0123456789abcdef0123456789abcdef"
	saved, err := s.Write(key, "draft", 0, json.RawMessage(`{"text":"unsent idea","files":[{"hash":"file"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := New(root).Read(key, "draft")
	if err != nil || !bytes.Equal(saved.Value, reopened.Value) {
		t.Fatal(reopened, err)
	}
	conversation, err := s.Read(testKey, "draft")
	if err != nil || conversation.Revision != 0 {
		t.Fatal("landing polluted conversation", conversation, err)
	}
	if _, err = s.Read(key, "view"); !errors.Is(err, ErrInvalid) {
		t.Fatal("landing has no transcript", err)
	}
	if _, err = s.Write(key, "draft", saved.Revision, json.RawMessage(`null`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Write(key, "draft", saved.Revision, saved.Value); !errors.Is(err, ErrConflict) {
		t.Fatal("stale landing resurrected sent draft", err)
	}
}

func TestDraftConcurrentSaveRestartAndClear(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for _, value := range []string{`{"text":"desktop"}`, `{"text":"phone"}`} {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			_, err := s.Write(testKey, "draft", 0, json.RawMessage(v))
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
			} else if !errors.Is(err, ErrConflict) {
				t.Error(err)
			}
		}(value)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent writers won %d times", wins)
	}
	fresh := New(root)
	saved, err := fresh.Read(testKey, "draft")
	if err != nil || saved.Revision != 1 {
		t.Fatal(saved, err)
	}
	// Retrying a lost acknowledgement does not produce another revision.
	retry, err := fresh.Write(testKey, "draft", 0, saved.Value)
	if err != nil || retry.Revision != 1 {
		t.Fatal(retry, err)
	}
	cleared, err := fresh.Write(testKey, "draft", 1, json.RawMessage(`null`))
	if err != nil || cleared.Revision != 2 {
		t.Fatal(cleared, err)
	}
	if _, err := fresh.Write(testKey, "draft", 1, saved.Value); !errors.Is(err, ErrConflict) {
		t.Fatal("stale device resurrected sent draft", err)
	}
	view, err := fresh.Write(testKey, "view", 0, json.RawMessage(`{"offset":123}`))
	if err != nil || view.Revision != 1 {
		t.Fatal("independent view slot", view, err)
	}
}

func TestStateRejectsTraversalMalformedAndFailedWrites(t *testing.T) {
	s := New(t.TempDir())
	for _, key := range []string{"../outside", testKey + "/other", "conversation-nope"} {
		if _, err := s.Read(key, "draft"); err == nil {
			t.Fatal(key)
		}
	}
	if _, err := s.Write(testKey, "draft", 0, json.RawMessage(`[]`)); err == nil {
		t.Fatal("non-object accepted")
	}
	if err := os.WriteFile(s.path(testKey, "draft"), []byte(`broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(testKey, "draft", 0, json.RawMessage(`{"text":"erase"}`)); err == nil {
		t.Fatal("overwrote corrupt data")
	}
	root := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root).Write(testKey, "draft", 0, json.RawMessage(`{"text":"pending"}`)); err == nil {
		t.Fatal("acknowledged failed persistence")
	}
}

func TestArtifactEditsKeepTheirBaseAndStaySeparateFromChat(t *testing.T) {
	s := New(t.TempDir())
	key := "artifact-0123456789abcdef"
	original := json.RawMessage(`{"text":"unfinished revision","baseRevision":"original-hash","sourceRevision":"older-hash","restore":true}`)
	saved, err := s.Write(key, "edit", 0, original)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := s.Read(key, "edit")
	if err != nil || !bytes.Equal(reopened.Value, saved.Value) {
		t.Fatal(reopened, err)
	}
	// A different device cannot replace either the text or its base revision.
	if _, err = s.Write(key, "edit", 0, json.RawMessage(`{"text":"other","baseRevision":"new-head"}`)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{key, "draft"}, {testKey, "edit"}, {"artifact-../../outside", "edit"}} {
		if _, err = s.Read(pair[0], pair[1]); !errors.Is(err, ErrInvalid) {
			t.Fatal(pair, err)
		}
	}
	if _, err = s.Write(key, "edit", saved.Revision, json.RawMessage(`null`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Write(key, "edit", saved.Revision, original); !errors.Is(err, ErrConflict) {
		t.Fatal("stale editor restored discarded draft", err)
	}
}

func TestWorkstreamsPersistWithConflictAndIndependentPins(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	original := json.RawMessage(`{"groups":{"ws-one":"Manifest"},"members":{"agent:alfred/chat":"ws-one"}}`)
	saved, err := s.Write("inbox", "workstreams", 0, original)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := New(root).Read("inbox", "workstreams")
	if err != nil || !bytes.Equal(saved.Value, reopened.Value) {
		t.Fatal(reopened, err)
	}
	if _, err = s.Write("inbox", "workstreams", 0, json.RawMessage(`{"groups":{},"members":{}}`)); !errors.Is(err, ErrConflict) {
		t.Fatal("stale membership accepted", err)
	}
	if _, err = s.Write("inbox", "workstreams", saved.Revision, json.RawMessage(`{"groups":{"ws-one":"Manifest"},"members":{}}`)); err != nil {
		t.Fatal(err)
	}
	pins, err := s.Read("inbox", "pins")
	if err != nil || pins.Revision != 0 {
		t.Fatal("membership affected pins", pins, err)
	}
}

func TestLifecyclePersistsAndRejectsStaleDevice(t *testing.T) {
	s := New(t.TempDir())
	first, err := s.Write("inbox", "lifecycle", 0, json.RawMessage(`{"items":{"terminal:codex/a":"archived"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Write("inbox", "lifecycle", 0, json.RawMessage(`{"items":{}}`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write: %v", err)
	}
	next, err := s.Write("inbox", "lifecycle", first.Revision, json.RawMessage(`{"items":{"terminal:codex/a":"deleted"}}`))
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.Read("inbox", "lifecycle")
	if err != nil || string(read.Value) != string(next.Value) {
		t.Fatalf("read: %+v %v", read, err)
	}
}
