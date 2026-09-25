package server

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

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
		id := recruiting.CandidateID(strings.TrimSuffix(entry.Name(), ".md"))
		row := chatContextRecord{Kind: "candidate", ID: id, Title: entry.Name(), Detail: id + " · unavailable", candidateSource: s.recruiting.Rel(rel)}
		raw, err := readCandidateContextSource(root, rel, id)
		if err != nil {
			row.contextError = err.Error()
			row.Detail += ": " + row.contextError
		} else {
			d := recruiting.ParseCandidate(string(raw))
			row.Title = d.Get("name")
			row.Detail = id + " · " + d.Get("stage") + " · " + d.Get("role")
			row.Route = candidateContextRoute(id)
			row.candidateText = string(raw)
		}
		out = append(out, row)
	}
	return out, nil
}
func candidateContextRoute(id string) string {
	return "#/aion/recruiting/candidate/" + url.PathEscape(id)
}

// Fail one source independently. Search may expose its canonical filename and
// unavailable status, but no record body or guessed frontmatter identity.
func readCandidateContextSource(root, rel, id string) ([]byte, error) {
	full, ok := safeVaultPath(root, rel)
	if !ok {
		return nil, errBadRequest("candidate source path unavailable")
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, errBadRequest("candidate source cannot be read")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errBadRequest("candidate source unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 64001))
	if err != nil {
		return nil, errBadRequest("candidate source cannot be read")
	}
	if len(raw) > 64000 {
		return nil, errBadRequest("candidate record exceeds supported text limits")
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return nil, errBadRequest("candidate source is not supported text")
	}
	d := recruiting.ParseCandidate(string(raw))
	if d.Get("id") != id || d.Get("name") == "" {
		return nil, errBadRequest("candidate record identity mismatch")
	}
	return raw, nil
}
