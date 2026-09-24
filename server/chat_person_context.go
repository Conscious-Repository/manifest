package server

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"manifest/artifacts"
)

func (s *Server) chatPersonRecords() ([]chatContextRecord, error) {
	if s.contacts == nil {
		return nil, fmt.Errorf("contacts unavailable")
	}
	people, err := s.contacts.List(time.Now())
	if err != nil {
		return nil, err
	}
	out := []chatContextRecord{}
	for _, p := range people {
		aliases := ""
		if s.index != nil {
			a, err := s.index.EntityAliases(p.Key)
			if err != nil {
				return nil, err
			}
			aliases = strings.Join(a, " ")
		}
		detail := p.Key
		if p.NotePath != "" {
			detail += " · " + p.NotePath
		}
		row := chatContextRecord{Kind: "person", ID: p.Key, Title: p.Display, Detail: detail, Route: "#/contacts/" + url.PathEscape(p.Key), aliases: aliases, profileNotePath: p.NotePath}
		if s.index != nil && p.NotePath != "" {
			unique, err := s.index.UniqueNoteName(p.NotePath)
			if err != nil {
				return nil, err
			}
			if !unique {
				row.ambiguousProfile = true
				row.Route = ""
				row.Detail += " · ambiguous name; select the note by path"
			}
		}
		out = append(out, row)
	}
	return out, nil
}

// Profile note bytes are read through the vault boundary, with no title-based
// resolution. A missing/oversized note cannot become an apparently empty profile.
func (s *Server) personContextNote(path string) ([]byte, error) {
	if s.index == nil {
		return nil, fmt.Errorf("profile note index unavailable")
	}
	full, ok := safeVaultPath(s.index.VaultRoot(), path)
	if !ok {
		return nil, errBadRequest("profile note path unavailable")
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, errBadRequest("profile note unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errBadRequest("profile note unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 64001))
	if err != nil {
		return nil, err
	}
	if len(raw) > 64000 || describeArtifactPreview(artifacts.Hash(raw), raw).Kind != "text" {
		return nil, errBadRequest("profile note exceeds supported text limits")
	}
	return raw, nil
}

func (s *Server) renderPersonContext(out *strings.Builder, selected chatContextRecord) error {
	if selected.ambiguousProfile {
		return errBadRequest("this person name resolves to multiple notes; choose knowledge notes and select the exact path instead")
	}
	var raw []byte
	if selected.profileNotePath != "" {
		var err error
		raw, err = s.personContextNote(selected.profileNotePath)
		if err != nil {
			return err
		}
	}
	p, ok := s.contacts.Page(selected.ID, time.Now())
	if !ok || p.Key != selected.ID || p.NotePath != selected.profileNotePath {
		return errBadRequest("person identity changed; search again")
	}
	contextField(out, "Contact key", p.Key)
	contextField(out, "Profile note", p.NotePath)
	contextField(out, "Role", p.Role)
	contextField(out, "Aliases", strings.Join(p.Aliases, ", "))
	contextField(out, "Linked email addresses", strings.Join(p.Emails, ", "))
	contextField(out, "Location", p.Location.Label)
	contextField(out, "Address", p.Location.Address)
	contextField(out, "Last met (calendar email match)", p.LastMet)
	contextField(out, "Last mentioned (dated note)", p.LastMentioned)
	if p.HasNote {
		fmt.Fprintf(out, "\n## Profile note (exact source)\n\n%s\n", raw)
	}
	if len(p.Firms) > 0 {
		out.WriteString("\n## Other linked entity references\n\n")
		for _, f := range p.Firms {
			fmt.Fprintf(out, "- %s [key: %s]\n", f.Display, f.Key)
		}
	}
	if len(p.Meetings) > 0 {
		out.WriteString("\n## Past meetings (calendar email match)\n\n")
		for _, m := range p.Meetings {
			fmt.Fprintf(out, "- %s · %s\n", m.Date, m.Title)
		}
	}
	if len(p.Upcoming) > 0 {
		out.WriteString("\n## Upcoming meeting matches\n\n")
		for _, m := range p.Upcoming {
			basis := "unconfirmed candidate match"
			if m.Confirmed {
				basis = "confirmed email match"
			}
			fmt.Fprintf(out, "- %s · %s · %s\n", m.Date, m.Title, basis)
		}
	}
	if len(p.Timeline) > 0 {
		out.WriteString("\n## Dated note references\n\n")
		for _, item := range p.Timeline {
			fmt.Fprintf(out, "- %s · %s · %s [source: %s; transcript: %t]\n", item.Date, item.Name, item.Path, item.SourceType, item.IsTranscript)
		}
	}
	if len(p.Mentions) > 0 {
		out.WriteString("\n## Undated note references\n\n")
		for _, m := range p.Mentions {
			fmt.Fprintf(out, "- %s · %s\n", m.Name, m.Path)
		}
	}
	if len(p.Transcripts) > 0 {
		out.WriteString("\n## Transcript references (contents excluded)\n\n")
		for _, tr := range p.Transcripts {
			fmt.Fprintf(out, "- %s · %s · %s [source: %s]\n", tr.Date, tr.Title, tr.Path, tr.Source)
		}
	}
	if len(p.Loops) > 0 {
		out.WriteString("\n## Open meeting follow-ups\n\n")
		for _, group := range p.Loops {
			fmt.Fprintf(out, "Source: %s · %s · %s\n", group.Date, group.Name, group.Path)
			for _, loop := range group.Loops {
				fmt.Fprintf(out, "- %s [line: %d; kind: %s]\n", loop.Text, loop.Line, loop.Kind)
			}
		}
	}
	return nil
}
