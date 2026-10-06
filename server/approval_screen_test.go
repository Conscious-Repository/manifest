package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/aion"
	"manifest/approvals"
	"manifest/jev"
	"manifest/typesafe"
)

// screenServer: a harness with an aion backlog, a tier map and a Jev fake
// answering P(track) = worth for every card.
func screenServer(t *testing.T, worth float64) (*Server, string, *typesafe.Fake) {
	s, root, f, _ := screenServerVault(t, worth)
	return s, root, f
}

func screenServerVault(t *testing.T, worth float64) (*Server, string, *typesafe.Fake, string) {
	t.Helper()
	t.Setenv(typesafe.EnvKey, "")
	tm := aion.TierMap{
		"2026-10-01 team sync.md":         {Tier: aion.TierOpen, Reason: "t", Bytes: 1},
		"2026-09-22 hiring brainstorm.md": {Tier: aion.TierInternal, Reason: "t", Bytes: 1},
		"2026-10-02 comp review.md":       {Tier: aion.TierHeld, Reason: "t", Bytes: 1},
	}
	s, root := visibilityServer(t, tm)
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "system/aion"), 0o755); err != nil {
		t.Fatal(err)
	}
	backlog := "# aion backlog\n\n" +
		"- [ ] Bring the MRI device to St. Louis before the Bedrock visit [id:: aion-bl/bring] [kind:: task] [owner:: BA] [source:: [[log/2026-09-22 hiring brainstorm]]] [captured:: 2026-09-22] [status:: open]\n" +
		"- [x] Send the investor update draft [id:: aion-bl/send] [kind:: task] [owner:: BA] [source:: [[log/2026-09-22 hiring brainstorm]]] [captured:: 2026-09-22] [status:: done] [done:: 2026-09-24]\n"
	if err := os.WriteFile(filepath.Join(vault, "system/aion/backlog.md"), []byte(backlog), 0o644); err != nil {
		t.Fatal(err)
	}
	s.aion = aion.NewStore(vault, "system/aion", nil)
	f := &typesafe.Fake{Answers: map[string]typesafe.Answer{"worth": typesafe.NoulAnswer(worth)}}
	s.jevJudge = &jev.Judge{Eval: f}
	s.jevAdviceDir = filepath.Join(t.TempDir(), "jev")
	return s, root, f, vault
}

func plantAionTask(t *testing.T, root, id, title, owner, rock, source string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"kind": "task", "title": title, "owner": owner, "rock": rock, "status": "open", "sources": []string{source}, "captured": "2026-10-01"})
	body := "---\ntype: aion-backlog\nid: " + id + "\naction: aion: task — " + title + "\nagent: extractor\nritual: aion\ncreated: " + time.Now().UTC().Format(time.RFC3339) +
		"\napply-path: system/aion/backlog.md\n---\n\nSource: " + source + "\n\n````aion\n" + string(payload) + "\n````\n"
	dir := filepath.Join(root, "artifacts", "approvals", "pending")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func screenRows(t *testing.T, s *Server, wait func(map[string]approvalRow) bool) map[string]approvalRow {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		rows := map[string]approvalRow{}
		for _, r := range s.approvalRows(nil) {
			rows[r.ID] = r
		}
		if wait == nil || wait(rows) || time.Now().After(deadline) {
			return rows
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Duplicates fold with the item they restate; the same meeting's own item is
// not its own duplicate; nothing folded is rejected or loses its controls.
func TestScreenFoldsWhatIsAlreadyTrackedOrDone(t *testing.T) {
	s, root, _ := screenServer(t, 0.9)
	plantAionTask(t, root, "dup", "Bring the MRI device to St Louis before the Bedrock visit", "BA", "aion/human-scale-spec", "log/2026-10-01 team sync")
	plantAionTask(t, root, "done", "Send the investor update draft", "BA", "aion/series-a-15m", "log/2026-10-01 team sync")
	plantAionTask(t, root, "same", "Bring the MRI device to St. Louis before the Bedrock visit", "BA", "aion/human-scale-spec", "log/2026-09-22 hiring brainstorm")
	plantAionTask(t, root, "new", "Order a replacement gradient amplifier", "HZ", "aion/human-scale-spec", "log/2026-10-01 team sync")
	rows := screenRows(t, s, func(r map[string]approvalRow) bool {
		return r["new"].Screen != nil && r["new"].Screen.State == jevStateAdvised
	})
	if v := rows["dup"].Screen; v == nil || !v.Fold || !strings.Contains(strings.Join(v.Reasons, " "), "already tracked") {
		t.Fatalf("a restated open item folds as already tracked: %+v", v)
	}
	if v := rows["done"].Screen; v == nil || !v.Fold || !strings.Contains(strings.Join(v.Reasons, " "), "already done on 2026-09-24") {
		t.Fatalf("a restated done item folds as already done: %+v", v)
	}
	if v := rows["same"].Screen; v == nil || v.Fold {
		t.Fatalf("the backlog item's own meeting is not a duplicate of itself: %+v", v)
	}
	if v := rows["new"].Screen; v == nil || v.Fold || v.Worth == nil || *v.Worth != 0.9 {
		t.Fatalf("new work Jev expects you to track stays in the lane: %+v", v)
	}
	if !rows["dup"].Allowed {
		t.Fatal("a folded card keeps its controls")
	}
}

// Jev's "worth tracking" folds below 0.2 — judged only for open/internal
// meetings; a held meeting's card is never sent and never folded by it.
func TestScreenFoldsUnlikelyAndNeverSendsHeld(t *testing.T) {
	s, root, f := screenServer(t, 0.1)
	plantAionTask(t, root, "meh", "Mention the new mug design at standup", "BA", "", "log/2026-10-01 team sync")
	plantAionTask(t, root, "held", "Finalize Heye's compensation letter", "BA", "aion/operations-health", "log/2026-10-02 comp review")
	rows := screenRows(t, s, func(r map[string]approvalRow) bool {
		return r["meh"].Screen != nil && r["meh"].Screen.State == jevStateAdvised
	})
	if v := rows["meh"].Screen; v == nil || !v.Fold || !strings.Contains(strings.Join(v.Reasons, " "), "unlikely you'd track it") {
		t.Fatalf("a card Jev judges unlikely folds with the reason: %+v", v)
	}
	if v := rows["held"].Screen; v == nil || v.Fold || v.State != "skipped" {
		t.Fatalf("a held meeting's card is not judged: %+v", v)
	}
	for _, r := range f.Requests {
		b, _ := json.Marshal(r.State)
		if strings.Contains(string(b), "compensation") {
			t.Fatal("a held meeting's card reached TypeSafe")
		}
	}
	if len(s.approvalsFor("held").List("rejected")) != 0 {
		t.Fatal("the screen never rejects")
	}
}

// Your edits are the labels: owner changes are recorded, and a change you
// keep making on a meeting series comes back as a one-tap hint.
func TestApprovalEditsLearnOwnerByMeetingSeries(t *testing.T) {
	s, root, _ := screenServer(t, 0.9)
	for i, src := range []string{"log/2026-09-04 standing waves", "log/2026-09-11 standing waves"} {
		id := "e" + string(rune('1'+i))
		plantAionTask(t, root, id, "Run the coil bench test", "BA", "aion/human-scale-spec", src)
		before, err := s.approvalsFor(id).LoadPending(id)
		if err != nil {
			t.Fatal(err)
		}
		pl, _ := aion.ParsePayloadFence(before.Body, aion.PayloadFence)
		pl.Owner = "HZ"
		s.recordApprovalEdits(before, pl)
	}
	if got := s.approvalEdits(); len(got) != 2 || got[0].Field != "owner" || got[0].From != "BA" || got[0].To != "HZ" || got[0].Meeting != "2026-09-04 standing waves" {
		t.Fatalf("each owner change is recorded with its meeting: %+v", got)
	}
	plantAionTask(t, root, "next", "Calibrate the gradient coil", "BA", "aion/human-scale-spec", "log/2026-09-18 standing waves")
	plantAionTask(t, root, "other", "Email the landlord", "BA", "aion/operations-health", "log/2026-09-18 rj sync")
	rows := screenRows(t, s, nil)
	if h := rows["next"].EditHints; len(h) != 1 || h[0].Field != "owner" || h[0].To != "HZ" || h[0].Count != 2 || h[0].Scope != "standing waves" {
		t.Fatalf("the series you keep correcting offers the correction: %+v", h)
	}
	if h := rows["other"].EditHints; len(h) != 0 {
		t.Fatalf("two edits on another series are not a rule for every meeting: %+v", h)
	}
	if rows["next"].Proposal.Body == "" || strings.Contains(rows["next"].Proposal.Body, `"owner":"HZ"`) {
		t.Fatal("a hint never edits the card by itself")
	}
}

var _ = approvals.TypeAionBacklog
