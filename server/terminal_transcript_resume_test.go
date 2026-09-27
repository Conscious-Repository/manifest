package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A growing transcript resumes its cached projection from the last complete
// line instead of re-reading the file: the result is exactly what a fresh
// whole-file read gives, at every cut — between lines, mid-line, between a
// tool call and its result, before and after a settings record.
func TestTranscriptResumeEqualsFullRead(t *testing.T) {
	for _, tc := range []struct{ kind, file string }{{"claude", "claude_session.jsonl"}, {"codex", "codex_rollout.jsonl"}} {
		src, err := os.ReadFile(filepath.Join("testdata", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		want := parseTranscript(tc.kind, bytes.NewReader(src))
		if want.Turns == nil {
			want.Turns = []termTurn{}
		}
		cuts := []int{}
		for i := 1; i < len(src); i += max(1, len(src)/97) { // mid-line cuts
			cuts = append(cuts, i)
		}
		for i, c := range src { // every line boundary
			if c == '\n' {
				cuts = append(cuts, i+1)
			}
		}
		for _, cut := range cuts {
			path := filepath.Join(t.TempDir(), "s.jsonl")
			if err := os.WriteFile(path, src[:cut], 0o600); err != nil {
				t.Fatal(err)
			}
			if _, ok := readTranscript(tc.kind, path, 0); !ok {
				t.Fatal("head read failed")
			}
			// the rest arrives in two appends; the middle read resumes too
			mid := cut + (len(src)-cut)/2
			for _, upto := range []int{mid, len(src)} {
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = f.Write(src[cut:upto])
				f.Close()
				cut = upto
				if _, ok := readTranscript(tc.kind, path, 0); !ok {
					t.Fatal("resumed read failed")
				}
			}
			got, _ := readTranscript(tc.kind, path, 0)
			if !reflect.DeepEqual(got, want) {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(want)
				t.Fatalf("%s cut at %d: resumed projection differs\n got %s\nwant %s", tc.kind, cut, g, w)
			}
		}
	}
}

// A resumed projection never mutates the value earlier callers hold.
func TestTranscriptResumeLeavesEarlierReadsAlone(t *testing.T) {
	src, _ := os.ReadFile(filepath.Join("testdata", "claude_session.jsonl"))
	lines := bytes.SplitAfter(src, []byte("\n"))
	head := bytes.Join(lines[:len(lines)/2], nil)
	path := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(path, head, 0o600)
	first, _ := readTranscript("claude", path, 0)
	before, _ := json.Marshal(first)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.Write(src[len(head):])
	f.Close()
	readTranscript("claude", path, 0)
	after, _ := json.Marshal(first)
	if !bytes.Equal(before, after) {
		t.Fatalf("an earlier read changed under a resume:\n%s\n%s", before, after)
	}
}

// A file rewritten in place (same inode, different bytes, larger) is read
// whole: the bytes before the cached offset no longer match.
func TestTranscriptRewriteIsReadWhole(t *testing.T) {
	src, _ := os.ReadFile(filepath.Join("testdata", "claude_session.jsonl"))
	path := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(path, src, 0o600)
	readTranscript("claude", path, 0)
	other := bytes.Replace(src, []byte(`"model"`), []byte(`"MODEL"`), 1)
	other = append(other, src...)
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	_, _ = f.Write(other)
	f.Close()
	got, _ := readTranscript("claude", path, 0)
	want := parseTranscript("claude", bytes.NewReader(other))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rewritten file: got %d turns, want %d", len(got.Turns), len(want.Turns))
	}
}

// Budget: a long live session (~20 MB) that grows by one exchange costs a
// read of the new bytes, not of the file. Measured 2026-09-27 on metis: a
// 27.6 MB Claude transcript took 330 ms to re-read whole on every inbox
// poll while it was being written.
func TestTranscriptGrowthBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.jsonl")
	var buf bytes.Buffer
	pad := strings.Repeat("output line of a long tool result ", 60)
	exchange := func(n int) {
		ts := time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC).Add(time.Duration(n) * time.Second).Format(time.RFC3339)
		fmt.Fprintf(&buf, `{"type":"user","timestamp":%q,"message":{"role":"user","content":"question %d"}}`+"\n", ts, n)
		fmt.Fprintf(&buf, `{"type":"assistant","timestamp":%q,"message":{"role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"looking"},{"type":"tool_use","id":"tu%d","name":"Bash","input":{"command":"ls"}}]}}`+"\n", ts, n)
		fmt.Fprintf(&buf, `{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu%d","content":%q}]}}`+"\n", ts, n, pad)
		fmt.Fprintf(&buf, `{"type":"assistant","timestamp":%q,"message":{"role":"assistant","model":"claude-opus-5-5","stop_reason":"end_turn","content":[{"type":"text","text":"answer %d"}]}}`+"\n", ts, n)
	}
	n := 0
	for buf.Len() < 20<<20 {
		exchange(n)
		n++
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readTranscript("claude", path, 0); !ok {
		t.Fatal("read failed")
	}
	size := int64(buf.Len())
	buf.Reset()
	exchange(n)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.Write(buf.Bytes())
	f.Close()
	before := transcriptParsedBytes.Load()
	start := time.Now()
	tr, _ := readTranscript("claude", path, 0)
	took := time.Since(start)
	parsed := transcriptParsedBytes.Load() - before
	if parsed > int64(buf.Len())*4 { // other tests may parse concurrently; a re-read would be ~20 MB
		t.Fatalf("growth read parsed %d bytes of a %d-byte file (appended %d)", parsed, size, buf.Len())
	}
	if took > 150*time.Millisecond { // measured ~1 ms; a whole re-read is ~250 ms
		t.Fatalf("growth read took %v", took)
	}
	if last := tr.Turns[len(tr.Turns)-1]; last.Who != "assistant" || len(tr.Turns) != 2*(n+1) {
		t.Fatalf("turns = %d, last %+v", len(tr.Turns), last)
	}
}
