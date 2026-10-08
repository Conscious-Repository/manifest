package server

// Construction inputs: bounded, sniffed owner uploads retained as immutable
// private artifacts, and membership-checked downloads. Uploads land only in
// the construction pool — never the shared chat pool, the property docs
// directory or the RE CAS — and the client's Content-Type is never trusted.

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"manifest/construction"
)

// constructionInputTypes is the private construction upload allowlist: still
// images, PDF and plain text. Active or container formats (HTML, SVG, XML,
// archives, executables) are refused — there is no archive auto-extract and no
// external resource resolution anywhere downstream.
var constructionInputTypes = map[string]string{
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/webp":      ".webp",
	"image/gif":       ".gif",
	"application/pdf": ".pdf",
	"text/plain":      ".txt",
}

// sniffConstructionInput classifies bytes by content, not by name or header.
func sniffConstructionInput(b []byte) (string, bool) {
	ct := http.DetectContentType(b)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(ct)
	if ct == "text/plain" {
		// DetectContentType calls SVG/markup "text/xml" or "text/html", but a
		// file that merely starts with whitespace could slip through as plain
		// text: refuse anything that looks like markup at all.
		head := strings.ToLower(string(b[:min(len(b), 1024)]))
		for _, marker := range []string{"<svg", "<html", "<script", "<?xml", "<!doctype", "<iframe", "<object"} {
			if strings.Contains(head, marker) {
				return ct, false
			}
		}
	}
	_, ok := constructionInputTypes[ct]
	return ct, ok
}

func (s *Server) handleConstructionInput(w http.ResponseWriter, r *http.Request) {
	sub, actor, ok := s.constructionBegin(w, r, true)
	if !ok {
		return
	}
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, construction.MaxInputBytes+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		constructionError(w, construction.Invalid("send multipart/form-data with fields requestId, expectedProblemRevision, role, label and one file"))
		return
	}
	fields := map[string]string{}
	var content []byte
	var name string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				constructionError(w, construction.ErrTooLarge)
				return
			}
			constructionError(w, construction.Invalid("malformed upload"))
			return
		}
		if part.FileName() != "" {
			if content != nil {
				constructionError(w, construction.Invalid("one file per upload"))
				return
			}
			b, err := io.ReadAll(io.LimitReader(part, construction.MaxInputBytes+1))
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					constructionError(w, construction.ErrTooLarge)
					return
				}
				constructionError(w, construction.Invalid("could not read the file"))
				return
			}
			if len(b) > construction.MaxInputBytes {
				constructionError(w, construction.ErrTooLarge)
				return
			}
			content, name = b, filepath.Base(strings.ReplaceAll(part.FileName(), `\`, "/"))
			continue
		}
		v, err := io.ReadAll(io.LimitReader(part, 4096))
		if err != nil {
			constructionError(w, construction.Invalid("malformed field"))
			return
		}
		fields[part.FormName()] = string(v)
	}
	if content == nil {
		constructionError(w, construction.Invalid("no file in the upload"))
		return
	}
	mimeType, allowed := sniffConstructionInput(content)
	if !allowed {
		constructionError(w, construction.Invalid("file type "+mimeType+" is not accepted for construction inputs (images, PDF or plain text only)"))
		return
	}
	role := fields["role"]
	if role == "" {
		role = map[bool]string{true: "photo", false: "document"}[strings.HasPrefix(mimeType, "image/")]
	}
	st, rc, err := s.construction.store.RetainInput(sub, id, construction.InputUpload{
		RequestID: fields["requestId"], ExpectedProblemRevision: fields["expectedProblemRevision"],
		Name: name, Mime: mimeType, Role: role, Label: fields["label"], Verification: fields["verification"], Content: content,
	}, actor)
	if err != nil {
		constructionError(w, err)
		return
	}
	constructionJSON(w, map[string]any{"receipt": rc, "view": s.constructionView(sub, st)})
}

// handleConstructionArtifact serves one artifact revision that this problem's
// membership lists: inputs, retained sources and derived exports. A content
// hash alone grants nothing; another project's identical bytes stay 404.
func (s *Server) handleConstructionArtifact(w http.ResponseWriter, r *http.Request) {
	sub, _, ok := s.constructionBegin(w, r, false)
	if !ok {
		return
	}
	id, artifactID, rev := r.PathValue("id"), r.PathValue("artifact"), r.URL.Query().Get("revision")
	st, err := s.construction.store.Load(sub, id)
	if err != nil {
		constructionError(w, err)
		return
	}
	b, err := s.construction.store.Content(sub, id, artifactID, rev)
	if err != nil {
		constructionError(w, err)
		return
	}
	name, mimeType := constructionArtifactMeta(st, artifactID, rev)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	// never trust a stored mime for active content: only the allowlisted
	// types render inline; everything else downloads
	disposition := "attachment"
	if strings.HasPrefix(mimeType, "image/") && mimeType != "image/svg+xml" {
		disposition = "inline"
	}
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+rev+`"`)
	_, _ = w.Write(b)
}

// constructionArtifactMeta finds the reference metadata (name, mime) for an
// artifact revision among the problem's inputs and derived records.
func constructionArtifactMeta(st *construction.State, artifactID, rev string) (string, string) {
	if p := st.Problem; p != nil {
		for _, in := range p.Inputs {
			if in.ArtifactID == artifactID && in.Revision == rev {
				return in.Name, in.Mime
			}
		}
	}
	if d := st.Derived; d != nil {
		for _, a := range d.Artifacts {
			if a.ArtifactID == artifactID && a.Revision == rev {
				return a.Name, derivedMime(a.Format)
			}
		}
	}
	if e := st.Evidence; e != nil {
		for _, src := range e.Sources {
			if src.Snapshot != nil && src.Snapshot.ID == artifactID && src.Snapshot.Revision == rev {
				return src.Title, src.Mime
			}
		}
	}
	return rev[:12], "application/octet-stream"
}

func derivedMime(format string) string {
	switch format {
	case "glb":
		return "model/gltf-binary"
	case "svg":
		return "image/svg+xml"
	case "pdf":
		return "application/pdf"
	case "png":
		return "image/png"
	case "package", "backup":
		return "application/zip"
	case "json":
		return "application/json"
	}
	return "application/octet-stream"
}
