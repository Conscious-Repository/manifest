package server

import (
	"net/http"
	"strings"
	"time"

	"manifest/realestate"
	"manifest/teamportal"
	"manifest/threads"
)

// The todo-panel thread layer (todo-panel plan D3): comments route THREE
// ways by todo id —
//
//   - `aion:` ids, when the team portal is configured, comment through the
//     EXISTING teamportal store: instantly team-visible on portal.aion.bio,
//     no new sharing machinery (attachments land content-addressed in the
//     same shared dir).
//   - real-estate ids (`prop:<slug>/<line>` and personal `real-estate/…`
//     domain ids) go to the SHARED RE thread store (realEstate.teamDir,
//     stood up now so a future RE surface reads it as-is).
//   - everything else — personal — goes to the private store in dataDir.
//     (dataDir never syncs: the cockpit is metis, so metis holds THE private
//     store; a laptop dev instance won't see those threads. Accepted per §8.)

type threadsCfg struct {
	private *threads.Store    // <dataDir>/todo-threads
	re      *threads.Store    // realEstate.teamDir (nil = fall back to private)
	aion    *teamportal.Store // the portal's team store (nil = fall back)
	aionFS  *threads.Store    // blob store rooted at the portal shared dir
	admin   teamportal.Identity
}

// UseThreads wires the thread stores (any but private may be nil).
func (s *Server) UseThreads(private, re *threads.Store, aionTeam *teamportal.Store, aionBlobs *threads.Store, adminEmail string) {
	s.threads = &threadsCfg{
		private: private, re: re, aion: aionTeam, aionFS: aionBlobs,
		admin: teamportal.Identity{Email: adminEmail, Name: "Benjamin"},
	}
}

// threadKind classifies a todo id for routing: "aion" | "re" | "private".
func (s *Server) threadKind(todoID string) string {
	if s.threads == nil {
		return "private"
	}
	if strings.HasPrefix(todoID, "aion:") && s.threads.aion != nil {
		return "aion"
	}
	if (strings.HasPrefix(todoID, "prop:") || strings.HasPrefix(todoID, "real-estate/")) && s.threads.re != nil {
		return "re"
	}
	return "private"
}

// threadStore returns the threads.Store backing a kind's BLOBS and (for
// re/private) its comments.
func (s *Server) threadStore(kind string) *threads.Store {
	switch kind {
	case "aion":
		return s.threads.aionFS
	case "re":
		return s.threads.re
	default:
		return s.threads.private
	}
}

// ownerIdentity is the thread author for panel writes (single-user cockpit).
func (s *Server) ownerIdentity() threads.Identity {
	name := "Benjamin"
	if s.threads != nil && s.threads.admin.Name != "" {
		name = s.threads.admin.Name
	}
	return threads.Identity{ID: "owner", Name: name}
}

// listThread returns a todo's thread in the uniform shape, regardless of
// which store holds it.
func (s *Server) listThread(todoID string) []threads.Comment {
	if s.threads == nil {
		return nil
	}
	if s.threadKind(todoID) == "aion" {
		item := strings.TrimPrefix(todoID, "aion:")
		var out []threads.Comment
		for _, c := range s.threads.aion.Ext().Comments[item] {
			files := make([]threads.FileRef, 0, len(c.Files))
			for _, f := range c.Files {
				files = append(files, threads.FileRef{Hash: f.Hash, Name: f.Name, Size: f.Size, Mime: f.Mime})
			}
			out = append(out, threads.Comment{
				ID: c.ID, TodoID: todoID, Action: threads.ActComment,
				Author: c.Author, AuthorName: c.AuthorName,
				Text: c.Text, Files: files, At: c.At,
			})
		}
		return out
	}
	return s.threadStore(s.threadKind(todoID)).Thread(todoID)
}

// addThreadEntry routes one entry to the right store. Structural actions
// (assign/plan/fire/result) always land in the threads-shaped store — for
// aion todos the TEXT of plain comments goes team-visible via teamportal,
// structural entries stay in the private machine trail.
func (s *Server) addThreadEntry(author threads.Identity, todoID, action, text string, mentions []string, files []threads.FileRef, meta map[string]any) (threads.Comment, error) {
	now := time.Now()
	if s.threads == nil {
		return threads.Comment{}, errBadRequest("threads not configured")
	}
	if s.threadKind(todoID) == "aion" && action == threads.ActComment {
		pf := make([]teamportal.FileRef, 0, len(files))
		for _, f := range files {
			pf = append(pf, teamportal.FileRef{Hash: f.Hash, Name: f.Name, Size: f.Size, Mime: f.Mime})
		}
		c, err := s.threads.aion.AddCommentWithFiles(s.threads.admin, strings.TrimPrefix(todoID, "aion:"), text, pf, now)
		if err != nil {
			return threads.Comment{}, err
		}
		return threads.Comment{ID: c.ID, TodoID: todoID, Action: threads.ActComment,
			Author: c.Author, AuthorName: c.AuthorName, Text: c.Text, Files: files, At: c.At}, nil
	}
	kind := s.threadKind(todoID)
	if kind == "aion" {
		kind = "private" // structural trail for aion todos stays private
	}
	return s.threadStore(kind).Add(author, todoID, action, text, mentions, files, meta, now)
}

// --- assignment (todo-panel plan Phase 3) ------------------------------------

// setTodoOwner patches the owner field in the todo's SOURCE file (the
// non-HTTP three-way sibling of handleTodoUpdate's owner patch).
func (s *Server) setTodoOwner(id, owner string) error {
	switch {
	case strings.HasPrefix(id, "aion:"):
		if s.aion == nil {
			return errBadRequest("aion not configured")
		}
		return s.aion.UpdateItem(strings.TrimPrefix(id, "aion:"), map[string]string{"owner": owner}, time.Now())
	case strings.HasPrefix(id, "prop:"):
		slug, lineID := splitPropID(id)
		if slug == "" {
			return errBadRequest("malformed property todo id")
		}
		list, rel, ok := s.realestate.LoadTodos(slug)
		if !ok {
			return errBadRequest("property not found")
		}
		t := list.Find(lineID)
		if t == nil {
			return errBadRequest("todo not found")
		}
		t.Owner = owner
		if err := s.vault.ReplaceSectionCap("realestate", rel, "todos", realestate.EmitPropertyTodos(list)); err != nil {
			return err
		}
		if s.index != nil {
			_ = s.index.ReindexPaths([]string{rel})
		}
		return nil
	default:
		doc, err := s.todosStore.Load()
		if err != nil {
			return err
		}
		_, t := doc.Find(id)
		if t == nil {
			return errBadRequest("todo not found")
		}
		t.Owner = owner
		return s.todosStore.Save(doc)
	}
}

// handleTodoAssign — the uniform assignee write: pins identity, ensures the
// plan record (assignee in frontmatter), patches [owner::] in the source
// file, logs the replayable `assign` thread action, and — when the token
// carries the hard `agent:` prefix (validated against the roster; unknown →
// 400) — kicks the plan-phase delegation (Phase 4 hook).
func (s *Server) handleTodoAssign(w http.ResponseWriter, r *http.Request) {
	var b struct{ ID, Owner string }
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.ID) == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	owner := strings.TrimSpace(b.Owner)
	harness := ""
	if strings.HasPrefix(owner, "agent:") {
		if harness = s.agentHarness(owner); harness == "" {
			httpError(w, errBadRequest("unknown agent "+owner+" — only configured harnesses are assignable"))
			return
		}
	}
	id, ok := s.pinTodoID(strings.TrimSpace(b.ID))
	if !ok {
		http.Error(w, "todo not found", http.StatusNotFound)
		return
	}
	if err := s.setTodoOwner(id, owner); err != nil {
		httpError(w, err)
		return
	}
	if err := s.setPlanAssignee(id, owner); err != nil {
		httpError(w, err)
		return
	}
	text := "assigned to " + orStr(owner, "me")
	if _, err := s.addThreadEntry(s.ownerIdentity(), id, threads.ActAssign, text, nil, nil,
		map[string]any{"assignee": owner}); err != nil {
		httpError(w, err)
		return
	}
	if harness != "" {
		if err := s.assignAgentHook(id, harness); err != nil {
			httpError(w, err)
			return
		}
	}
	writeJSON(w, map[string]any{"ok": true, "record": s.readPlanRecord(id), "thread": s.listThread(id)})
}

// --- endpoints ---------------------------------------------------------------

func (s *Server) handleTodoThreadGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	writeJSON(w, map[string]any{"thread": s.listThread(id)})
}

// handleTodoThreadPost adds a comment. The composer uploads files first
// (handleTodoThreadFile) and posts their refs here. Mentions are structural
// roster tokens (typeahead-inserted — no new syntax); a mentioned agent
// triggers the comment-phase spool (Phase 4 hook).
func (s *Server) handleTodoThreadPost(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID, Text string
		Mentions []string
		Files    []threads.FileRef
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.ID) == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	id, ok := s.pinTodoID(strings.TrimSpace(b.ID))
	if !ok {
		http.Error(w, "todo not found", http.StatusNotFound)
		return
	}
	c, err := s.addThreadEntry(s.ownerIdentity(), id, threads.ActComment, b.Text, b.Mentions, b.Files, nil)
	if err != nil {
		httpError(w, err)
		return
	}
	s.threadMentionHook(id, b.Mentions, b.Text) // agent mentions → Phase 4 spool
	writeJSON(w, map[string]any{"ok": true, "comment": c, "thread": s.listThread(id)})
}

// threadMentionHook reacts to agent mentions in a fresh comment. Phase 4
// wires the comment-phase spool; until then it is deliberately inert.
func (s *Server) threadMentionHook(todoID string, mentions []string, text string) {
	_ = todoID
	_ = mentions
	_ = text
}

// assignAgentHook kicks the plan-phase delegation on agent assignment.
// Phase 4 replaces the body with the real spool; inert until then.
func (s *Server) assignAgentHook(todoID, harness string) error {
	_ = todoID
	_ = harness
	return nil
}

// handleTodoThreadFile stores one attachment blob (raw body) and returns its
// content-addressed ref — the composer attaches refs on the comment POST.
func (s *Server) handleTodoThreadFile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if id == "" || name == "" {
		httpError(w, errBadRequest("id and name are required"))
		return
	}
	if s.threads == nil {
		httpError(w, errBadRequest("threads not configured"))
		return
	}
	st := s.threadStore(s.threadKind(id))
	if st == nil {
		httpError(w, errBadRequest("no store for this todo"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, threads.MaxBlobSize+1)
	ref, err := st.SaveBlob(r.Body, name, r.Header.Get("Content-Type"))
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "file": ref})
}

// handleTodoThreadBlob serves a stored attachment.
func (s *Server) handleTodoThreadBlob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	hash := r.PathValue("hash")
	if s.threads == nil {
		http.Error(w, "threads not configured", http.StatusServiceUnavailable)
		return
	}
	st := s.threadStore(s.threadKind(id))
	p := ""
	if st != nil {
		p = st.BlobPath(hash)
	}
	if p == "" && s.threads.private != nil { // tolerate stale routing
		p = s.threads.private.BlobPath(hash)
	}
	if p == "" {
		http.Error(w, "no such file", http.StatusNotFound)
		return
	}
	if dl := r.URL.Query().Get("dl"); dl == "1" {
		w.Header().Set("Content-Disposition", "attachment")
	}
	http.ServeFile(w, r, p)
}
