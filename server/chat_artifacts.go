package server

// Conversation workspaces retain immutable bytes in the existing artifact
// registry. A task plan's editable section remains authoritative in the vault.
import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"manifest/agentchat"
	"manifest/artifacts"
	"manifest/mdfm"
	"manifest/record"
)

var errPlanRevision = errors.New("the plan changed since you opened it; review the latest version before saving")

func (s *Server) snapshotTaskPlan(id, text, note string) (artifacts.Artifact, error) {
	return s.snapshotTaskPlanAs(id, text, note, "owner")
}
func (s *Server) snapshotTaskPlanAs(id, text, note, actor string) (artifacts.Artifact, error) {
	if s.artifactReg == nil {
		return artifacts.Artifact{}, errors.New("artifact versions unavailable")
	}
	// Keep an empty plan representable without changing the registry's nonempty contract.
	content := strings.TrimSpace(text) + "\n"
	res, err := s.artifactReg.Put(artifacts.Put{Kind: artifacts.KindPlan, Title: "Plan", Harness: "vault", Ref: s.todoPlans.rel(id) + "#plan", Content: []byte(content), Actor: actor, Note: note, Provenance: artifacts.Provenance{Source: "task-plan", Task: id}})
	if err == nil {
		s.artifactEvent(res, actor)
	}
	return res.Artifact, err
}

func (s *Server) handleTaskPlanWorkspace(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" || s.todoPlans == nil || s.vault == nil || s.artifactReg == nil {
		http.Error(w, "plan workspace unavailable", http.StatusNotFound)
		return
	}
	var a artifacts.Artifact
	// Observe and retain the plan under the writer's file lock; this does not edit it.
	err := s.vault.UpdateCap("todo-plans", s.todoPlans.rel(id), func(raw []byte) ([]byte, error) {
		fm, body := mdfm.Split(string(raw))
		if raw == nil || record.Unquote(fm["todo"]) != id {
			return nil, errors.New("plan not found")
		}
		var err error
		a, err = s.snapshotTaskPlan(id, planRecordSection(body, "plan"), "Observed working plan")
		return raw, err
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	v := s.artifactView(a, nil)
	b, err := s.artifactReg.Content(a.Head)
	if err != nil {
		httpError(w, err)
		return
	}
	v.Content = string(b)
	writeJSON(w, v)
}

// saveTaskPlanVersion checks against the live section, retaining the old bytes
// BEFORE changing the file. Restoring is an ordinary new version, never a rewind.
func (s *Server) saveTaskPlanVersion(id, text, expected string) error {
	return s.saveTaskPlanVersionAs(id, text, expected, "owner", "Saved plan revision", nil)
}
func (s *Server) saveTaskPlanVersionAs(id, text, expected, actor, note string, authorize func() error) error {
	if expected == "" {
		return errBadRequest("expected plan revision is required")
	}
	if s.todoPlans == nil || s.vault == nil || s.artifactReg == nil {
		return errors.New("plan versions unavailable")
	}
	err := s.vault.ReplaceSectionCapChecked("todo-plans", s.todoPlans.rel(id), "plan", text, func(raw []byte) error {
		if authorize != nil {
			if err := authorize(); err != nil {
				return err
			}
		}
		fm, body := mdfm.Split(string(raw))
		if record.Unquote(fm["todo"]) != id {
			return errors.New("plan identity mismatch")
		}
		old := planRecordSection(body, "plan")
		if artifacts.Hash([]byte(strings.TrimSpace(old)+"\n")) != expected {
			return errPlanRevision
		}
		_, err := s.snapshotTaskPlan(id, old, "Before edit")
		return err
	}, "description", "plan")
	if err != nil {
		return err
	}
	// Re-read under the same lock as observers. If an external edit intervenes,
	// retain the requested version and let the next workspace read observe latest.
	// This is not a cross-file transaction: a crash after the vault replacement
	// can leave the new content without this actor receipt. A later GET observes
	// actual vault bytes; callers must retain uncertain drafts and never replay
	// against a newly observed base without explicit review.
	return s.vault.UpdateCap("todo-plans", s.todoPlans.rel(id), func(raw []byte) ([]byte, error) {
		if _, err := s.snapshotTaskPlanAs(id, text, note, actor); err != nil {
			return nil, err
		}
		_, body := mdfm.Split(string(raw))
		_, err := s.snapshotTaskPlan(id, planRecordSection(body, "plan"), "Observed working plan")
		return raw, err
	})
}

type artifactContextRef = agentchat.ArtifactReference

func (s *Server) runtimeArtifactScope(se termSession) string {
	if o := se.Origin; o != nil && o.Mode == "continue" {
		if o.Backend == "terminal" {
			return terminalConversation(termSession{ID: o.ID, Kind: o.Agent}).Key
		}
		return agentConversation("hermes", o.Agent, o.ID, "private", "").Key
	}
	return terminalConversation(se).Key
}

func privateArtifactScope(sess agentchat.Session) string {
	if o := sess.Origin; o != nil && o.Mode == "continue" && o.Backend == "terminal" {
		return terminalConversation(termSession{ID: o.ID, Kind: o.Agent}).Key
	}
	return sessionConversation(sess).Key
}

func (s *Server) originArtifactScope(o agentchat.Origin) string {
	if o.Backend == "terminal" && s.terminal != nil {
		if se, ok := s.terminal.find(o.ID); ok {
			return s.runtimeArtifactScope(se)
		}
	}
	if s.agentChat != nil {
		if se, _, _, ok := s.agentChat.store.Get(o.Agent, o.ID); ok {
			return privateArtifactScope(se)
		}
	}
	return ""
}

// A standalone conversation can discuss its own snapshot without inventing a
// task. Related conversations receive only versions explicitly handed to them.
func (s *Server) scopedArtifactContext(task, scope string, refs, handed []artifactContextRef) (string, error) {
	if len(refs) == 0 {
		return "", nil
	}
	if s.artifactReg == nil || len(refs) > 8 {
		return "", errBadRequest("artifact context unavailable")
	}
	for _, ref := range refs {
		allowed := false
		if a, ok := s.artifactReg.Get(ref.ID); ok && a.Provenance.Source == "runtime-changes" {
			allowed = scope != "" && a.Provenance.Session == scope
			for _, h := range handed {
				if h == ref {
					allowed = true
				}
			}
		}
		if !allowed {
			if task == "" {
				return "", errBadRequest("artifact is not linked to this conversation")
			}
			if _, err := s.taskArtifactContext(task, []artifactContextRef{ref}); err != nil {
				return "", err
			}
		}
	}
	return s.retainedArtifactContext(refs)
}

// Resolve exact, explicitly selected versions before accepting a message. No
// basename matching, implicit directory attachment, or silent truncation.
func (s *Server) taskArtifactContext(taskID string, refs []artifactContextRef) (string, error) {
	if len(refs) == 0 {
		return "", nil
	}
	if len(refs) > 8 || s.artifactReg == nil {
		return "", errBadRequest("too many or unavailable artifact references")
	}
	allowed := map[string]bool{}
	for _, a := range s.artifactReg.List(artifacts.Filter{}) {
		if a.Provenance.Task == taskID {
			allowed[a.ID] = true
		}
	}
	arts := s.artifactReg.List(artifacts.Filter{})
	outputs, inputs := artifacts.TaskArtifacts(taskID, s.artifactBindings(), arts)
	for _, id := range append(outputs, inputs...) {
		allowed[id] = true
	}
	for _, ref := range refs {
		if !allowed[ref.ID] {
			return "", errBadRequest("artifact is not linked to this task")
		}
	}
	return s.retainedArtifactContext(refs)
}

// Only called after task-scoped acceptance, using the persisted references.
// Later task reassignment cannot change the bytes an accepted instruction uses.
func (s *Server) retainedArtifactContext(refs []artifactContextRef) (string, error) {
	if len(refs) == 0 {
		return "", nil
	}
	if len(refs) > 8 || s.artifactReg == nil {
		return "", errBadRequest("artifact context unavailable")
	}
	var out strings.Builder
	for _, ref := range refs {
		a, ok := s.artifactReg.Get(ref.ID)
		if !ok {
			return "", errBadRequest("artifact unavailable")
		}
		rev, ok := a.Revision(ref.Revision)
		if !ok {
			return "", errBadRequest("artifact revision unavailable")
		}
		b, err := s.artifactReg.Content(ref.Revision)
		if err != nil {
			return "", err
		}
		if len(b) > 64000 || out.Len()+len(b) > 96000 {
			return "", errBadRequest("selected artifact is too large; select a smaller document")
		}
		mime := http.DetectContentType(b)
		if strings.IndexByte(string(b), 0) >= 0 || !utf8.Valid(b) || (!strings.HasPrefix(mime, "text/") && mime != "application/json") {
			return "", errBadRequest("this artifact needs a supported text extraction before discussing it")
		}
		fmt.Fprintf(&out, "\n\n<referenced-artifact id=%q revision=%q version=%q>\n%s\n</referenced-artifact>", ref.ID, ref.Revision, fmt.Sprint(rev.N), string(b))
	}
	return out.String(), nil
}

func (s *Server) handleArtifactContent(w http.ResponseWriter, r *http.Request) {
	if !s.artifactsOK(w) {
		return
	}
	a, ok := s.artifactReg.Get(r.URL.Query().Get("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	rev := orStr(r.URL.Query().Get("rev"), a.Head)
	if _, ok := a.Revision(rev); !ok {
		http.NotFound(w, r)
		return
	}
	b, err := s.artifactReg.Content(rev)
	if err != nil {
		httpError(w, err)
		return
	}
	mime := http.DetectContentType(b)
	if !strings.HasPrefix(mime, "image/") && mime != "application/pdf" {
		mime = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Write(b)
}
