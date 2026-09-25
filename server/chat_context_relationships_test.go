package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/agentchat"
	"manifest/artifacts"
	"manifest/tasks"
	"manifest/vaultwriter"
)

// relationshipsFixture: a native chat server (fake Hermes runner recording
// its prompt) with the owner's task list and a private vault note, so one
// journey can cross records → delivery → output → search → provenance.
func relationshipsFixture(t *testing.T) (*Server, *agentchat.Store, string, string) {
	t.Helper()
	prompt := filepath.Join(t.TempDir(), "prompt")
	s, st, _ := agentChatFixture(t, strings.Replace(echoStub, "sleep 0.3", `printf '%s' "$prompt" > '`+prompt+`'`, 1))
	pool, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg, err := artifacts.NewRegistry(pool)
	if err != nil {
		t.Fatal(err)
	}
	s.UseArtifacts(pool)
	s.UseArtifactRegistry(reg)
	vault := t.TempDir()
	taskText := "# Tasks\n\n## Inbox\n- [ ] Same title [todo:: inbox/first] [priority:: high] [depends:: work/second]\n\n## Work\n- [ ] Same title [todo:: work/second]\n- [ ] Private salary note task PRIVATE_TASK_TITLE [todo:: work/private]\n"
	if err := os.WriteFile(filepath.Join(vault, "to do.md"), []byte(taskText), 0o644); err != nil {
		t.Fatal(err)
	}
	vw := vaultwriter.New(vault).Grant(vaultwriter.Capability{Name: "todos", Pattern: "to do*", Actor: vaultwriter.ActorUserAction})
	s.UseVault(vw)
	s.UseTasks(tasks.NewStore(vault, "to do.md", vw.BindAbs("todos")))
	return s, st, vault, prompt
}

// The evidence the relationships row demands: start from a task without a
// duplicate record, search the exact record by typeahead across kinds,
// deliver the reviewed bytes, capture the output, find it by content and
// return to the producing run, and keep every private surface off the
// portal.
func TestWorkbenchContextRelationshipsJourney(t *testing.T) {
	s, st, vault, prompt := relationshipsFixture(t)
	before, err := os.ReadFile(filepath.Join(vault, "to do.md"))
	if err != nil {
		t.Fatal(err)
	}
	// 1. Cross-kind typeahead: equal titles stay separate exact IDs, and an
	//    unavailable kind is named beside the results rather than hiding them.
	code, found := artifactsDo(t, s, "GET", "/api/chat/records?kind=any&q=same+title", "")
	if code != 200 {
		t.Fatal(code, found)
	}
	ids := []string{}
	for _, row := range found["records"].([]any) {
		r := row.(map[string]any)
		ids = append(ids, r["kind"].(string)+":"+r["id"].(string))
	}
	if strings.Join(ids, ",") != "task:inbox/first,task:work/second" {
		t.Fatal("cross-kind search merged or lost exact identities", ids)
	}
	unavailable := map[string]bool{}
	for _, u := range found["unavailable"].([]any) {
		unavailable[u.(map[string]any)["kind"].(string)] = true
	}
	if !unavailable["goal"] || !unavailable["note"] || unavailable["task"] {
		t.Fatal("unavailable kinds must be named, available ones not", found["unavailable"])
	}
	for _, kind := range []string{"schedule", "calendar"} {
		if unavailable[kind] {
			t.Fatal("cross-kind search must not read date-scoped or provider-backed kinds", kind)
		}
	}
	// 2. Review the exact record and retain it; the task list is untouched.
	snapshot := contextPreview(t, s, "task", "inbox/first")
	content := snapshot["content"].(string)
	if !strings.Contains(content, "Priority: high") || !strings.Contains(content, "Depends on: work/second") || !strings.Contains(content, "Blocked by: work/second") {
		t.Fatal("dependency/priority relationships missing from context", content)
	}
	if strings.Contains(content, "PRIVATE_TASK_TITLE") {
		t.Fatal("another task leaked into the snapshot")
	}
	code, ref := contextRetain(t, s, "task", "inbox/first", snapshot["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	// 3. Start from the task: one conversation linked to it, no duplicate task.
	id, err := st.Create("alfred", "", "Task chat", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTask("alfred", id, "inbox/first"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "to do.md"), append(before, "- [ ] Later edit [todo:: inbox/later]\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	base := "/api/agents/chat/alfred/sessions/" + id
	send := map[string]any{"text": "Work the reviewed task", "requestId": "relationships-send-001", "explicitArtifacts": true, "artifacts": []artifactContextRef{{ID: ref["id"].(string), Revision: ref["revision"].(string)}}}
	if code, out := agentChatJSON(t, s, "POST", base+"/messages", send); code != 200 {
		t.Fatal(code, out)
	}
	sess := waitIdle(t, st, "alfred", id)
	raw, err := os.ReadFile(prompt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), content) || !strings.Contains(string(raw), `source-task="inbox/first"`) || strings.Contains(string(raw), "inbox/later") || strings.Contains(string(raw), "PRIVATE_TASK_TITLE") {
		t.Fatal("delivered context is not the reviewed version, or leaked other records", string(raw))
	}
	if sess.Task != "inbox/first" || len(sess.Deliveries) != 1 || sess.Deliveries[0].Context.Task != "inbox/first" {
		t.Fatal("task link lost or duplicated", sess)
	}
	if after, _ := os.ReadFile(filepath.Join(vault, "to do.md")); !strings.HasPrefix(string(after), string(before)) || strings.Count(string(after), "[todo:: inbox/first]") != 1 {
		t.Fatal("task record duplicated or rewritten", string(after))
	}
	if code, _ := agentChatJSON(t, s, "POST", base+"/messages", send); code != 200 {
		t.Fatal(code)
	}
	if again := waitIdle(t, st, "alfred", id); len(again.Deliveries) != 1 {
		t.Fatal("retry ran the instruction twice", again.Deliveries)
	}
	// 4. Capture the output, search it by content, return to the producing run.
	sess, body, _, _ := st.Get("alfred", id)
	outputs := chatOutputs(sess, body)
	if len(outputs) != 1 {
		t.Fatal(outputs)
	}
	code, captured := agentChatJSON(t, s, "POST", base+"/output", map[string]any{"delivery": outputs[0].Delivery, "hash": outputs[0].Hash})
	if code != 200 {
		t.Fatal(code, captured)
	}
	code, list := artifactsDo(t, s, "GET", "/api/artifacts?sources=1&q="+url.QueryEscape("REPLY to:"), "")
	if code != 200 {
		t.Fatal(code, list)
	}
	var producing map[string]any
	for _, row := range list["artifacts"].([]any) {
		a := row.(map[string]any)
		if a["id"] == captured["id"] {
			producing = a
		}
	}
	if producing == nil {
		t.Fatal("captured output not found by content", list)
	}
	prov := producing["provenance"].(map[string]any)
	if prov["delivery"] != "relationships-send-001" || prov["session"] != sessionConversation(sess).Key {
		t.Fatal("output provenance does not name the producing run", prov)
	}
	routes := map[string]string{}
	for _, link := range producing["sources"].([]any) {
		l := link.(map[string]any)
		routes[l["kind"].(string)] = l["route"].(string)
	}
	if routes["conversation"] != "#/chat/a/alfred/"+id {
		t.Fatal("no route back to the producing conversation", routes)
	}
	// The retained record snapshot itself resolves back to its source task.
	code, sources := artifactsDo(t, s, "GET", "/api/artifacts/get?sources=1&id="+url.QueryEscape(ref["id"].(string)), "")
	if code != 200 {
		t.Fatal(code, sources)
	}
	linked := false
	for _, link := range sources["sources"].([]any) {
		l := link.(map[string]any)
		if l["kind"] == "task" && l["id"] == "inbox/first" && l["route"] == "#/tasks/inbox%2Ffirst" {
			linked = true
		}
	}
	if !linked {
		t.Fatal("snapshot lost its source task", sources)
	}
	// 5. Privacy: the portal serves none of it; a shared input refuses the
	//    private selection before any runtime.
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/api/chat/records?kind=any&q=same", "/api/artifacts?q=REPLY", "/api/artifacts/get?id=" + url.QueryEscape(ref["id"].(string)), base + "/skills", base} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", route, nil))
		if w.Code == 200 {
			t.Fatal("portal served a private surface", route)
		}
	}
}

// A context snapshot leaving with a share is named by the private record it
// came from, so the owner reviewing the envelope sees "task inbox/first" and
// not only a file called inbox/first#context-<hash>.
func TestShareReviewNamesPrivateRecordSnapshots(t *testing.T) {
	s, st, _, _ := relationshipsFixture(t)
	team, _ := chatFixture(t)
	s.chat = team.chat
	snapshot := contextPreview(t, s, "task", "inbox/first")
	code, ref := contextRetain(t, s, "task", "inbox/first", snapshot["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	other, err := s.artifactReg.Put(artifacts.Put{Ref: "plans/plain.md", Content: []byte("ordinary plan")})
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Create("kairos-private", "kairos-private", "Share me", "")
	if err != nil {
		t.Fatal(err)
	}
	sess, body, _, _ := st.Get("kairos-private", id)
	sess.Deliveries = []agentchat.Delivery{{ID: "done", State: agentchat.DeliveryCompleted, Context: &agentchat.MessageContext{Artifacts: []agentchat.ArtifactReference{{ID: ref["id"].(string), Revision: ref["revision"].(string)}, {ID: other.Artifact.ID, Revision: other.Revision.Hash}}}}}
	review := s.chatShareReview(context.Background(), sess, body, nil)
	if len(review.Blockers) != 0 || len(review.Files) != 2 {
		t.Fatalf("%+v", review)
	}
	byName := map[string]chatShareFile{}
	for _, f := range review.Files {
		byName[f.Name] = f
	}
	if f := byName["inbox/first#context-"+snapshot["revision"].(string)]; f.Record != "task inbox/first" {
		t.Fatalf("snapshot not named by its record: %+v", review.Files)
	}
	if byName["plans/plain.md"].Record != "" {
		t.Fatal("an ordinary artifact is not a record snapshot", byName["plans/plain.md"])
	}
	raw, _ := json.Marshal(review.Files)
	if !strings.Contains(string(raw), `"record":"task inbox/first"`) {
		t.Fatal("record label missing from the reviewed envelope", string(raw))
	}
}
