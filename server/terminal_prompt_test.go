package server

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Screens captured from live Claude Code 2.1 and Codex 0.154 panes (120×40).
func readPromptScreen(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile("testdata/screen-prompts/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func promptLabels(p *termPrompt) []string {
	var out []string
	for _, o := range p.Options {
		out = append(out, o.Label)
	}
	return out
}

func TestParseTermPromptClaudePermission(t *testing.T) {
	p := parseTermPrompt(readPromptScreen(t, "perm"))
	if p == nil {
		t.Fatal("no prompt")
	}
	if p.Title != "Do you want to proceed?" || p.Header != "Bash command" || p.Selected != 0 || p.Multi {
		t.Fatalf("got %+v", p)
	}
	if len(p.Options) != 4 || p.Options[0].Label != "Yes" || p.Options[3].Label != "No" || !p.Options[3].Followup {
		t.Fatalf("options %q", promptLabels(p))
	}
	// the wrapped option is one label
	if !strings.HasSuffix(p.Options[1].Label, "scratchpad/probe from this project") || strings.Contains(p.Options[1].Label, "e1 ") {
		t.Fatalf("wrapped label %q", p.Options[1].Label)
	}
	code := false
	for _, l := range p.Body {
		if l.Code && l.Text == "echo hello > a.txt" {
			code = true
		}
		if strings.HasPrefix(l.Text, "Tip:") {
			t.Fatal("tip kept")
		}
	}
	if !code {
		t.Fatalf("command not shown as code: %+v", p.Body)
	}
}

func TestParseTermPromptAskUserQuestion(t *testing.T) {
	p := parseTermPrompt(readPromptScreen(t, "ask1"))
	if p == nil || p.Title != "What is your favorite color?" || p.Multi {
		t.Fatalf("got %+v", p)
	}
	if got := strings.Join(promptLabels(p), "|"); got != "Red|Green|Blue|Type something.|Chat about this" {
		t.Fatalf("options %s", got)
	}
	if p.Options[0].Detail != "The color red" || !p.Options[3].Text || p.Options[4].Number != "5" {
		t.Fatalf("details %+v", p.Options)
	}
	if len(p.Tabs) != 2 || p.Tabs[0].Label != "Color" || p.Tabs[0].Done || p.Tabs[1].Label != "Pets" {
		t.Fatalf("tabs %+v", p.Tabs)
	}

	m := parseTermPrompt(readPromptScreen(t, "ask2"))
	if m == nil || !m.Multi || m.Title != "Which pets do you have?" {
		t.Fatalf("multi %+v", m)
	}
	if got := strings.Join(promptLabels(m), "|"); got != "Cat|Dog|Fish|Type something|Submit|Chat about this" {
		t.Fatalf("multi options %s", got)
	}
	if m.Options[0].Checked == nil || *m.Options[0].Checked || m.Options[0].Detail != "A cat" || !m.Options[4].Submit || m.Options[4].Index != 4 || m.Options[5].Index != 5 {
		t.Fatalf("multi rows %+v", m.Options)
	}
	if !m.Tabs[0].Done || m.Tabs[1].Done {
		t.Fatalf("multi tabs %+v", m.Tabs)
	}

	r := parseTermPrompt(readPromptScreen(t, "ask3"))
	if r == nil || r.Title != "Ready to submit your answers?" || strings.Join(promptLabels(r), "|") != "Submit answers|Cancel" {
		t.Fatalf("review %+v", r)
	}
	if r.Header != "Review your answers" || len(r.Body) != 4 || r.Body[len(r.Body)-1].Text != "→ Cat, Fish" {
		t.Fatalf("review body %+v", r.Body)
	}
}

func TestParseTermPromptTrustAndCodex(t *testing.T) {
	p := parseTermPrompt(readPromptScreen(t, "trust"))
	if p == nil || strings.Join(promptLabels(p), "|") != "No, exit|Yes, I trust this folder" || p.Selected != 0 || p.Options[0].Followup {
		t.Fatalf("trust %+v", p)
	}
	c := parseTermPrompt(readPromptScreen(t, "codex-perm"))
	if c == nil || c.Title != "Would you like to run the following command?" {
		t.Fatalf("codex %+v", c)
	}
	if got := promptLabels(c); len(got) != 3 || got[0] != "Yes, proceed" || !c.Options[2].Followup {
		t.Fatalf("codex options %q", got)
	}
	if c.Body[len(c.Body)-1].Text != "$ touch made.txt" || !c.Body[len(c.Body)-1].Code {
		t.Fatalf("codex body %+v", c.Body)
	}
}

func TestParseTermPromptIgnoresPlainScreens(t *testing.T) {
	idle := []string{"● Done.", "", "─────", "❯ ", "─────", "  ⏸ manual mode on · ? for shortcuts"}
	if p := parseTermPrompt(idle); p != nil {
		t.Fatalf("idle screen parsed as %+v", p)
	}
	codex := []string{"• Ran touch made.txt", "› Ask Codex to do anything", "  gpt-6-astra default · ~/x"}
	if p := parseTermPrompt(codex); p != nil {
		t.Fatalf("codex composer parsed as %+v", p)
	}
	a := parseTermPrompt(readPromptScreen(t, "perm"))
	b := parseTermPrompt(readPromptScreen(t, "perm-amend"))
	if a.Revision == b.Revision {
		t.Fatal("different dialogs share a revision")
	}
}

// A short pane: Claude scrolls its list and marks the cut with ↑/↓.
func TestParseTermPromptScrolledList(t *testing.T) {
	top := parseTermPrompt(readPromptScreen(t, "short-top"))
	if top == nil || top.Title != "Which fruit do you want?" || !top.Clipped || top.Selected != 0 {
		t.Fatalf("top %+v", top)
	}
	var idx []int
	for _, o := range top.Options {
		idx = append(idx, o.Index)
	}
	if fmt.Sprint(idx) != "[0 1 2 5]" || top.Options[2].Label != "plum" {
		t.Fatalf("positions %v %+v", idx, top.Options)
	}
	down := parseTermPrompt(readPromptScreen(t, "short-scrolled"))
	if down == nil || down.Options[0].Label != "pear" || down.Options[0].Index != 1 || down.Selected != 3 || down.Revision != top.Revision {
		t.Fatalf("scrolled %+v", down)
	}
	if !promptSame(top, down) {
		t.Fatal("a scroll is not a different prompt")
	}
	owner := parseTermPrompt(readPromptScreen(t, "short-owner"))
	if owner == nil || !strings.HasSuffix(owner.Title, "How should they appear?") || strings.Join(promptLabels(owner), "|") != "Leave them out|Type something.|Chat about this" {
		t.Fatalf("owner %+v", owner)
	}
	if owner.Selected != 2 || len(owner.Tabs) != 1 {
		t.Fatalf("owner cursor/tabs %+v", owner)
	}
}
