package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"manifest/artifacts"
	"manifest/extract"
)

// THE APPLICANT'S OWN SUBMISSION — resumes and structured answers, pulled from
// Ashby on the owner's action and rendered here (owner ask 2026-09-04).
//
// ⚠ WHERE THE BYTES LIVE. A resume is PII: it goes into the artifacts pool
// (dataDir, outside the vault) exactly like a chat attachment, and the vault
// record keeps a REFERENCE — the hash and the applicant's own filename. The
// per-domain index is the access list, so a hash from another domain does not
// exist on this route.
//
// ⚠ WHY IT IS PULLED, NOT MIRRORED. application.list omits resumeFileHandle
// entirely (only application.info carries it), so mirroring every resume would
// mean one extra API call per applicant on every sync AND ~60 strangers' PII
// copied onto this box before anyone looked at them. One click, one applicant.

// recruitingArtifactDomain is the artifact index that owns candidate files.
const recruitingArtifactDomain = "recruiting"

// maxResumeBytes bounds one download. Ashby's own upload cap is well under
// this; the bound exists so a wrong URL cannot fill a disk.
const maxResumeBytes = 25 << 20

// POST …/ashby/detail/{id} — read one linked candidate's application in full
// and, when it carries a file, store that file and stamp it on the record.
func (s *Server) handleRecruitingAshbyDetail(w http.ResponseWriter, r *http.Request) {
	if !s.ashbyReady(w) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	ctx, cancel := ashbyCtx(r)
	defer cancel()

	detail, err := s.ashbySync.Detail(ctx, id)
	if err != nil {
		ashbyError(w, err)
		return
	}
	out := map[string]any{"detail": detail}
	if detail.ResumeURL != "" {
		ref, hasText, err := s.storeAshbyResume(ctx, detail.ResumeName, detail.ResumeURL)
		if err != nil {
			// the application facts are still worth returning — the failure
			// names itself beside them rather than replacing them
			out["resumeError"] = err.Error()
		} else {
			if err := s.ashbySync.RecordResume(id, ref.Name, ref.Hash, time.Now()); err != nil {
				httpError(w, err)
				return
			}
			out["resume"] = map[string]any{
				"hash": ref.Hash, "name": ref.Name, "size": ref.Size,
				"mime": ref.Mime, "hasText": hasText,
			}
		}
	}
	out["view"] = s.recruiting.View()
	writeJSON(w, out)
}

// storeAshbyResume downloads one file from its short-lived signed URL into the
// artifacts pool and extracts its text. The stored mime is SNIFFED, never
// claimed: a file that is really HTML must not be stored as a PDF and served
// back inline (the same rule the chat-upload route enforces).
func (s *Server) storeAshbyResume(ctx context.Context, name, url string) (artifacts.Ref, bool, error) {
	if s.artifacts == nil {
		return artifacts.Ref{}, false, fmt.Errorf("artifact storage is unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return artifacts.Ref{}, false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return artifacts.Ref{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return artifacts.Ref{}, false, fmt.Errorf("the file store answered HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResumeBytes+1))
	if err != nil {
		return artifacts.Ref{}, false, err
	}
	if len(data) > maxResumeBytes {
		return artifacts.Ref{}, false, fmt.Errorf("that file is larger than the %d MB limit", maxResumeBytes>>20)
	}
	if len(data) == 0 {
		return artifacts.Ref{}, false, fmt.Errorf("the file store returned nothing")
	}
	if name = sanitizeUploadName(name); name == "" {
		name = "resume.pdf"
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	ref, err := s.artifacts.Save(bytes.NewReader(data), name, http.DetectContentType(head))
	if err != nil {
		return artifacts.Ref{}, false, err
	}
	if err := s.artifacts.Add(recruitingArtifactDomain, artifacts.Entry{
		Ref: ref, By: "ashby", At: time.Now(),
	}); err != nil {
		return artifacts.Ref{}, false, err
	}
	// the text is what makes a resume READ here rather than merely download
	res := extract.DocLimit(name, data, extract.MaxStored)
	if res.HasText {
		_ = s.artifacts.PutExtract(ref.Hash, res.Text)
	}
	return ref, res.HasText, nil
}

// GET …/recruiting/resume/{hash} — the stored file itself. The recruiting
// index is the access list; a hash it does not own has no answer here.
func (s *Server) handleRecruitingResume(w http.ResponseWriter, r *http.Request) {
	if s.recruiting == nil {
		http.Error(w, "recruiting unavailable", http.StatusServiceUnavailable)
		return
	}
	hash := strings.TrimSpace(r.PathValue("hash"))
	if strings.HasSuffix(hash, "/text") {
		s.serveResumeText(w, strings.TrimSuffix(hash, "/text"))
		return
	}
	// the same serve the chat attachments use: nosniff, the SNIFFED mime, and
	// attachment disposition for anything not known-safe to render inline
	s.handleChatAttachGet(recruitingArtifactDomain, w, r, hash)
}

// serveResumeText answers the extracted text, for rendering the resume in the
// inspector without handing the browser a PDF to paint.
func (s *Server) serveResumeText(w http.ResponseWriter, hash string) {
	if s.artifacts == nil || !s.artifacts.Owns(recruitingArtifactDomain, hash) {
		http.Error(w, "no such file", http.StatusNotFound)
		return
	}
	text, ok := s.artifacts.Extract(hash)
	meta, _ := s.artifacts.Lookup(recruitingArtifactDomain, hash)
	writeJSON(w, map[string]any{
		"hash": hash, "name": meta.Name, "hasText": ok, "text": text,
	})
}

