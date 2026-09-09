package recruiting

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Application is a role consideration, not a duplicate person. Only safe
// lifecycle metadata is mirrored here; resumes and contact data stay separate.
type Application struct {
	Resume    ResumeRef `json:"resume,omitempty"`
	ID        string    `json:"id"`
	JobID     string    `json:"jobId"`
	Role      string    `json:"role"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Stage     string    `json:"stage"`
	StageID   string    `json:"stageId"`
	UpdatedAt string    `json:"updatedAt,omitempty"`
}

func (d *CandidateDoc) Applications() []Application {
	var apps []Application
	_ = json.Unmarshal([]byte(d.Get("ashby_applications")), &apps)
	return apps
}
func (d *CandidateDoc) HasApplication(id string) bool {
	if id != "" && d.Get("ashby_application_id") == id {
		return true
	}
	for _, app := range d.Applications() {
		if app.ID == id {
			return true
		}
	}
	return false
}
func applicationActive(status, stage string) bool {
	if status == "" {
		status = stage
	}
	return !strings.EqualFold(status, "Hired") && !strings.EqualFold(status, "Archived")
}
func (a *AshbySync) mirrorApplication(slug string, app AshbyApplication, now time.Time) error {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	d := a.store.LoadCandidate(slug)
	if d.Get("ashby_candidate_id") != app.CandidateID {
		return errf("application candidate mismatch")
	}
	role := ""
	for _, rs := range a.store.RoleSlugs() {
		r := a.store.LoadRole(rs)
		if r.Get("ashby_job_id") == app.JobID {
			role = r.Get("id")
			if role == "" {
				role = "role/" + rs
			}
			break
		}
	}
	apps := d.Applications()
	item := Application{ID: app.ID, JobID: app.JobID, Role: role, Title: app.JobTitle, Status: app.Status, Stage: ashbyStageOf(app), StageID: app.StageID, UpdatedAt: app.UpdatedAt}
	found := false
	for i := range apps {
		if apps[i].ID == app.ID {
			item.Resume = apps[i].Resume
			apps[i] = item
			found = true
			break
		}
	}
	if !found {
		apps = append(apps, item)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
	data, err := json.Marshal(apps)
	if err != nil {
		return err
	}
	d.Set("ashby_applications", string(data))
	if d.Get("ashby_application_id") == app.ID {
		d.Set("ashby_stage", ashbyStageOf(app))
		d.Set("ashby_status", app.Status)
		// A transfer updates the legacy primary role too. Other application roles
		// are retained independently and never overwrite this compatibility field.
		if role != "" {
			d.Set("role", role)
		}
	}
	d.Set("ashby_synced", now.UTC().Format("2006-01-02"))
	return a.store.SaveCandidate(slug, d)
}

func (a *AshbySync) ApplicationStages(ctx context.Context, candidate, appID string) ([]AshbyInterviewStage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, d, err := a.store.resolve(candidate)
	if err != nil {
		return nil, err
	}
	if !d.HasApplication(appID) {
		return nil, errf("application does not belong to this candidate")
	}
	app, err := a.client.GetApplication(ctx, appID)
	if err != nil {
		return nil, err
	}
	return a.client.ListInterviewStages(ctx, app.InterviewPlanID)
}

func (a *AshbySync) RecordApplicationResume(candidate, appID, name, hash string, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	slug, d, err := a.store.resolve(candidate)
	if err != nil {
		return err
	}
	if !d.HasApplication(appID) {
		return errf("application does not belong to this candidate")
	}
	apps := d.Applications()
	for i := range apps {
		if apps[i].ID == appID {
			apps[i].Resume = ResumeRef{Hash: hash, Name: name}
		}
	}
	b, err := json.Marshal(apps)
	if err != nil {
		return err
	}
	d.Set("ashby_applications", string(b))
	if appID == d.Get("ashby_application_id") {
		d.Set("ashby_resume", hash)
		d.Set("ashby_resume_name", name)
	}
	return a.store.SaveCandidate(slug, d)
}
