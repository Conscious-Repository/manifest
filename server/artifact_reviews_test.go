package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"manifest/artifacts"
	"manifest/record"
	"manifest/vaultwriter"
)

func TestArtifactReviewExactVersionReplayAndConflict(t *testing.T) {
	s, vault, _ := artifactFixture(t)
	s.UseVault(vaultwriter.New(vault).Grant(vaultwriter.Capability{Name: "artifact-reviews", Zone: record.ZoneSystem, Pattern: "system/workbench/reviews/**", Actor: vaultwriter.ActorUserAction}))
	s.UseArtifactReviews("system/workbench/reviews")
	first, err := s.artifactReg.Put(artifacts.Put{Ref: "report.md", Content: []byte("first\nsecond\nthird"), Provenance: artifacts.Provenance{Session: "conversation-one", Run: "run-one"}})
	if err != nil {
		t.Fatal(err)
	}
	id, hash := first.Artifact.ID, first.Artifact.Head
	call := func(method, rev string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(method, "/api/artifacts/reviews?id="+id+"&revision="+rev, bytes.NewReader(b)))
		return w
	}
	read := func(rev string) artifactReviews {
		w := call("GET", rev, nil)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var out artifactReviews
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	base := read(hash)
	request := map[string]any{"request_id": "decision-123", "record_version": base.RecordVersion, "state": "accepted", "note": "Reviewed output"}
	if w := call("POST", hash, request); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	accepted := read(hash)
	if accepted.State != "accepted" || len(accepted.Entries) != 1 || accepted.Entries[0].Run != "run-one" {
		t.Fatal(accepted)
	}
	if w := call("POST", hash, request); w.Code != 200 {
		t.Fatal("lost response retry rejected", w.Code)
	}
	if len(read(hash).Entries) != 1 {
		t.Fatal("retry appended duplicate")
	}
	request["note"] = "Changed replay"
	if w := call("POST", hash, request); w.Code != 409 {
		t.Fatal("changed action reused request ID", w.Code)
	}
	// A newer artifact revision is independently unreviewed; old acceptance stays.
	next, err := s.artifactReg.Put(artifacts.Put{ID: id, Ref: "report.md", Content: []byte("new content")})
	if err != nil {
		t.Fatal(err)
	}
	if read(next.Artifact.Head).State != "not_requested" || read(hash).State != "accepted" {
		t.Fatal("acceptance leaked to next version")
	}
	request = map[string]any{"request_id": "decision-456", "record_version": accepted.RecordVersion, "state": "changes_requested", "note": "Correct the second line", "start": 2, "end": 2}
	// Hand edits are preserved and invalidate the full-record revision.
	file := filepath.Join(vault, "system/workbench/reviews", id+".md")
	raw, _ := os.ReadFile(file)
	raw = append(raw, []byte("\nOwner annotation.\n")...)
	_ = os.WriteFile(file, raw, 0600)
	if w := call("POST", hash, request); w.Code != 409 {
		t.Fatal("stale source overwritten", w.Code)
	}
	request["record_version"] = read(hash).RecordVersion
	if w := call("POST", hash, request); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	revised := read(hash)
	if revised.State != "changes_requested" || len(revised.Entries) != 2 || revised.Entries[1].Start != 2 {
		t.Fatal(revised)
	}
	raw, _ = os.ReadFile(file)
	if !bytes.Contains(raw, []byte("Owner annotation.")) {
		t.Fatal("owner annotation lost")
	}
	request["request_id"] = "decision-789"
	request["record_version"] = revised.RecordVersion
	request["end"] = 99
	if w := call("POST", hash, request); w.Code != 400 {
		t.Fatal("invalid source range accepted", w.Code)
	}
	// The sidebar projects only the current head; older acceptance cannot hide
	// an unreviewed revision, and linked task scopes are distinct.
	status := httptest.NewRecorder()
	s.Handler().ServeHTTP(status, httptest.NewRequest("GET", "/api/chat/review-status", nil))
	if status.Code != 200 || !bytes.Contains(status.Body.Bytes(), []byte(`"unreviewed":1`)) || !bytes.Contains(status.Body.Bytes(), []byte(`"accepted":0`)) {
		t.Fatal(status.Code, status.Body.String())
	}
	// Review cannot change artifact content, revision, or original immutable event.
	got, _ := s.artifactReg.Get(id)
	if got.Head != next.Artifact.Head || len(got.Revisions) != 2 {
		t.Fatal(got)
	}
	if read(hash).Entries[0].Note != "Reviewed output" {
		t.Fatal("old decision rewritten")
	}
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, "/api/artifacts/reviews?id="+id, nil))
		if w.Code == 200 {
			t.Fatal("portal exposed owner review")
		}
	}
}

func TestDiffHunkReviewAnchorsHistoricalSnapshot(t *testing.T) {
	s, vault, _ := artifactFixture(t)
	s.UseVault(vaultwriter.New(vault).Grant(vaultwriter.Capability{Name: "artifact-reviews", Zone: record.ZoneSystem, Pattern: "system/workbench/reviews/**", Actor: vaultwriter.ActorUserAction}))
	s.UseArtifactReviews("system/workbench/reviews")
	original := []byte("diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -20 +20 @@\n-old\n+new\n")
	first, err := s.artifactReg.Put(artifacts.Put{Ref: "changes.diff", Content: original})
	if err != nil {
		t.Fatal(err)
	}
	id, hash := first.Artifact.ID, first.Artifact.Head
	call := func(method, revision string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(method, "/api/artifacts/reviews?id="+id+"&revision="+revision, bytes.NewReader(raw)))
		return w
	}
	var base artifactReviews
	if err = json.Unmarshal(call("GET", hash, nil).Body.Bytes(), &base); err != nil {
		t.Fatal(err)
	}
	// The working snapshot advances while the owner reviews the older hunk.
	latest, err := s.artifactReg.Put(artifacts.Put{ID: id, Content: []byte("different shorter snapshot\n")})
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"request_id": "hunk-review-001", "record_version": base.RecordVersion, "state": "changes_requested", "note": "File: file\nHunk: @@ -20 +20 @@\nExplain this change.", "start": 4, "end": 6}
	w := call("POST", hash, request)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var reviewed artifactReviews
	if err = json.Unmarshal(w.Body.Bytes(), &reviewed); err != nil {
		t.Fatal(err)
	}
	if len(reviewed.Entries) != 1 || reviewed.Entries[0].Revision != hash || reviewed.Entries[0].Start != 4 || reviewed.Entries[0].End != 6 {
		t.Fatal(reviewed)
	}
	content, err := s.artifactReg.Content(reviewed.Entries[0].Revision)
	if err != nil || !bytes.Equal(content, original) {
		t.Fatal("historical anchor lost", err)
	}
	current, _ := s.artifactReg.Get(id)
	if current.Head != latest.Artifact.Head {
		t.Fatal("review changed current snapshot")
	}
	if again := call("POST", hash, request); again.Code != 200 {
		t.Fatal("lost acknowledgment did not reconcile", again.Code)
	}
	request["start"] = 5
	if changed := call("POST", hash, request); changed.Code != 409 {
		t.Fatal("request identity changed anchored range", changed.Code)
	}
	request["request_id"] = "hunk-review-002"
	request["record_version"] = reviewed.RecordVersion
	if changed := call("POST", latest.Artifact.Head, request); changed.Code != 400 {
		t.Fatal("old range accepted against shorter new snapshot", changed.Code)
	}
}
