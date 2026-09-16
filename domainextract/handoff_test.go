package domainextract

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/approvals"
	"manifest/connectorhandoff"
)

// Synthetic handoffs exercise mechanics only, never live semantic parity.
func prepareHandoff(t *testing.T, data, ritual string, revision uint64) string {
	t.Helper()
	root := t.TempDir()
	replay := false
	h := Handoff{1, "extractor/" + ritual, revision, &replay, strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)}
	b, _ := json.Marshal(h)
	hash := fmt.Sprintf("%x", sha256.Sum256(b))
	if err := atomic(filepath.Join(data, "domain-extraction", "handoffs", h.Duty, hash+".json"), b); err != nil {
		t.Fatal(err)
	}
	f := connectorhandoff.RecordFence{Version: 1, Revision: revision, Duty: h.Duty, PreviousOwner: "excalibur", Owner: "manifest", Action: "transfer", Evidence: hash, At: "2026-09-16T00:00:00Z"}
	b, _ = json.Marshal(f)
	if err := atomic(filepath.Join(root, "vessel/state/dispatch-fence", h.Duty, fmt.Sprintf("%020d.json", revision)), b); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestHandoffReadiness(t *testing.T) {
	for _, ritual := range []string{"aion", "real-estate", "ooda-email"} {
		t.Run(ritual, func(t *testing.T) {
			data, root := t.TempDir(), t.TempDir()
			if state, _ := Readiness(data, root, ritual, false); state != "legacy-retiring" {
				t.Fatal(state)
			}
			if state, _ := Readiness(data, root, ritual, true); state != "blocked" {
				t.Fatal(state)
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("readiness wrote state")
			}
			root = prepareHandoff(t, data, ritual, 1)
			if state, _ := Readiness(data, root, ritual, true); state != "successor-enabled" {
				t.Fatal(state)
			}
			if state, _ := Readiness(data, root, ritual, false); state != "successor-disabled" {
				t.Fatal(state)
			}
			f, _ := connectorhandoff.DutyFenceSnapshot(root, "extractor/"+ritual)
			path := filepath.Join(data, "domain-extraction/handoffs", f.Duty, f.Evidence+".json")
			b, _ := os.ReadFile(path)
			b = append(b, ' ')
			os.WriteFile(path, b, 0600)
			if _, err := ReadHandoff(data, f); err == nil {
				t.Fatal("changed receipt accepted")
			}
		})
	}
}

func TestHistoricalJobsQuarantinedAndFenceHeld(t *testing.T) {
	data, vault := t.TempDir(), t.TempDir()
	root := prepareHandoff(t, data, "aion", 1)
	input := inputFixture()
	writeInput(t, vault, input)
	ap := approvals.NewStore(t.TempDir())
	s := New(context.Background(), data, vault, root, Config{Aion: true}, nil, ap)
	candidates, _ := ValidateReply(input, replyFixture)
	j := Job{ID: input.ID(), Input: input, State: "verified", Candidates: candidates}
	s.save(j)
	s.sweep()
	b, _ := os.ReadFile(filepath.Join(s.dir, j.ID+".json"))
	json.Unmarshal(b, &j)
	if j.State != "uncertain" || j.Replay || len(ap.List("pending")) != 0 {
		t.Fatal(j)
	}
	// A held shared fence prevents even verified publication on another process.
	j.Version = 1
	j.OwnershipRevision = 1
	j.State = "verified"
	s.save(j)
	_, release, err := connectorhandoff.AcquireDutyFence(root, "extractor/aion")
	if err != nil {
		t.Fatal(err)
	}
	s.sweep()
	release()
	if len(ap.List("pending")) != 0 {
		t.Fatal("published under another owner's lock")
	}
	// A job from another revision can never publish under this one.
	j.OwnershipRevision = 2
	s.save(j)
	s.sweep()
	b, _ = os.ReadFile(filepath.Join(s.dir, j.ID+".json"))
	json.Unmarshal(b, &j)
	if j.State != "refused" || len(ap.List("pending")) != 0 {
		t.Fatal(j)
	}
}

func TestReceiptContractRejectsUnreviewedOrReplayEvidence(t *testing.T) {
	for _, mode := range []string{"missing-replay", "replay", "wrong-duty", "wrong-revision", "future-version", "missing-semantic", "duplicate", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			data := t.TempDir()
			root := prepareHandoff(t, data, "aion", 1)
			f, _ := connectorhandoff.DutyFenceSnapshot(root, "extractor/aion")
			b, _ := os.ReadFile(filepath.Join(data, "domain-extraction/handoffs", f.Duty, f.Evidence+".json"))
			var fields map[string]any
			json.Unmarshal(b, &fields)
			switch mode {
			case "missing-replay":
				delete(fields, "replay")
			case "replay":
				fields["replay"] = true
			case "wrong-duty":
				fields["duty"] = "extractor/real-estate"
			case "wrong-revision":
				fields["revision"] = 2
			case "future-version":
				fields["version"] = 2
			case "missing-semantic":
				delete(fields, "liveSemanticSha256")
			case "unknown":
				fields["autoApprove"] = true
			}
			b, _ = json.Marshal(fields)
			if mode == "duplicate" {
				b = append([]byte(`{"version":1,`), b[1:]...)
			}
			f.Evidence = fmt.Sprintf("%x", sha256.Sum256(b))
			if err := atomic(filepath.Join(data, "domain-extraction/handoffs", f.Duty, f.Evidence+".json"), b); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadHandoff(data, f); err == nil {
				t.Fatal("unsafe receipt accepted")
			}
		})
	}
}
