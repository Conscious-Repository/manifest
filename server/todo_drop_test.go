package server

import (
	"encoding/json"
	"manifest/teamportal"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/aion"
	"manifest/record"
	"manifest/vaultwriter"
)

func TestTaskDropRemovesPlan(t *testing.T) {
	for _, id := range []string{"manifest/x", "personal/x", "inbox/thing", "re/x", "aion:x", "re:x", "prop:761-maple/rough-in/rough-electrical"} {
		for _, withPlan := range []bool{false, true} {
			t.Run(id+"/plan="+map[bool]string{true: "yes", false: "no"}[withPlan], func(t *testing.T) {
				s, vault := unifiedHarness(t)
				auditDir := t.TempDir()
				s.vault.WithAudit(auditDir).Grant(
					vaultwriter.Capability{Name: "todo-plans", Zone: record.ZoneSystem, Pattern: "system/todo-plans/**", Actor: vaultwriter.ActorUserAction},
					vaultwriter.Capability{Name: "todo-plans-agent", Zone: record.ZoneSystem, Pattern: "system/todo-plans/**", Actor: vaultwriter.ActorApprovedProposal},
				)
				s.UseTaskPlans("system/todo-plans")
				if err := os.WriteFile(s.tasksStore.Path(), []byte("## Manifest\n- [ ] x\n## Personal\n- [ ] x\n## Inbox\n- [ ] thing\n- [ ] live\n## RE\n- [ ] x\n"), 0644); err != nil {
					t.Fatal(err)
				}
				for _, domain := range []string{"aion", "realestate"} {
					root := "system/" + domain
					if err := os.MkdirAll(filepath.Join(vault, root), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(vault, root, "backlog.md"), []byte(aion.REBacklogSeed+"- [ ] x [id:: x] [kind:: task] [owner:: BA] [status:: open]\n"), 0644); err != nil {
						t.Fatal(err)
					}
					store := aion.NewStore(vault, root, testWriteAbs)
					if domain == "aion" {
						s.aion = store
					} else {
						s.re = store
					}
				}
				const live = "inbox/live"
				if err := s.writePlanSection("todo-plans-agent", live, "plan", "keep live plan"); err != nil {
					t.Fatal(err)
				}
				livePath := filepath.Join(vault, s.todoPlans.rel(live))
				before, err := os.ReadFile(livePath)
				if err != nil {
					t.Fatal(err)
				}
				// Ordinary task edits must preserve the same live plan bytes.
				edit := httptest.NewRecorder()
				s.handleTaskUpdate(edit, httptest.NewRequest("POST", "/api/tasks/update", strings.NewReader(`{"id":"inbox/live","waiting":"reply"}`)))
				if edit.Code != 200 {
					t.Fatalf("update: %d %s", edit.Code, edit.Body.String())
				}
				if withPlan {
					if err := s.writePlanSection("todo-plans", id, "description", "owner context"); err != nil {
						t.Fatal(err)
					}
					if err := s.writePlanSection("todo-plans-agent", id, "plan", "agent plan"); err != nil {
						t.Fatal(err)
					}
				}
				body, _ := json.Marshal(map[string]string{"id": id})
				w := httptest.NewRecorder()
				s.handleTaskDrop(w, httptest.NewRequest("POST", "/api/tasks/drop", strings.NewReader(string(body))))
				if w.Code != 200 {
					t.Fatalf("drop: %d %s", w.Code, w.Body.String())
				}
				doc, err := s.tasksStore.Load()
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range s.unifiedRows(doc, time.Now()) {
					if row.ID == id {
						t.Fatal("dropped task still on board")
					}
				}
				if _, err := os.Stat(filepath.Join(vault, s.todoPlans.rel(id))); !os.IsNotExist(err) {
					t.Fatalf("plan survived: %v", err)
				}
				after, err := os.ReadFile(livePath)
				if err != nil || string(after) != string(before) {
					t.Fatalf("live plan changed: %v", err)
				}
				audit, err := os.ReadFile(filepath.Join(auditDir, "write-audit.log"))
				if err != nil {
					t.Fatal(err)
				}
				removed := strings.Contains(string(audit), s.todoPlans.rel(id)+"\ttodo-plans\tuser-action (remove)\t-")
				if removed != withPlan {
					t.Fatalf("unexpected removal audit: %s", audit)
				}
			})
		}
	}
}

func TestPlanRemovalProtectsIdentityAndFailedDrop(t *testing.T) {
	s, vault := panelFixture(t)
	// Different exact ids can share a slug. Never delete the other record.
	const live = "prop:a/b"
	if err := s.writePlanSection("todo-plans-agent", live, "plan", "live"); err != nil {
		t.Fatal(err)
	}
	if err := s.removePlanRecord("prop:a-b"); err == nil {
		t.Fatal("expected collision refusal")
	}
	if !s.readPlanRecord(live).Exists {
		t.Fatal("collision deleted live plan")
	}
	if _, err := os.Stat(filepath.Join(vault, s.todoPlans.rel(live))); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handleTaskDrop(w, httptest.NewRequest("POST", "/api/tasks/drop", strings.NewReader(`{"id":"prop:a/b"}`)))
	if w.Code == 200 {
		t.Fatal("unconfigured drop succeeded")
	}
	if !s.readPlanRecord(live).Exists {
		t.Fatal("failed drop deleted plan")
	}
}

func TestBacklogDeleteRemovesPlan(t *testing.T) {
	for _, prefix := range []string{"aion:", "re:"} {
		t.Run(prefix, func(t *testing.T) {
			s, vault := panelFixture(t)
			root := "system/aion"
			if err := os.MkdirAll(filepath.Join(vault, root), 0755); err != nil {
				t.Fatal(err)
			}
			store := aion.NewStore(vault, root, testWriteAbs)
			item := &aion.BacklogItem{Kind: aion.KindTask, Text: "drop me", Owner: "BA", Status: aion.StatusOpen}
			if err := store.AddItem(item); err != nil {
				t.Fatal(err)
			}
			s.aion, s.re = store, store
			id := prefix + item.ID
			if err := s.writePlanSection("todo-plans-agent", id, "plan", "agent plan"); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/delete", nil)
			r.SetPathValue("id", item.ID)
			w := httptest.NewRecorder()
			if prefix == "aion:" {
				s.handleAionBacklogDelete(w, r)
			} else {
				s.handleReBacklogDelete(w, r)
			}
			if w.Code != 200 {
				t.Fatalf("delete: %d %s", w.Code, w.Body.String())
			}
			if s.readPlanRecord(id).Exists {
				t.Fatal("deleted backlog plan survived")
			}
		})
	}
}

func TestPortalArchiveRemovesPlan(t *testing.T) {
	s, _, team, baseID, backlog := syncFixture(t)
	vault := filepath.Dir(filepath.Dir(filepath.Dir(backlog)))
	plans, _ := panelFixtureAt(t, vault, t.TempDir())
	s.vault, s.todoPlans = plans.vault, plans.todoPlans
	id := "aion:" + baseID
	if err := s.writePlanSection("todo-plans-agent", id, "plan", "agent plan"); err != nil {
		t.Fatal(err)
	}
	snap, ok := s.AionArchiveSnapshot(baseID)
	if !ok {
		t.Fatal("missing archive snapshot")
	}
	now := time.Now()
	if err := team.Archive(teamportal.Identity{Email: "owner@aion.bio"}, snap, now); err != nil {
		t.Fatal(err)
	}
	s.SyncPortalToVault(now)
	if s.readPlanRecord(id).Exists {
		t.Fatal("archived plan survived")
	}
	// A clean backlog still retries cleanup left behind by a prior failure.
	if err := s.ensurePlanRecord(id, ""); err != nil {
		t.Fatal(err)
	}
	s.SyncPortalToVault(now.Add(time.Second))
	if s.readPlanRecord(id).Exists {
		t.Fatal("archive cleanup was not retried")
	}
}
