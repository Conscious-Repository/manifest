package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"manifest/artifacts"
	"manifest/chatthreads"
	"manifest/extract"
)

// CHAT ATTACHMENTS — a person drops a PDF, a spreadsheet or a photo on a chat
// message; it lands in the shared content-addressed pool as that portal's
// artifact, and the agent gets to read it.
//
// The reference rides the `context` array the composers already send, as
// `file/<sha256>`. That is why nothing in the ChatAsk signature changes: the
// client was already sending structural ids, and resolveChatContext was
// already the place ids become real content.

// UseArtifacts wires the shared attachment pool.
func (s *Server) UseArtifacts(a *artifacts.Store) { s.artifacts = a }

// attachPrefix marks a context id as an attachment reference.
const attachPrefix = "file/"

// attachInlineHead is how much of a document's text rides IN the order. The
// full extract goes to a file in the agent's own harness root, which it opens
// when it needs more — so this is a preview, not the payload.
const attachInlineHead = 2500

// attachTTL prunes materialized copies out of the harness roots. The pool
// keeps the originals; these are working copies for one agent to read.
const attachTTL = 14 * 24 * time.Hour

// uploadExtAllow is what a person may attach. Deliberately narrower than the
// browser's accept list: every entry here is either text we can extract or an
// image the vision endpoint can look at. Anything that executes in a browser
// (.html, .svg) is absent on purpose — these blobs are served back to a team.
var uploadExtAllow = map[string]bool{
	".pdf": true, ".docx": true, ".xlsx": true, ".csv": true, ".tsv": true,
	".txt": true, ".md": true, ".json": true, ".log": true,
	".png": true, ".jpg": true, ".jpeg": true, ".heic": true, ".webp": true, ".gif": true,
}

// inlineOK is the small set we let a browser render in place. Everything else
// downloads, so an uploaded file can never execute in the portal's origin.
var inlineOK = map[string]bool{
	"application/pdf": true, "image/png": true, "image/jpeg": true,
	"image/gif": true, "image/webp": true,
}

// sniffAgrees checks the bytes against the claimed extension. The repo trusts
// client Content-Type everywhere else; on a route any team member can POST to,
// that is not good enough — a .png that is really HTML must not be stored as
// an image and served back inline.
func sniffAgrees(ext, sniffed string) bool {
	fam := func(s string) string {
		if i := strings.IndexByte(s, '/'); i > 0 {
			return s[:i]
		}
		return s
	}
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic":
		// heic sniffs as application/octet-stream on Go's table — allow it
		return fam(sniffed) == "image" || sniffed == "application/octet-stream"
	case ".pdf":
		return sniffed == "application/pdf"
	case ".docx", ".xlsx":
		// zip containers
		return strings.Contains(sniffed, "zip") || sniffed == "application/octet-stream"
	default: // text-ish: reject anything that sniffs binary-executable
		return fam(sniffed) == "text" || sniffed == "application/octet-stream" ||
			strings.HasPrefix(sniffed, "application/json")
	}
}

// handleChatAttach stores one uploaded file and records it as this portal's
// artifact. Raw body + ?name= — the shape handleTaskThreadFile already uses.
func (s *Server) handleChatAttach(domain string, w http.ResponseWriter, r *http.Request, byEmail, byName string) {
	if s.artifacts == nil {
		http.Error(w, "attachments are not configured", http.StatusServiceUnavailable)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	thread := strings.TrimSpace(r.URL.Query().Get("thread"))
	if name == "" {
		httpError(w, errBadRequest("name is required"))
		return
	}
	name = sanitizeUploadName(name)
	ext := strings.ToLower(filepath.Ext(name))
	if !uploadExtAllow[ext] {
		httpError(w, errBadRequest("can't attach "+orStr(ext, "a file with no extension")+
			" — documents (pdf, docx, xlsx, csv, txt, md) and images"))
		return
	}
	// sniff the first 512 bytes, then hand the store an unbroken stream: the
	// head we peeked at, rejoined in front of the rest.
	body := http.MaxBytesReader(w, r.Body, artifacts.MaxBlobSize+1)
	head := make([]byte, 512)
	n, err := io.ReadFull(body, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	if n == 0 {
		httpError(w, errBadRequest("empty file"))
		return
	}
	sniffed := http.DetectContentType(head[:n])
	if !sniffAgrees(ext, sniffed) {
		httpError(w, errBadRequest("that file's contents don't match its "+ext+" extension ("+sniffed+")"))
		return
	}
	ref, err := s.artifacts.Save(io.MultiReader(bytes.NewReader(head[:n]), body), name, sniffed)
	if err != nil {
		httpError(w, errBadRequest(err.Error()))
		return
	}
	if err := s.artifacts.Add(domain, artifacts.Entry{
		Ref: ref, By: byName, ByEmail: byEmail, At: time.Now(), Thread: thread,
	}); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "file": ref, "id": attachPrefix + ref.Hash})
}

// handleChatAttachGet serves an attachment IF this portal owns it. The index
// is the access list: one pool, and an AION hash simply does not exist for an
// OODA partner who somehow learned it.
func (s *Server) handleChatAttachGet(domain string, w http.ResponseWriter, r *http.Request, hash string) {
	if s.artifacts == nil || !s.artifacts.Owns(domain, hash) {
		http.Error(w, "no such file", http.StatusNotFound)
		return
	}
	path := s.artifacts.BlobPath(hash)
	if path == "" {
		http.Error(w, "no such file", http.StatusNotFound)
		return
	}
	meta, _ := s.artifacts.Lookup(domain, hash)
	// never echo the uploader's claimed type, and never let a browser sniff:
	// both are how a stored file becomes stored XSS.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if meta.Mime != "" {
		w.Header().Set("Content-Type", meta.Mime)
	}
	disp := "attachment"
	if inlineOK[meta.Mime] && r.URL.Query().Get("dl") != "1" {
		disp = "inline"
	}
	w.Header().Set("Content-Disposition", disp+"; filename=\""+safeDispositionName(meta.Name)+"\"")
	http.ServeFile(w, r, path)
}

// sanitizeUploadName reduces a filename to its base and strips anything that
// could steer a path or a header.
func sanitizeUploadName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`/:*?"<>|`, r) {
			return ' '
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	for name == "." || name == ".." {
		name = ""
	}
	if len(name) > 120 {
		name = name[len(name)-120:]
	}
	return name
}

// safeDispositionName keeps a filename from breaking out of the header.
func safeDispositionName(n string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == ';' || r == '\\' {
			return '_'
		}
		return r
	}, n)
}

// --- what the agent gets ----------------------------------------------------

// chatAttachments pulls the file/ ids out of a context list and returns the
// ATTACHMENTS block for the work order plus the refs to record on the message.
// budget bounds the block; the full text always reaches the agent as a file in
// its own harness root, so trimming the preview costs nothing.
func (s *Server) chatAttachments(ag *chatAgent, ids []string, budget int) (string, []artifacts.Ref) {
	if s.artifacts == nil {
		return "", nil
	}
	var refs []artifacts.Ref
	for _, id := range ids {
		if !strings.HasPrefix(strings.TrimSpace(id), attachPrefix) {
			continue
		}
		hash := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), attachPrefix))
		if !s.artifacts.Owns(ag.Domain, hash) {
			continue // not this portal's artifact — silently not context
		}
		e, ok := s.artifacts.Lookup(ag.Domain, hash)
		if !ok {
			continue
		}
		refs = append(refs, e.Ref)
	}
	if len(refs) == 0 {
		return "", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ATTACHMENTS (%d) — files a person attached to this message:\n", len(refs))
	// Each attachment gets an equal slice, minus the framing this loop writes
	// around the preview (the name line, the two paths, the fences, the
	// clipped-note). Whatever this returns is clamped to `budget` at the end,
	// so the caller's arithmetic cannot be defeated by a long filename.
	per := budget / len(refs)
	for _, ref := range refs {
		fmt.Fprintf(&b, "- %s · %s · %s\n", ref.Name, orStr(ref.Mime, "unknown type"), humanBytes(ref.Size))
		orig, text := s.materializeAttachment(ag, ref)
		if orig != "" {
			fmt.Fprintf(&b, "  file: %s\n", orig)
		}
		switch {
		case text == "":
			b.WriteString("  (no text layer — open the file above to look at it)\n")
		default:
			if tp := s.attachTextPath(ag, ref); tp != "" {
				fmt.Fprintf(&b, "  full text: %s\n", tp)
			}
			head := text
			cap := per - 600 // the framing lines this loop writes
			if cap < 200 {
				cap = 200
			}
			clipped := false
			if len(head) > cap {
				head, clipped = head[:cap], true
			}
			b.WriteString("  ```\n")
			for _, ln := range strings.Split(strings.TrimRight(head, "\n"), "\n") {
				b.WriteString("  " + ln + "\n")
			}
			b.WriteString("  ```\n")
			if clipped {
				fmt.Fprintf(&b, "  [preview only — %s more characters are in the full-text file above, which you can read]\n",
					strconv.Itoa(len(text)-cap))
			}
		}
	}
	// Hard guarantee: the block never exceeds its budget, whatever the
	// filenames or line structure did above. Without this the composed order
	// sat within single digits of the spool cap, and the next long thread
	// title would have pushed it over — losing the tail of the protocol.
	out := b.String()
	if len(out) > budget {
		cut := budget
		for cut > 0 && !utf8Start(out[cut]) {
			cut--
		}
		out = out[:cut] + "\n[attachment previews clipped to fit this message — the full-text paths above are complete]\n"
	}
	return out, refs
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// attachDir is where an agent's readable copies live, inside its own harness
// tree. zeck's systemd sandbox makes its root the only path it can reach at
// all, so this is not a preference — it is the only place a file can be handed
// over. The path comes from the CONFIGURED harness (spirits.Store.Root), never
// from chatAgent.Root, which is a display string for the engine sidebar.
func (s *Server) attachDir(ag *chatAgent) string {
	if ag == nil {
		return ""
	}
	h := s.findHarness(ag.Name)
	if h == nil || h.Spirits == nil || h.Spirits.Root() == "" {
		return ""
	}
	return filepath.Join(h.Spirits.Root(), "artifacts", "attach")
}

func (s *Server) attachTextPath(ag *chatAgent, ref artifacts.Ref) string {
	if s.attachDir(ag) == "" {
		return ""
	}
	return filepath.Join(s.attachDir(ag), ref.Hash+".txt")
}

// materializeAttachment copies the original into the agent's harness root and
// writes its extracted text beside it. Returns (originalPath, text).
//
// The original goes over too, not just the text: images have no text layer and
// the serving endpoint can look at them, so the agent needs the actual file.
func (s *Server) materializeAttachment(ag *chatAgent, ref artifacts.Ref) (string, string) {
	src := s.artifacts.BlobPath(ref.Hash)
	if src == "" {
		return "", ""
	}
	// the extract is cached per blob — the same document attached twice, by
	// anyone, in either portal, is only ever read once
	text, ok := s.artifacts.Extract(ref.Hash)
	if !ok {
		data, err := os.ReadFile(src)
		if err != nil {
			return "", ""
		}
		res := extract.DocLimit(ref.Name, data, extract.MaxStored)
		if res.HasText {
			text = res.Text
			_ = s.artifacts.PutExtract(ref.Hash, text)
		}
	}
	dir := s.attachDir(ag)
	if dir == "" {
		return "", text
	}
	if err := os.MkdirAll(dir, 0o775); err != nil {
		return "", text
	}
	s.pruneAttachDir(dir)
	dst := filepath.Join(dir, ref.Hash+artifacts.SafeExt(ref.Name))
	if _, err := os.Stat(dst); err != nil {
		if data, err := os.ReadFile(src); err == nil {
			if err := writeFileAtomic(dst, data, 0o664); err != nil {
				return "", text
			}
		}
	}
	if text != "" {
		tp := s.attachTextPath(ag, ref)
		if _, err := os.Stat(tp); err != nil {
			_ = writeFileAtomic(tp, []byte(text), 0o664)
		}
	}
	return dst, text
}

// pruneAttachDir drops working copies older than attachTTL. The pool keeps the
// originals; nothing is lost, and a harness root does not grow without bound.
func (s *Server) pruneAttachDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-attachTTL)
	for _, e := range entries {
		fi, err := e.Info()
		if err == nil && !fi.IsDir() && fi.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// chatMessageFiles resolves the file/ ids on an order into the refs the thread
// renders as chips. Display only — the agent's copy is composed separately.
func (s *Server) chatMessageFiles(ag *chatAgent, ids []string) []chatthreads.FileRef {
	if s.artifacts == nil || ag == nil {
		return nil
	}
	var out []chatthreads.FileRef
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !strings.HasPrefix(id, attachPrefix) {
			continue
		}
		hash := strings.TrimPrefix(id, attachPrefix)
		if e, ok := s.artifacts.Lookup(ag.Domain, hash); ok {
			out = append(out, chatthreads.FileRef{
				Hash: e.Hash, Name: e.Name, Size: e.Size, Mime: e.Mime,
			})
		}
	}
	return out
}

// The two portals' entry points. Each names its own artifact domain, which is
// what keeps one shared pool from resolving the other business's files.
func (s *Server) AionChatAttach(w http.ResponseWriter, r *http.Request, email, name string) {
	s.handleChatAttach("aion", w, r, email, name)
}
func (s *Server) AionChatAttachGet(w http.ResponseWriter, r *http.Request, hash string) {
	s.handleChatAttachGet("aion", w, r, hash)
}
func (s *Server) OodaChatAttach(w http.ResponseWriter, r *http.Request, email, name string) {
	s.handleChatAttach("ooda", w, r, email, name)
}
func (s *Server) OodaChatAttachGet(w http.ResponseWriter, r *http.Request, hash string) {
	s.handleChatAttachGet("ooda", w, r, hash)
}
