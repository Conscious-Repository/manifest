package ledger

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAppendDayRoundTrip(t *testing.T) {
	st, err := New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	for i, kind := range []string{"thread.comment", "run.completed", "chat.assistant"} {
		if err := st.Append(Entry{TS: base.Add(time.Duration(i) * time.Minute), Source: "thread", Kind: kind, Actor: "owner"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.Day("2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Kind != "thread.comment" || got[2].Kind != "chat.assistant" {
		t.Fatalf("round trip: %+v", got)
	}
	if days := st.Days(); len(days) != 1 || days[0] != "2026-08-15" {
		t.Fatalf("days: %v", days)
	}
}

func TestDayBucketingIsLocal(t *testing.T) {
	chi, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("no tzdata")
	}
	st, err := New(t.TempDir(), chi)
	if err != nil {
		t.Fatal(err)
	}
	// 23:30 in Chicago on Aug 15 = 04:30 UTC Aug 16 — must land in the LOCAL date
	ts := time.Date(2026, 8, 15, 23, 30, 0, 0, chi)
	if err := st.Append(Entry{TS: ts, Source: "chat", Kind: "chat.user", Actor: "owner"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Day("2026-08-15"); len(got) != 1 {
		t.Fatalf("entry missing from local day: %+v", got)
	}
	if got, _ := st.Day("2026-08-16"); len(got) != 0 {
		t.Fatalf("entry leaked into the UTC day: %+v", got)
	}
}

func TestTornLastLineTolerated(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Append(Entry{TS: time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC), Source: "run", Kind: "run.completed", Actor: "hermes"}); err != nil {
		t.Fatal(err)
	}
	// simulate a crash mid-append: a torn trailing line
	f, err := os.OpenFile(filepath.Join(dir, "2026-08-15.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"ts":"2026-08-15T10:00:00Z","kind":"run.f`); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got, err := st.Day("2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "run.completed" {
		t.Fatalf("torn line must be skipped: %+v", got)
	}
	// and appends keep working after the tear
	if err := st.Append(Entry{TS: time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC), Source: "run", Kind: "run.failed", Actor: "hermes"}); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.Day("2026-08-15"); len(got) != 2 {
		t.Fatalf("append after tear: %+v", got)
	}
}

func TestConcurrentAppends(t *testing.T) {
	st, err := New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = st.Append(Entry{TS: ts, Source: "thread", Kind: "thread.comment", Actor: "owner"})
		}()
	}
	wg.Wait()
	got, err := st.Day("2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 {
		t.Fatalf("want 20 entries, got %d", len(got))
	}
}

func TestSnip(t *testing.T) {
	if s := Snip("short", 280); s != "short" {
		t.Fatal(s)
	}
	long := ""
	for i := 0; i < 300; i++ {
		long += "x"
	}
	if s := Snip(long, 280); len([]rune(s)) != 281 { // 280 + ellipsis
		t.Fatalf("len %d", len([]rune(s)))
	}
}
