package server

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func runtimeTmuxFixture(run func(...string) ([]byte, error)) *tmuxTerminalRuntime {
	return &tmuxTerminalRuntime{server: &Server{terminal: &termCfg{run: run}}}
}
func TestTerminalRuntimeOutageIsUnknown(t *testing.T) {
	rt := runtimeTmuxFixture(func(...string) ([]byte, error) { return nil, errors.New("socket unavailable") })
	ob, err := rt.Inspect(context.Background(), terminalIdentity{Backend: "tmux", Session: "manifest_abcdef12"})
	if err == nil || ob.Process != "unknown" || ob.AgentState != "unknown" || ob.Connectivity != "unavailable" {
		t.Fatalf("outage: %+v %v", ob, err)
	}
}
func TestTerminalRuntimeObservationNeverClaimsAgentCompletion(t *testing.T) {
	for _, dead := range []string{"0", "1"} {
		rt := runtimeTmuxFixture(func(...string) ([]byte, error) { return []byte("manifest_abcdef12\t%1\t" + dead + "\t42\n"), nil })
		ob, err := rt.Inspect(context.Background(), terminalIdentity{Backend: "tmux", Session: "manifest_abcdef12"})
		if err != nil || ob.AgentState != "unknown" {
			t.Fatalf("process evidence invented agent truth: %+v %v", ob, err)
		}
		if ob.Connectivity != "connected" {
			t.Fatal(ob)
		}
	}
}
func TestTerminalRuntimeRefusesReplacementOccupant(t *testing.T) {
	calls := 0
	rt := runtimeTmuxFixture(func(...string) ([]byte, error) { calls++; return []byte("manifest_abcdef12\t%1\t0\t99\n"), nil })
	id := terminalIdentity{Backend: "tmux", Session: "manifest_abcdef12", Pane: "%1", Occupant: "42"}
	if err := rt.SendKey(context.Background(), id, "y"); err == nil {
		t.Fatal("sent to replacement occupant")
	}
	if calls != 1 {
		t.Fatalf("mutation issued: %d calls", calls)
	}
}
func TestTerminalRuntimeRefusesDialogText(t *testing.T) {
	rt := runtimeTmuxFixture(func(args ...string) ([]byte, error) {
		switch args[0] {
		case "list-panes":
			return []byte("manifest_abcdef12\t%1\t0\t42\n"), nil
		case "capture-pane":
			return []byte("Do you trust the files in this folder?\n❯ No, exit"), nil
		default:
			t.Fatalf("sent text into dialog: %v", args)
			return nil, nil
		}
	})
	if err := rt.SendText(context.Background(), terminalIdentity{Session: "manifest_abcdef12"}, "hello"); err == nil {
		t.Fatal("accepted dialog")
	}
}
func TestTerminalRuntimeLegacyLaunchUsesBuilder(t *testing.T) {
	var got []string
	rt := runtimeTmuxFixture(func(args ...string) ([]byte, error) { got = append([]string{}, args...); return nil, nil })
	se := termSession{ID: "abcdef12", Kind: "codex", Cwd: "/a quoted cwd", ResumeID: "12345678-abcd", Started: true, BoardBrief: "/work/brief.md", Model: "pinned-model"}
	want, err := rt.server.termLaunchArgs(se, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rt.Create(context.Background(), se); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("launch diverged: %v", got)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "codex resume --yolo 12345678-abcd -m 'pinned-model'") || !strings.Contains(joined, "-x 120 -y 32") {
		t.Fatal(joined)
	}
}
func TestTerminalRuntimeUnsupportedWaitDoesNotSendOrInventIdle(t *testing.T) {
	rt := runtimeTmuxFixture(func(...string) ([]byte, error) { t.Fatal("unsupported wait sent command"); return nil, nil })
	id := terminalIdentity{Session: "manifest_abcdef12"}
	ob, err := rt.Prompt(context.Background(), id, "hello", terminalWait{State: "idle", Timeout: time.Millisecond})
	if !errors.Is(err, errTerminalUnsupported) || ob.AgentState != "unknown" {
		t.Fatalf("%+v %v", ob, err)
	}
}
func TestTerminalRuntimeCancelledContextDoesNotControlProcess(t *testing.T) {
	rt := runtimeTmuxFixture(func(...string) ([]byte, error) { t.Fatal("control after cancellation"); return nil, nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rt.Create(ctx, termSession{ID: "abcdef12"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
