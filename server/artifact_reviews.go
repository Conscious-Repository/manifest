package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"manifest/artifacts"
	"manifest/record"
	"manifest/vaultwriter"
)

const artifactReviewBlock = "manifest-artifact-review"

var reviewID = regexp.MustCompile(`^[a-zA-Z0-9-]{8,80}$`)
var reviewArtifactID = regexp.MustCompile(`^[a-f0-9]{16}$`)
var errReviewConflict = errors.New("review changed; reload before recording another decision")

type artifactReview struct {
	ID       string `json:"id"`
	Artifact string `json:"artifact"`
	Revision string `json:"revision"`
	State    string `json:"state"`
	Note     string `json:"note,omitempty"`
	Start    int    `json:"start,omitempty"`
	End      int    `json:"end,omitempty"`
	// RangeHash fingerprints the exact selected lines of Revision, so a
	// range carried elsewhere can be checked against the bytes it named.
	RangeHash string `json:"range_hash,omitempty"`
	Path      string `json:"path"`
	Thread    string `json:"thread,omitempty"`
	Run       string `json:"run,omitempty"`
	Task      string `json:"task,omitempty"`
	Actor     string `json:"actor"`
	At        string `json:"at"`
}
type artifactReviews struct {
	RecordVersion string           `json:"record_version"`
	Revision      string           `json:"revision"`
	State         string           `json:"state"`
	Entries       []artifactReview `json:"entries"`
	// Read-time projection, never persisted: the selected and latest version
	// numbers, and where each recorded range stands in the latest version.
	Version     int                            `json:"version,omitempty"`
	Head        string                         `json:"head,omitempty"`
	HeadVersion int                            `json:"head_version,omitempty"`
	Anchors     map[string]artifactRangeAnchor `json:"anchors,omitempty"`
}

// artifactRangeAnchor reports whether a recorded range's exact lines still
// read the same in the latest version. It is evidence for the reviewer, not a
// decision: the recorded range stays bound to its own revision.
type artifactRangeAnchor struct {
	State string `json:"state"` // unchanged | moved | changed | repeated | unavailable
	Start int    `json:"start,omitempty"`
	End   int    `json:"end,omitempty"`
}

// reviewLines is the exact line slice a review range names, or false when
// the revision is not text or the range exceeds it.
func reviewLines(content []byte, start, end int) ([]string, bool) {
	if !utf8.Valid(content) || bytes.ContainsRune(content, 0) {
		return nil, false
	}
	lines := strings.Split(string(content), "\n")
	if start < 1 || end < start || end > len(lines) {
		return nil, false
	}
	return lines[start-1 : end], true
}

func reviewRangeHash(lines []string) string {
	return artifacts.Hash([]byte(strings.Join(lines, "\n")))
}

// anchorRange locates want in the latest version: same place, one other
// place, several places, or nowhere.
func anchorRange(head []string, start int, want []string) artifactRangeAnchor {
	match := func(at int) bool {
		if at < 0 || at+len(want) > len(head) {
			return false
		}
		for i, line := range want {
			if head[at+i] != line {
				return false
			}
		}
		return true
	}
	if match(start - 1) {
		return artifactRangeAnchor{State: "unchanged", Start: start, End: start + len(want) - 1}
	}
	found := -1
	for at := 0; at+len(want) <= len(head); at++ {
		if !match(at) {
			continue
		}
		if found >= 0 {
			return artifactRangeAnchor{State: "repeated"}
		}
		found = at
	}
	if found < 0 {
		return artifactRangeAnchor{State: "changed"}
	}
	return artifactRangeAnchor{State: "moved", Start: found + 1, End: found + len(want)}
}

// project adds version numbers and latest-version anchors to a review read.
func (s *Server) projectArtifactReviews(a artifacts.Artifact, out *artifactReviews) {
	if v, ok := a.Revision(out.Revision); ok {
		out.Version = v.N
	}
	out.Head, out.HeadVersion = a.Head, a.HeadRevision().N
	var head []string
	headText := false
	for _, e := range out.Entries {
		if e.Start == 0 || e.Revision == a.Head {
			continue
		}
		if out.Anchors == nil {
			out.Anchors = map[string]artifactRangeAnchor{}
			if raw, err := s.artifactReg.Content(a.Head); err == nil && len(raw) <= 1<<20 {
				head, headText = reviewLines(raw, 1, bytes.Count(raw, []byte("\n"))+1)
			}
		}
		raw, err := s.artifactReg.Content(e.Revision)
		want, ok := reviewLines(raw, e.Start, e.End)
		if err != nil || !ok || !headText || len(raw) > 1<<20 {
			out.Anchors[e.ID] = artifactRangeAnchor{State: "unavailable"}
			continue
		}
		out.Anchors[e.ID] = anchorRange(head, e.Start, want)
	}
}

func (s *Server) UseArtifactReviews(root string) { s.artifactReviewsRoot = path.Clean(root) }
func parseArtifactReviews(raw []byte, id, revision string) (artifactReviews, error) {
	out := artifactReviews{RecordVersion: vaultwriter.Revision(raw), Revision: revision, State: "not_requested", Entries: []artifactReview{}}
	blocks, err := record.JSONBlocks(string(raw), artifactReviewBlock)
	if err != nil {
		return out, err
	}
	seen := map[string]bool{}
	for _, block := range blocks {
		var e artifactReview
		if json.Unmarshal(block, &e) != nil || e.Artifact != id || !reviewID.MatchString(e.ID) || seen[e.ID] || !validReviewState(e.State) {
			return out, errors.New("invalid review history; original retained")
		}
		seen[e.ID] = true
		out.Entries = append(out.Entries, e)
		if e.Revision == revision && e.State != "comment" {
			out.State = e.State
		}
	}
	return out, nil
}
func validReviewState(state string) bool {
	return state == "ready_for_review" || state == "accepted" || state == "changes_requested" || state == "comment"
}

// Owner-only cockpit route. Records a version-specific decision; it cannot
// dispatch an agent, approve an external action, or close a linked task.
func (s *Server) handleArtifactReviews(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if s.artifactReg == nil || s.vault == nil || s.artifactReviewsRoot == "" {
		http.Error(w, "artifact reviews unavailable", 503)
		return
	}
	id := r.URL.Query().Get("id")
	if !reviewArtifactID.MatchString(id) {
		http.Error(w, "invalid artifact", 400)
		return
	}
	a, ok := s.artifactReg.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	revision := r.URL.Query().Get("revision")
	if revision == "" && r.Method != http.MethodGet {
		// A decision is about the bytes the reviewer saw, never whatever
		// happens to be the head when the request lands.
		http.Error(w, "review revision required", 428)
		return
	}
	if revision == "" {
		revision = a.Head
	}
	found := false
	for _, v := range a.Revisions {
		if v.Hash == revision {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "artifact revision not found", 404)
		return
	}
	rel := path.Join(s.artifactReviewsRoot, id+".md")
	var out artifactReviews
	var err error
	if r.Method == http.MethodGet {
		var raw []byte
		raw, err = s.vault.ReadVaultFile(rel)
		if errors.Is(err, os.ErrNotExist) {
			raw = nil
			err = nil
		}
		if err == nil {
			out, err = parseArtifactReviews(raw, id, revision)
		}
	} else {
		var b struct {
			RequestID string `json:"request_id"`
			Expected  string `json:"record_version"`
			State     string `json:"state"`
			Note      string `json:"note"`
			Start     int    `json:"start"`
			End       int    `json:"end"`
			RangeHash string `json:"range_hash"`
		}
		if decode(r, &b) != nil || !reviewID.MatchString(b.RequestID) || !validReviewState(b.State) || len(b.Note) > 16000 || ((b.State == "changes_requested" || b.State == "comment") && strings.TrimSpace(b.Note) == "") {
			http.Error(w, "review requires a valid decision and notes for changes or comments", 400)
			return
		}
		if b.Expected == "" {
			http.Error(w, "review version required", 428)
			return
		}
		if b.Start < 0 || b.End < b.Start || (b.Start == 0 && b.End != 0) {
			http.Error(w, "invalid line range", 400)
			return
		}
		rangeHash := ""
		if b.Start > 0 {
			content, e := s.artifactReg.Content(revision)
			if e != nil {
				httpError(w, e)
				return
			}
			if !utf8.Valid(content) || bytes.ContainsRune(content, 0) {
				http.Error(w, "line ranges require a text artifact", 400)
				return
			}
			lines, ok := reviewLines(content, b.Start, b.End)
			if !ok {
				http.Error(w, "line range exceeds selected version", 400)
				return
			}
			rangeHash = reviewRangeHash(lines)
			// The caller names the lines it displayed; different bytes at
			// those numbers mean the selection was made against other text.
			if b.RangeHash != "" && b.RangeHash != rangeHash {
				http.Error(w, "selected lines do not match this version; reselect them", 412)
				return
			}
		} else if b.RangeHash != "" {
			http.Error(w, "invalid line range", 400)
			return
		}
		err = s.vault.UpdateCap("artifact-reviews", rel, func(raw []byte) ([]byte, error) {
			var e error
			out, e = parseArtifactReviews(raw, id, revision)
			if e != nil {
				return nil, e
			}
			for _, old := range out.Entries {
				if old.ID == b.RequestID {
					if old.Revision != revision || old.State != b.State || old.Note != b.Note || old.Start != b.Start || old.End != b.End {
						return nil, errReviewConflict
					}
					return raw, nil
				}
			}
			if out.RecordVersion != b.Expected {
				return nil, errReviewConflict
			}
			if len(raw) > 2<<20 {
				return nil, errors.New("review history exceeds editing limit")
			}
			entry := artifactReview{ID: b.RequestID, Artifact: id, Revision: revision, State: b.State, Note: b.Note, Start: b.Start, End: b.End, RangeHash: rangeHash, Path: a.Ref, Thread: a.Provenance.Session, Run: a.Provenance.Run, Task: a.Provenance.Task, Actor: "owner", At: time.Now().UTC().Format(time.RFC3339Nano)}
			doc := string(raw)
			if len(raw) == 0 {
				doc = fmt.Sprintf("# Artifact review\n\nArtifact: %s\n", id)
			}
			doc, e = record.AppendJSONBlock(doc, artifactReviewBlock, entry)
			if e != nil {
				return nil, e
			}
			out, e = parseArtifactReviews([]byte(doc), id, revision)
			return []byte(doc), e
		})
	}
	if a, ok = s.artifactReg.Get(id); ok {
		s.projectArtifactReviews(a, &out)
	}
	if errors.Is(err, errReviewConflict) {
		w.WriteHeader(409)
		writeJSON(w, out)
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, out)
}
