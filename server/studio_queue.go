package server

import (
	"net/http"
	"strings"

	"manifest/vaultwriter"
)

// Content Studio — the Queue tab (iteration-2 §1/§3): a live, editable view of the
// vault's x posts.md (# drafts / # queue / # posted). Edits are user actions
// (the app's explicit-save exception) — guarded, byte-preserving, and conflict-safe
// (replace-by-exact-match; a file changed underneath → clean refusal + reload).

// handleStudioQueue serves the three parsed sections plus migration state.
func (s *Server) handleStudioQueue(w http.ResponseWriter, r *http.Request) {
	if s.studio == nil || s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "studio/vault unavailable", http.StatusServiceUnavailable)
		return
	}
	b, err := s.vault.ReadVaultFile(s.xPostsFile)
	content := ""
	if err == nil {
		content = string(b)
	}
	doc := vaultwriter.ParseXPosts(content)
	resp := map[string]any{"sections": doc, "xPostsFile": s.xPostsFile}
	if doc.NeedsMigration {
		if preview, ok := vaultwriter.PreviewMigration(content); ok {
			resp["migrationPreview"] = preview
			resp["migrationCurrent"] = content
		}
	}
	writeJSON(w, resp)
}

// handleStudioMigrate restructures x posts.md to # drafts / # queue / # posted.
func (s *Server) handleStudioMigrate(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.vault.MigrateXPosts(s.xPostsFile); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleStudioBullet handles inline edit / delete / add on a # drafts or # queue
// bullet. The path {op} is edit|delete|add.
func (s *Server) handleStudioBullet(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Section     string `json:"section"`
		Original    string `json:"original"`
		Replacement string `json:"replacement"`
		Bullet      string `json:"bullet"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	var err error
	switch r.PathValue("op") {
	case "edit":
		err = s.vault.ReplaceBullet(s.xPostsFile, b.Section, ensureBullet(b.Original), ensureBullet(b.Replacement))
	case "delete":
		err = s.vault.DeleteBullet(s.xPostsFile, b.Section, ensureBullet(b.Bullet))
	case "add":
		err = s.vault.AddBullet(s.xPostsFile, b.Section, ensureBullet(b.Bullet))
	default:
		httpError(w, errBadRequest("unknown bullet op"))
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleStudioQueuePosted moves a # queue bullet to # posted and, if it matches a
// studio draft, stamps that draft posted (+ records the queued-final text as tune
// evidence, §9).
func (s *Server) handleStudioQueuePosted(w http.ResponseWriter, r *http.Request) {
	if s.studio == nil || s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "studio/vault unavailable", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Bullet string `json:"bullet"`
		URL    string `json:"url"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	bullet := ensureBullet(b.Bullet)
	if err := s.vault.MoveBulletToPosted(s.xPostsFile, bullet); err != nil {
		httpError(w, err)
		return
	}
	// best-effort draft linkage: a draft whose effective text equals the bullet
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(bullet), "- "))
	for _, d := range s.studio.List() {
		if strings.TrimSpace(d.Effective()) == text {
			_, _ = s.studio.MarkPosted(d.ID, b.URL)
			if strings.TrimSpace(d.Effective()) != strings.TrimSpace(d.Text) {
				_, _ = s.studio.SetQueuedFinal(d.ID, text) // §9: the edit delta is the purest voice signal
			}
			break
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleStudioConsumeSeed removes a seed-developed draft's original # drafts bullet
// (§1) — called after approving, when the owner leaves "consume seed" checked.
func (s *Server) handleStudioConsumeSeed(w http.ResponseWriter, r *http.Request) {
	if s.studio == nil || s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "studio/vault unavailable", http.StatusServiceUnavailable)
		return
	}
	d, ok := s.studio.Get(r.PathValue("id"))
	if !ok {
		http.Error(w, "draft not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(d.Seed) == "" {
		writeJSON(w, map[string]bool{"ok": true, "noSeed": true})
		return
	}
	if err := s.vault.DeleteBullet(s.xPostsFile, "drafts", ensureBullet(d.Seed)); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ensureBullet normalizes a bullet body to a "- …" line (the editor sends the text;
// the file stores it with the dash).
func ensureBullet(s string) string {
	t := strings.TrimRight(s, "\n")
	if strings.HasPrefix(strings.TrimSpace(t), "-") {
		return t
	}
	return "- " + strings.TrimSpace(t)
}
