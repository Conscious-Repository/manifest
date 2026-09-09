package server

import (
	"strings"
	"testing"
)

func TestAionRolesProjectionUsesPublicOpenings(t *testing.T) {
	s, _ := leakVault(t)
	r := s.recruiting.LoadRole("mri-engineer")
	r.Set("status", "open")
	r.Set("published", "true")
	r.Set("title", "Live MRI title")
	r.Set("job_url", "https://jobs.ashbyhq.com/aion/job-one")
	if err := s.recruiting.SaveRole("mri-engineer", r); err != nil {
		t.Fatal(err)
	}
	got := string(s.aionRecruitingHiringMD())
	if !strings.Contains(got, "Live MRI title") || !strings.Contains(got, "https://jobs.ashbyhq.com/aion/job-one") {
		t.Fatal(got)
	}
	for _, canary := range recruitingCanaries {
		if strings.Contains(got, canary) {
			t.Fatal("private candidate data leaked")
		}
	}
	s.aionDataDir = t.TempDir()
	l := newAionLive(s)
	before := l.sourceRevision()
	r.Set("status", "closed")
	if err := s.recruiting.SaveRole("mri-engineer", r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(s.aionRecruitingHiringMD()), "Live MRI title") {
		t.Fatal("closed opening published")
	}
	if l.sourceRevision() == before {
		t.Fatal("role change did not invalidate live projection")
	}
	if err := l.refresh(true); err != nil {
		t.Fatal(err)
	}
}
