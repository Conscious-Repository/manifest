package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/spirits"
	"manifest/threads"
)

func codingFixture(t *testing.T) *Server {
	t.Helper()
	s, _ := assignFixture(t)
	dir := t.TempDir()
	s.UseTerminal(filepath.Join(dir, "terminals.json"), filepath.Join(dir, "tmux"), dir)
	repo := filepath.Join(dir, "checkout")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	s.UseCodingRepo(repo)
	// Exercise the real socket adapter and persisted launch journal without an agent.
	s.terminal.run = func(args ...string) ([]byte, error) { return nil, nil }
	s.terminal.herdr = herdrFixture(t, func(c net.Conn, r herdrFixtureRequest) {
		switch r.Method {
		case "session.snapshot":
			herdrFixtureSnapshot(c, "working", 1)
		case "workspace.create":
			herdrFixtureReply(c, map[string]any{"root_pane": herdrFixturePane("unknown", 1)})
		default:
			herdrFixtureReply(c, map[string]any{})
		}
	})
	s.terminal.herdr.server = s
	return s
}

func TestCodingOwnersDispatchAndReview(t *testing.T) {
	for _, owner := range []string{"claude", "codex"} {
		t.Run(owner, func(t *testing.T) {
			s := codingFixture(t)
			task := "inbox/wire-the-fence"
			if s.agentHarness("agent:"+owner) != owner {
				t.Fatal("owner does not resolve")
			}
			w := httptest.NewRecorder()
			s.handleAgentChatRoster(w, httptest.NewRequest("GET", "/api/agents/chat/roster", nil))
			if !strings.Contains(w.Body.String(), `"name":"`+owner+`"`) {
				t.Fatal(w.Body.String())
			}
			roster := s.agentRoster()
			found := false
			for _, row := range roster {
				if row["id"] == "agent:"+owner {
					found = true
				}
			}
			if !found {
				t.Fatal("missing picker owner")
			}
			if len(s.teamAgentRoster()) != 0 {
				t.Fatal("coding owners leaked to team portal")
			}
			if _, err := s.assignTask(s.ownerIdentity(), task, "agent:"+owner); err != nil {
				t.Fatal(err)
			}
			if got := s.readPlanRecord(task).Assignee; got != "agent:"+owner {
				t.Fatal(got)
			}
			h := s.findHarness(owner)
			runs := h.Spirits.Runs()
			if len(runs) != 1 || !strings.Contains(runs[0].Request, "[phase:: go]") {
				t.Fatalf("assignment plan-gated: %+v", runs)
			}
			run := runs[0]
			sessions := s.terminal.load()
			if len(sessions) != 1 || sessions[0].Kind != owner || !sessions[0].Started {
				t.Fatalf("sessions %+v", sessions)
			}
			raw, err := os.ReadFile(sessions[0].BoardBrief)
			if err != nil || !strings.Contains(string(raw), "wire the fence") || !strings.Contains(string(raw), "commit and push") {
				t.Fatalf("brief %v %s", err, raw)
			}
			if strings.Contains(sessions[0].execLaunch(), "Read the complete work order") {
				t.Fatal("reopen replays task")
			}
			if got := s.delegationIndex()[task]; got.State != "running" {
				t.Fatalf("premature completion %+v", got)
			}
			result := codingResult{Status: "completed", Summary: "Implemented the fix. Tests passed.", ArtifactURL: "https://github.com/Conscious-Repository/manifest/commit/1234567"}
			body, _ := json.Marshal(result)
			if err := boardWrite(filepath.Join(filepath.Dir(sessions[0].BoardBrief), "result.json"), body); err != nil {
				t.Fatal(err)
			}
			// Simulate a process restart: no session goroutine or live pane needed.
			s.UseTerminal(s.terminal.regPath, s.terminal.tmuxTmp, s.terminal.defaultWd)
			index := s.delegationIndex()
			d := index[task]
			if d.State != "done" || d.Phase != "go" || d.ArtifactRef == "" {
				t.Fatalf("not the existing Review projection: %+v", d)
			}
			s.agentLoopSweep(index)
			s.agentLoopSweep(index)
			count := 0
			for _, c := range s.listThread(task) {
				if c.Author == "agent:"+owner && strings.Contains(c.Text, "result delivered") {
					count++
					if !strings.Contains(c.Text, result.ArtifactURL) || c.Meta["artifactRef"] != d.ArtifactRef {
						t.Fatalf("missing review link %+v", c)
					}
				}
			}
			if count != 1 {
				t.Fatalf("result notes = %d", count)
			}
			w = httptest.NewRecorder()
			s.handleSpiritsFileGet(w, httptest.NewRequest("GET", "/api/spirits/file?harness="+owner+"&path="+d.ArtifactRef, nil))
			if w.Code != 200 || !strings.Contains(w.Body.String(), result.ArtifactURL) {
				t.Fatalf("artifact %d %s", w.Code, w.Body.String())
			}
			if !s.threads.private.HasAction(task, threads.ActResult, run.ID) {
				t.Fatal("missing result marker")
			}
		})
	}
}

func TestCodingMentionAndDo(t *testing.T) {
	for _, owner := range []string{"claude", "codex"} {
		for _, mode := range []string{"comment", "ask", "do", "plan"} {
			t.Run(owner+"/"+mode, func(t *testing.T) {
				s := codingFixture(t)
				task := "inbox/wire-the-fence"
				text := "@" + owner + " fix the fence using the blue brief"
				requestMode := mode
				if mode == "plan" {
					text = "@" + owner + "::plan draft the complete migration plan"
					requestMode = "comment"
				}
				if _, err := s.postAndDispatch(task, requestMode, "agent:"+owner, nil, nil, text); err != nil {
					t.Fatal(err)
				}
				h := s.findHarness(owner)
				runs := h.Spirits.Runs()
				if len(runs) != 1 {
					t.Fatalf("mention dispatch %+v", runs)
				}
				want := "go"
				if mode == "plan" {
					want = "plan"
				}
				if !strings.Contains(runs[0].Request, "[phase:: "+want+"]") {
					t.Fatalf("wrong phase %+v", runs)
				}
				brief, _ := os.ReadFile(s.terminal.load()[0].BoardBrief)
				if !strings.Contains(string(brief), text) {
					t.Fatal("lost opening brief/thread")
				}
			})
		}
	}
}

func TestCodingFailureAndBusy(t *testing.T) {
	s := codingFixture(t)
	task := "inbox/wire-the-fence"
	h := s.findHarness("codex")
	if err := s.startCodingTask(h, task, "go", "", "execute"); err != nil {
		t.Fatal(err)
	}
	if err := s.startCodingTask(s.findHarness("claude"), task, "go", "", "execute"); !errors.Is(err, spirits.ErrAlreadyActive) {
		t.Fatalf("shared checkout gate %v", err)
	}
	r := h.Spirits.Runs()[0]
	if err := boardReport(h, r.ID, task, "go", "", "running", "", time.Now().Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	codingFixtureStopped(t, s)
	if d := s.delegationIndex()[task]; d.State != "failed" {
		t.Fatalf("closed pane counted as completion %+v", d)
	}
	// Newer success must supersede historical failure.
	run := boardRunID()
	if err := boardArtifact(h, run, "Fixed. [diff](https://example.com/diff)"); err != nil {
		t.Fatal(err)
	}
	if err := boardReport(h, run, task, "go", "", "completed", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if d := s.delegationIndex()[task]; d.State != "done" {
		t.Fatalf("old failure hides success %+v", d)
	}
}

func TestAlfredAutoExecuteTier(t *testing.T) {
	s := codingFixture(t)
	task := "inbox/wire-the-fence"
	h := s.findHarness("hermes")
	// An operational Ask remains one comment turn, with execution authorization
	// in its prompt rather than a forced plan phase or a second dispatch.
	if err := s.spoolTaskWorkOrderAs(h, "agent:alfred", task, "comment", "look up this candidate's publications and attach a short brief", "info"); err != nil {
		t.Fatal(err)
	}
	prompt := h.Spirits.Queued()[0].Request
	for _, want := range []string{"AUTO-EXECUTE now", "Do not propose a plan or wait for a fire", "standing_authorization", "human_approval", "look up this candidate's publications"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(prompt, "Do not execute anything.") {
		t.Fatal("old comment prohibition overrides auto-execute")
	}
	// Simulate the agent's classified result; this verifies ingestion, not LLM judgment.
	// Remove the legacy spool so it cannot outrank the simulated completion.
	for _, q := range h.Spirits.Queued() {
		if err := os.Remove(filepath.Join(h.Spirits.Root(), "vessel", "spool", q.File)); err != nil {
			t.Fatal(err)
		}
	}
	s.materializeHermesBrief(task, "agent:alfred", "comment", "info", "[tier:: executed]\nFound the publications. Attached the brief. Sources verified. Extra sentence. [source](https://example.com/paper)")
	index := s.delegationIndex()
	if d := index[task]; d.State != "done" || d.ArtifactRef == "" {
		t.Fatalf("auto result not reviewable %+v", d)
	}
	s.agentLoopSweep(index)
	if s.readPlanRecord(task).Plan != "" {
		t.Fatal("Tier 1 plan-gated")
	}
	found := false
	for _, c := range s.listThread(task) {
		if c.Meta["artifactRef"] != nil {
			found = true
			if len(strings.Fields(c.Text)) > 280 || strings.Contains(c.Text, "Extra sentence") {
				t.Fatal(c.Text)
			}
		}
	}
	if !found {
		t.Fatal("missing Tier-1 result link")
	}
	long := "# plan\n\n" + strings.Repeat("A complete plan step. ", 100)
	s.materializeHermesBrief(task, "agent:alfred", "comment", "info", "[tier:: plan]\n"+long)
	if got := s.readPlanRecord(task).Plan; strings.TrimSpace(got) != strings.TrimSpace(long) {
		t.Fatalf("Tier 2 plan truncated (%d)", len(got))
	}
	s.materializeHermesBrief(task, "agent:alfred", "comment", "info", "[tier:: answer]\nIt is zoned residential.")
	if got := s.readPlanRecord(task).Plan; strings.TrimSpace(got) != strings.TrimSpace(long) {
		t.Fatal("inquiry changed plan")
	}
}

func TestCodingSendBackAndResume(t *testing.T) {
	s := codingFixture(t)
	task := "inbox/wire-the-fence"
	w := httptest.NewRecorder()
	s.handleDelegate(w, httptest.NewRequest("POST", "/api/tasks/delegate", strings.NewReader(`{"id":"inbox/wire-the-fence","harness":"codex","spirit":"codex","ritual":"delegate","comment":"also fix the edge case"}`)))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	h := s.findHarness("codex")
	if len(h.Spirits.Queued()) != 0 || len(h.Spirits.Runs()) != 1 {
		t.Fatal("send-back left an unconsumed spool")
	}
	se := s.terminal.load()[0]
	dir := filepath.Dir(se.BoardBrief)
	if err := boardWrite(filepath.Join(dir, "events.jsonl"), []byte("{\"type\":\"thread.started\",\"thread_id\":\"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee\"}\n")); err != nil {
		t.Fatal(err)
	}
	// Crash between terminal registration and writing the session handle.
	if err := os.Remove(filepath.Join(dir, "session")); err != nil {
		t.Fatal(err)
	}
	s.delegationIndex()
	se, _ = s.terminal.find(se.ID)
	if se.ResumeID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" || !strings.Contains(se.execLaunch(), "codex resume --yolo "+se.ResumeID) {
		t.Fatalf("resume %+v", se)
	}
	body, _ := json.Marshal(codingResult{Status: "blocked", Summary: "Push failed; work remains local."})
	if err := boardWrite(filepath.Join(dir, "result.json"), body); err != nil {
		t.Fatal(err)
	}
	if d := s.delegationIndex()[task]; d.State != "failed" {
		t.Fatalf("blocked became review %+v", d)
	}
}

func TestCodingExplicitPlanRemainsComplete(t *testing.T) {
	s := codingFixture(t)
	task := "inbox/wire-the-fence"
	if _, err := s.postAndDispatch(task, "comment", "", nil, nil, "@claude::plan plan this"); err != nil {
		t.Fatal(err)
	}
	plan := "# plan\n\n" + strings.Repeat("Implement and verify the migration. ", 100)
	raw, _ := json.Marshal(codingResult{Status: "completed", Summary: plan})
	if err := boardWrite(filepath.Join(filepath.Dir(s.terminal.load()[0].BoardBrief), "result.json"), raw); err != nil {
		t.Fatal(err)
	}
	index := s.delegationIndex()
	if index[task].State != "plan-ready" {
		t.Fatal(index[task])
	}
	s.agentLoopSweep(index)
	if strings.TrimSpace(s.readPlanRecord(task).Plan) != strings.TrimSpace(plan) {
		t.Fatal("coding plan was capped")
	}
}

func TestCodingOwnersStayPersonal(t *testing.T) {
	s := codingFixture(t)
	for _, owner := range []string{"claude", "codex"} {
		s.threadDialogHook("inbox/wire-the-fence", nil, "@"+owner+" change the code")
		if len(s.findHarness(owner).Spirits.Runs()) != 0 {
			t.Fatal("portal mention started coding work")
		}
		if s.AionAssign("item", "agent:"+owner, "member@example.com", "Member") == nil {
			t.Fatal("team member assigned coding work")
		}
	}
}

func TestCodingInterruptedWorkRecovery(t *testing.T) {
	for _, stopped := range []string{"exit", "closed", "invalid-result"} {
		t.Run(stopped, func(t *testing.T) {
			s := codingFixture(t)
			task := "inbox/wire-the-fence"
			h := s.findHarness("codex")
			if err := s.startCodingTask(h, task, "go", "", "execute"); err != nil {
				t.Fatal(err)
			}
			r := h.Spirits.Runs()[0]
			se := s.terminal.load()[0]
			dir := filepath.Dir(se.BoardBrief)
			if err := os.WriteFile(filepath.Join(se.Cwd, "unfinished.js"), []byte("fix remains here"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := boardReport(h, r.ID, task, "go", "", "running", "", time.Now().Add(-2*time.Minute)); err != nil {
				t.Fatal(err)
			}
			if stopped == "exit" {
				if err := boardWrite(filepath.Join(dir, "exit"), []byte("0")); err != nil {
					t.Fatal(err)
				}
			} else {
				codingFixtureStopped(t, s)
			}
			if stopped == "invalid-result" {
				if err := boardWrite(filepath.Join(dir, "result.json"), []byte(`{"status":"completed"}`)); err != nil {
					t.Fatal(err)
				}
			}
			index := s.delegationIndex()
			if d := index[task]; d.State != "failed" || d.ArtifactRef == "" {
				t.Fatalf("missing recovery artifact: %+v", d)
			}
			raw, err := os.ReadFile(filepath.Join(dir, "recovery.md"))
			if err != nil || !strings.Contains(string(raw), "unfinished.js") || !strings.Contains(string(raw), "Uncommitted work found") {
				t.Fatalf("missing checkout evidence: %s (%v)", raw, err)
			}
			s.agentLoopSweep(index)
			s.agentLoopSweep(s.delegationIndex())
			count := 0
			for _, c := range s.listThread(task) {
				if strings.Contains(c.Text, "Uncommitted work found") {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("recovery comments = %d", count)
			}
			// A late result survives a server restart and replaces the failure.
			s.UseTerminal(s.terminal.regPath, s.terminal.tmuxTmp, s.terminal.defaultWd)
			body, _ := json.Marshal(codingResult{Status: "completed", Summary: "Validated, committed and pushed.", ArtifactURL: "https://example.com/commit/123"})
			if err := boardWrite(filepath.Join(dir, "result.json"), body); err != nil {
				t.Fatal(err)
			}
			index = s.delegationIndex()
			if index[task].State != "done" {
				t.Fatalf("late result not recovered: %+v", index[task])
			}
			s.agentLoopSweep(index)
			if !s.threads.private.HasAction(task, threads.ActResult, r.ID) {
				t.Fatal("recovery comment suppressed the final result")
			}
			if raw, err := os.ReadFile(filepath.Join(se.Cwd, "unfinished.js")); err != nil || string(raw) != "fix remains here" {
				t.Fatal("recovery changed local work")
			}
		})
	}
}

func TestCodingRecoveryInspection(t *testing.T) {
	s := codingFixture(t)
	if body := s.codingRecovery(t.TempDir(), ""); !strings.Contains(body, "No uncommitted files") {
		t.Fatal(body)
	}
	s.UseCodingRepo(t.TempDir())
	if body := s.codingRecovery(t.TempDir(), ""); !strings.Contains(body, "Checkout inspection failed") {
		t.Fatal(body)
	}
}

// Confirm death through a successful inventory, never a socket/command error.
func codingFixtureStopped(t *testing.T, s *Server) {
	t.Helper()
	// Keep the persisted generation: replace only the fixture snapshot response
	// by a legacy backend inventory to exercise legacy confirmed-absence semantics.
	for _, se := range s.terminal.load() {
		se.Backend = "tmux"
		se.Runtime = terminalIdentity{}
		s.terminal.upsert(se)
	}
	s.terminal.run = func(args ...string) ([]byte, error) { return []byte(""), nil }
}
