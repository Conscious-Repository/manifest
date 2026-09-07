package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodingModelAskDoMentionOverridesDefault(t *testing.T) {
	for _, mode := range []string{"ask", "do"} {
		for _, agent := range []string{"", "agent:alfred", "agent:codex"} {
			t.Run(mode+"/"+agent, func(t *testing.T) {
				s := codingFixture(t)
				if _, err := s.postAndDispatch("inbox/wire-the-fence", mode, agent,
					[]string{"agent:codex"}, nil, "@codex::model:gpt-5.5 fix the fence"); err != nil {
					t.Fatal(err)
				}
				sessions := s.terminal.load()
				if len(sessions) != 1 || sessions[0].Kind != "codex" || sessions[0].Model != "gpt-5.5" {
					t.Fatalf("mention did not reach coding launch: %+v", sessions)
				}
				if !strings.Contains(sessions[0].boardLaunch(), " -m "+shQuote("gpt-5.5")) {
					t.Fatal(sessions[0].boardLaunch())
				}
			})
		}
	}
}

func TestCodingModelDispatchPrecedence(t *testing.T) {
	s := codingFixture(t)
	for _, tc := range []struct {
		agent, mention, wantAgent, wantModel, wantIntent string
	}{
		{"agent:codex::model:best", "agent:claude::model:opus", "agent:codex", "best", "info"},
		{"agent:codex::plan", "agent:codex::model:gpt-5.5", "agent:codex", "gpt-5.5", "plan"},
		{"agent:codex", "agent:nobody::model:opus", "agent:codex", "", "info"},
		{"agent:codex", "", "agent:codex", "", "info"},
	} {
		p := s.resolveDispatch("inbox/wire-the-fence", "ask", tc.agent, []string{tc.mention})
		if p == nil || p.Agent != tc.wantAgent || p.Model != tc.wantModel || p.Intent != tc.wantIntent {
			t.Errorf("agent %q, mention %q: %+v", tc.agent, tc.mention, p)
		}
	}
}

func TestCodingModelTaskCreate(t *testing.T) {
	for _, suffix := range []string{"", " !do", "::plan", "::plan !do"} {
		t.Run(suffix, func(t *testing.T) {
			s := codingFixture(t)
			body, err := json.Marshal(map[string]string{"text": "repair the gate @codex::model:gpt-5.5" + suffix})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			s.handleTaskAdd(w, httptest.NewRequest("POST", "/api/tasks/item", strings.NewReader(string(body))))
			if w.Code != 200 || strings.Contains(w.Body.String(), "dispatchError") {
				t.Fatalf("create: %d %s", w.Code, w.Body.String())
			}
			sessions := s.terminal.load()
			if len(sessions) != 1 || sessions[0].Model != "gpt-5.5" ||
				!strings.Contains(sessions[0].boardLaunch(), " -m "+shQuote("gpt-5.5")) {
				t.Fatalf("capture lost model: %+v", sessions)
			}
			brief, err := os.ReadFile(sessions[0].BoardBrief)
			if err != nil || !strings.Contains(string(brief), "MODEL: gpt-5.5") ||
				strings.Contains(string(brief), "Do not implement") != strings.Contains(suffix, "::plan") {
				t.Fatalf("capture lost model or intent: %v\n%s", err, brief)
			}
			raw, err := os.ReadFile(s.tasksStore.Path())
			if err != nil || strings.Contains(string(raw), "gpt-5.5") || strings.Contains(string(raw), "@codex") {
				t.Fatalf("address leaked into task: %v\n%s", err, raw)
			}
		})
	}
}

func TestCodingModelGrammar(t *testing.T) {
	for _, tc := range []struct {
		token, intent, model string
		warning              bool
	}{
		{"codex", "", "gpt-6-astra", false},
		{"claude", "", "fable", false},
		{"codex::plan", "plan", "gpt-6-astra", false},
		{"codex::model:gpt-5.5", "", "gpt-5.5", false},
		{"claude::model:opus", "", "opus", false},
		{"codex::model:gpt-5.5::plan", "plan", "gpt-5.5", false},
		{"claude::brief::model:opus", "brief", "opus", false},
		{"codex::model:best", "", "gpt-6-astra", false},
		{"codex::model:typo::plan", "plan", "gpt-6-astra", true},
		{"claude::model:", "", "fable", true},
		{"claude::model:opus::model:fable", "", "fable", true},
		{"codex::model:opus", "", "gpt-6-astra", true},
	} {
		t.Run(tc.token, func(t *testing.T) {
			s := codingFixture(t)
			mentions := s.textMentions("Please @" + tc.token + " do this")
			if len(mentions) != 1 {
				t.Fatal(mentions)
			}
			base, intent, requested := parseAgentToken(mentions[0])
			model, note := codingModel(strings.TrimPrefix(base, "agent:"), requested)
			if intent != tc.intent || model != tc.model || (note != "") != tc.warning {
				t.Fatalf("%s %s %s", intent, model, note)
			}
		})
	}
}

func TestCodingModelDispatchPersistence(t *testing.T) {
	for _, owner := range []string{"codex", "claude"} {
		for _, mode := range []string{"comment", "ask", "do", "agent-field"} {
			t.Run(owner+"/"+mode, func(t *testing.T) {
				s := codingFixture(t)
				model := "gpt-5.5"
				flag := " -m "
				if owner == "claude" {
					model, flag = "opus", " --model "
				}
				agent, text := "agent:"+owner, "@"+owner+"::model:"+model+"::plan plan the fence"
				requestMode := mode
				if mode == "agent-field" {
					requestMode, agent, text = "ask", agent+"::plan::model:"+model, "plan the fence"
				}
				// The composer may send a bare structural mention alongside typed options.
				if _, err := s.postAndDispatch("inbox/wire-the-fence", requestMode, agent, []string{"agent:" + owner}, nil, text); err != nil {
					t.Fatal(err)
				}
				sessions := s.terminal.load()
				if len(sessions) != 1 {
					t.Fatal(sessions)
				}
				se := sessions[0]
				if se.Model != model {
					t.Fatal(se)
				}
				if !strings.Contains(se.boardLaunch(), flag+shQuote(model)) {
					t.Fatal(se.boardLaunch())
				}
				if !strings.Contains(se.execLaunch(), flag+shQuote(model)) || strings.Contains(se.execLaunch(), "Read the complete work order") {
					t.Fatal(se.execLaunch())
				}
				brief, err := os.ReadFile(se.BoardBrief)
				if err != nil || !strings.Contains(string(brief), "MODEL: "+model) || !strings.Contains(string(brief), "Do not implement") {
					t.Fatalf("%v %s", err, brief)
				}
				found := false
				for _, c := range s.listThread("inbox/wire-the-fence") {
					if c.Meta["model"] == model {
						found = true
					}
				}
				if !found {
					t.Fatal("model missing from thread")
				}
				s.UseTerminal(s.terminal.regPath, s.terminal.tmuxTmp, s.terminal.defaultWd)
				recovered, ok := s.terminal.find(se.ID)
				if !ok || recovered.Model != model || !strings.Contains(recovered.execLaunch(), flag+shQuote(model)) {
					t.Fatal(recovered)
				}
			})
		}
	}
}

func TestCodingModelFallbackAndRetry(t *testing.T) {
	s := codingFixture(t)
	task := "inbox/wire-the-fence"
	if err := s.startCodingTask(s.findHarness("claude"), task, "go", "", "execute"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.postAndDispatch(task, "comment", "", nil, nil, "@codex::model:gpt-5.5 fix it"); err != nil {
		t.Fatal(err)
	}
	r := s.findHarness("claude").Spirits.Runs()[0]
	if err := boardReport(s.findHarness("claude"), r.ID, task, "go", "", "completed", "done", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.relaySweep(nil)
	found := false
	for _, se := range s.terminal.load() {
		if se.Kind == "codex" && se.Model == "gpt-5.5" {
			found = true
		}
	}
	if !found {
		t.Fatal("pending retry lost override")
	}
	s = codingFixture(t)
	if _, err := s.postAndDispatch(task, "comment", "", nil, nil, "@codex::model:typo::plan plan it"); err != nil {
		t.Fatal(err)
	}
	se := s.terminal.load()[0]
	raw, _ := os.ReadFile(filepath.Clean(se.BoardBrief))
	if se.Model != "gpt-6-astra" || !strings.Contains(string(raw), "Unknown or malformed") {
		t.Fatal(string(raw))
	}
	found = false
	for _, c := range s.listThread(task) {
		if strings.Contains(c.Text, "Unknown or malformed") {
			found = true
		}
	}
	if !found {
		t.Fatal("fallback note missing")
	}
}

func TestBoardLaunchDefaultModelFlags(t *testing.T) {
	for _, owner := range []string{"codex", "claude"} {
		se := termSession{Kind: owner, BoardBrief: "/tmp/work order/brief.md", ResumeID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}
		model, _ := codingModel(owner, "")
		flag := " -m "
		if owner == "claude" {
			flag = " --model "
		}
		if !strings.Contains(se.boardLaunch(), flag+shQuote(model)) {
			t.Fatal(se.boardLaunch())
		}
	}
}
