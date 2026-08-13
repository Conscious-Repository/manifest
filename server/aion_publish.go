package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"manifest/aion"
	"manifest/goals"
)

// The AION publish effector (aion-domain spec §5): one button, the whole
// export contract, one commit. PREVIEW writes nothing — it renders the
// contract in memory and diffs against the checkout; CONFIRM (carrying the
// preview's content hash) writes ONLY contract paths through the canary
// chokepoint, then git add/commit/push and a permanent receipt. A commit
// whose push failed surfaces as `unpushed` and the next publish completes
// push-only — no half-published state is reachable.

// ---- git runner ----

func gitRun(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// never hang on a credential prompt — fail fast and surface the stage
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		tail := text
		if len(tail) > 400 {
			tail = tail[len(tail)-400:]
		}
		return text, fmt.Errorf("git %s: %v — %s", strings.Join(args, " "), err, tail)
	}
	return text, nil
}

const (
	gitLocalTimeout = 30 * time.Second
	gitPushTimeout  = 120 * time.Second
)

// reconcileAndRetryPush recovers a non-fast-forward publish push. The portal
// checkout can fall behind when a portal-source commit is pushed from another
// machine; a publish commit only touches the data-contract paths, disjoint
// from that source, so fetch the remote tip, rebase the publish commit(s) onto
// it, and push again. On a clean recovery it returns the new HEAD (the rebase
// rewrote the hash) and a nil error. If the fetch fails, or the rebase hits a
// real conflict (aborted so the checkout is left clean), it returns the caller's
// original commit and the original push error — same recoverable failed-push
// state as before, just now self-healing for the common disjoint case.
func reconcileAndRetryPush(p aionPortalCfg, commit string, pushErr error) (string, error) {
	if _, err := gitRun(p.Path, gitPushTimeout, "fetch", p.Remote, p.Branch); err != nil {
		return commit, pushErr
	}
	if _, err := gitRun(p.Path, gitLocalTimeout, "rebase", "FETCH_HEAD"); err != nil {
		_, _ = gitRun(p.Path, gitLocalTimeout, "rebase", "--abort")
		return commit, pushErr
	}
	if h, err := gitRun(p.Path, gitLocalTimeout, "rev-parse", "HEAD"); err == nil {
		commit = strings.TrimSpace(h)
	}
	if _, err := gitRun(p.Path, gitPushTimeout, "push", p.Remote, p.Branch); err != nil {
		return commit, err
	}
	return commit, nil
}

// ---- publish receipts (permanent audit trail, dataDir tier) ----

type aionPublishRecord struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"` // ok | failed
	Stage        string   `json:"stage,omitempty"`
	Commit       string   `json:"commit,omitempty"`
	Files        []string `json:"files,omitempty"`
	Error        string   `json:"error,omitempty"`
	At           string   `json:"at"`
	Acknowledged bool     `json:"acknowledged"`
}

// aionPublishLog is the append-only receipts store at
// <dataDir>/aion/publishes.json — outcomes of actions the system took
// (attention kind: receipt, LifecyclePermanent).
type aionPublishLog struct {
	mu   sync.Mutex
	path string
}

func newAionPublishLog(dataDir string) *aionPublishLog {
	return &aionPublishLog{path: filepath.Join(dataDir, "aion", "publishes.json")}
}

func (l *aionPublishLog) list() []aionPublishRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.load()
}

func (l *aionPublishLog) load() []aionPublishRecord {
	var out []aionPublishRecord
	if b, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func (l *aionPublishLog) append(r aionPublishRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	recs := append(l.load(), r)
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return
	}
	b, _ := json.MarshalIndent(recs, "", "  ")
	_ = os.WriteFile(l.path, b, 0644)
}

func (l *aionPublishLog) ack(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	recs := l.load()
	for i := range recs {
		if recs[i].ID == id {
			recs[i].Acknowledged = true
			b, _ := json.MarshalIndent(recs, "", "  ")
			_ = os.WriteFile(l.path, b, 0644)
			return true
		}
	}
	return false
}

func (s *Server) publishLog() *aionPublishLog {
	if s.aionPublishes == nil {
		s.aionPublishes = newAionPublishLog(s.aionDataDir)
	}
	return s.aionPublishes
}

// ---- contract rendering (server side: joins the aion docs + goals area) ----

func (s *Server) aionExportInput(publishedAt string) aion.ExportInput {
	return aion.ExportInput{
		People:       s.aion.LoadPeople(),
		VTO:          s.aion.LoadVTO(),
		Backlog:      s.aion.LoadBacklog(),
		Heuristics:   s.aion.LoadHeuristics(),
		Finances:     s.aion.LoadFinances(),
		HiringMD:     []byte(s.aion.RawFile("hiring.md")),
		ReferencesMD: []byte(s.aion.RawFile("references.md")),
		Goals:        s.aionExportGoals(),
		PublishedAt:  publishedAt,
	}
}

// aionExportGoals maps the goals package's `## Aion` area into the export
// graph: annuals → 1yr, rocks → rock (serves a 1yr id), rock children → 30.
func (s *Server) aionExportGoals() []aion.ExportGoal {
	area := s.aionGoalsArea()
	if area == nil {
		return []aion.ExportGoal{}
	}
	var out []aion.ExportGoal
	status := func(g goals.GoalView) string {
		if g.Checked || strings.EqualFold(g.Status, "done") {
			return "done"
		}
		return "open"
	}
	owner := func(g goals.GoalView) *string {
		if g.Owner == "" || strings.EqualFold(g.Owner, "me") {
			return nil
		}
		o := g.Owner
		return &o
	}
	// rocks serving each annual become its children (1:many — a rock may
	// feed several annuals; Series A feeds all of them)
	serving := map[string][]string{}
	for _, r := range area.Rocks {
		for _, sv := range r.Serves {
			serving[sv] = append(serving[sv], r.ID)
		}
	}
	nonNil := func(ids []string) []string {
		if ids == nil {
			return []string{}
		}
		return ids
	}
	for _, a := range area.Annuals {
		out = append(out, aion.ExportGoal{
			ID: a.ID, Title: a.Text, Horizon: "1yr", Status: status(a),
			Aliases: nonNil(a.Aliases), Owner: owner(a), Children: nonNil(serving[a.ID]),
		})
	}
	for _, r := range area.Rocks {
		var serves *string
		if len(r.Serves) > 0 {
			v := r.Serves[0] // primary link (portal back-compat)
			serves = &v
		}
		var kids []string
		for _, c := range r.Children {
			kids = append(kids, c.ID)
			out = append(out, aion.ExportGoal{
				ID: c.ID, Title: c.Text, Horizon: "30", Status: status(c),
				Owner: owner(c), Children: []string{},
			})
		}
		out = append(out, aion.ExportGoal{
			ID: r.ID, Title: r.Text, Horizon: "rock", Status: status(r),
			Serves: serves, ServesAll: append([]string{}, r.Serves...),
			Aliases: nonNil(r.Aliases), Owner: owner(r), Quarter: r.Quarter,
			Start: r.Start, Due: r.Due, Children: nonNil(kids),
		})
	}

	// Historic rocks: closed Aion rocks from the quarter archives (the
	// one-time reconstruction of past 90-day priorities). They export as
	// status:done rocks with a `closed` date so the portal can place them in
	// the past cone; live ids already emitted above win any collision.
	live := map[string]bool{}
	for _, g := range out {
		live[g.ID] = true
	}
	for _, aq := range s.goals.LoadAllArchives() {
		for _, e := range aq.Entries {
			if !strings.HasPrefix(e.GoalID, "aion/") || live[e.GoalID] {
				continue
			}
			live[e.GoalID] = true
			var serves *string
			if len(e.Serves) > 0 {
				v := e.Serves[0]
				serves = &v
			}
			out = append(out, aion.ExportGoal{
				ID: e.GoalID, Title: e.Text, Horizon: "rock", Status: "done",
				Serves: serves, ServesAll: append([]string{}, e.Serves...),
				Owner: nil, Quarter: aq.Quarter, Closed: e.Closed, Children: []string{},
			})
		}
	}
	return out
}

// ---- the canary chokepoint ----

// writeAionContract is the ONLY function that writes into the checkout. It
// refuses any path outside the compiled contract list (after cleaning) —
// the contract-paths-only guarantee.
func writeAionContract(checkoutRoot, rel string, data []byte) error {
	clean := filepath.ToSlash(filepath.Clean(rel))
	allowed := false
	for _, p := range aion.ContractPaths() {
		if clean == p {
			allowed = true
			break
		}
	}
	if !allowed || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return fmt.Errorf("refusing non-contract path %q", rel)
	}
	abs := filepath.Join(checkoutRoot, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	return os.WriteFile(abs, data, 0644)
}

// ---- preview / publish ----

type aionPublishFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // new | changed | unchanged
	Diff   string `json:"diff,omitempty"`
}

type aionPreviewResult struct {
	files     []aionPublishFile
	blockers  []string
	untracked []string // untracked files inside the contract paths — resolvable via the baseline commit, not a hard blocker
	hash      string
	unpushed  int
	rendered  map[string][]byte
}

// aionPreflight checks the checkout is usable. Hard problems come back as
// blockers (human sentences, never raw porcelain); untracked files inside
// the contract paths come back separately — the panel offers to preserve
// them in a baseline commit instead of dead-ending.
func (s *Server) aionPreflight() (blockers, untracked []string) {
	p := s.aionPortal
	if p.Path == "" {
		return []string{"aionPortal.path not configured"}, nil
	}
	if fi, err := os.Stat(p.Path); err != nil || !fi.IsDir() {
		return []string{fmt.Sprintf("checkout %s not found", p.Path)}, nil
	}
	if _, err := gitRun(p.Path, gitLocalTimeout, "rev-parse", "--git-dir"); err != nil {
		return []string{"not a git repository: " + p.Path}, nil
	}
	if branch, err := gitRun(p.Path, gitLocalTimeout, "rev-parse", "--abbrev-ref", "HEAD"); err != nil {
		blockers = append(blockers, "cannot read branch: "+err.Error())
	} else if branch != p.Branch {
		blockers = append(blockers, fmt.Sprintf("checkout is on %q, configured branch is %q", branch, p.Branch))
	}
	gitDir, _ := gitRun(p.Path, gitLocalTimeout, "rev-parse", "--git-dir")
	if gitDir != "" {
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(p.Path, gitDir)
		}
		for _, marker := range []string{"MERGE_HEAD", "rebase-merge", "rebase-apply"} {
			if _, err := os.Stat(filepath.Join(gitDir, marker)); err == nil {
				blockers = append(blockers, "checkout has a merge/rebase in progress")
				break
			}
		}
	}
	// SCOPED dirty check: only uncommitted changes to the NINE contract
	// paths themselves matter (they would be clobbered or swept into our
	// commit). Sibling files in the same dirs — roadmap.js is hand-
	// maintained portal-side by contract — and everything else in the
	// checkout are ignored by design: the effector only ever writes/adds
	// the nine explicit paths. MODIFIED tracked contract files are a hard
	// block (hand edits publish would silently clobber); UNTRACKED ones are
	// recoverable — the baseline action preserves them in a commit first.
	statusArgs := append([]string{"status", "--porcelain", "--"}, aion.ContractPaths()...)
	if status, err := gitRun(p.Path, gitLocalTimeout, statusArgs...); err != nil {
		blockers = append(blockers, "git status failed: "+err.Error())
	} else if status != "" {
		var modified []string
		for _, line := range strings.Split(status, "\n") {
			// gitRun trims the output, which can eat the first line's
			// leading status char — parse each line independently of column
			// offsets: <code> <path>, path taken after the first space.
			trimmed := strings.TrimSpace(line)
			i := strings.IndexByte(trimmed, ' ')
			if i <= 0 {
				continue
			}
			code := trimmed[:i]
			path := strings.Trim(strings.TrimSpace(trimmed[i+1:]), `"`)
			if strings.HasPrefix(code, "?") {
				untracked = append(untracked, path)
			} else {
				modified = append(modified, path)
			}
		}
		if len(modified) > 0 {
			blockers = append(blockers,
				"hand-edited contract files — publish would overwrite them, so commit or discard them in the checkout first: "+
					strings.Join(modified, ", "))
		}
	}
	return blockers, untracked
}

// handleAionPublishBaseline preserves pre-existing portal files in their own
// commit ("keep what's there, then take over") — the self-serve fix for the
// untracked-files refusal. It commits the WHOLE public/portal tree verbatim
// (one feature unit — leaving src/ behind while committing data/ would split
// the portal across commits); nothing is written or modified, only added.
func (s *Server) handleAionPublishBaseline(w http.ResponseWriter, r *http.Request) {
	blockers, untracked := s.aionPreflight()
	if len(blockers) > 0 {
		httpError(w, errBadRequest(strings.Join(blockers, "; ")))
		return
	}
	if len(untracked) == 0 {
		httpError(w, errBadRequest("nothing to preserve — the contract paths are clean"))
		return
	}
	p := s.aionPortal
	if _, err := gitRun(p.Path, gitLocalTimeout, "add", "--", "public/portal"); err != nil {
		httpError(w, err)
		return
	}
	if _, err := gitRun(p.Path, gitLocalTimeout, "commit", "-m",
		"aion publish: baseline — preserve pre-manifest portal files (manifest)"); err != nil {
		httpError(w, err)
		return
	}
	commit, _ := gitRun(p.Path, gitLocalTimeout, "rev-parse", "HEAD")
	writeJSON(w, map[string]any{"ok": true, "commit": commit})
}

func (s *Server) aionPreview(publishedAt string) (aionPreviewResult, error) {
	res := aionPreviewResult{}
	res.blockers, res.untracked = s.aionPreflight()
	if len(res.blockers) > 0 && s.aionPortal.Path == "" {
		return res, nil
	}
	rendered, err := aion.RenderContract(s.aionExportInput(publishedAt))
	if err != nil {
		res.blockers = append(res.blockers, err.Error())
		return res, nil
	}
	res.rendered = rendered

	// content hash over everything except meta.json (timestamp churn)
	paths := aion.ContractPaths()
	h := sha256.New()
	for _, p := range paths {
		if strings.HasSuffix(p, "meta.json") {
			continue
		}
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(rendered[p])
	}
	res.hash = hex.EncodeToString(h.Sum(nil))[:16]

	if s.aionPortal.Path != "" {
		for _, p := range paths {
			cur, err := os.ReadFile(filepath.Join(s.aionPortal.Path, filepath.FromSlash(p)))
			f := aionPublishFile{Path: p}
			switch {
			case err != nil:
				f.Status = "new"
				f.Diff = lineDiff(nil, rendered[p])
			case bytes.Equal(cur, rendered[p]):
				f.Status = "unchanged"
			default:
				f.Status = "changed"
				f.Diff = lineDiff(cur, rendered[p])
			}
			if strings.HasSuffix(p, "meta.json") && f.Status == "changed" {
				f.Status = "unchanged" // timestamp-only churn is not a change
				f.Diff = ""
			}
			res.files = append(res.files, f)
		}
		if n, err := gitRun(s.aionPortal.Path, gitLocalTimeout, "rev-list", "--count", "@{u}..HEAD"); err == nil {
			res.unpushed, _ = strconv.Atoi(strings.TrimSpace(n))
		}
	}
	return res, nil
}

func (s *Server) handleAionPublishPreview(w http.ResponseWriter, r *http.Request) {
	res, err := s.aionPreview(time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		httpError(w, err)
		return
	}
	last, commit := s.aionLastPublished()
	// acceptance gate preview: same split the push enforces, so the UI can
	// show format errors (would block) + coverage warnings before publishing.
	gateErrs, gateWarns := aion.AcceptContract(res.rendered, aionInScopeQuarters(time.Now()))
	writeJSON(w, map[string]any{
		"files":         res.files,
		"blockers":      res.blockers,
		"untracked":     res.untracked,
		"hash":          res.hash,
		"unpushed":      res.unpushed,
		"lastPublished": last,
		"lastCommit":    commit,
		"errors":        gateErrs,
		"warnings":      gateWarns,
	})
}

func (s *Server) handleAionPublish(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Hash string `json:"hash"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	publishedAt := time.Now().UTC().Format(time.RFC3339)
	res, err := s.aionPreview(publishedAt)
	if err != nil {
		httpError(w, err)
		return
	}
	fail := func(stage string, err error) {
		rec := aionPublishRecord{
			ID: "pub-" + strconv.FormatInt(time.Now().UnixNano(), 36), Status: "failed",
			Stage: stage, Error: err.Error(), At: publishedAt,
		}
		s.publishLog().append(rec)
		writeJSON(w, map[string]any{"ok": false, "stage": stage, "error": err.Error()})
	}
	if len(res.blockers) > 0 {
		fail("preflight", fmt.Errorf("%s", strings.Join(res.blockers, "; ")))
		return
	}
	if len(res.untracked) > 0 {
		fail("preflight", fmt.Errorf("untracked files inside the contract paths (%s) — preserve them in a baseline commit first (the PUBLISH panel offers this)", strings.Join(res.untracked, ", ")))
		return
	}
	if res.hash != b.Hash {
		http.Error(w, "vault changed since preview — re-preview", http.StatusConflict)
		return
	}
	var changed []string
	for _, f := range res.files {
		if f.Status != "unchanged" {
			changed = append(changed, f.Path)
		}
	}
	if len(changed) == 0 && res.unpushed == 0 {
		httpError(w, errBadRequest("nothing to publish"))
		return
	}

	// Pre-push acceptance gate (sync-contract §4): format errors block the
	// push (no git op, failed receipt); coverage warnings ride the receipt.
	// Runs on the rendered bytes — nothing is written to the checkout yet.
	gateErrs, gateWarns := aion.AcceptContract(res.rendered, aionInScopeQuarters(time.Now()))
	if len(gateErrs) > 0 {
		fail("validate", fmt.Errorf("%s", strings.Join(gateErrs, "; ")))
		return
	}

	p := s.aionPortal
	if len(changed) > 0 {
		// meta.json rides along with every content commit
		writeSet := append(append([]string{}, changed...), "public/portal/data/meta.json")
		sort.Strings(writeSet)
		seen := map[string]bool{}
		for _, rel := range writeSet {
			if seen[rel] {
				continue
			}
			seen[rel] = true
			if err := writeAionContract(p.Path, rel, res.rendered[rel]); err != nil {
				fail("write", err)
				return
			}
		}
		addArgs := append([]string{"add", "--"}, aion.ContractPaths()...)
		if _, err := gitRun(p.Path, gitLocalTimeout, addArgs...); err != nil {
			fail("add", err)
			return
		}
		msg := "aion publish: " + strings.Join(sectionNames(changed), ", ") + " (manifest)"
		if _, err := gitRun(p.Path, gitLocalTimeout, "commit", "-m", msg); err != nil {
			fail("commit", err)
			return
		}
	}
	commit, err := gitRun(p.Path, gitLocalTimeout, "rev-parse", "HEAD")
	if err != nil {
		fail("commit", err)
		return
	}
	if _, err := gitRun(p.Path, gitPushTimeout, "push", p.Remote, p.Branch); err != nil {
		// A remote advanced elsewhere (typically a portal-source commit pushed
		// from another checkout) makes the push non-fast-forward. Publish
		// commits only ever touch the data-contract paths, disjoint from portal
		// source, so replay them onto the remote tip and retry once. A genuine
		// content conflict aborts the rebase cleanly and falls through to the
		// failed-push receipt (unchanged behaviour, still recoverable).
		commit, err = reconcileAndRetryPush(p, commit, err)
		if err != nil {
			// the commit exists locally — record it so the receipt carries the
			// hash and the next publish completes push-only
			rec := aionPublishRecord{
				ID: "pub-" + strconv.FormatInt(time.Now().UnixNano(), 36), Status: "failed",
				Stage: "push", Commit: commit, Files: changed, Error: err.Error(), At: publishedAt,
			}
			s.publishLog().append(rec)
			writeJSON(w, map[string]any{"ok": false, "stage": "push", "error": err.Error(), "commit": commit})
			return
		}
	}
	rec := aionPublishRecord{
		ID: "pub-" + strconv.FormatInt(time.Now().UnixNano(), 36), Status: "ok",
		Commit: commit, Files: changed, At: publishedAt, Acknowledged: true,
	}
	s.publishLog().append(rec)
	writeJSON(w, map[string]any{"ok": true, "commit": commit, "pushed": true, "files": changed, "warnings": gateWarns})
}

// aionInScopeQuarters is the current + next quarter — the window whose live
// rocks the acceptance gate expects to carry start/due (coverage warning).
func aionInScopeQuarters(now time.Time) map[string]bool {
	cur := goals.CurrentQuarter(now)
	return map[string]bool{cur: true, nextQuarter(cur): true}
}

// nextQuarter advances "2026-Q3" → "2026-Q4" → "2027-Q1".
func nextQuarter(q string) string {
	if len(q) != 7 {
		return q
	}
	year, _ := strconv.Atoi(q[:4])
	switch q[6] {
	case '1':
		return fmt.Sprintf("%d-Q2", year)
	case '2':
		return fmt.Sprintf("%d-Q3", year)
	case '3':
		return fmt.Sprintf("%d-Q4", year)
	default:
		return fmt.Sprintf("%d-Q1", year+1)
	}
}

func (s *Server) handleAionPublishAck(w http.ResponseWriter, r *http.Request) {
	if !s.publishLog().ack(r.PathValue("id")) {
		httpError(w, errBadRequest("no such publish record"))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// sectionNames maps changed contract paths to short section names for the
// commit message.
func sectionNames(paths []string) []string {
	var out []string
	for _, p := range paths {
		base := filepath.Base(p)
		out = append(out, strings.TrimSuffix(strings.TrimSuffix(base, ".json"), ".md"))
	}
	sort.Strings(out)
	return out
}

// ---- the publish block for GET /api/aion ----

func (s *Server) aionPublishInfo() map[string]any {
	info := map[string]any{
		"configured": s.aionPortal.Path != "",
		"checkout":   s.aionPortal.Path,
		"remote":     s.aionPortal.Remote,
		"branch":     s.aionPortal.Branch,
	}
	last, commit := s.aionLastPublished()
	info["lastPublished"] = last
	info["lastCommit"] = commit
	info["history"] = s.publishLog().list()
	if s.aionPortal.Path == "" {
		return info
	}
	// per-section dirty dots: current render vs checkout bytes — no stored
	// state, self-healing against external checkout edits
	dirty := map[string]bool{}
	if rendered, err := aion.RenderContract(s.aionExportInput("preview")); err == nil {
		for _, p := range aion.ContractPaths() {
			if strings.HasSuffix(p, "meta.json") {
				continue
			}
			name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(p), ".json"), ".md")
			cur, err := os.ReadFile(filepath.Join(s.aionPortal.Path, filepath.FromSlash(p)))
			dirty[name] = err != nil || !bytes.Equal(cur, rendered[p])
		}
	} else {
		info["renderError"] = err.Error()
	}
	info["dirty"] = dirty
	return info
}

// aionLastPublished is the newest ok receipt, cross-checked against the
// checkout's own git log — a newer external commit wins, annotated.
func (s *Server) aionLastPublished() (string, string) {
	var last, commit string
	for _, r := range s.publishLog().list() {
		if r.Status == "ok" {
			last, commit = r.At, r.Commit
		}
	}
	if s.aionPortal.Path != "" {
		if out, err := gitRun(s.aionPortal.Path, gitLocalTimeout,
			"log", "-1", "--format=%H%x09%cI", "--", "public/portal"); err == nil && out != "" {
			parts := strings.SplitN(out, "\t", 2)
			if len(parts) == 2 && parts[0] != commit {
				if commit == "" || parts[1] > last {
					return parts[1] + " (external)", parts[0]
				}
			}
		}
	}
	return last, commit
}

// ---- minimal line diff (first diff surface in the app) ----

// lineDiff renders a compact unified-style diff of two byte slices. Small
// files only (the contract files) — a simple LCS is plenty.
func lineDiff(a, b []byte) string {
	al := splitLines(a)
	bl := splitLines(b)
	// LCS table
	n, m := len(al), len(bl)
	if n*m > 4_000_000 { // safety valve; contract files are tiny
		return fmt.Sprintf("(diff too large: %d → %d lines)", n, m)
	}
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if al[i] == bl[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case al[i] == bl[j]:
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, "- "+al[i])
			i++
		default:
			out = append(out, "+ "+bl[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "- "+al[i])
	}
	for ; j < m; j++ {
		out = append(out, "+ "+bl[j])
	}
	if len(out) > 200 {
		out = append(out[:200], fmt.Sprintf("… %d more lines", len(out)-200))
	}
	return strings.Join(out, "\n")
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}
