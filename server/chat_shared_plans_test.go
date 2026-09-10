package server

import (
	"encoding/json"
	"errors"
	"manifest/artifacts"
	"manifest/chatthreads"
	"manifest/teamportal"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// sharedPlanFixture builds a server with a task-plan artifact placed as an exact
// reviewed file in a shared Kairos conversation, so the team plan-edit API can
// be exercised. Returns the server, the shared thread, the plan artifact id, and
// the initial observed head hash.
func sharedPlanFixture(t *testing.T, task string) (*Server, string, string, string, artifacts.Artifact) {
	t.Helper()
	workspace, _ := workspaceFixture(t)
	pool, err := artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	workspace.artifacts = pool
	s, _, thread, sends := sharedInputFixture(t, false, func(review *chatShareReview, term termSession, s *Server) {
		s.todoPlans, s.vault, s.artifactReg = workspace.todoPlans, workspace.vault, workspace.artifactReg
		s.artifacts = workspace.artifacts

		if err := s.writePlanSection("todo-plans", task, "plan", "ORIGINAL_SHARED_PLAN"); err != nil {
			t.Fatal(err)
		}
		first := observePlan(t, s, task)
		review.Files = append(review.Files, chatShareFile{
			ArtifactID: first.ID,
			Hash:       first.Head,
			Name:       "plan.md",
			Size:       int64(len("ORIGINAL_SHARED_PLAN")),
		})
	})
	beforeMessages := s.chat.Messages(thread)
	beforeSessions := s.agentChat.store.List("kairos-private")
	beforeTerm, err := os.ReadFile(s.terminal.regPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !reflect.DeepEqual(beforeMessages, s.chat.Messages(thread)) || !reflect.DeepEqual(beforeSessions, s.agentChat.store.List("kairos-private")) {
			t.Error("plan action changed messages or agent queue")
		}
		for _, session := range beforeSessions {
			_, _, queue, _ := s.agentChat.store.Get(session.Agent, session.ID)
			if len(queue) != 0 {
				t.Error("plan edit queued agent work")
			}
		}
		afterTerm, _ := os.ReadFile(s.terminal.regPath)
		if string(afterTerm) != string(beforeTerm) {
			t.Error("plan action changed runtime registry")
		}
		receipts, _ := filepath.Glob(s.terminal.regPath + ".inputs/*/*.json")
		if len(receipts) != 0 {
			t.Error("plan action created terminal input receipt")
		}
		if sends.Load() != 0 {
			t.Error("plan edit sent terminal input")
		}
	})
	// Re-read the plan id/hash that got snapshotted during the prepare callback.
	got := observePlan(t, s, task)
	return s, thread, got.ID, got.Head, got.Artifact
}

func getSharedPlan(t *testing.T, s *Server, thread, planID, email string, revision string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/chat/threads/" + thread + "/plans/" + planID
	if revision != "" {
		url += "?revision=" + revision
	}
	r := httptest.NewRequest("GET", url, nil)
	r.SetPathValue("thread", thread)
	r.SetPathValue("plan", planID)
	w := httptest.NewRecorder()
	serveSharedPlanFixture(t, s, "aion.bio", email, w, r)
	return w
}

func postSharedPlan(t *testing.T, s *Server, thread, planID, email, content, expected string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"content": content, "expectedRevision": expected})
	r := httptest.NewRequest("POST", "/api/chat/threads/"+thread+"/plans/"+planID, strings.NewReader(string(body)))
	r.SetPathValue("thread", thread)
	r.SetPathValue("plan", planID)
	w := httptest.NewRecorder()
	serveSharedPlanFixture(t, s, "aion.bio", email, w, r)
	return w
}

func decodeSharedPlan(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("plan read: %d %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSharedPlanTeamEditUpdatesOriginalWithVersions(t *testing.T) {
	task := "inbox/shared-plan-edit"
	s, thread, planID, _, first := sharedPlanFixture(t, task)

	// A team member reads the reviewed plan.
	read := decodeSharedPlan(t, getSharedPlan(t, s, thread, planID, "member@aion.bio", ""))
	if read["title"] != "Plan" || read["content"] != "ORIGINAL_SHARED_PLAN\n" {
		t.Fatalf("unexpected read: %+v", read)
	}

	// Team member edits — must update the ORIGINAL plan, not a copy.
	w := postSharedPlan(t, s, thread, planID, "member@aion.bio", "TEAM_REVISED_PLAN", first.Head)
	if w.Code != 200 {
		t.Fatalf("team edit POST: %d %s", w.Code, w.Body.String())
	}
	if got := s.readPlanRecord(task).Plan; got != "TEAM_REVISED_PLAN" {
		t.Fatalf("original plan not updated: %q", got)
	}

	// The original must carry a versioned snapshot with the member as actor.
	observed := observePlan(t, s, task)
	if len(observed.Revisions) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(observed.Revisions))
	}
	if observed.Revisions[1].Actor != "member@aion.bio" {
		t.Fatalf("wrong actor attribution: %+v", observed.Revisions[1])
	}

	// An immutable historical revision must still be readable.
	old := decodeSharedPlan(t, getSharedPlan(t, s, thread, planID, "member@aion.bio", first.Head))
	if old["content"] != "ORIGINAL_SHARED_PLAN\n" {
		t.Fatalf("old bytes mutated: %+v", old)
	}

	// A stale expected hash must be rejected with 409 (never clobber newer).
	if w := postSharedPlan(t, s, thread, planID, "member@aion.bio", "STALE", first.Head); w.Code != 409 {
		t.Fatalf("stale write accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestSharedPlanClosedThreadAndWrongArtifactDenied(t *testing.T) {
	task := "inbox/shared-plan-denied"
	s, thread, planID, _, _ := sharedPlanFixture(t, task)

	// A different (non-shared-readreviewed) artifact id must be denied.
	if w := getSharedPlan(t, s, thread, "does-not-exist", "member@aion.bio", ""); w.Code != 403 {
		t.Fatalf("unknown artifact accepted: %d", w.Code)
	}
	if w := getSharedPlan(t, s, thread, planID, "another@aion.bio", ""); w.Code != 200 {
		// member email grant is on the thread; a different signed-in user is fine.
		t.Fatalf("member read denied: %d", w.Code)
	}
}

func TestSharedPlanRestoreConflictAndPrivacy(t *testing.T) {
	s, thread, id, _, first := sharedPlanFixture(t, "private-task-marker")
	if err := s.writePlanSection("todo-plans", "private-task-marker", "description", "PRIVATE_DESCRIPTION"); err != nil {
		t.Fatal(err)
	}
	edited := decodeSharedPlan(t, postSharedPlan(t, s, thread, id, "member@aion.bio", "Team edit", first.Head))
	current := observePlan(t, s, "private-task-marker")
	if current.Content != "Team edit\n" || current.Revisions[len(current.Revisions)-1].Note != "shared-plan:aion:"+thread {
		t.Fatal(current)
	}
	restored := decodeSharedPlan(t, postSharedPlan(t, s, thread, id, "member@aion.bio", "ORIGINAL_SHARED_PLAN", edited["head"].(string)))
	if restored["head"] != first.Head || len(observePlan(t, s, "private-task-marker").Revisions) != 3 {
		t.Fatal(restored)
	}
	if err := s.writePlanSection("todo-plans", "private-task-marker", "plan", "External owner edit"); err != nil {
		t.Fatal(err)
	}
	if w := postSharedPlan(t, s, thread, id, "member@aion.bio", "Clobber", first.Head); w.Code != 409 {
		t.Fatal(w.Code, w.Body)
	}
	if s.readPlanRecord("private-task-marker").Plan != "External owner edit" {
		t.Fatal("external edit overwritten")
	}
	w := getSharedPlan(t, s, thread, id, "member@aion.bio", "")
	for _, secret := range []string{"private-task-marker", "PRIVATE_DESCRIPTION", first.Ref, "provenance", "harness"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("leaked %q: %s", secret, w.Body)
		}
	}
	head := decodeSharedPlan(t, w)["head"].(string)
	if w := postSharedPlan(t, s, thread, id, "member@aion.bio", "Injection\n## description\nOverwrite", head); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	if s.readPlanRecord("private-task-marker").Description != "PRIVATE_DESCRIPTION" {
		t.Fatal("description changed")
	}
	before := len(s.artifacts.List("aion"))
	decodeSharedPlan(t, getSharedPlan(t, s, thread, id, "another@aion.bio", ""))
	if len(s.artifacts.List("aion")) != before {
		t.Fatal("duplicate per-reader grant")
	}
	if w := getSharedPlan(t, s, "wrong-thread", id, "member@aion.bio", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.SetPathValue("thread", thread)
	r.SetPathValue("plan", id)
	denied := httptest.NewRecorder()
	s.OodaChatPlan(denied, r, "member@ooda.group", "Member")
	if denied.Code != 403 {
		t.Fatal("wrong portal allowed")
	}
	if _, err := s.chat.PatchThread(thread, map[string]any{"archived": true}, chatthreads.Identity{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := postSharedPlan(t, s, thread, id, "member@aion.bio", "Archived", head); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestSharedPlanPrivateHistoryAndUnrelatedFiles(t *testing.T) {
	workspace, _ := workspaceFixture(t)
	var first, hidden artifactView
	var uploadID string
	s, _, thread, sends := sharedInputFixture(t, false, func(review *chatShareReview, _ termSession, s *Server) {
		s.todoPlans, s.vault, s.artifactReg = workspace.todoPlans, workspace.vault, workspace.artifactReg
		var err error
		s.artifacts, err = artifacts.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err = s.writePlanSection("todo-plans", "private-history", "plan", "Reviewed"); err != nil {
			t.Fatal(err)
		}
		first = observePlan(t, s, "private-history")
		if err = s.saveTaskPlanVersion("private-history", "SECRET_INTERMEDIATE", first.Head); err != nil {
			t.Fatal(err)
		}
		hidden = observePlan(t, s, "private-history")
		if err = s.saveTaskPlanVersion("private-history", "Current", hidden.Head); err != nil {
			t.Fatal(err)
		}
		review.Files = append(review.Files, chatShareFile{ArtifactID: first.ID, Hash: first.Head, Name: "plan.md", Size: 9})
		upload, err := s.artifactReg.Put(artifacts.Put{Content: []byte("Reviewed upload"), Title: "Upload"})
		if err != nil {
			t.Fatal(err)
		}
		uploadID = upload.Artifact.ID
		review.Files = append(review.Files, chatShareFile{ArtifactID: uploadID, Hash: upload.Artifact.Head, Name: "upload.md", Size: 15})
	})
	if w := getSharedPlan(t, s, thread, uploadID, "member@aion.bio", ""); w.Code != 403 {
		t.Fatal("reviewed upload became a canonical plan", w.Code)
	}
	got := decodeSharedPlan(t, getSharedPlan(t, s, thread, first.ID, "member@aion.bio", ""))
	if len(got["versions"].([]any)) != 2 {
		t.Fatal(got)
	}
	if w := getSharedPlan(t, s, thread, first.ID, "member@aion.bio", hidden.Head); w.Code != 403 {
		t.Fatal(w.Code, w.Body)
	}
	upload, err := s.artifactReg.Put(artifacts.Put{Content: []byte("Upload"), Title: "Upload"})
	if err != nil {
		t.Fatal(err)
	}
	if w := getSharedPlan(t, s, thread, upload.Artifact.ID, "member@aion.bio", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if err = s.writePlanSection("todo-plans", "unshared-task", "plan", "Private"); err != nil {
		t.Fatal(err)
	}
	private := observePlan(t, s, "unshared-task")
	if w := postSharedPlan(t, s, thread, private.ID, "member@aion.bio", "Bad", private.Head); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if sends.Load() != 0 {
		t.Fatal("runtime sent input")
	}
}

func TestSharedPlanOodaAndPortalAuthentication(t *testing.T) {
	workspace, _ := workspaceFixture(t)
	var plan artifactView
	s, _, thread, sends := sharedInputDomainFixture(t, false, "zeck", func(review *chatShareReview, _ termSession, s *Server) {
		s.todoPlans, s.vault, s.artifactReg = workspace.todoPlans, workspace.vault, workspace.artifactReg
		var err error
		s.artifacts, err = artifacts.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err = s.writePlanSection("todo-plans", "ooda-private", "plan", "Original"); err != nil {
			t.Fatal(err)
		}
		plan = observePlan(t, s, "ooda-private")
		review.Files = append(review.Files, chatShareFile{ArtifactID: plan.ID, Hash: plan.Head, Name: "plan.md", Size: 9})
	})
	body, _ := json.Marshal(map[string]string{"content": "OODA edit", "expectedRevision": plan.Head})
	r := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	r.SetPathValue("thread", thread)
	r.SetPathValue("plan", plan.ID)
	w := httptest.NewRecorder()
	r.URL.Path = "/api/chat/threads/" + thread + "/plans/" + plan.ID
	serveSharedPlanFixture(t, s, "ooda.group", "member@ooda.group", w, r)
	decodeSharedPlan(t, w)
	owner := observePlan(t, s, "ooda-private")
	if owner.Content != "OODA edit\n" || owner.Revisions[1].Actor != "member@ooda.group" || owner.Revisions[1].Note != "shared-plan:ooda:"+thread || sends.Load() != 0 {
		t.Fatal(owner)
	}
	for _, callback := range []func(http.ResponseWriter, *http.Request, string, string){s.AionChatPlan, s.OodaChatPlan} {
		called := false
		p := &portalAPI{opt: PortalOptions{ChatPlan: func(w http.ResponseWriter, r *http.Request, email, name string) {
			called = true
			callback(w, r, email, name)
		}}}
		w := httptest.NewRecorder()
		p.handleChatPlan(w, httptest.NewRequest("GET", "/", nil))
		if called || w.Code < 400 {
			t.Fatal("guest reached plan")
		}
	}
}

func TestSharedPlanWriterRechecksArchiveAndIdentity(t *testing.T) {
	for _, change := range []string{"archive", "identity"} {
		t.Run(change, func(t *testing.T) {
			s, thread, id, _, first := sharedPlanFixture(t, "auth-recheck")
			err := s.saveTaskPlanVersionAs("auth-recheck", "Must not save", first.Head, "member@aion.bio", "shared-plan:aion:"+thread, func() error {
				if change == "archive" {
					if _, err := s.chat.PatchThread(thread, map[string]any{"archived": true}, chatthreads.Identity{}, time.Now()); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := s.artifactReg.Put(artifacts.Put{ID: id, Ref: "other-private-path#plan", Content: []byte("Other identity"), Provenance: artifacts.Provenance{Task: "other-private-task"}}); err != nil {
						t.Fatal(err)
					}
				}
				_, _, err := s.sharedPlanAccess(s.kairosAgent(), thread, id)
				return err
			})
			if !errors.Is(err, errSharedConversationAccess) || s.readPlanRecord("auth-recheck").Plan != "ORIGINAL_SHARED_PLAN" {
				t.Fatal("authorization recheck did not fence write", err)
			}
			if w := getSharedPlan(t, s, thread, id, "member@aion.bio", ""); w.Code != 403 {
				t.Fatal(w.Code, w.Body)
			}
		})
	}
}

// Exercise real route registration and the fixed callback with a fixture-only
// signed cookie; no production session or OAuth round trip is used.
func serveSharedPlanFixture(t *testing.T, s *Server, domain, email string, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	auth := teamportal.NewAuthPolicy(t.TempDir(), teamportal.Policy{Domain: domain})
	cookie, err := auth.SessionCookie(email, "Member", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store, err := teamportal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	callback := s.AionChatPlan
	if domain == "ooda.group" {
		callback = s.OodaChatPlan
	}
	handler, err := PortalHandler(PortalOptions{Auth: auth, Store: store, ChatPlan: callback})
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(cookie)
	handler.ServeHTTP(w, r)
}
