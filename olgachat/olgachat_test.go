package olgachat

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseReply(t *testing.T) {
	r := ParseReply("Sounds good — start with the quotes.\n```json\n{\"proposals\":[ /* note */ {\"kind\":\"task.add\",\"text\":\"Get quotes\"}]}\n```")
	if r.Text != "Sounds good — start with the quotes." || len(r.Proposals) != 1 || r.Proposals[0].Text != "Get quotes" {
		t.Fatalf("%+v", r)
	}
	// a broken block is a plain reply, never a guessed action
	r = ParseReply("Hi!\n```json\n{\"route\": confirm}\n```")
	if r.Text != "Hi!" || r.Route != "" || len(r.Proposals) != 0 {
		t.Fatalf("%+v", r)
	}
	if r := ParseReply("No block at all"); r.Text != "No block at all" {
		t.Fatalf("%+v", r)
	}
}

func TestComposeCarriesContextAndShape(t *testing.T) {
	p := Compose(ModeTask, map[string]any{"task": map[string]string{"text": "roof on"}}, []Exchange{{"Olga", "hi"}, {"Liber", "hello"}}, "what first?", time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	for _, want := range []string{"ONE task", "roof on", "Olga: hi", "Liber: hello", "Olga: what first?", "Wednesday, October 7, 2026", `"proposals"`} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, p)
		}
	}
}

func TestBuilderCommandLineIsTheOneShape(t *testing.T) {
	args := ClaudeArgs("/w/olga-builder.json", "", "do it")
	if err := CheckClaudeArgs(args); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{
		append(append([]string(nil), args...), "--dangerously-skip-permissions"),
		append(append([]string(nil), args...), "--add-dir", "/private"),
		{"-p", "x", "--model", "claude-sonnet-5-5"},
		{"-p", "x", "--permission-mode", "bypassPermissions"},
	} {
		if CheckClaudeArgs(bad) == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--setting-sources", "--strict-mcp-config", "--disable-slash-commands", "--permission-mode dontAsk", "--model " + BuilderModel} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
	if !strings.Contains(string(builderSettings), `"Read(//private/**)"`) || !strings.Contains(string(builderSettings), `"Edit(./server/web/olga/**)"`) {
		t.Fatal("builder settings lost their fence")
	}
}

// Only her layer can become a preview: a change touching anything else is
// reported, never offered.
func TestChangedFlagsFilesOutsideHerLayer(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	g := &GitBuilder{}
	ctx := context.Background()
	mustGit := func(args ...string) {
		if _, err := g.git(ctx, dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit("init", "-q")
	os.MkdirAll(filepath.Join(dir, "server/web/olga"), 0o755)
	os.MkdirAll(filepath.Join(dir, "server/web/js"), 0o755)
	os.WriteFile(filepath.Join(dir, "server/web/olga/olga.css"), []byte("a{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "server/web/js/90-todos.js"), []byte("x"), 0o644)
	mustGit("add", "-A")
	mustGit("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")
	os.WriteFile(filepath.Join(dir, "server/web/olga/olga.css"), []byte("a{color:red}"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".olga-data"), 0o755)
	os.WriteFile(filepath.Join(dir, ".olga-data/x"), []byte("x"), 0o644)
	files, outside, err := g.changed(ctx, dir)
	if err != nil || len(files) != 1 || len(outside) != 0 {
		t.Fatalf("files %v outside %v err %v", files, outside, err)
	}
	os.WriteFile(filepath.Join(dir, "server/web/js/90-todos.js"), []byte("y"), 0o644)
	os.WriteFile(filepath.Join(dir, "server/web/olga/new.js"), []byte("1"), 0o644)
	files, outside, _ = g.changed(ctx, dir)
	if len(files) != 3 || len(outside) != 1 || outside[0] != "server/web/js/90-todos.js" {
		t.Fatalf("files %v outside %v", files, outside)
	}
}

func TestRecoverSettlesInterruptedTurns(t *testing.T) {
	dir := t.TempDir()
	st := &Store{Private: filepath.Join(dir, "olga"), Shared: filepath.Join(dir, "home"), Write: func(p string, b []byte) error {
		os.MkdirAll(filepath.Dir(p), 0o755)
		return os.WriteFile(p, b, 0o600)
	}}
	th := &Thread{ID: "c-abc", Kind: KindApp, Turns: []Turn{{ID: "t1", Who: "olga", Text: "hi"}, {ID: "t2", Who: "liber", Status: StatusThinking}}}
	if err := st.Save(th); err != nil {
		t.Fatal(err)
	}
	s := &Service{Store: st}
	s.Recover()
	got, _ := st.App("c-abc")
	if got.Turns[1].Status != StatusFailed || got.Turns[1].Text == "" || got.Busy() {
		t.Fatalf("%+v", got.Turns[1])
	}
	if Leaks(got.Turns[1].Text) || !Leaks("powered by Claude") || Leaks("a solution for the roof") {
		t.Fatal("leak check")
	}
}

type fakeVoice struct{}

func (fakeVoice) Ask(_ context.Context, p string, _ []string) (VoiceAnswer, error) {
	if strings.Contains(p, "Manifest will now build") {
		return VoiceAnswer{Text: "On it.\n```json\n{\"brief\":\"bigger titles\"}\n```"}, nil
	}
	return VoiceAnswer{Text: "Here is a preview."}, nil
}

type fakeBuilder struct {
	used *Change
}

func (f *fakeBuilder) Build(_ context.Context, ch *Change, _ string, _ []Exchange, _ []string) (BuildResult, error) {
	ch.Worktree = "/work/" + ch.ID
	return BuildResult{Summary: "Titles are bigger", Files: []string{"server/web/olga/custom.css"}, Session: "s1"}, nil
}
func (f *fakeBuilder) PreviewPath(ch *Change) string { return "/preview/" + ch.ID + "/" }
func (f *fakeBuilder) Discard(*Change) error         { return nil }
func (f *fakeBuilder) Use(_ context.Context, ch *Change) error {
	f.used = ch
	ch.Commit = "abc"
	return nil
}
func (f *fakeBuilder) Undo(context.Context, *Change) error { return nil }
func (f *fakeBuilder) Settled(*Change) string              { return "live" }

// A built change keeps its worktree through the save, so Use can ship it.
func TestBuildThenUseKeepsTheWorktree(t *testing.T) {
	dir := t.TempDir()
	st := &Store{Private: filepath.Join(dir, "olga"), Shared: filepath.Join(dir, "home"), Write: func(p string, b []byte) error {
		os.MkdirAll(filepath.Dir(p), 0o755)
		return os.WriteFile(p, b, 0o600)
	}}
	fb := &fakeBuilder{}
	restarted := make(chan bool, 1)
	s := &Service{Store: st, Voice: fakeVoice{}, Builder: fb, Restart: func() { restarted <- true }}
	th := &Thread{ID: "c-abc", Kind: KindApp, Turns: []Turn{{ID: "o1", Who: "olga", Text: "bigger titles"}, {ID: "l1", Who: "liber", Status: StatusThinking}}}
	st.Save(th)
	s.buildTurn(Ref{KindApp, "c-abc"}, "bigger titles", nil, "l1", "", nil, map[string]any{})
	got, _ := st.App("c-abc")
	var card *Card
	for i := range got.Turns {
		for j := range got.Turns[i].Cards {
			card = &got.Turns[i].Cards[j]
		}
	}
	if card == nil || card.State != StateReady {
		t.Fatalf("card %+v", card)
	}
	if _, err := s.Act(Ref{KindApp, "c-abc"}, card.ID, "use"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("no restart after Use")
	}
	if fb.used == nil || fb.used.Worktree == "" {
		t.Fatalf("Use got %+v — the worktree was lost", fb.used)
	}
	s.Recover()
	got, _ = st.App("c-abc")
	_, c := got.Card(card.ID)
	if c.State != StateLive || got.Server.Changes[c.ChangeID].Commit != "abc" {
		t.Fatalf("after restart %+v", c)
	}
}
