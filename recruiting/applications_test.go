package recruiting

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestApplicationLifecycleAcrossRoles(t *testing.T) {
	h := newAshbyHarness(t)
	role := h.store.LoadRole("mechanical-engineer")
	role.Set("ashby_job_id", "job_mech")
	if err := h.store.SaveRole("mechanical-engineer", role); err != nil {
		t.Fatal(err)
	}
	h.fake.jobs = []map[string]any{{"id": "job_mech", "title": "Mechanical Engineer", "status": "Closed"}}
	h.fake.candidates["person_multi"] = wireCandidate("person_multi", "Multi Person", "multi@example.test", "")
	wire := func(id, job, status string) map[string]any {
		return map[string]any{"id": id, "candidate": map[string]any{"id": "person_multi"}, "job": map[string]any{"id": job, "title": job}, "status": status, "currentInterviewStage": map[string]any{"id": "st_1", "title": status, "interviewPlanId": "plan_1"}}
	}
	h.fake.apps["app_one"] = wire("app_one", "job_mech", "Hired")
	h.fake.apps["app_two"] = wire("app_two", "job_mri", "Active")
	for i := 0; i < 2; i++ {
		if _, err := h.sync.SyncBack(context.Background(), true, testNow.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	d := h.store.LoadCandidate("multi-person")
	apps := d.Applications()
	if len(apps) != 2 {
		t.Fatalf("expected two applications on one person, got %+v; raw %s", apps, d.Get("ashby_applications"))
	}
	if !IsActive(d.View("multi-person", nil)) {
		t.Fatal("active second application hidden by hire")
	}
	if got := h.store.LoadRole("mechanical-engineer").Get("status"); got != "closed" {
		t.Fatalf("job lifecycle %q", got)
	}
	// Only the explicitly selected application changes; another person's ID is rejected.
	if _, err := h.sync.ChangeStage(context.Background(), d.Get("id"), "st_phone", "", "owner", testNow, "unrelated"); err == nil {
		t.Fatal("unrelated application accepted")
	}
	if _, err := h.sync.ChangeStage(context.Background(), d.Get("id"), "st_archived", "reason_1", "owner", testNow, "app_two"); err != nil {
		t.Fatal(err)
	}
	d = h.store.LoadCandidate("multi-person")
	if IsActive(d.View("multi-person", nil)) {
		t.Fatal("hired + archived person still active")
	}
	for _, a := range d.Applications() {
		if a.ID == "app_one" && a.Status != "Hired" {
			t.Fatal("other application changed")
		}
	}
	// Reopen and transfer: identity and application count stay stable.
	h.fake.apps["app_two"] = wire("app_two", "job_mech", "Active")
	if _, err := h.sync.SyncBack(context.Background(), true, testNow); err != nil {
		t.Fatal(err)
	}
	d = h.store.LoadCandidate("multi-person")
	if !IsActive(d.View("multi-person", nil)) {
		t.Fatal("reopened application remains hidden")
	}
	for _, a := range d.Applications() {
		if a.ID == "app_two" && a.Role != "role/mechanical-engineer" {
			t.Fatal("transfer not reflected")
		}
	}
	if strings.Count(SerializeCandidate(d), "person_multi") < 1 {
		t.Fatal("identity lost")
	}
}
