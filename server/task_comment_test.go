package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapTaskComment(t *testing.T) {
	for _, phase := range []string{"comment", "ask", "", "other", "plan", "go"} {
		for _, tc := range []struct{ name, input, want string }{
			{"short", "Answer.\n\nA list follows.", "Answer.\n\nA list follows."},
			{"exact", strings.Repeat("word ", 280), strings.Repeat("word ", 280)},
			{"sentence", strings.Repeat("word ", 269) + "done. " + strings.Repeat("extra ", 20), strings.Repeat("word ", 269) + "done.…"},
			{"word", strings.Repeat("éclair\n", 281), strings.TrimSpace(strings.Repeat("éclair\n", 280)) + "…"},
		} {
			t.Run(phase+"/"+tc.name, func(t *testing.T) {
				want := tc.want
				if phase == "plan" || phase == "go" {
					want = tc.input
				}
				got := capTaskComment(phase, tc.input)
				if got != want || !utf8.ValidString(got) {
					t.Fatalf("unexpected boundary: %q", got)
				}
				if isTaskCommentPhase(phase) && len(strings.Fields(got)) > 280 {
					t.Fatal("over ceiling")
				}
			})
		}
	}
}

func TestTaskCommentPromptPhases(t *testing.T) {
	for _, phase := range []string{"comment", "ask", "", "plan", "go"} {
		for _, intent := range []string{"", "brief", "info"} {
			srv := personaFixture(t)
			h := srv.findHarness("hermes")
			if err := srv.spoolTaskWorkOrder(h, "inbox/research-zoning", phase, "owner input", intent); err != nil {
				t.Fatal(err)
			}
			prompt := h.Spirits.Queued()[0].Request
			if got := strings.Contains(prompt, "at most 280 words"); got != isTaskCommentPhase(phase) {
				t.Fatalf("phase %q intent %q: %s", phase, intent, prompt)
			}
			if intent != "" && !strings.Contains(prompt, "PERSONA (how to respond") {
				t.Fatal("lost explicit persona")
			}
		}
	}
	srv := &Server{}
	if !strings.Contains(srv.taskCommentPrompt("ask"), "at most 280 words") {
		t.Fatal("missing fallback without records")
	}
	srv = personaFixture(t)
	if err := os.WriteFile(filepath.Join(srv.personasCfg.absDir, "comment.md"), []byte("---\nintent: comment\nenabled: true\n---\nOwner edited wording."), 0644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(srv.taskCommentPrompt("comment"), "Owner edited wording.") {
		t.Fatal("record edit ignored")
	}
}

func TestTaskCommentMaterializationPhases(t *testing.T) {
	brief := "# Plan\n\n" + strings.Repeat("A complete sentence. ", 100)
	for _, phase := range []string{"comment", "ask", "plan", "go"} {
		for _, intent := range []string{"", "info"} {
			srv, _ := panelFixture(t)
			id := "inbox/brevity"
			srv.materializeHermesBrief(id, "", phase, intent, brief)
			var reply string
			for _, c := range srv.listThread(id) {
				reply += c.Text
			}
			if isTaskCommentPhase(phase) {
				if reply != capTaskComment(phase, brief) {
					t.Fatalf("%s/%s reply not capped: %q", phase, intent, reply)
				}
				if srv.readPlanRecord(id).Plan != "" {
					t.Fatal("comment wrote a plan")
				}
			} else if phase == "plan" && intent == "" {
				if strings.TrimSpace(srv.readPlanRecord(id).Plan) != strings.TrimSpace(brief) {
					t.Fatal("plan was truncated")
				}
			} else if !strings.Contains(reply, strings.TrimSpace(brief)) {
				t.Fatalf("%s result was truncated", phase)
			}
		}
	}
}

func TestUntaggedCommentIngestionCap(t *testing.T) {
	srv := personaFixture(t)
	id := "inbox/research-zoning"
	brief := strings.Repeat("A complete sentence. ", 100)
	fakeRunReq(t, srv, "brevity", "ask [todo:: "+id+"] [phase:: comment]", brief)
	sweep(srv)
	th := srv.listThread(id)
	if len(th) != 1 || th[0].Text != capTaskComment("comment", strings.TrimSpace(brief)) {
		t.Fatalf("unexpected reply: %+v", th)
	}
	if srv.readPlanRecord(id).Plan != "" {
		t.Fatal("comment wrote plan")
	}
	sweep(srv)
	if len(srv.listThread(id)) != 1 {
		t.Fatal("duplicate reply")
	}
}
