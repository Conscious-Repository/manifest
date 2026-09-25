package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# body\n"
	if name != "" || description != "" {
		body = "---\nname: " + name + "\ndescription: \"" + description + "\"\n---\n# body\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The inventory is a read of the profile's skill folder at request time:
// nested categories, frontmatter names with folder fallback, a missing
// profile folder reported as unavailable, and never a claim of what a turn
// loaded. Portal agents have no private session record and answer 404.
func TestNativeSkillInventoryReadsProfileFolder(t *testing.T) {
	s, st, _ := agentChatFixture(t, echoStub)
	home := t.TempDir()
	s.hosts.Hermes.Home = home
	writeSkill(t, filepath.Join(home, "skills", "email", "triage"), "email-inbox-triage", "Triage an inbox: prioritize threads.")
	writeSkill(t, filepath.Join(home, "skills", "plain"), "", "")
	writeSkill(t, filepath.Join(home, "profiles", "kairos-private", "skills", "aion", "investor-copy"), "aion-investor-copy", strings.Repeat("d", 300))
	if err := os.WriteFile(filepath.Join(home, "skills", "README.md"), []byte("not a skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A symlinked SKILL.md is not followed: the inventory reports files under the root only.
	if err := os.Symlink(filepath.Join(home, "profiles", "kairos-private", "skills", "aion", "investor-copy", "SKILL.md"), filepath.Join(home, "skills", "plain", "linked.md")); err == nil {
		if err := os.Mkdir(filepath.Join(home, "skills", "linked"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(home, "profiles", "kairos-private", "skills", "aion", "investor-copy", "SKILL.md"), filepath.Join(home, "skills", "linked", "SKILL.md")); err != nil {
			t.Fatal(err)
		}
	}
	id, err := st.Create("alfred", "", "Skills", "")
	if err != nil {
		t.Fatal(err)
	}
	code, out := agentChatJSON(t, s, "GET", "/api/agents/chat/alfred/sessions/"+id+"/skills", nil)
	if code != http.StatusOK || out["adapter"] != "hermes-oneshot" || out["source"] != "on-disk" {
		t.Fatal(code, out)
	}
	skills := out["skills"].([]any)
	if len(skills) != 2 {
		t.Fatalf("expected the two default-profile skills only: %v", skills)
	}
	first, second := skills[0].(map[string]any), skills[1].(map[string]any)
	if first["name"] != "email-inbox-triage" || first["path"] != "email/triage/SKILL.md" || first["description"] != "Triage an inbox: prioritize threads." || first["root"] != "Hermes default profile skills" {
		t.Fatal("frontmatter name/path lost", first)
	}
	if second["name"] != "plain" || second["path"] != "plain/SKILL.md" {
		t.Fatal("folder fallback name lost", second)
	}
	roots := out["roots"].([]any)
	if len(roots) != 1 || roots[0].(map[string]any)["path"] != filepath.Join(home, "skills") || roots[0].(map[string]any)["available"] != true || roots[0].(map[string]any)["count"] != float64(2) {
		t.Fatal("root not reported", roots)
	}
	if note, _ := out["note"].(string); !strings.Contains(note, "loads a skill only when it names it") {
		t.Fatal("inventory must not read as runtime telemetry", note)
	}
	// A profile agent reads its own profile folder; the description is bounded.
	kid, err := st.Create("kairos-private", "kairos-private", "Skills", "")
	if err != nil {
		t.Fatal(err)
	}
	code, out = agentChatJSON(t, s, "GET", "/api/agents/chat/kairos-private/sessions/"+kid+"/skills", nil)
	if code != http.StatusOK {
		t.Fatal(code, out)
	}
	skills = out["skills"].([]any)
	if len(skills) != 1 || skills[0].(map[string]any)["name"] != "aion-investor-copy" || len(skills[0].(map[string]any)["description"].(string)) > 210 {
		t.Fatalf("profile inventory wrong: %v", skills)
	}
	if roots[0].(map[string]any)["label"] != "Hermes default profile skills" || out["roots"].([]any)[0].(map[string]any)["path"] != filepath.Join(home, "profiles", "kairos-private", "skills") {
		t.Fatal("profile root wrong", out["roots"])
	}
	// A missing folder is unavailable, not an empty success.
	zid, err := st.Create("zeck-private", "zeck-private", "Skills", "")
	if err != nil {
		t.Fatal(err)
	}
	code, out = agentChatJSON(t, s, "GET", "/api/agents/chat/zeck-private/sessions/"+zid+"/skills", nil)
	root := out["roots"].([]any)[0].(map[string]any)
	if code != http.StatusOK || root["available"] != false || root["error"] != "folder does not exist" || len(out["skills"].([]any)) != 0 {
		t.Fatal("missing profile folder misreported", code, out)
	}
	// Reading the inventory changes nothing in the conversation record.
	if sess, _, _, ok := st.Get("alfred", id); !ok || len(sess.Deliveries) != 0 {
		t.Fatal("inventory read touched the conversation", sess)
	}
	// Portal agents and unknown sessions have no private inventory.
	for _, path := range []string{"/api/agents/chat/kairos/sessions/" + id + "/skills", "/api/agents/chat/alfred/sessions/nope/skills"} {
		if code, _ := agentChatJSON(t, s, "GET", path, nil); code != http.StatusNotFound {
			t.Fatal(path, code)
		}
	}
	// The team portal never serves the route at all.
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/agents/chat/alfred/sessions/"+id+"/skills", nil))
	if w.Code == http.StatusOK {
		t.Fatal("portal served a private skill inventory")
	}
}

// Coding runtimes resolve their roots from the registry row: Claude Code's
// user folder plus the working folder's project skills; Codex its own
// folder; a legacy tmux row reports nothing rather than guessing.
func TestTerminalSkillInventoryFollowsRegistryRow(t *testing.T) {
	s := New(nil, nil, nil)
	s.UseTerminal(filepath.Join(t.TempDir(), "terminals.json"), t.TempDir(), t.TempDir())
	config, project, codex := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	t.Setenv("CODEX_HOME", codex)
	writeSkill(t, filepath.Join(config, "skills", "synced", "abc", "lrs-writing-review"), "lrs-writing-review", "Judge writing.")
	writeSkill(t, filepath.Join(project, ".claude", "skills", "deploy"), "deploy", "Ship it.")
	writeSkill(t, filepath.Join(codex, "skills", ".system", "review-agent"), "review-agent", "Review.")
	s.terminal.upsert(termSession{ID: "abcdef1234567890", Kind: "claude", Backend: "herdr", Cwd: project})
	s.terminal.upsert(termSession{ID: "abcdef1234567891", Kind: "codex", Backend: "herdr", Cwd: project})
	s.terminal.upsert(termSession{ID: "abcdef1234567892", Kind: "claude", Cwd: project})
	code, out := agentChatJSON(t, s, "GET", "/api/terminal/session/abcdef1234567890/skills", nil)
	if code != http.StatusOK || out["adapter"] != "herdr-claude" || out["source"] != "on-disk" {
		t.Fatal(code, out)
	}
	names := []string{}
	for _, sk := range out["skills"].([]any) {
		names = append(names, sk.(map[string]any)["name"].(string)+"@"+sk.(map[string]any)["root"].(string))
	}
	if strings.Join(names, ",") != "deploy@Project skills in the working folder,lrs-writing-review@Claude Code user skills" {
		t.Fatal(names)
	}
	if roots := out["roots"].([]any); len(roots) != 2 || roots[1].(map[string]any)["path"] != filepath.Join(project, ".claude", "skills") {
		t.Fatal("project root missing", roots)
	}
	code, out = agentChatJSON(t, s, "GET", "/api/terminal/session/abcdef1234567891/skills", nil)
	if code != http.StatusOK || out["adapter"] != "herdr-codex" || len(out["skills"].([]any)) != 1 || out["skills"].([]any)[0].(map[string]any)["path"] != ".system/review-agent/SKILL.md" {
		t.Fatal(code, out)
	}
	code, out = agentChatJSON(t, s, "GET", "/api/terminal/session/abcdef1234567892/skills", nil)
	if code != http.StatusOK || out["adapter"] != "tmux-legacy" || out["source"] != "not-reported" || len(out["roots"].([]any)) != 0 {
		t.Fatal("legacy row invented an inventory", code, out)
	}
	if code, _ := agentChatJSON(t, s, "GET", "/api/terminal/session/ffffffffffffffff/skills", nil); code != http.StatusNotFound {
		t.Fatal(code)
	}
}
