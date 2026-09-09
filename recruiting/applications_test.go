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

func TestAshbyCatchUpAdvancesAndDoesNotRepollFreshState(t *testing.T) {
	h := newAshbyHarness(t)
	h.fake.candidates["catchup"] = wireCandidate("catchup", "Catchup Applicant", "catchup@example.test", "")
	h.fake.apps["catchup_app"] = map[string]any{"id": "catchup_app", "candidate": map[string]any{"id": "catchup"}, "job": map[string]any{"id": "job_mri"}, "status": "Active", "currentInterviewStage": map[string]any{"id": "screen", "title": "Initial Screen", "interviewPlanId": "plan_1"}}
	if err := h.sync.SyncIfStale(context.Background(), time.Minute, testNow); err != nil {
		t.Fatal(err)
	}
	h.fake.mu.Lock()
	calls := len(h.fake.calls)
	h.fake.mu.Unlock()
	if err := h.sync.SyncIfStale(context.Background(), time.Minute, testNow.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	h.fake.mu.Lock()
	after := len(h.fake.calls)
	h.fake.mu.Unlock()
	if after != calls {
		t.Fatal("fresh state repolled")
	}
	h.fake.apps["catchup_app"]["currentInterviewStage"] = map[string]any{"id": "round2", "title": "Second Round", "interviewPlanId": "plan_1"}
	if err := h.sync.SyncIfStale(context.Background(), time.Minute, testNow.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	d := h.store.LoadCandidate("catchup-applicant")
	if d.Get("ashby_stage") != "Second Round" || len(d.Applications()) != 1 || d.Applications()[0].Stage != "Second Round" {
		t.Fatalf("advancement not mirrored: %+v", d.Applications())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.sync.SyncIfStale(ctx, time.Minute, testNow.Add(4*time.Minute)); err == nil {
		t.Fatal("canceled refresh succeeded")
	}
	if got := h.sync.State().LastSync; got != testNow.Add(2*time.Minute).UTC().Format(time.RFC3339) {
		t.Fatal("failed refresh advanced checkpoint", got)
	}
}
