package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"manifest/realestate"
)

// The RE publish effector — the AION gesture pointed at the ooda portal
// (RE spec §4). TWO contract paths: src/data/deals.json (recomposed from the
// vault's source sidecars — the checkout's array owns order) and
// src/engine/defaults.js (assumptions.md rendered over the checkout's module,
// byte-stable so a no-edit publish diffs empty). PREVIEW writes nothing;
// CONFIRM carries the preview's content hash, writes ONLY contract paths
// through the canary chokepoint, then git add/commit/push + a permanent
// receipt. A commit whose push failed surfaces as `unpushed` and the next
// publish completes push-only. Manifest owns inputs + assumptions; the portal
// runs the engine — no calc function ever crosses this boundary.

// reContractPaths is the compiled allow-list — the ONLY paths the effector
// may touch in the checkout.
var reContractPaths = []string{"src/data/deals.json", "src/engine/defaults.js"}

// rePortalCfg reuses the aion coordinates shape: the re-portal checkout on
// origin/main (Cloudflare builds from main).
func (s *Server) rePortalCfg() aionPortalCfg {
	return aionPortalCfg{Path: s.rePortalPath, Remote: "origin", Branch: "main"}
}

func (s *Server) rePubLog() *aionPublishLog {
	if s.rePublishes == nil {
		s.rePublishes = &aionPublishLog{path: filepath.Join(s.aionDataDir, "realestate", "publishes.json")}
	}
	return s.rePublishes
}

// writeReContract is the canary chokepoint — the only function that writes
// into the re-portal checkout; anything off the contract list is refused.
func writeReContract(checkoutRoot, rel string, data []byte) error {
	clean := filepath.ToSlash(filepath.Clean(rel))
	allowed := false
	for _, p := range reContractPaths {
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

// reRenderContract renders both contract files in memory against the CURRENT
// checkout bytes (deals order + defaults formatting both derive from them).
func (s *Server) reRenderContract() (map[string][]byte, []string, error) {
	p := s.rePortalCfg()
	tmplRaw, err := os.ReadFile(filepath.Join(p.Path, "src", "data", "deals.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("no deals.json in the checkout — is rePortalPath a re-portal clone?")
	}
	var template []json.RawMessage
	if err := json.Unmarshal(tmplRaw, &template); err != nil {
		return nil, nil, fmt.Errorf("checkout deals.json is not a JSON array: %v", err)
	}
	out, _, _, kept, err := s.reComposeDeals(template)
	if err != nil {
		return nil, nil, err
	}
	pretty, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	curDef, err := os.ReadFile(filepath.Join(p.Path, "src", "engine", "defaults.js"))
	if err != nil {
		return nil, nil, fmt.Errorf("no src/engine/defaults.js in the checkout")
	}
	def, err := realestate.RenderPortalDefaults(curDef, s.loadAssumptions().Values)
	if err != nil {
		return nil, nil, err
	}
	return map[string][]byte{
		"src/data/deals.json":    append(pretty, '\n'),
		"src/engine/defaults.js": def,
	}, kept, nil
}

// rePreflight mirrors aionPreflight scoped to the TWO contract paths — both
// are tracked portal files, so ANY porcelain status on them is a hard block
// (publish would clobber or sweep hand edits).
func (s *Server) rePreflight() []string {
	p := s.rePortalCfg()
	if p.Path == "" {
		return []string{"rePortalPath not configured"}
	}
	if fi, err := os.Stat(p.Path); err != nil || !fi.IsDir() {
		return []string{fmt.Sprintf("checkout %s not found", p.Path)}
	}
	if _, err := gitRun(p.Path, gitLocalTimeout, "rev-parse", "--git-dir"); err != nil {
		return []string{"not a git repository: " + p.Path}
	}
	var blockers []string
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
	statusArgs := append([]string{"status", "--porcelain", "--"}, reContractPaths...)
	if status, err := gitRun(p.Path, gitLocalTimeout, statusArgs...); err != nil {
		blockers = append(blockers, "git status failed: "+err.Error())
	} else if status != "" {
		var touched []string
		for _, line := range strings.Split(status, "\n") {
			trimmed := strings.TrimSpace(line)
			if i := strings.IndexByte(trimmed, ' '); i > 0 {
				touched = append(touched, strings.Trim(strings.TrimSpace(trimmed[i+1:]), `"`))
			}
		}
		if len(touched) > 0 {
			blockers = append(blockers,
				"hand-edited contract files — publish would overwrite them, so commit or discard them in the checkout first: "+
					strings.Join(touched, ", "))
		}
	}
	return blockers
}

type rePreviewResult struct {
	files    []aionPublishFile
	blockers []string
	kept     []string
	hash     string
	unpushed int
	rendered map[string][]byte
}

func (s *Server) rePreview() rePreviewResult {
	res := rePreviewResult{blockers: s.rePreflight()}
	p := s.rePortalCfg()
	if p.Path == "" {
		return res
	}
	rendered, kept, err := s.reRenderContract()
	if err != nil {
		res.blockers = append(res.blockers, err.Error())
		return res
	}
	res.rendered, res.kept = rendered, kept

	h := sha256.New()
	for _, path := range reContractPaths {
		h.Write([]byte(path))
		h.Write([]byte{0})
		h.Write(rendered[path])
	}
	res.hash = hex.EncodeToString(h.Sum(nil))[:16]

	for _, path := range reContractPaths {
		cur, err := os.ReadFile(filepath.Join(p.Path, filepath.FromSlash(path)))
		f := aionPublishFile{Path: path}
		switch {
		case err != nil:
			f.Status = "new"
			f.Diff = lineDiff(nil, rendered[path])
		case bytes.Equal(cur, rendered[path]):
			f.Status = "unchanged"
		default:
			f.Status = "changed"
			f.Diff = lineDiff(cur, rendered[path])
		}
		res.files = append(res.files, f)
	}
	if n, err := gitRun(p.Path, gitLocalTimeout, "rev-list", "--count", "@{u}..HEAD"); err == nil {
		res.unpushed, _ = strconv.Atoi(strings.TrimSpace(n))
	}
	return res
}

func (s *Server) handleRePublishPreview(w http.ResponseWriter, r *http.Request) {
	res := s.rePreview()
	last, commit := s.reLastPublished()
	writeJSON(w, map[string]any{
		"files":         res.files,
		"blockers":      res.blockers,
		"kept":          res.kept,
		"hash":          res.hash,
		"unpushed":      res.unpushed,
		"lastPublished": last,
		"lastCommit":    commit,
	})
}

func (s *Server) handleRePublish(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Hash string `json:"hash"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	publishedAt := time.Now().UTC().Format(time.RFC3339)
	res := s.rePreview()
	fail := func(stage string, err error) {
		s.rePubLog().append(aionPublishRecord{
			ID: "repub-" + strconv.FormatInt(time.Now().UnixNano(), 36), Status: "failed",
			Stage: stage, Error: err.Error(), At: publishedAt,
		})
		writeJSON(w, map[string]any{"ok": false, "stage": stage, "error": err.Error()})
	}
	if len(res.blockers) > 0 {
		fail("preflight", fmt.Errorf("%s", strings.Join(res.blockers, "; ")))
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
	p := s.rePortalCfg()
	if len(changed) > 0 {
		for _, rel := range changed {
			if err := writeReContract(p.Path, rel, res.rendered[rel]); err != nil {
				fail("write", err)
				return
			}
		}
		addArgs := append([]string{"add", "--"}, reContractPaths...)
		if _, err := gitRun(p.Path, gitLocalTimeout, addArgs...); err != nil {
			fail("add", err)
			return
		}
		msg := "re publish: " + strings.Join(sectionNames(changed), ", ") + " (manifest)"
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
		// a remote advanced elsewhere (e.g. a portal-source commit) makes this
		// non-fast-forward; publish commits touch only the data-contract paths,
		// so rebase onto the remote tip and retry once (self-healing) — a real
		// conflict aborts clean and falls through to the failed-push receipt.
		commit, err = reconcileAndRetryPush(p, commit, err)
		if err != nil {
			s.rePubLog().append(aionPublishRecord{
				ID: "repub-" + strconv.FormatInt(time.Now().UnixNano(), 36), Status: "failed",
				Stage: "push", Commit: commit, Files: changed, Error: err.Error(), At: publishedAt,
			})
			writeJSON(w, map[string]any{"ok": false, "stage": "push", "error": err.Error(), "commit": commit})
			return
		}
	}
	s.rePubLog().append(aionPublishRecord{
		ID: "repub-" + strconv.FormatInt(time.Now().UnixNano(), 36), Status: "ok",
		Commit: commit, Files: changed, At: publishedAt, Acknowledged: true,
	})
	writeJSON(w, map[string]any{"ok": true, "commit": commit, "pushed": true, "files": changed})
}

func (s *Server) handleRePublishAck(w http.ResponseWriter, r *http.Request) {
	if !s.rePubLog().ack(r.PathValue("id")) {
		httpError(w, errBadRequest("no such publish record"))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// rePublishInfo is the rail's publish block: configured + per-file dirty dots
// (current render vs checkout bytes — no stored state) + receipts.
func (s *Server) rePublishInfo() map[string]any {
	info := map[string]any{
		"configured": s.rePortalPath != "",
		"checkout":   s.rePortalPath,
	}
	last, commit := s.reLastPublished()
	info["lastPublished"] = last
	info["lastCommit"] = commit
	info["history"] = s.rePubLog().list()
	if s.rePortalPath == "" {
		return info
	}
	dirty := map[string]bool{}
	if rendered, _, err := s.reRenderContract(); err == nil {
		for _, path := range reContractPaths {
			name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".json"), ".js")
			cur, err := os.ReadFile(filepath.Join(s.rePortalPath, filepath.FromSlash(path)))
			dirty[name] = err != nil || !bytes.Equal(cur, rendered[path])
		}
	} else {
		info["renderError"] = err.Error()
	}
	info["dirty"] = dirty
	return info
}

// reLastPublished is the newest ok receipt, cross-checked against the
// checkout's own log over the contract paths — a newer external commit wins.
func (s *Server) reLastPublished() (string, string) {
	var last, commit string
	for _, r := range s.rePubLog().list() {
		if r.Status == "ok" {
			last, commit = r.At, r.Commit
		}
	}
	if s.rePortalPath != "" {
		logArgs := append([]string{"log", "-1", "--format=%H%x09%cI", "--"}, reContractPaths...)
		if out, err := gitRun(s.rePortalPath, gitLocalTimeout, logArgs...); err == nil && out != "" {
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
