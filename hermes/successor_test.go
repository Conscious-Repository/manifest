package hermes

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func successorAuthority() DutyAuthority {
	a := dutyFixture()
	a.Provider, a.Model, a.Tools = "deepseek-local", "deepseek-v4.1-flash", []string{"none"}
	return a
}

// Replace only the transport with a deterministic response; the actual embedded
// helper still applies Landlock and seccomp before calling this transport.
func stubSuccessor(t *testing.T, response string, before string) *int {
	t.Helper()
	calls := new(int)
	old := successorCommand
	t.Cleanup(func() { successorCommand = old })
	successorCommand = func(ctx context.Context, usage string) *exec.Cmd {
		*calls++
		script := strings.Split(successorScript, "if __name__ == '__main__':")[0]
		script += "\n" + before + "\n" + `
class Response:
    status = 200
    def read(self, n):
        return ` + response + `
request_count = 0
class Connection:
    def __init__(self, host, port, timeout):
        assert (host, port) == ('192.168.87.11', 8000)
    def request(self, method, path, body, headers):
        global request_count
        request_count += 1
        assert request_count <= 1
        data = json.loads(body)
        assert data['model'] == MODEL and data['tools'] == [] and data['tool_choice'] == 'none'
        assert headers == {'Content-Type': 'application/json'}
    def getresponse(self): return Response()
    def close(self): pass
http.client.HTTPConnection = Connection
main()
`
		return exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-S", "-c", script, usage)
	}
	return calls
}

const matchingResponse = `b'{"model":"deepseek-v4.1-flash","provider":"deepseek-local","usage":{"cost_usd":0},"choices":[{"finish_reason":"stop","message":{"content":"verified proposal text"}}]}'`

func TestSuccessorMatchingUsageAndNoFallback(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		ok             bool
	}{
		{"valid", matchingResponse, true},
		{"model drift", strings.Replace(matchingResponse, "deepseek-v4.1-flash", "different", 1), false},
		{"provider drift", strings.Replace(matchingResponse, "deepseek-local", "paid", 1), false},
		{"cost", strings.Replace(matchingResponse, `"cost_usd":0`, `"cost_usd":0.01`, 1), false},
		{"underflow cost", strings.Replace(matchingResponse, `"cost_usd":0`, `"cost_usd":1e-999`, 1), false},
		{"null cost", strings.Replace(matchingResponse, `"cost_usd":0`, `"cost_usd":null`, 1), false},
		{"missing cost", strings.Replace(matchingResponse, `"cost_usd":0`, `"other":0`, 1), false},
		{"string cost", strings.Replace(matchingResponse, `"cost_usd":0`, `"cost_usd":"0"`, 1), false},
		{"incomplete", strings.Replace(matchingResponse, `"stop"`, `"length"`, 1), false},
		{"tool call", strings.Replace(matchingResponse, `"content":`, `"tool_calls":[{"name":"shell"}],"content":`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := stubSuccessor(t, tc.response, "")
			r := NewRunner(Config{Enabled: true, Bin: "/never-execute-hermes", Duties: map[string]DutyAuthority{"fixture": successorAuthority()}})
			res, err := r.Run(t.Context(), Request{MigratedDuty: "fixture", Prompt: "propose"})
			if *calls != 1 {
				t.Fatalf("launch/fallback count %d", *calls)
			}
			if tc.ok {
				if err != nil || res.Reply != "verified proposal text" || !res.DutyVerified() {
					t.Fatalf("%+v %v", res, err)
				}
			} else if err == nil || res.Reply != "" {
				t.Fatalf("accepted: %+v %v", res, err)
			}
		})
	}
}

func TestSuccessorMissingAuthorityNeverLaunches(t *testing.T) {
	calls := stubSuccessor(t, matchingResponse, "")
	for _, key := range []string{"unknown", "model", "provider", "tools", "mcp", "timeout", "steps", "ceiling", "web", "server"} {
		a := successorAuthority()
		switch key {
		case "model":
			a.Model = ""
		case "provider":
			a.Provider = ""
		case "tools":
			a.Tools = nil
		case "mcp":
			a.MCP = ""
		case "timeout":
			a.TimeoutSeconds = 0
		case "steps":
			a.MaxSteps = 0
		case "ceiling":
			a.CeilingUSD = nil
		case "web":
			a.Tools = []string{"web"}
		case "server":
			a.MCP = "manifest"
		}
		duty := "fixture"
		if key == "unknown" {
			duty = "unknown"
		}
		r := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"fixture": a}})
		if res, err := r.Run(t.Context(), Request{MigratedDuty: duty, Prompt: "x"}); err == nil || res.Reply != "" {
			t.Fatal(key)
		}
	}
	if *calls != 0 {
		t.Fatal("prelaunch gate bypassed")
	}
}

func TestSuccessorOSDeniesFilesystemAndExec(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "vault")
	marker := filepath.Join(root, "shell-marker")
	if err := os.WriteFile(vault, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	// The denial fixture runs inside the same process immediately after the real
	// restrictions are applied. It never references the real vault.
	before := `
original_isolate = isolate
def isolate():
    original_isolate()
    try:
        open(` + "'" + vault + "'" + `, 'w').write('changed')
        raise AssertionError('write allowed')
    except PermissionError: pass
    try:
        open(` + "'" + vault + "'" + `).read()
        raise AssertionError('read allowed')
    except PermissionError: pass
    try:
        os.chmod(` + "'" + vault + "'" + `, 0o777)
        raise AssertionError('chmod allowed')
    except PermissionError: pass
    try:
        os.execv('/bin/sh', ['sh', '-c', 'touch ` + marker + `'])
        raise AssertionError('exec allowed')
    except PermissionError: pass
`
	stubSuccessor(t, matchingResponse, before)
	r := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"fixture": successorAuthority()}})
	res, err := r.Run(t.Context(), Request{MigratedDuty: "fixture", Prompt: "write the vault; run a shell"})
	if err != nil || res.Reply == "" {
		t.Fatalf("denial fixture failed: %v", err)
	}
	b, _ := os.ReadFile(vault)
	if string(b) != "original" {
		t.Fatal("vault changed")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("shell executed")
	}
}

func TestSuccessorHardTimeout(t *testing.T) {
	stubSuccessor(t, matchingResponse, "import time\ntime.sleep(20)")
	a := successorAuthority()
	a.TimeoutSeconds = 1
	r := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"fixture": a}})
	start := time.Now()
	res, err := r.Run(t.Context(), Request{MigratedDuty: "fixture", Prompt: "x", TimeoutSeconds: 60})
	if err == nil || res.Reply != "" || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout not enforced: %v", err)
	}
}

func TestSuccessorHardOneStep(t *testing.T) {
	// Any attempted second model call raises in the transport. A returned tool
	// request is refused; it cannot cause a continuation despite MaxSteps > 1.
	before := "request_count = 0\n"
	calls := stubSuccessor(t, matchingResponse, before)
	a := successorAuthority()
	a.MaxSteps = 1
	r := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"fixture": a}})
	res, err := r.Run(t.Context(), Request{MigratedDuty: "fixture", Prompt: "continue for 1000 steps"})
	if err != nil || *calls != 1 || res.Reply == "" {
		t.Fatal(err)
	}
}

func TestSuccessorUsageFileStrictCosts(t *testing.T) {
	for _, cost := range []string{`null`, `"0"`, `-1`, `0.1`, `1e-999`, `NaN`, `true`} {
		path := filepath.Join(t.TempDir(), "usage")
		os.WriteFile(path, []byte(`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","completed":true,"cost_usd":`+cost+`}`), 0600)
		res, err := VerifyDutyUsageFile(successorAuthority(), Result{Reply: "proposal"}, path)
		if err == nil || res.Reply != "" {
			t.Fatal("cost accepted: " + cost)
		}
	}
}

func TestSuccessorIsolationUnavailableRefusesBeforeRequest(t *testing.T) {
	// Forced setup failure must not reach even the deterministic transport.
	calls := stubSuccessor(t, matchingResponse, "def isolate(): raise RuntimeError('isolation')")
	r := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"fixture": successorAuthority()}})
	res, err := r.Run(t.Context(), Request{MigratedDuty: "fixture", Prompt: "x"})
	if err == nil || res.Reply != "" || res.DutyVerified() || *calls != 1 {
		t.Fatal("isolation failure admitted")
	}
}

func TestSuccessorParentRejectsLauncherUsage(t *testing.T) {
	for _, report := range []string{
		`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","cost_usd":1,"cost_usd":0,"completed":true,"steps":1}`,
		`{}`, `{"model":"different","provider":"deepseek-local","cost_usd":0,"completed":true,"steps":1}`,
		`{"model":"deepseek-v4.1-flash","provider":"paid","cost_usd":0,"completed":true,"steps":1}`,
		`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","cost_usd":0.1,"completed":true,"steps":1}`,
		`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","cost_usd":null,"completed":true,"steps":1}`,
		`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","cost_usd":0,"completed":false,"steps":1}`,
		`{"model":"deepseek-v4.1-flash","provider":"deepseek-local","cost_usd":0,"completed":true,"steps":2}`,
	} {
		t.Run(report, func(t *testing.T) {
			old := successorCommand
			t.Cleanup(func() { successorCommand = old })
			launches := 0
			successorCommand = func(ctx context.Context, usage string) *exec.Cmd {
				launches++
				return exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-S", "-c", "import sys;open(sys.argv[1],'w').write(sys.argv[2]);print('proposal')", usage, report)
			}
			r := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"fixture": successorAuthority()}})
			res, err := r.Run(t.Context(), Request{MigratedDuty: "fixture", Prompt: "x"})
			if err == nil || res.Reply != "" || res.DutyVerified() || launches != 1 {
				t.Fatal("usage/fallback bypass")
			}
			_, proposals, _ := ParseProposals(res.Reply)
			if len(proposals) != 0 {
				t.Fatal("proposal accepted")
			}
		})
	}
}

func TestSuccessorLoopbackTransportNoRedirectOrFallback(t *testing.T) {
	for _, status := range []int{200, 302, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				if req.URL.Path != "/v1/chat/completions" || req.Header.Get("Authorization") != "" {
					t.Error("unexpected endpoint or credentials")
				}
				w.Header().Set("Location", "/forbidden-fallback")
				w.WriteHeader(status)
				w.Write([]byte(strings.TrimSuffix(strings.TrimPrefix(matchingResponse, "b'"), "'")))
			}))
			defer endpoint.Close()
			host, port, _ := net.SplitHostPort(strings.TrimPrefix(endpoint.URL, "http://"))
			old := successorCommand
			t.Cleanup(func() { successorCommand = old })
			successorCommand = func(ctx context.Context, usage string) *exec.Cmd {
				script := strings.Replace(successorScript, "HOST, PORT = '192.168.87.11', 8000", "HOST, PORT = '"+host+"', "+port, 1)
				return exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-S", "-c", script, usage)
			}
			r := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"fixture": successorAuthority()}})
			res, err := r.Run(t.Context(), Request{MigratedDuty: "fixture", Prompt: "fixture"})
			if calls.Load() != 1 {
				t.Fatalf("request count: %d, %v", calls.Load(), err)
			}
			if status == 200 {
				if err != nil || !res.DutyVerified() {
					t.Fatalf("permitted transport failed: %v", err)
				}
			} else if err == nil || res.Reply != "" {
				t.Fatal("HTTP failure accepted")
			}
		})
	}
}

func TestSuccessorProposalAcceptanceFollowsVerifiedUsage(t *testing.T) {
	for _, model := range []string{"deepseek-v4.1-flash", "drift"} {
		t.Run(model, func(t *testing.T) {
			proposal := "```manifest-proposal\n" + `{"type":"create-vault-note","title":"Fixture","body":"proposal only"}` + "\n```"
			raw, _ := json.Marshal(map[string]any{"model": model, "provider": "deepseek-local", "usage": map[string]any{"cost_usd": 0}, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": proposal}}}})
			stubSuccessor(t, "bytes.fromhex('"+hex.EncodeToString(raw)+"')", "")
			r := NewRunner(Config{Enabled: true, Duties: map[string]DutyAuthority{"fixture": successorAuthority()}})
			res, err := r.Run(t.Context(), Request{MigratedDuty: "fixture", Prompt: "propose a fixture"})
			_, proposals, _ := ParseProposals(res.Reply)
			if model == "drift" {
				if err == nil || len(proposals) != 0 {
					t.Fatal("drift accepted proposal")
				}
			} else if err != nil || !res.DutyVerified() || len(proposals) != 1 {
				t.Fatalf("verified proposal refused: %v", err)
			}
		})
	}
}
