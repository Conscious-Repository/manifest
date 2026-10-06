package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/goals"
	"manifest/jev"
	"manifest/typesafe"
	"manifest/vault"
	"manifest/vaultindex"
)

// ownerServer: the screen fixture plus a vault holding one transcript, the
// team roster and an Aion goal tree.
func ownerServer(t *testing.T, pick string, conf float64) (*Server, string, *typesafe.Fake) {
	t.Helper()
	s, root, _, vaultDir := screenServerVault(t, 0.9)
	write := func(rel, body string) {
		p := filepath.Join(vaultDir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("log/2026-10-01 team sync.md", "---\ncategories: [sync, aion]\n---\n**Benjamin:** Where are we on the coil?\n**Heye Groß:** Still waiting on parts.\n**Benjamin:** Hannah, can you run the die back experiment next week to see why we cannot replicate the paper?\n**Hannah Zmuda:** Yes.\n")
	write("log/2026-10-02 comp review.md", "---\ncategories: [aion]\n---\n**Benjamin:** I'll finalize the compensation letter for the offer.\n")
	write("system/aion/people.md", "- [initials:: BA] [name:: Benjamin Anderson] [role:: CEO]\n- [initials:: HZ] [name:: Hannah Zmuda] [role:: Founding Scientist]\n- [initials:: HG] [name:: Heye Groß] [role:: Founding Engineer]\n")
	write("goals.md", "# Goals\n\n## Aion\n\n### Rocks (90-day)\n- [ ] Series A 15M [goal:: aion/series-a-15m] [aliases:: fundraising]\n- [ ] Mouse data [goal:: aion/mouse-to-pig]\n")
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vaultDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	s.index = ix
	vidx, err := vault.NewIndex(vault.Config{Root: vaultDir, GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	s.goals = goals.NewStore(vidx, vaultDir, "goals.md", testWrite)
	f := &typesafe.Fake{Answers: map[string]typesafe.Answer{"owner": typesafe.ChoiceAnswer(pick, conf, map[string]float64{pick: conf})}}
	s.jevJudge = &jev.Judge{Eval: f}
	return s, root, f
}

func ownerRows(t *testing.T, s *Server, settled func(map[string]approvalRow) bool) map[string]approvalRow {
	t.Helper()
	return screenRows(t, s, settled)
}

// The card shows the line and who the transcript says spoke it; a confident
// owner pick that differs is offered, a timid or agreeing one is not; a held
// meeting never reaches Jev; a goal that names no goal is flagged.
func TestOwnerEvidenceAndConfidentSuggestion(t *testing.T) {
	s, root, f := ownerServer(t, "HZ", 0.9)
	plantAionTaskQuote(t, root, "dieback", "Run the die-back experiment", "BA", "aion/mouse-to-pig", "log/2026-10-01 team sync", "Hannah, can you run the die back experiment next week")
	plantAionTaskQuote(t, root, "ghost", "Plan the ultrasound work group", "BA", "aion/ultrasound-platform", "log/2026-10-01 team sync", "Still waiting on parts")
	plantAionTaskQuote(t, root, "alias", "Send the deck", "BA", "aion/fundraising", "log/2026-10-01 team sync", "Where are we on the coil")
	plantAionTaskQuote(t, root, "held", "Finalize the compensation letter", "BA", "aion/series-a-15m", "log/2026-10-02 comp review", "finalize the compensation letter")
	rows := ownerRows(t, s, func(r map[string]approvalRow) bool {
		return r["dieback"].OwnerEvidence != nil && r["dieback"].OwnerEvidence.Suggest != ""
	})
	d := rows["dieback"].OwnerEvidence
	if d == nil || d.Speaker != "Benjamin" || !strings.Contains(d.Quote, "die back") {
		t.Fatalf("the card shows the line and its diarized speaker: %+v", d)
	}
	if d.Suggest != "HZ" || d.SuggestName != "Hannah Zmuda" {
		t.Fatalf("a confident, different owner pick is offered: %+v", d)
	}
	if g := rows["ghost"].OwnerEvidence; g == nil || !g.RockMissing {
		t.Fatalf("a tether that names no goal is flagged: %+v", g)
	}
	if a := rows["alias"].OwnerEvidence; a != nil && a.RockMissing {
		t.Fatalf("a goal's alias is a goal: %+v", a)
	}
	if h := rows["held"].OwnerEvidence; h == nil || h.Suggest != "" {
		t.Fatalf("a held meeting still shows its line but gets no Jev pick: %+v", h)
	}
	for _, r := range f.Requests {
		if strings.Contains(strings.ToLower(mustJSON(t, r.State)), "compensation") {
			t.Fatal("a held meeting reached TypeSafe")
		}
	}
	if strings.Contains(rows["dieback"].Proposal.Body, `"owner":"HZ"`) {
		t.Fatal("a suggestion never edits the card")
	}

	// a timid pick, or one that agrees with the card, is not offered
	for _, c := range []struct {
		pick string
		conf float64
	}{{"HZ", 0.6}, {"BA", 0.95}} {
		s2, root2, _ := ownerServer(t, c.pick, c.conf)
		plantAionTaskQuote(t, root2, "dieback", "Run the die-back experiment", "BA", "aion/mouse-to-pig", "log/2026-10-01 team sync", "Hannah, can you run the die back experiment next week")
		ownerRows(t, s2, nil) // queues the judgment
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			c := s2.jevCache()
			c.mu.Lock()
			e, ok := c.mem[jevKindOwner+"/dieback"]
			c.mu.Unlock()
			if ok && e.State != "" {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		rows := ownerRows(t, s2, nil)
		if v := rows["dieback"].OwnerEvidence; v == nil || v.Suggest != "" {
			t.Fatalf("pick %s at %.2f must not be offered: %+v", c.pick, c.conf, v)
		}
	}
}

func plantAionTaskQuote(t *testing.T, root, id, title, owner, rock, source, quote string) {
	t.Helper()
	plantAionTask(t, root, id, title, owner, rock, source)
	p := filepath.Join(root, "artifacts", "approvals", "pending", id+".md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Replace(string(b), `"captured":"2026-10-01"`, `"captured":"2026-10-01","quote":"`+quote+`"`, 1)
	if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}
