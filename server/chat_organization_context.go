package server

import (
	"fmt"
	"net/url"
	"strings"
)

func (s *Server) chatOrganizationRecords() ([]chatContextRecord, error) {
	if s.contacts == nil || s.index == nil {
		return nil, fmt.Errorf("organization records unavailable")
	}
	out := []chatContextRecord{}
	for _, p := range s.contacts.Organizations() {
		row := chatContextRecord{Kind: "organization", ID: p.Key, Title: p.Display, Detail: p.Key + " · marked organization/firm", profileNotePath: p.NotePath}
		if _, ok := s.index.Entity(p.Key); ok {
			row.Route = "#/contacts/" + url.PathEscape(p.Key)
		}
		if p.NotePath != "" {
			row.Detail += " · " + p.NotePath
			unique, err := s.index.UniqueNoteName(p.NotePath)
			if err != nil {
				return nil, err
			}
			if !unique || !s.index.ContextNote(p.NotePath) {
				row.contextError = "organization profile is ambiguous or outside authored knowledge; select an authorized exact note path instead"
				row.Route = ""
			}
			aliases, err := s.index.EntityAliases(p.Key)
			if err != nil {
				return nil, err
			}
			row.aliases = strings.Join(aliases, " ")
		}
		out = append(out, row)
	}
	return out, nil
}
func (s *Server) renderOrganizationContext(out *strings.Builder, selected chatContextRecord) error {
	contextField(out, "Classification", "Organization/firm, explicitly marked by the owner")
	contextField(out, "Organization key", selected.ID)
	contextField(out, "Profile note", selected.profileNotePath)
	contextField(out, "Profile aliases", selected.aliases)
	if selected.profileNotePath != "" {
		raw, err := s.personContextNote(selected.profileNotePath)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "\n## Organization profile (exact source)\n\n%s\n", raw)
	} else {
		out.WriteString("\nNo profile note is linked to this organization.\n")
	}
	out.WriteString("\nLinked note contents, recruiting records and fundraising summaries are excluded. This reference does not create a person or organization.\n")
	return nil
}
