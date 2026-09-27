package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The profile list is served from cache while one background call refreshes
// it, within a bounded age: a request never waits on the ~1.5 s CLI while
// the answer is under hermesProfilesStale old, and past that it waits for a
// fresh one rather than show an old list as current. (2026-09-27: every chat
// inbox after each 30 s expiry took ~1.6 s.)
func TestHermesProfilesServedWhileRefreshing(t *testing.T) {
	model := filepath.Join(t.TempDir(), "model")
	if err := os.WriteFile(model, []byte("gpt-5"), 0o600); err != nil {
		t.Fatal(err)
	}
	// `profile list` is slow, like the real CLI; scout's model is read from a file
	script := `#!/bin/sh
if [ "$1" = "profile" ]; then
 if [ "$2" = "show" ]; then
   m=claude-x; [ "$3" = "scout" ] && m=$(cat ` + model + `)
   printf 'Profile: %s\nModel: %s (custom)\n' "$3" "$m"
   exit 0
 fi
 sleep 0.6
 printf 'Profile   Model   Gateway   Alias   Distribution\n'
 printf '◆ default   claude-x   —   —   —\n'
 printf 'scout   x   —   scout   —\n'
 exit 0
fi
`
	s, _, _ := agentChatFixture(t, script)
	scout := func() (string, time.Duration) {
		start := time.Now()
		ps, _ := s.hermesProfilesCached(context.Background())
		for _, p := range ps {
			if p.Name == "scout" {
				return p.Model, time.Since(start)
			}
		}
		return "", time.Since(start)
	}
	age := func(d time.Duration) {
		c := s.agentChat
		c.pmu.Lock()
		c.profAt = time.Now().Add(-d)
		c.pmu.Unlock()
	}
	if m, took := scout(); m != "gpt-5" || took < 500*time.Millisecond {
		t.Fatalf("first read: %q in %v (must wait for the CLI)", m, took)
	}
	// stale within the bound: the cached answer at once, a refresh behind it
	_ = os.WriteFile(model, []byte("gpt-6"), 0o600)
	age(hermesProfilesFresh + time.Second)
	if m, took := scout(); m != "gpt-5" || took > 300*time.Millisecond {
		t.Fatalf("stale read: %q in %v (must not wait)", m, took)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if m, _ := scout(); m == "gpt-6" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the background refresh never landed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// past the bound: wait for a fresh answer
	_ = os.WriteFile(model, []byte("gpt-7"), 0o600)
	age(hermesProfilesStale + time.Second)
	if m, took := scout(); m != "gpt-7" || took < 500*time.Millisecond {
		t.Fatalf("expired read: %q in %v (must wait for a fresh list)", m, took)
	}
}
