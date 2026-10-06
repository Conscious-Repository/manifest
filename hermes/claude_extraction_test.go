package hermes

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// The Claude stand-in runs as bounded as the lab path: no tools, no MCP, no
// settings (no hooks), no session, a scratch directory, no inherited keys,
// the prompt on stdin — and only with the owner's recorded choice. A receipt
// that is not one successful turn on one model is refused.
func TestClaudeExtractionIsBoundedAndOwnerChosen(t *testing.T) {
	old := claudeExtractionCommand
	t.Cleanup(func() { claudeExtractionCommand = old })
	var gotArgs []string
	reply := `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"result":"{\"candidates\":[]}","total_cost_usd":0.01,"modelUsage":{"claude-sonnet-5-5":{}}}`
	claudeExtractionCommand = func(ctx context.Context, bin string, args ...string) *exec.Cmd {
		gotArgs = args
		return exec.CommandContext(ctx, "/usr/bin/python3", "-c", `import os,sys
assert sys.stdin.read()=="the transcript prompt"
assert os.getcwd()==os.environ['TMPDIR'] and 'manifest-claude-extraction-' in os.getcwd()
assert 'ANTHROPIC_API_KEY' not in os.environ and 'OPENAI_API_KEY' not in os.environ
print(`+"'"+strings.ReplaceAll(reply, `\"`, `\\"`)+"'"+`)`)
	}
	t.Setenv("ANTHROPIC_API_KEY", "must-not-pass")
	t.Setenv("OPENAI_API_KEY", "must-not-pass")
	r := NewRunner(Config{Enabled: true})
	choice := ClaudeExtraction{Binary: "/usr/local/bin/claude", Model: "sonnet", OwnerAction: "chat 2026-10-06"}
	res, err := r.RunClaudeExtraction(context.Background(), "aion", "the transcript prompt", choice)
	if err != nil || !res.DutyVerified() || res.Reply != `{"candidates":[]}` || res.Model != "claude-sonnet-5-5" || res.Extraction.Provider != ClaudeExtractionProvider || res.Extraction.Status != 200 {
		t.Fatalf("a clean run: %+v %v", res, err)
	}
	want := []string{"-p", "--model", "sonnet", "--tools", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--setting-sources", "", "--no-session-persistence", "--output-format", "json"}
	if strings.Join(gotArgs, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %q", gotArgs)
	}
	if _, err := r.RunClaudeExtraction(context.Background(), "aion", "p", ClaudeExtraction{Binary: "/usr/local/bin/claude", Model: "sonnet"}); err == nil {
		t.Fatal("no recorded owner choice must refuse")
	}
	if _, err := r.RunClaudeExtraction(context.Background(), "re-intake", "p", choice); err == nil {
		t.Fatal("a duty outside the extractor set must refuse")
	}
	reply = strings.Replace(reply, `"num_turns":1`, `"num_turns":3`, 1)
	if _, err := r.RunClaudeExtraction(context.Background(), "aion", "the transcript prompt", choice); err == nil {
		t.Fatal("a multi-turn receipt must refuse")
	}
}
