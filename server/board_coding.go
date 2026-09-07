package server

// Coding owners use the terminal launcher and the harness file contract. The
// durable result, never pane output/liveness, is the authority for completion.
import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"manifest/spirits"
)

func isCodingAgent(name string) bool { return name == "claude" || name == "codex" }

func (s *Server) UseCodingRepo(dir string) {
	if s.terminal != nil {
		s.terminal.codingRepo = dir
	}
}

func boardRunID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

// All files are replaced atomically: a sweep never ingests half a report.
func boardWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".board-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func boardReport(h *Harness, run, task, phase, persona, outcome, body string, started time.Time) error {
	request := "[todo:: " + task + "] [phase:: " + phase + "]"
	if persona != "" {
		request += " [persona:: " + persona + "]"
	}
	raw := fmt.Sprintf("---\nrun: %s\nspirit: %s\nritual: delegate\nrequest: %s\nstarted: %s\noutcome: %s\n---\n%s\n", run, h.Name, request, started.UTC().Format(time.RFC3339Nano), outcome, body)
	return boardWrite(filepath.Join(h.Spirits.Root(), "artifacts", "runs", run+".md"), []byte(raw))
}

func boardArtifact(h *Harness, run, body string) error {
	raw := fmt.Sprintf("---\nrun: %s\ntitle: task result\ndate: %s\n---\n%s\n", run, time.Now().UTC().Format(time.RFC3339Nano), body)
	return boardWrite(filepath.Join(h.Spirits.Root(), "artifacts", "library", run+".md"), []byte(raw))
}

func (s *Server) startCodingTask(h *Harness, task, phase, extra, intent string) error {
	c := s.terminal
	if c == nil || c.codingRepo == "" || h.Spirits == nil {
		return errBadRequest("coding checkout is not configured (boardRepo)")
	}
	c.boardMu.Lock()
	defer c.boardMu.Unlock()
	// One active coding task per checkout: both owners share this writer lane.
	for _, name := range []string{"claude", "codex"} {
		if other := s.findHarness(name); other != nil && other.Spirits != nil {
			for _, r := range other.Spirits.Runs() {
				if r.Outcome == "running" {
					return spirits.ErrAlreadyActive
				}
			}
		}
	}
	cmd := exec.Command("git", "-C", c.codingRepo, "rev-parse", "--show-toplevel")
	root, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("coding checkout: %w", err)
	}
	cwd := strings.TrimSpace(string(root))
	// Explicit plan intent remains complete/unbounded. Every other coding
	// dispatch gets the work, including Ask and bare @mentions.
	if intent != "plan" {
		phase = "go"
	}
	text, ok := s.openTaskText(task)
	if !ok {
		return errBadRequest("todo not found")
	}
	rec := s.readPlanRecord(task)
	run, started := boardRunID(), time.Now()
	dir := filepath.Join(h.Spirits.Root(), "work", run)
	briefPath := filepath.Join(dir, "brief.md")
	resultPath := filepath.Join(dir, "result.json")
	instruction := "Execute the task directly, using the supplied plan if relevant. Verify the change, stage ONLY intended paths, commit and push over the configured SSH origin. Do not force push. Preserve unrelated work. If another writer or a conflict prevents safe progress, report blocked. Completion requires a successful push (unless no code changes were needed). Include the pushed commit/diff or PR URL in artifactURL."
	if phase == "plan" {
		instruction = "Produce a complete, concrete plan (no length cap), or questions if blocked. Do not implement or commit/push in this explicitly requested plan turn."
	}
	prompt := fmt.Sprintf(`# Board work order
You are %s, directly assigned by Benjamin. This file is the durable handoff and contains the owner's context. Read the checkout's AGENTS.md, ARCHITECTURE.md and applicable UI conventions. Work in %s.

%s

TASK: %s

DESCRIPTION / SUPPLIED BRIEF:
%s

CURRENT PLAN:
%s

THREAD (oldest first):
%s

OWNER'S NEW INSTRUCTION:
%s

%s

RESULT CONTRACT: As your LAST action, atomically write %s (write a temporary file then rename) with one JSON object:
{"status":"completed|blocked","summary":"markdown deliverable: changes, validation, limitations; complete plan for plan turns","artifactURL":"https://... pushed commit, diff or PR; empty for plan/no-change results"}.
Use status completed only when the requested work is complete and any changes were committed AND pushed. Failures, unanswered questions and unfinished work must use blocked. Never claim a push or test you did not perform. The board reads this durable file even after your pane closes; terminal output alone cannot complete the task.
`, h.Name, cwd, instruction, text, rec.Description, rec.Plan, s.threadTail(task, len(s.listThread(task))), extra, s.hermesAttachments(task), resultPath)
	if err := boardWrite(briefPath, []byte(prompt)); err != nil {
		return err
	}
	if err := boardReport(h, run, task, phase, "", "running", "Reading durable work order: "+briefPath, started); err != nil {
		return err
	}
	se, _, err := s.createAgentTermSession(h.Name, cwd, h.Name+" · "+text, briefPath)
	if err == nil {
		err = boardWrite(filepath.Join(dir, "session"), []byte(se.ID))
	}
	if err != nil {
		_ = boardReport(h, run, task, phase, "", "failed", err.Error(), started)
		return err
	}
	return nil
}

// The noninteractive CLI runs inside the same detached, resumable tmux session
// as the terminal rail. Reopening uses normal resume, never replays this order.
func (se termSession) boardLaunch() string {
	prompt := shQuote("Read the complete work order at " + se.BoardBrief + " and carry it through. Write the durable result as instructed there.")
	command := "codex exec --json --yolo " + prompt + " | tee " + shQuote(filepath.Join(filepath.Dir(se.BoardBrief), "events.jsonl"))
	if se.Kind == "claude" {
		command = "claude --print --dangerously-skip-permissions --session-id " + shQuote(se.ResumeID) + " " + prompt
	}
	exitPath := filepath.Join(filepath.Dir(se.BoardBrief), "exit")
	return termTmpExport + `umask 077; set -o pipefail; export PATH="$HOME/.local/bin:$HOME/.bun/bin:/opt/homebrew/bin:$PATH"; ` + command + "; board_exit=$?; printf '%s' \"$board_exit\" > " + shQuote(exitPath+".tmp") + "; mv " + shQuote(exitPath+".tmp") + " " + shQuote(exitPath)
}

type codingResult struct {
	Status      string `json:"status"`
	Summary     string `json:"summary"`
	ArtifactURL string `json:"artifactURL"`
}

func (s *Server) codingResultSweep() {
	if s.terminal == nil {
		return
	}
	c := s.terminal
	c.boardMu.Lock()
	defer c.boardMu.Unlock()
	for _, name := range []string{"claude", "codex"} {
		h := s.findHarness(name)
		if h == nil || h.Spirits == nil {
			continue
		}
		for _, r := range h.Spirits.Runs() {
			if r.Outcome != "running" {
				continue
			}
			tm := todoTokenRe.FindStringSubmatch(r.Request)
			pm := phaseTokenRe.FindStringSubmatch(r.Request)
			if tm == nil || pm == nil {
				continue
			}
			started, _ := time.Parse(time.RFC3339Nano, r.Started)
			dir := filepath.Join(h.Spirits.Root(), "work", r.ID)
			session, _ := os.ReadFile(filepath.Join(dir, "session"))
			if !termIDRe.Match(session) {
				// Recover the tiny window between registering/spawning a terminal and
				// persisting its handle beside the report.
				for _, se := range c.load() {
					if se.BoardBrief == filepath.Join(dir, "brief.md") {
						session = []byte(se.ID)
						_ = boardWrite(filepath.Join(dir, "session"), session)
						break
					}
				}
			}
			if name == "codex" {
				s.codingResume(dir)
			}
			raw, readErr := os.ReadFile(filepath.Join(dir, "result.json"))
			var result codingResult
			if readErr == nil && json.Unmarshal(raw, &result) == nil && strings.TrimSpace(result.Summary) != "" && (result.Status == "completed" || result.Status == "blocked") {
				body := result.Summary
				if result.ArtifactURL != "" {
					body += "\n\n[review changes](" + result.ArtifactURL + ")"
				}
				if err := boardArtifact(h, r.ID, body); err != nil {
					continue
				}
				outcome := "completed"
				if result.Status == "blocked" {
					outcome = "failed"
				}
				_ = boardReport(h, r.ID, tm[1], pm[1], "", outcome, body, started)
				continue
			}
			// A crashed/closed pane is a failed run, never a successful result. The
			// grace allows startup to persist the session id before liveness checks.
			if time.Since(started) < time.Minute {
				continue
			}
			_, exitErr := os.Stat(filepath.Join(dir, "exit"))
			dead := !termIDRe.Match(session)
			if termIDRe.Match(session) {
				_, err := c.tmuxOut("has-session", "-t", tmuxName(string(session)))
				dead = err != nil
			}
			if exitErr == nil || dead {
				_ = boardReport(h, r.ID, tm[1], pm[1], "", "failed", "The coding session stopped without a valid durable result. Reopen the session or send the task back with a comment.", started)
			}
		}
	}
}

// Codex mints its thread id at startup (unlike Claude's --session-id). Recover
// it from its own JSON event stream so the existing resume action stays exact.
func (s *Server) codingResume(dir string) {
	session, err := os.ReadFile(filepath.Join(dir, "session"))
	if err != nil {
		return
	}
	se, ok := s.terminal.find(string(session))
	if !ok || se.ResumeID != "" {
		return
	}
	f, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		return
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for n := 0; n < 16 && scan.Scan(); n++ {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
		}
		if json.Unmarshal(scan.Bytes(), &event) == nil && event.Type == "thread.started" && resumeIDRe.MatchString(event.ThreadID) {
			se.ResumeID = event.ThreadID
			s.terminal.upsert(se)
			return
		}
	}
}
