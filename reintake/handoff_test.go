package reintake

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"manifest/hermes"
)

func TestProductionCanonicalOriginalAndExtractIdentity(t *testing.T) {
	dir, cfg, a, c, reply := productionFixture(t)
	// The original binary CAS must remain payload.doc; the extracted text is
	// independently hash-addressed and supplied verbatim to the bounded model.
	original := fmtSource([]byte("synthetic original binary"))
	reply = strings.ReplaceAll(reply, c.Source, original)
	c.TextSource, c.Source, c.Documents, c.Context = c.Source, original, []string{original}, "contractors/properties/work nodes from existing domain"
	_, r, err := runStaged(context.Background(), dir, cfg, a, c, func(_ context.Context, _ hermes.DutyAuthority, prompt string) (boundedCompletion, error) {
		if !strings.Contains(prompt, c.TextSource) || !strings.Contains(prompt, original) || !strings.Contains(prompt, "synthetic confidential document sentinel") || !strings.Contains(prompt, c.Context) {
			t.Fatal("lost exact identity/context")
		}
		return boundedCompletion{reply: reply, verified: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "verified" {
		t.Fatal(r)
	}
}

func TestProductionUploadReservationConcurrentAndRestart(t *testing.T) {
	dir := t.TempDir()
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ReserveUpload(dir) == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal(wins.Load())
	}
	if ReserveUpload(dir) == nil {
		t.Fatal("restart retried")
	}
	if PilotStatus(dir) != "stopped; owner review/reset required" {
		t.Fatal(PilotStatus(dir))
	}
}

func TestProductionStageExtractAndReceiptRecheck(t *testing.T) {
	for _, text := range []string{"", strings.Repeat("x", 16001), "\x00", "\xff"} {
		if _, err := StageExtract(t.TempDir(), text); err == nil {
			t.Fatal("bad text accepted")
		}
	}
	dir, cfg, a, c, reply := productionFixture(t)
	if err := ReserveUpload(dir); err != nil {
		t.Fatal(err)
	}
	p, r, err := runStaged(context.Background(), dir, cfg, a, c, func(context.Context, hermes.DutyAuthority, string) (boundedCompletion, error) {
		return boundedCompletion{reply: reply, verified: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckCandidateReceipt(dir, c, p, r); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(dir, ProductionPath, "run.jsonl")
	original, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(string(original), `"state":"verified"`, `"state":"verified","unknown":true`, 1), strings.Replace(string(original), `"state":"verified"`, `"state":"verified","state":"verified"`, 1), string(original) + "{}\n"} {
		if err := os.WriteFile(receiptPath, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if CheckCandidateReceipt(dir, c, p, r) == nil {
			t.Fatal("malformed durable evidence permitted filing")
		}
	}
	if err := os.WriteFile(receiptPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, ProductionPath, "run.jsonl")); err != nil {
		t.Fatal(err)
	}
	if CheckCandidateReceipt(dir, c, p, r) == nil {
		t.Fatal("lost receipt permitted filing")
	}
	if ReserveUpload(dir) == nil {
		t.Fatal("lost evidence reopened upload")
	}
	fresh := t.TempDir()
	source, err := StageExtract(fresh, "exact extract text")
	if err != nil || source != fmtSource([]byte("exact extract text")) {
		t.Fatal(source, err)
	}
	root, err := productionRoot(fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	text, err := readStaged(root, source)
	if err != nil || text != "exact extract text" {
		t.Fatal(text, err)
	}
	if _, err := StageExtract(fresh, text); err == nil {
		t.Fatal("staging overwritten")
	}
}
