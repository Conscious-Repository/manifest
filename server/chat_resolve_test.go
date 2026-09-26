package server

import (
	"os"
	"path/filepath"
	"testing"

	"manifest/spirits"
)

// TestChatResolveNamesTheOwningStore: spirit and agent session ids share one
// shape (20060102-150405-xxxx), so a bare #/chat/<id> cannot say which store
// owns it. The resolver asks every store and names the owner; "no owner" is
// only reported once every store was asked (2026-09-26: a live Alfred thread
// deep-linked as #/chat/<id> was reported "deleted or archived").
func TestChatResolveNamesTheOwningStore(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "spirits", "concierge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "spirits", "concierge", "chat.md"), []byte("chat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := spirits.NewStore(root)
	s.UseSpirits(sp)
	s.terminal = &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}

	alfred, err := st.Create("alfred", "", "gutters", "")
	if err != nil {
		t.Fatal(err)
	}
	spirit, err := sp.CreateChatSession("concierge", "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	s.terminal.upsert(termSession{ID: "0123456789abcdef", Kind: "claude"})

	owners := func(id string) chatResolution {
		code, r := agentChatJSON(t, s, "GET", "/api/chat/resolve?id="+id, nil)
		if code != 200 {
			t.Fatalf("resolve %s: %d", id, code)
		}
		var out chatResolution
		for _, o := range r["owners"].([]any) {
			m := o.(map[string]any)
			out.Owners = append(out.Owners, chatOwner{m["backend"].(string), m["agent"].(string), m["route"].(string)})
		}
		for _, c := range r["unavailable"].([]any) {
			out.Unavailable = append(out.Unavailable, c.(string))
		}
		return out
	}
	if got := owners(alfred); len(got.Owners) != 1 || got.Owners[0] != (chatOwner{"hermes", "alfred", "#/chat/a/alfred/" + alfred}) {
		t.Fatalf("alfred id resolved to %+v", got.Owners)
	}
	if got := owners(spirit); len(got.Owners) != 1 || got.Owners[0] != (chatOwner{"spirit", "", "#/chat/" + spirit}) {
		t.Fatalf("spirit id resolved to %+v", got.Owners)
	}
	if got := owners("0123456789abcdef"); len(got.Owners) != 1 || got.Owners[0] != (chatOwner{"terminal", "claude", "#/chat/a/claude/0123456789abcdef"}) {
		t.Fatalf("terminal id resolved to %+v", got.Owners)
	}
	missing := owners("20260101-000000-dead")
	if len(missing.Owners) != 0 || len(missing.Unavailable) != 0 {
		t.Fatalf("an id no store holds must resolve to no owner with every store asked: %+v", missing)
	}
	s.terminal = nil
	if got := owners("20260101-000000-dead"); len(got.Unavailable) != 1 || got.Unavailable[0] != "terminal" {
		t.Fatalf("a store that cannot be asked must be named, not counted as a miss: %+v", got)
	}
	if code, _ := agentChatJSON(t, s, "GET", "/api/chat/resolve", nil); code != 400 {
		t.Fatalf("empty id: %d", code)
	}
}
