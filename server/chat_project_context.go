package server

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type projectContextRecord struct {
	Instructions string
	Members      []string
	Folders      []string
	Priorities   map[string]int
}

// Read one canonical project record; never hydrate member transcripts or infer
// projects from folder names. Equal display names retain distinct project IDs.
func (s *Server) chatProjectRecords() ([]chatContextRecord, error) {
	if s.vault == nil || s.chatProjectsPath == "" {
		return nil, fmt.Errorf("project records unavailable")
	}
	raw, err := s.vault.ReadVaultFile(s.chatProjectsPath)
	if err != nil {
		return nil, err
	}
	snapshot, err := readProjectRecord(raw)
	if err != nil {
		return nil, err
	}
	var value struct {
		Groups     map[string]string `json:"groups"`
		Members    map[string]string `json:"members"`
		Folders    map[string]string `json:"folders"`
		Priorities map[string]int    `json:"priorities"`
		Contexts   map[string]struct {
			Instructions string `json:"instructions"`
		} `json:"contexts"`
	}
	if err = json.Unmarshal(snapshot.Value, &value); err != nil {
		return nil, err
	}
	out := []chatContextRecord{}
	for id, title := range value.Groups {
		p := &projectContextRecord{Instructions: value.Contexts[id].Instructions, Priorities: value.Priorities}
		for key, group := range value.Members {
			if group == id {
				p.Members = append(p.Members, key)
			}
		}
		for key, group := range value.Folders {
			if group == id {
				p.Folders = append(p.Folders, key)
			}
		}
		sort.Strings(p.Members)
		sort.Strings(p.Folders)
		out = append(out, chatContextRecord{Kind: "project", ID: id, Title: title, Detail: id + " · " + s.chatProjectsPath, Route: "#/chat/project/" + url.PathEscape(id), project: p})
	}
	return out, nil
}
func (s *Server) renderProjectContext(out *strings.Builder, selected chatContextRecord) {
	p := selected.project
	contextField(out, "Source record", s.chatProjectsPath)
	out.WriteString("\n## Saved instructions and reference links\n\n")
	if p.Instructions == "" {
		out.WriteString("No saved instructions.\n")
	} else {
		out.WriteString(p.Instructions)
		out.WriteByte('\n')
	}
	out.WriteString("\n## Conversation references (contents excluded)\n\n")
	for _, key := range p.Members {
		fmt.Fprintf(out, "- %s", key)
		if priority := p.Priorities[key]; priority > 0 {
			fmt.Fprintf(out, " [priority: %d]", priority)
		}
		out.WriteByte('\n')
	}
	if len(p.Members) == 0 {
		out.WriteString("No assigned conversations.\n")
	}
	out.WriteString("\n## Saved folder associations (contents excluded)\n\n")
	for _, key := range p.Folders {
		fmt.Fprintf(out, "- %s\n", key)
	}
	if len(p.Folders) == 0 {
		out.WriteString("No saved folder associations.\n")
	}
	out.WriteString("\nThis reviewed reference does not change the receiving conversation's project or provision a folder.\n")
}
