package server

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"manifest/recruiting"
)

// Candidate context is the exact selected canonical record, including its
// evidence references and owner notes. Referenced files are never expanded.
func (s *Server) chatCandidateRecords() ([]chatContextRecord, error) {
	if s.recruiting == nil {
		return nil, fmt.Errorf("recruiting records unavailable")
	}
	root := s.recruiting.Path("")
	entries, err := os.ReadDir(s.recruiting.Path("candidates"))
	if err != nil {
		return nil, err
	}
	out := []chatContextRecord{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		rel := "candidates/" + entry.Name()
		full, ok := safeVaultPath(root, rel)
		if !ok {
			return nil, errBadRequest("candidate source path unavailable")
		}
		f, err := os.Open(full)
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			f.Close()
			return nil, errBadRequest("candidate source unavailable")
		}
		raw, err := io.ReadAll(io.LimitReader(f, 64001))
		f.Close()
		if err != nil {
			return nil, err
		}
		if len(raw) > 64000 {
			return nil, errBadRequest("candidate record exceeds supported text limits")
		}
		d := recruiting.ParseCandidate(string(raw))
		slug := strings.TrimSuffix(entry.Name(), ".md")
		id := recruiting.CandidateID(slug)
		if d.Get("id") != id || d.Get("name") == "" {
			return nil, errBadRequest("candidate record identity mismatch")
		}
		out = append(out, chatContextRecord{Kind: "candidate", ID: id, Title: d.Get("name"), Detail: id + " · " + d.Get("stage") + " · " + d.Get("role"), Route: candidateContextRoute(id), candidateSource: s.recruiting.Rel(rel), candidateText: string(raw)})
	}
	return out, nil
}
func candidateContextRoute(id string) string {
	return "#/aion/recruiting/candidate/" + url.PathEscape(id)
}
