package server

// FIRST-CLASS ARTIFACTS (manifest P1 Phase 1) — the server face of the
// artifacts registry (artifacts/object.go):
//
//   - read:   GET /api/artifacts (list) · GET /api/artifacts/get?id= (one,
//             with its derived task links and how to OPEN it — the head still
//             opens through /api/spirits/file, the one harness read path;
//             ?content=1 reads a revision's bytes from the pool by hash)
//   - write:  POST /api/artifacts/create · /api/artifacts/revise — inline
//             content or a harness-relative ref the spirits read path can see
//   - bind:   POST /api/tasks/artifacts — [outputs::] / [inputs::] on the
//             task line, by reference (todo_coord.go's routing)
//   - emit:   the ledger sweep registers the brief a completed run wrote, so
//             an agent's deliverable becomes an artifact with provenance
//             {run, task} and no one has to file it by hand.
//
// Every create/revise/bind appends a ledger event tagged object={artifact,id}
// (and the task as a related ref), so an artifact's lifecycle is
// reconstructible from the event log alone (GET /api/ledger/history).

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"manifest/artifacts"
	"manifest/ledger"
	"manifest/tasks"
)

// UseArtifactRegistry wires the artifact object store.
func (s *Server) UseArtifactRegistry(r *artifacts.Registry) { s.artifactReg = r }

// artifactOpen is how the client opens an artifact's current bytes: through
// the spirits read API (path + harness tag) — or, for a legacy in-vault
// harness, the note view. Never both (the two-media rule, server/artifacts.go).
type artifactOpen struct {
	Path    string `json:"path,omitempty"`    // harness-relative → /api/spirits/file?path=&harness=
	Harness string `json:"harness,omitempty"` // harness tag ("" = primary)
	Note    string `json:"note,omitempty"`    // vault-relative → the note view
}

// artifactView is one artifact on the wire: the object, its derived task
// links, how to open it, and (on request) a revision's content.
type artifactView struct {
	artifacts.Artifact
	Links   artifacts.Links `json:"links"`
	Open    *artifactOpen   `json:"open,omitempty"`
	Content string          `json:"content,omitempty"`
}

func (s *Server) artifactView(a artifacts.Artifact, links map[string]artifacts.Links) artifactView {
	v := artifactView{Artifact: a, Links: links[a.ID]}
	if v.Links.Producers == nil {
		v.Links.Producers = []string{}
	}
	if v.Links.Consumers == nil {
		v.Links.Consumers = []string{}
	}
	if a.Ref != "" {
		if h := s.findHarness(orStr(a.Harness, s.primaryHarnessName())); h != nil {
			note, ref := s.artifactRefSplit(*h, a.Ref)
			switch {
			case note != "":
				v.Open = &artifactOpen{Note: note}
			case ref != "":
				v.Open = &artifactOpen{Path: ref, Harness: s.harnessTag(h.Name)}
			}
		}
	}
	return v
}

// artifactBindings projects every task line that carries an artifact field
// (open AND done — a finished task still produced its artifact) into the
// registry's binding shape. Personal lines and property trees; backlog items
// carry neither field yet.
func (s *Server) artifactBindings() []artifacts.Binding {
	var out []artifacts.Binding
	add := func(id string, t *tasks.Task) {
		if t != nil && (len(t.Outputs) > 0 || len(t.Inputs) > 0) {
			out = append(out, artifacts.Binding{Task: id, Outputs: t.Outputs, Inputs: t.Inputs})
		}
	}
	if s.tasksStore != nil {
		if doc, err := s.tasksStore.Load(); err == nil {
			for _, dom := range doc.Domains {
				dom.AllTasks(func(_ *tasks.Bucket, t *tasks.Task) { add(t.ID, t) })
			}
		}
	}
	if s.realestate != nil {
		if props, err := s.realestate.Properties(); err == nil {
			for _, p := range props {
				for _, t := range p.Tasks {
					add("prop:"+p.Slug+"/"+t.ID, t)
				}
			}
		}
	}
	return out
}

// artifactLinks is the derived artifact → {producers, consumers} index over
// the given artifacts and every bound task line.
func (s *Server) artifactLinks(arts []artifacts.Artifact) map[string]artifacts.Links {
	return artifacts.LinkIndex(s.artifactBindings(), arts)
}

// artifactRefIndex maps harness name + ref → artifact id, for the rows that
// already carry a harness-relative ArtifactRef (delegations) to name their
// object without a second lookup each.
func (s *Server) artifactRefIndex() map[string]string {
	idx := map[string]string{}
	if s.artifactReg == nil {
		return idx
	}
	for _, a := range s.artifactReg.List(artifacts.Filter{}) {
		if a.Ref != "" {
			idx[orStr(a.Harness, s.primaryHarnessName())+"\n"+a.Ref] = a.ID
		}
	}
	return idx
}

// taskArtifactsView answers the panel's "what did this task produce / what
// does it consume": the bound ids resolved against the registry (an id the
// registry doesn't know still lists, as just its id — never dropped).
func (s *Server) taskArtifactsView(taskID string) map[string]any {
	var arts []artifacts.Artifact
	if s.artifactReg != nil {
		arts = s.artifactReg.List(artifacts.Filter{})
	}
	byID := map[string]artifacts.Artifact{}
	for _, a := range arts {
		byID[a.ID] = a
	}
	links := artifacts.LinkIndex(s.artifactBindings(), arts)
	outputs, inputs := artifacts.TaskArtifacts(taskID, s.artifactBindings(), arts)
	project := func(ids []string) []any {
		out := []any{}
		for _, id := range ids {
			if a, ok := byID[id]; ok {
				out = append(out, s.artifactView(a, links))
			} else {
				out = append(out, map[string]any{"id": id, "unknown": true})
			}
		}
		return out
	}
	return map[string]any{"outputs": project(outputs), "inputs": project(inputs)}
}

// --- the ledger hooks ---------------------------------------------------------

// artifactEvent mirrors a create/revise into the ledger, tagged with the
// artifact as the object and its provenance task/run as related refs — so
// both the artifact's and the task's histories carry the line.
func (s *Server) artifactEvent(res artifacts.PutResult, actor string) {
	if !res.Changed {
		return
	}
	a, rev := res.Artifact, res.Revision
	kind := "artifact.revised"
	if res.Created {
		kind = "artifact.created"
	}
	meta := map[string]any{"hash": rev.Hash, "version": rev.N, "artifactKind": a.Kind, "size": rev.Size}
	if rev.Parent != "" {
		meta["parent"] = rev.Parent
	}
	if a.Provenance.Source != "" {
		meta["source"] = a.Provenance.Source
	}
	text := orStr(a.Title, a.Ref)
	if rev.Note != "" {
		text += " — " + rev.Note
	}
	s.ledger(ledger.Entry{TS: rev.At, Source: "artifact", Kind: kind, Actor: orStr(actor, orStr(rev.Actor, "owner")),
		Object: ledger.Object{Kind: ledger.ObjArtifact, ID: a.ID},
		Task:   a.Provenance.Task, Run: a.Provenance.Run, Session: a.Provenance.Session, Harness: a.Harness,
		Ref: a.Ref, Text: ledger.Snip(text, 280), Meta: meta})
}

// artifactBoundEvent records a task ↔ artifact edge change on the artifact's
// history (the task rides as the related ref).
func (s *Server) artifactBoundEvent(taskID, artifactID, role string, bound bool) {
	kind := "artifact.bound"
	if !bound {
		kind = "artifact.unbound"
	}
	s.ledger(ledger.Entry{Source: "artifact", Kind: kind, Actor: "owner",
		Object: ledger.Object{Kind: ledger.ObjArtifact, ID: artifactID}, Task: taskID,
		Text: taskID + " " + role + " " + artifactID, Meta: map[string]any{"role": role, "task": taskID}})
}

// --- readers ----------------------------------------------------------------

// readHarnessRef reads a harness-relative ref through the spirits read
// allow-list — the same door /api/spirits/file opens — returning the bytes
// and the canonical name of the tree they came from. want may be a name, a
// tag, or "" (first tree that has it).
func (s *Server) readHarnessRef(want, ref string) ([]byte, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, "", errBadRequest("ref is required")
	}
	offList := false
	for _, h := range s.eachHarness() {
		if h.Spirits == nil || (want != "" && h.Name != want && s.harnessTag(h.Name) != want) {
			continue
		}
		content, allowed, err := h.Spirits.ReadFile(ref)
		if !allowed {
			offList = true
			continue
		}
		if err != nil {
			continue
		}
		return []byte(content), h.Name, nil
	}
	if offList {
		return nil, "", errBadRequest("that path is not readable through the spirits file API")
	}
	return nil, "", errBadRequest("no harness has " + ref)
}

// --- handlers ---------------------------------------------------------------

func (s *Server) artifactsOK(w http.ResponseWriter) bool {
	if s.artifactReg == nil {
		http.Error(w, "artifacts are not configured", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// handleArtifactsList — GET /api/artifacts?kind=&task=&run=&harness=&ref=.
// Most recently changed first, each with its derived task links.
func (s *Server) handleArtifactsList(w http.ResponseWriter, r *http.Request) {
	if s.artifactReg == nil {
		writeJSON(w, map[string]any{"artifacts": []any{}, "count": 0})
		return
	}
	q := r.URL.Query()
	f := artifacts.Filter{
		Kind: strings.TrimSpace(q.Get("kind")), Task: strings.TrimSpace(q.Get("task")),
		Run: strings.TrimSpace(q.Get("run")), Harness: strings.TrimSpace(q.Get("harness")),
		Ref: strings.TrimSpace(q.Get("ref")),
	}
	if f.Harness != "" { // a tag on the wire names the primary
		if h := s.findHarness(f.Harness); h != nil {
			f.Harness = h.Name
		}
	}
	arts := s.artifactReg.List(f)
	links := s.artifactLinks(arts)
	out := []artifactView{}
	for _, a := range arts {
		out = append(out, s.artifactView(a, links))
	}
	writeJSON(w, map[string]any{"artifacts": out, "count": len(out)})
}

// handleArtifactGet — GET /api/artifacts/get?id=&content=1&rev=<hash>. The
// object with its links and open link; content=1 adds the head's bytes (or
// the named revision's) from the pool — any version, not just the file's
// current state.
func (s *Server) handleArtifactGet(w http.ResponseWriter, r *http.Request) {
	if !s.artifactsOK(w) {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	a, ok := s.artifactReg.Get(id)
	if !ok {
		http.Error(w, "no such artifact", http.StatusNotFound)
		return
	}
	v := s.artifactView(a, s.artifactLinks([]artifacts.Artifact{a}))
	if r.URL.Query().Get("content") == "1" {
		hash := orStr(strings.TrimSpace(r.URL.Query().Get("rev")), a.Head)
		if _, ok := a.Revision(hash); !ok {
			http.Error(w, "no such revision", http.StatusNotFound)
			return
		}
		b, err := s.artifactReg.Content(hash)
		if err != nil {
			httpError(w, err)
			return
		}
		v.Content = string(b)
	}
	writeJSON(w, v)
}

// artifactPutBody is the create/revise request: inline content, or a
// harness-relative ref read through the spirits allow-list.
type artifactPutBody struct {
	ID      string   `json:"id"` // revise only
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Harness string   `json:"harness"`
	Ref     string   `json:"ref"`
	Content string   `json:"content"`
	Actor   string   `json:"actor"`
	Note    string   `json:"note"`
	Source  string   `json:"source"`
	Task    string   `json:"task"`
	Run     string   `json:"run"`
	Session string   `json:"session"`
	Inputs  []string `json:"inputs"`
}

// artifactPut is the one path behind create and revise: resolve the bytes,
// put, ledger, answer with the view.
func (s *Server) artifactPut(w http.ResponseWriter, r *http.Request, revise bool) {
	if !s.artifactsOK(w) {
		return
	}
	var b artifactPutBody
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	b.ID = strings.TrimSpace(b.ID)
	if revise && b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	if !revise {
		b.ID = ""
	}
	content := []byte(b.Content)
	harness := strings.TrimSpace(b.Harness)
	if len(content) == 0 && strings.TrimSpace(b.Ref) != "" {
		got, name, err := s.readHarnessRef(harness, b.Ref)
		if err != nil {
			httpError(w, err)
			return
		}
		content, harness = got, name
	} else if harness != "" {
		if h := s.findHarness(harness); h != nil {
			harness = h.Name
		}
	}
	if len(content) == 0 {
		httpError(w, errBadRequest("content or a readable ref is required"))
		return
	}
	actor := orStr(strings.TrimSpace(b.Actor), "owner")
	res, err := s.artifactReg.Put(artifacts.Put{
		ID: b.ID, Kind: b.Kind, Title: strings.TrimSpace(b.Title), Harness: harness, Ref: b.Ref,
		Content: content, Actor: actor, Note: strings.TrimSpace(b.Note), At: time.Now(),
		Provenance: artifacts.Provenance{
			Source: orStr(strings.TrimSpace(b.Source), "manual"), Task: strings.TrimSpace(b.Task),
			Run: strings.TrimSpace(b.Run), Session: strings.TrimSpace(b.Session), Inputs: b.Inputs,
		},
	})
	if err != nil {
		if strings.Contains(err.Error(), "no such artifact") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		httpError(w, err)
		return
	}
	s.artifactEvent(res, actor)
	v := s.artifactView(res.Artifact, s.artifactLinks([]artifacts.Artifact{res.Artifact}))
	writeJSON(w, map[string]any{"artifact": v, "created": res.Created, "changed": res.Changed, "revision": res.Revision})
}

// handleArtifactCreate — POST /api/artifacts/create. A ref already
// registered takes the bytes as a new revision (the address is the place).
func (s *Server) handleArtifactCreate(w http.ResponseWriter, r *http.Request) {
	s.artifactPut(w, r, false)
}

// handleArtifactRevise — POST /api/artifacts/revise {id, content|ref, note}.
func (s *Server) handleArtifactRevise(w http.ResponseWriter, r *http.Request) {
	s.artifactPut(w, r, true)
}

// handleTaskArtifacts — POST /api/tasks/artifacts: replace ({outputs: [...]}
// / {inputs: [...]}) or edit one edge ({addOutput|addInput|removeOutput|
// removeInput: id}) on the task line owning the id. Ids are references — an
// id the registry doesn't know is accepted (it lists as unknown), so a task
// can name an artifact another machine holds.
func (s *Server) handleTaskArtifacts(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ID           string
		Outputs      *[]string
		Inputs       *[]string
		AddOutput    string
		AddInput     string
		RemoveOutput string
		RemoveInput  string
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.ID) == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	b.ID = strings.TrimSpace(b.ID)
	if b.Outputs == nil && b.Inputs == nil && strings.TrimSpace(b.AddOutput+b.AddInput+b.RemoveOutput+b.RemoveInput) == "" {
		httpError(w, errBadRequest("outputs, inputs, addOutput, addInput, removeOutput, or removeInput is required"))
		return
	}
	var events []func()
	sw := &statusWriter{ResponseWriter: w}
	s.coordMutate(sw, b.ID, func(t *tasks.Task) error {
		beforeOut, beforeIn := append([]string{}, t.Outputs...), append([]string{}, t.Inputs...)
		if b.Outputs != nil {
			t.SetOutputs(*b.Outputs)
		}
		if b.Inputs != nil {
			t.SetInputs(*b.Inputs)
		}
		if a := strings.TrimSpace(b.AddOutput); a != "" {
			t.AddOutput(a)
		}
		if a := strings.TrimSpace(b.AddInput); a != "" {
			t.AddInput(a)
		}
		if rm := strings.TrimSpace(b.RemoveOutput); rm != "" {
			t.RemoveOutput(rm)
		}
		if rm := strings.TrimSpace(b.RemoveInput); rm != "" {
			t.RemoveInput(rm)
		}
		// the edge diff becomes the ledger lines, after the file is saved
		for role, pair := range map[string][2][]string{"output": {beforeOut, t.Outputs}, "input": {beforeIn, t.Inputs}} {
			role, before, after := role, pair[0], pair[1]
			for _, id := range diffIDs(before, after) {
				id := id
				events = append(events, func() { s.artifactBoundEvent(b.ID, id, role, true) })
			}
			for _, id := range diffIDs(after, before) {
				id := id
				events = append(events, func() { s.artifactBoundEvent(b.ID, id, role, false) })
			}
		}
		return nil
	})
	if sw.ok() { // the file took the edit — only then is the edge a fact
		for _, fn := range events {
			fn()
		}
	}
}

// statusWriter remembers whether the wrapped handler answered 2xx.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) ok() bool { return w.status >= 200 && w.status < 300 }

// diffIDs lists the ids in b that a lacks.
func diffIDs(a, b []string) []string {
	have := map[string]bool{}
	for _, x := range a {
		have[x] = true
	}
	var out []string
	for _, x := range b {
		if !have[x] {
			out = append(out, x)
		}
	}
	return out
}

// --- the run writer ----------------------------------------------------------

// registerRunBrief files the brief a completed run wrote as an artifact
// {kind brief, provenance run+task} — idempotent (same bytes at the same ref
// is the same object), so the sweep can call it on every completed run it
// mirrors. Returns the put result when something was registered.
func (s *Server) registerRunBrief(h *Harness, runID, taskID, actor string, at time.Time) (artifacts.PutResult, error) {
	if s.artifactReg == nil || h == nil || h.Spirits == nil {
		return artifacts.PutResult{}, errors.New("artifacts: registry or harness unavailable")
	}
	doc, ok := libraryDocForRun(*h, runID, harnessLibrary(*h))
	if !ok || doc.Ref == "" {
		return artifacts.PutResult{}, errors.New("no brief for run " + runID)
	}
	content, allowed, err := h.Spirits.ReadFile(doc.Ref)
	if !allowed || err != nil || content == "" {
		return artifacts.PutResult{}, errors.New("brief unreadable: " + doc.Ref)
	}
	res, err := s.artifactReg.Put(artifacts.Put{
		Kind: artifacts.KindBrief, Title: doc.Title, Harness: h.Name, Ref: doc.Ref,
		Content: []byte(content), Actor: actor, At: at,
		Provenance: artifacts.Provenance{Source: "run", Task: taskID, Run: runID},
	})
	if err != nil {
		return res, err
	}
	s.artifactEvent(res, actor)
	return res, nil
}
