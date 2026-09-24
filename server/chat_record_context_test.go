package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/artifacts"
)

func recordContextFixture(t *testing.T) (*Server, string, string) {
	t.Helper()
	s, root, _ := artifactFixture(t)
	taskText := "# Tasks\n\n## Inbox\n- [ ] Same title [todo:: inbox/first] [priority:: high] [depends:: work/second]\n\n## Work\n- [ ] Same title [todo:: work/second]\n- [x] Finished task [todo:: work/done]\n"
	if err := os.WriteFile(filepath.Join(root, "to do.md"), []byte(taskText), 0644); err != nil {
		t.Fatal(err)
	}
	s.UseTaskPlans("system/todo-plans")
	if err := os.MkdirAll(filepath.Join(root, "system/todo-plans"), 0755); err != nil {
		t.Fatal(err)
	}
	plan := "---\ntodo: inbox/first\nassignee: agent:alfred\n---\n## description\n\nOWNER_CONTEXT_ONLY\n\n## plan\n\nREVIEWED_PLAN_ONLY\n"
	if err := os.WriteFile(filepath.Join(root, s.todoPlans.rel("inbox/first")), []byte(plan), 0644); err != nil {
		t.Fatal(err)
	}
	g, goalRoot := goalsServer(t, "# Goals\n\n## Work\n\n### 1-year — 2026\n- [ ] Annual target [goal:: work/annual]\n\n### Rocks (90-day)\n- [ ] Parent goal [goal:: work/parent] [serves:: work/annual] [quarter:: 2026-Q3]\n    - [ ] Selected stage [goal:: work/stage] [due:: 2026-10-01]\n        - [x] Frozen step\n- [ ] Unrelated secret goal [goal:: work/unrelated]\n")
	s.goals = g.goals
	return s, root, goalRoot
}
func contextPreview(t *testing.T, s *Server, kind, id string) map[string]any {
	t.Helper()
	code, out := artifactsDo(t, s, "GET", "/api/chat/records/preview?kind="+kind+"&id="+url.QueryEscape(id), "")
	if code != 200 {
		t.Fatal(code, out)
	}
	return out
}
func contextRetain(t *testing.T, s *Server, kind, id, rev string) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"kind": kind, "id": id, "revision": rev})
	return artifactsDo(t, s, "POST", "/api/chat/records/retain", string(b))
}

func TestRecordContextTaskIdentityAndStaleness(t *testing.T) {
	s, root, _ := recordContextFixture(t)
	before, err := os.ReadFile(filepath.Join(root, "to do.md"))
	if err != nil {
		t.Fatal(err)
	}
	code, result := artifactsDo(t, s, "GET", "/api/chat/records?kind=task&q=same", "")
	if code != 200 {
		t.Fatal(code, result)
	}
	rows := result["records"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["id"] == rows[1].(map[string]any)["id"] {
		t.Fatal(rows)
	}
	serialized, _ := json.Marshal(rows)
	if strings.Contains(string(serialized), "OWNER_CONTEXT_ONLY") {
		t.Fatal("search exposed narrative")
	}
	note := contextPreview(t, s, "task", "inbox/first")
	text := note["content"].(string)
	for _, want := range []string{"Record ID: inbox/first", "OWNER_CONTEXT_ONLY", "REVIEWED_PLAN_ONLY", "Priority: high", "Depends on: work/second", "Blocked by: work/second"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
	other := contextPreview(t, s, "task", "work/second")
	if strings.Contains(other["content"].(string), "OWNER_CONTEXT_ONLY") {
		t.Fatal("other task narrative leaked")
	}
	if len(s.artifactReg.List(artifacts.Filter{})) != 0 {
		t.Fatal("preview retained artifact")
	}
	code, ref := contextRetain(t, s, "task", "inbox/first", note["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	a, _ := s.artifactReg.Get(ref["id"].(string))
	if a.Provenance.Task != "" || a.Provenance.Source != "task-context" {
		t.Fatal("reference became task ownership", a)
	}
	if code, retry := contextRetain(t, s, "task", "inbox/first", note["revision"].(string)); code != 200 || retry["id"] != ref["id"] || len(s.artifactReg.List(artifacts.Filter{})) != 1 {
		t.Fatal(code, retry)
	}
	links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]
	if len(links) != 1 || links[0].Kind != "task" || links[0].Route != "#/tasks/inbox%2Ffirst" {
		t.Fatal(links)
	}
	after, _ := os.ReadFile(filepath.Join(root, "to do.md"))
	if string(before) != string(after) {
		t.Fatal("context modified original tasks")
	}
	path := filepath.Join(root, s.todoPlans.rel("inbox/first"))
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "REVIEWED_PLAN_ONLY", "CHANGED_PLAN", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := contextRetain(t, s, "task", "inbox/first", note["revision"].(string)); code != 409 {
		t.Fatal("stale plan accepted", code)
	}
	refs := []artifactContextRef{{ID: a.ID, Revision: a.Head}}
	if _, err := s.selectedArtifactContext(false, "", "", refs, nil); err == nil {
		t.Fatal("implicit task context accepted")
	}
	ctx, err := s.selectedArtifactContext(true, "", "", refs, nil)
	if err != nil || !strings.Contains(ctx, text) || strings.Contains(ctx, "CHANGED_PLAN") || !strings.Contains(ctx, `source-task="inbox/first"`) {
		t.Fatal(ctx, err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "todo: inbox/first", "todo: other/task", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := artifactsDo(t, s, "GET", "/api/chat/records/preview?kind=task&id=inbox/first", ""); code == 200 {
		t.Fatal("plan identity collision leaked")
	}
	for _, id := range []string{"work/done", "missing/task"} {
		if code, _ := artifactsDo(t, s, "GET", "/api/chat/records/preview?kind=task&id="+url.QueryEscape(id), ""); code == 200 {
			t.Fatal(id)
		}
	}
}

func TestRecordContextGoalBranchAndRevision(t *testing.T) {
	s, _, root := recordContextFixture(t)
	before, _ := os.ReadFile(filepath.Join(root, "goals.md"))
	note := contextPreview(t, s, "goal", "work/stage")
	text := note["content"].(string)
	for _, want := range []string{"Record ID: work/stage", "Ancestor: Parent goal [work/parent]", "Due: 2026-10-01", "Frozen step"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
	if strings.Contains(text, "Unrelated secret goal") {
		t.Fatal("unrelated branch leaked", text)
	}
	parent := contextPreview(t, s, "goal", "work/parent")
	if !strings.Contains(parent["content"].(string), "Serves: work/annual") || !strings.Contains(parent["content"].(string), "Selected stage") {
		t.Fatal(parent)
	}
	code, ref := contextRetain(t, s, "goal", "work/stage", note["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	a, _ := s.artifactReg.Get(ref["id"].(string))
	links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]
	if len(links) != 1 || links[0].Kind != "goal" || links[0].Route != "#/goals/work%2Fstage" {
		t.Fatal(links)
	}
	after, _ := os.ReadFile(filepath.Join(root, "goals.md"))
	if string(before) != string(after) {
		t.Fatal("context changed source goal")
	}
	if err := os.WriteFile(filepath.Join(root, "goals.md"), []byte(strings.Replace(string(before), "Parent goal", "Renamed parent", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := contextRetain(t, s, "goal", "work/stage", note["revision"].(string)); code != 409 {
		t.Fatal("changed ancestry accepted", code)
	}
	if code, out := artifactsDo(t, s, "GET", "/api/chat/records?kind=goal&q=renamed", ""); code != 200 || len(out["records"].([]any)) != 3 {
		t.Fatal("ancestry search", code, out)
	}
	if code, _ := contextRetain(t, s, "task", "work/stage", note["revision"].(string)); code == 200 {
		t.Fatal("record kind substitution")
	}
}

func TestRecordContextDeliveryAndSharedBoundary(t *testing.T) {
	records, _, _ := recordContextFixture(t)
	note := contextPreview(t, records, "task", "inbox/first")
	code, ref := contextRetain(t, records, "task", "inbox/first", note["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	s, se, sends := terminalReceiptFixture(t, false, func(text string) {
		if !strings.Contains(text, note["content"].(string)) || !strings.Contains(text, `source-task="inbox/first"`) {
			t.Error("missing exact record context", text)
		}
	})
	s.artifactReg = records.artifactReg
	payload := map[string]any{"text": "Discuss the selected task", "requestId": "receipt-input-001", "explicitArtifacts": true, "artifacts": []artifactContextRef{{ID: ref["id"].(string), Revision: ref["revision"].(string)}}}
	body, _ := json.Marshal(payload)
	if w := receiptInput(s, se.ID, string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := receiptInput(s, se.ID, string(body)); w.Code != 200 || sends.Load() != 1 {
		t.Fatal(w.Code, w.Body.String(), sends.Load())
	}
	shared, sharedSE, _, sharedSends := sharedInputFixture(t, false)
	shared.artifactReg = records.artifactReg
	if w := receiptInput(shared, sharedSE.ID, string(body)); w.Code == 200 || sharedSends.Load() != 0 {
		t.Fatal("shared record leakage", w.Code, sharedSends.Load())
	}
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/api/chat/records?kind=task&q=same", "/api/chat/records/preview?kind=goal&id=work/stage", "/api/chat/records/retain"} {
		for _, method := range []string{"GET", "POST"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, route, strings.NewReader(string(body))))
			if w.Code == 200 {
				t.Fatal("portal record leakage", route, method)
			}
		}
	}
}

func TestRecordContextReadFailuresAndKnowledgeCompatibility(t *testing.T) {
	s, _, _ := recordContextFixture(t)
	code, out := artifactsDo(t, s, "GET", "/api/chat/records?kind=task&q=rough+electrical", "")
	if code != 200 || len(out["records"].([]any)) != 1 {
		t.Fatal("property tasks omitted", code, out)
	}
	row := out["records"].([]any)[0].(map[string]any)
	if !strings.HasPrefix(row["id"].(string), "prop:761-maple/") {
		t.Fatal("property identity lost", row)
	}
	property := contextPreview(t, s, "task", row["id"].(string))
	if !strings.Contains(property["content"].(string), "Source: property") {
		t.Fatal(property)
	}
	s.index.Close()
	if code, _ := artifactsDo(t, s, "GET", "/api/chat/records?kind=task&q=rough", ""); code == 200 {
		t.Fatal("property read failure became empty results")
	}
	if code, _ := artifactsDo(t, s, "GET", "/api/chat/records/preview?kind=task&id=inbox/first", ""); code == 200 {
		t.Fatal("partial task set accepted after source read failure")
	}
	noteContextIndex(t, s)
	code, out = artifactsDo(t, s, "GET", "/api/chat/records?kind=note&q=contextneedle", "")
	if code != 200 || len(out["records"].([]any)) != 1 {
		t.Fatal(code, out)
	}
	old := contextPreview(t, s, "note", "one/same.md")
	code, ref := contextRetain(t, s, "note", "one/same.md", old["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	a, _ := s.artifactReg.Get(ref["id"].(string))
	if a.Provenance.Source != "knowledge-context" || knowledgeContextPath(a) != "one/same.md" {
		t.Fatal(a)
	}
	for _, route := range []string{"/api/chat/records?kind=unknown", "/api/chat/records?kind=goal&q=" + strings.Repeat("x", 257), "/api/chat/records/preview?kind=note&id=system/secret.md"} {
		if code, _ := artifactsDo(t, s, "GET", route, ""); code == 200 {
			t.Fatal(route)
		}
	}
}
