package recruiting

import (
	"context"
	"manifest/recruiting/sources"
	"strings"
	"testing"
	"time"
)

const summaryReply = `{"competencies":{"text":"Builds MRI reconstruction software","evidence":[0]},"relevance":{"text":"This work may support AION MRI instrumentation","evidence":[0]}}`

func summaryFixture(t *testing.T) (*RunStore, Run) {
	t.Helper()
	d := sources.CandidateDraft{Name: "Ada Example", Role: "role/mri-engineer", Brief: &sources.CandidateBrief{Model: "fixture", Items: []sources.BriefItem{{Section: "work", Text: "MRI software"}}, Evidence: []sources.Evidence{{URLOrFile: "https://example.org/ada", Snippet: "Ada Example builds MRI reconstruction software."}}}}
	rs, _, _ := testRunStore(t, &fakeAdapter{id: "fake", drafts: []sources.CandidateDraft{d}})
	run, err := rs.Execute(context.Background(), RunRequest{Source: "fake", Query: "Ada"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	rs.mu.Lock()
	run, err = rs.load(run.ID)
	if err == nil {
		run.Drafts[0].Enhancement = &LookupResult{Brief: true}
		err = rs.writeRun(run, nil)
	}
	rs.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return rs, run
}
func TestSummaryFixedFormatAndCitations(t *testing.T) {
	p := SummaryPacket{Role: Role{Title: "MRI Engineer"}, Evidence: []sources.Evidence{{URLOrFile: "https://example.org/ada", Snippet: "MRI reconstruction software"}}}
	got, err := summaryText(summaryReply, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Competencies: Builds MRI", "AION relevance:", "Current location not established; no mutual connections recorded."} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
	if strings.Count(got, ".") != 3 {
		t.Fatalf("not three sentences: %s", got)
	}
	p.Location = "Boston"
	p.Connections = []PathClaim{{Path: "Owner → Pat Example → Ada Example", Inferred: true}}
	got, err = summaryText(summaryReply, p)
	if err != nil || !strings.Contains(got, "current location unverified") || !strings.Contains(got, "possible introduction path: Owner → Pat Example → Ada Example (inferred)") {
		t.Fatalf("%s %v", got, err)
	}
	for _, bad := range []string{`{}`, strings.ReplaceAll(summaryReply, `[0]`, `[9]`), strings.Replace(summaryReply, "Builds MRI reconstruction software", "First sentence. Second sentence", 1)} {
		if _, err := summaryText(bad, p); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
func TestSummaryCachesStableInputsAndPersists(t *testing.T) {
	rs, run := summaryFixture(t)
	calls := 0
	complete := func(_ context.Context, p SummaryPacket) (string, string, error) {
		calls++
		if p.Name != "Ada Example" {
			t.Fatal("missing context")
		}
		return summaryReply, "kairos-model", nil
	}
	first, err := rs.Summarize(context.Background(), run.ID, "d1", complete, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if first.Drafts[0].Summary == nil || first.Drafts[0].Summary.Agent != "kairos-private" {
		t.Fatal("missing attribution")
	}
	rs.mu.Lock()
	disk, _ := rs.load(run.ID)
	disk.Drafts[0].Draft.Brief.GeneratedAt = testNow.Add(time.Hour)
	disk.Drafts[0].Draft.Brief.Evidence[0].RetrievedAt = testNow.Add(time.Hour)
	err = rs.writeRun(disk, nil)
	rs.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	second, err := rs.Summarize(context.Background(), run.ID, "d1", complete, testNow.Add(time.Hour))
	if err != nil || calls != 1 || !second.Drafts[0].Summary.GeneratedAt.Equal(testNow) {
		t.Fatalf("cache miss: %d %v", calls, err)
	}
	got, err := rs.Get(run.ID)
	if err != nil || got.Drafts[0].Summary.Text != first.Drafts[0].Summary.Text {
		t.Fatal("summary lost")
	}
}
func TestSummaryFailureKeepsEnhancedEvidenceAndSkipsIncompleteEnhance(t *testing.T) {
	rs, run := summaryFixture(t)
	result, err := rs.Summarize(context.Background(), run.ID, "d1", nil, testNow)
	if err != nil || result.Drafts[0].SummaryError == "" || result.Drafts[0].Draft.Brief == nil {
		t.Fatalf("%+v %v", result, err)
	}
	rs.mu.Lock()
	disk, _ := rs.load(run.ID)
	disk.Drafts[0].Enhancement.Brief = false
	err = rs.writeRun(disk, nil)
	rs.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	_, err = rs.Summarize(context.Background(), run.ID, "d1", func(context.Context, SummaryPacket) (string, string, error) {
		t.Error("summarized failed enhancement")
		return "", "", nil
	}, testNow)
	if err != nil {
		t.Fatal(err)
	}
}
func TestSummaryDoesNotOverwriteAConcurrentDecision(t *testing.T) {
	rs, run := summaryFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := rs.Summarize(context.Background(), run.ID, "d1", func(context.Context, SummaryPacket) (string, string, error) {
			close(entered)
			<-release
			return summaryReply, "fixture", nil
		}, testNow)
		done <- err
	}()
	<-entered
	_, err := rs.Reject(run.ID, "d1", "owner decision", testNow)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil || !strings.Contains(err.Error(), "changed during Kairos") {
		t.Fatalf("%v", err)
	}
	got, _ := rs.Get(run.ID)
	if got.Drafts[0].Status != DraftRejected || got.Drafts[0].Summary != nil {
		t.Fatal("overwrote decision")
	}
}
