package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"manifest/approvals"
	"manifest/realestate"
	"manifest/spirits"
)

// The INTAKE lane (overhaul §5): drop a bid/contract/estimate document on
// the RE overview → the blob lands content-addressed in the CAS → a TEXT
// EXTRACT note is written beside it → the extractor spirit's re-intake
// ritual is spooled with the extract path + the matching context (contractor
// roster, active properties). The ritual files ONE re-contract proposal into
// the approvals inbox; the owner adjusts amounts on the FEED card and
// confirms — the spirit proposes, never decides.

// handleREIntake — POST /api/realestate/intake?name=<orig>: raw body bytes.
func (s *Server) handleREIntake(w http.ResponseWriter, r *http.Request) {
	if s.reFiles == nil || s.realestate == nil {
		http.Error(w, "intake not available", http.StatusServiceUnavailable)
		return
	}
	if s.spirits == nil {
		http.Error(w, "no engine harness configured", http.StatusServiceUnavailable)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		httpError(w, errBadRequest("name is required"))
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, realestate.MaxCASSize))
	if err != nil {
		httpError(w, errBadRequest("upload too large or unreadable"))
		return
	}
	ref, err := s.reFiles.Save(data, name, r.Header.Get("Content-Type"), time.Now())
	if err != nil {
		httpError(w, err)
		return
	}
	// the text extract — the ritual's readable window into the document
	text, how := extractDocText(name, data)
	hash := strings.TrimPrefix(ref.Ref, "sha256:")
	extractRel := path.Join(s.realestateRootOr(), "files", hash+".extract.md")
	extract := "---\ncategories: [re-extract]\nsource: \"" + name + "\"\ndoc: \"" + ref.Ref + "\"\nextracted: " +
		time.Now().Format("2006-01-02") + "\nvia: " + how + "\n---\n\n# extract — " + name + "\n\n" + text + "\n"
	if err := s.vault.WriteCap("re-files", extractRel, []byte(extract)); err != nil {
		httpError(w, err)
		return
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{extractRel})
	}
	req := s.intakeRequest(name, ref.Ref, extractRel)
	if err := s.spirits.SpoolRunNow("extractor", "re-intake", req, ""); err != nil {
		if err == spirits.ErrAlreadyActive {
			httpError(w, errBadRequest("an intake run is already parsing — wait for its proposal, then drop the next document"))
			return
		}
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ref": ref.Ref, "name": ref.Name, "existed": ref.Existed, "spooled": true})
}

// intakeRequest packs the run request: the document, its extract note, and
// the matching context the ritual needs (roster + active properties).
func (s *Server) intakeRequest(name, ref, extractRel string) string {
	var b strings.Builder
	b.WriteString("Parse ONE uploaded document into ONE re-contract proposal.\n")
	b.WriteString("document: " + name + "\n")
	b.WriteString("cas: " + ref + "\n")
	b.WriteString("extract note (vault.read this): " + extractRel + "\n")
	b.WriteString("contractor records (slug — name — scopes):\n")
	for _, c := range s.realestate.Contractors() {
		b.WriteString("- " + c.Slug + " — " + c.Name)
		if len(c.Scopes) > 0 {
			b.WriteString(" — " + strings.Join(c.Scopes, ", "))
		}
		b.WriteString("\n")
	}
	b.WriteString("active properties (slug — address):\n")
	if props, err := s.realestate.Properties(); err == nil {
		for _, p := range props {
			if p.Hidden || (p.Control != "owned" && p.Entity == "") {
				continue
			}
			b.WriteString("- " + p.Slug + " — " + p.Address + "\n")
		}
	}
	// Existing contract records, so the dedupe check is a lookup in the request
	// rather than vault archaeology. The 20260818-233432 protocol failure
	// started exactly there: the ritual said "read the contracts directory",
	// vault.read refuses directories, and the run burned every step searching
	// before collapsing. Data beats instructions.
	b.WriteString("existing contract records (slug — contractor — status — total — properties):\n")
	for _, c := range s.realestate.Contracts() {
		var props []string
		for _, a := range c.Allocations {
			props = append(props, a.Property)
		}
		b.WriteString(fmt.Sprintf("- %s — %s — %s — $%.0f — %s\n",
			c.Slug, c.Contractor, c.Status, c.Total, strings.Join(props, ", ")))
	}
	return b.String()
}

// handleApprovalReContract — POST /api/spirits/approvals/{id}/recontract:
// the FEED card's adjust-amounts edit (rewrites the payload fence).
func (s *Server) handleApprovalReContract(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		http.Error(w, "approvals disabled", http.StatusServiceUnavailable)
		return
	}
	var payload approvals.ReContractPayload
	if err := decode(r, &payload); err != nil {
		httpError(w, err)
		return
	}
	if err := s.approvalsFor(r.PathValue("id")).SetReContractPayload(r.PathValue("id"), payload); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// extractDocText pulls the readable text out of an uploaded document:
// pdftotext for PDFs (when installed), the document.xml strip for docx,
// UTF-8 passthrough for text. Images and failures yield a marker the ritual
// treats as "no text layer — extract nothing you cannot read".
func extractDocText(name string, data []byte) (string, string) {
	const maxLen = 55000 // vault.read truncates at 60k — leave frontmatter room
	clamp := func(s string) string {
		if len(s) > maxLen {
			return s[:maxLen] + "\n\n[truncated]"
		}
		return s
	}
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".pdf":
		if pt, err := exec.LookPath("pdftotext"); err == nil {
			cmd := exec.Command(pt, "-layout", "-", "-")
			cmd.Stdin = bytes.NewReader(data)
			var out bytes.Buffer
			cmd.Stdout = &out
			if cmd.Run() == nil && strings.TrimSpace(out.String()) != "" {
				return clamp(out.String()), "pdftotext"
			}
		}
		return "(no text could be extracted from this PDF — no pdftotext or empty text layer)", "none"
	case ".docx":
		if txt := docxText(data); strings.TrimSpace(txt) != "" {
			return clamp(txt), "docx-xml"
		}
		return "(no text could be extracted from this docx)", "none"
	case ".txt", ".md", ".csv":
		return clamp(string(data)), "verbatim"
	default:
		return "(binary document — no text layer extracted; kind: " + ext + ")", "none"
	}
}

var docxTagRe = regexp.MustCompile(`<[^>]+>`)

// docxText unzips word/document.xml and strips the markup, preserving
// paragraph and tab structure well enough for extraction.
func docxText(data []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 8<<20))
		rc.Close()
		if err != nil {
			return ""
		}
		s := string(raw)
		s = strings.ReplaceAll(s, "</w:p>", "\n")
		s = strings.ReplaceAll(s, "<w:tab/>", "\t")
		s = strings.ReplaceAll(s, "<w:br/>", "\n")
		s = docxTagRe.ReplaceAllString(s, "")
		s = strings.ReplaceAll(s, "&amp;", "&")
		s = strings.ReplaceAll(s, "&lt;", "<")
		s = strings.ReplaceAll(s, "&gt;", ">")
		s = strings.ReplaceAll(s, "&quot;", "\"")
		s = strings.ReplaceAll(s, "&apos;", "'")
		return s
	}
	return ""
}
