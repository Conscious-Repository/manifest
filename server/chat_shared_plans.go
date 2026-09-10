package server

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"manifest/artifacts"
	"manifest/mdfm"
	"manifest/record"
)

type sharedPlanSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Only task plans explicitly included in the reviewed share become editable.
// A private task link or a domain-owned upload is not an editing grant.
func (s *Server) sharedPlans(review chatShareReview) []sharedPlanSummary {
	out := []sharedPlanSummary{}
	if s.artifactReg == nil {
		return out
	}
	seen := map[string]bool{}
	for _, f := range review.Files {
		if seen[f.ArtifactID] {
			continue
		}
		a, ok := s.artifactReg.Get(f.ArtifactID)
		if !ok || a.Provenance.Source != "task-plan" || a.Provenance.Task == "" || a.Harness != "vault" || s.todoPlans == nil || a.Ref != s.todoPlans.rel(a.Provenance.Task)+"#plan" {
			continue
		}
		if _, ok = a.Revision(f.Hash); !ok {
			continue
		}
		seen[a.ID] = true
		out = append(out, sharedPlanSummary{a.ID, "Plan"})
	}
	return out
}
func (s *Server) sharedPlanAccess(ag *chatAgent, thread, id string) (artifacts.Artifact, chatShareReview, error) {
	review, err := s.sharedConversationReview(ag, thread)
	if err != nil {
		return artifacts.Artifact{}, review, err
	}
	for _, p := range s.sharedPlans(review) {
		if p.ID == id {
			a, ok := s.artifactReg.Get(id)
			if ok && a.Provenance.Source == "task-plan" && a.Harness == "vault" && s.todoPlans != nil && a.Ref == s.todoPlans.rel(a.Provenance.Task)+"#plan" {
				return a, review, nil
			}
		}
	}
	return artifacts.Artifact{}, review, errSharedConversationAccess
}
func (s *Server) AionChatPlan(w http.ResponseWriter, r *http.Request, email, name string) {
	s.sharedPlan(s.kairosAgent(), w, r, email, name)
}
func (s *Server) OodaChatPlan(w http.ResponseWriter, r *http.Request, email, name string) {
	s.sharedPlan(s.zeckAgent(), w, r, email, name)
}

func (s *Server) sharedPlan(ag *chatAgent, w http.ResponseWriter, r *http.Request, email, name string) {
	thread, id := r.PathValue("thread"), r.PathValue("plan")
	a, review, err := s.sharedPlanAccess(ag, thread, id)
	if err != nil {
		http.Error(w, errSharedConversationAccess.Error(), http.StatusForbidden)
		return
	}
	if s.vault == nil || s.todoPlans == nil || s.artifacts == nil {
		http.Error(w, "Plan workspace unavailable", 503)
		return
	}
	note := "shared-plan:" + ag.Domain + ":" + thread
	if r.Method == http.MethodPost {
		var b struct {
			Content  string `json:"content"`
			Expected string `json:"expectedRevision"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256000)
		if err = decode(r, &b); err != nil || len(b.Content) > 64000 || !utf8.ValidString(b.Content) || strings.ContainsRune(b.Content, 0) || !artifacts.ValidHash(b.Expected) || sharedPlanReservedHeading(b.Content) {
			http.Error(w, "A valid base revision and a text plan of up to 64,000 bytes are required", 400)
			return
		}
		// A guessed older private revision must not serve as a write precondition.
		if a.Head != b.Expected {
			http.Error(w, errPlanRevision.Error(), 409)
			return
		}
		err = s.saveTaskPlanVersionAs(a.Provenance.Task, b.Content, b.Expected, email, note, func() error {
			current, _, e := s.sharedPlanAccess(ag, thread, id)
			if e == nil && !sameSharedPlan(current, a) {
				return errSharedConversationAccess
			}
			return e
		})
		if err != nil {
			if errors.Is(err, errPlanRevision) {
				http.Error(w, err.Error(), 409)
			} else if errors.Is(err, errSharedConversationAccess) {
				http.Error(w, err.Error(), 403)
			} else {
				http.Error(w, "Save not confirmed. Reload the plan before retrying; your draft can be retained.", 503)
			}
			return
		}
	}
	// The vault's plan section remains authoritative. Retain owner/external edits
	// before showing its current version, without exposing other task sections.
	err = s.vault.UpdateCap("todo-plans", s.todoPlans.rel(a.Provenance.Task), func(raw []byte) ([]byte, error) {
		if current, _, e := s.sharedPlanAccess(ag, thread, id); e != nil || !sameSharedPlan(current, a) {
			return nil, errSharedConversationAccess
		}
		fm, body := mdfm.Split(string(raw))
		if raw == nil || record.Unquote(fm["todo"]) != a.Provenance.Task {
			return nil, errors.New("plan missing")
		}
		var e error
		a, e = s.snapshotTaskPlan(a.Provenance.Task, planRecordSection(body, "plan"), "Observed working plan")
		if e == nil && a.ID != id {
			e = errors.New("plan identity changed")
		}
		return raw, e
	})
	if err != nil {
		if errors.Is(err, errSharedConversationAccess) {
			http.Error(w, errSharedConversationAccess.Error(), 403)
		} else {
			http.Error(w, "Plan workspace unavailable. Reload before retrying a save.", 503)
		}
		return
	}
	// Current original plan is live, but pre-sharing private history is not a
	// blanket grant. Expose reviewed versions and this conversation's edits.
	allowed := map[string]bool{a.Head: true}
	visible := map[int]bool{a.HeadRevision().N: true}
	for _, f := range review.Files {
		if f.ArtifactID == id {
			allowed[f.Hash] = true
			if v, ok := a.Revision(f.Hash); ok {
				visible[v.N] = true
			}
		}
	}
	type version struct {
		N     int       `json:"n"`
		Hash  string    `json:"hash"`
		Actor string    `json:"actor"`
		At    time.Time `json:"at"`
	}
	versions := []version{}
	for _, v := range a.Revisions {
		if visible[v.N] || v.Note == note {
			allowed[v.Hash] = true
			versions = append(versions, version{v.N, v.Hash, v.Actor, v.At})
		}
	}
	hash := orStr(r.URL.Query().Get("revision"), a.Head)
	if !allowed[hash] {
		http.Error(w, errSharedConversationAccess.Error(), 403)
		return
	}
	content, e := s.artifactReg.Content(hash)
	if e != nil || artifacts.Hash(content) != hash {
		http.Error(w, "Plan content unavailable", 503)
		return
	}
	if current, _, accessErr := s.sharedPlanAccess(ag, thread, id); accessErr != nil || !sameSharedPlan(current, a) {
		http.Error(w, errSharedConversationAccess.Error(), 403)
		return
	}
	// Reuse the attachment grant and exact-file send paths, including receipts.
	file, e := s.artifacts.Save(bytes.NewReader(content), "plan-"+id+".md", "text/markdown")
	if e != nil || file.Hash != hash {
		http.Error(w, "Plan file access unavailable", 503)
		return
	}
	if e = s.artifacts.Add(ag.Domain, artifacts.Entry{Ref: file, Thread: thread, ByEmail: "shared-plan", By: "Shared plan", At: time.Now()}); e != nil {
		http.Error(w, "Plan file access unavailable", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"id": id, "title": "Plan", "head": a.Head, "revision": hash, "content": string(content), "versions": versions, "file": file})
}

// Reserved record boundaries cannot be introduced through a plan-only editor.
func sharedPlanReservedHeading(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.ToLower(strings.TrimRight(line, " \t\r"))
		if line == "## description" || line == "## plan" {
			return true
		}
	}
	return false
}
func sameSharedPlan(a, b artifacts.Artifact) bool {
	return a.ID == b.ID && a.Harness == b.Harness && a.Ref == b.Ref && a.Provenance.Task == b.Provenance.Task && a.Provenance.Source == b.Provenance.Source
}
