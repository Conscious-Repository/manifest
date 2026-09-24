package server

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"manifest/artifacts"
)

// Search only registered, immutable heads. It never traverses the filesystem
// named by an artifact ref. The private cockpit router owns this capability.
func (s *Server) searchArtifacts(rows []artifacts.Artifact, query string) ([]artifacts.Artifact, int) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return rows, 0
	}
	matches := func(text string) bool {
		text = strings.ToLower(text)
		for _, term := range terms {
			if !strings.Contains(text, term) {
				return false
			}
		}
		return true
	}
	out := []artifacts.Artifact{}
	skipped := 0
	for _, a := range rows {
		metadata := strings.Join([]string{a.ID, a.Title, a.Ref, a.Kind, a.Actor, a.Harness, a.Provenance.Task, a.Provenance.Run, a.Provenance.Session}, "\n")
		if matches(metadata) {
			out = append(out, a)
			continue
		}
		// Bound reads and make unsupported content visible in the search status.
		if a.HeadRevision().Size > 1<<20 {
			skipped++
			continue
		}
		b, err := s.artifactReg.Content(a.Head)
		if err != nil || !utf8.Valid(b) || strings.ContainsRune(string(b), '\x00') {
			skipped++
			continue
		}
		if matches(metadata + "\n" + string(b)) {
			out = append(out, a)
		}
	}
	return out, skipped
}

type artifactSourceLink struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Route string `json:"route"`
	Label string `json:"label"`
}

// Resolve source keys against recorded native identities, never titles or an
// opaque legacy session ID. Continued runtimes have their own execution link
// while the artifact's session key still names the original conversation.
func (s *Server) artifactSourceLinks(rows []artifacts.Artifact) map[string][]artifactSourceLink {
	out := map[string][]artifactSourceLink{}
	conversations := map[string]artifactSourceLink{}
	runtimes := map[string]termSession{}
	needSessions := false
	for _, a := range rows {
		if a.Provenance.Session != "" {
			needSessions = true
			break
		}
	}
	if needSessions {
		if s.terminal != nil {
			for _, se := range s.terminal.load() {
				d := terminalConversation(se)
				conversations[d.Key] = artifactSourceLink{"conversation", d.Key, d.Route, orStr(se.Name, se.Kind)}
				runtimes[se.ID] = se
			}
		}
		if s.agentChat != nil {
			for _, agent := range s.agentChat.store.Agents() {
				for _, se := range s.agentChat.store.List(agent) {
					d := sessionConversation(se)
					conversations[d.Key] = artifactSourceLink{"conversation", d.Key, d.Route, orStr(se.Title, se.Agent)}
				}
			}
		}
	}
	knownRecords := map[string]map[string]bool{}
	for _, a := range rows {
		if a.Provenance.Source == "knowledge-context" && a.Harness == "vault" && s.index != nil {
			rel := knowledgeContextPath(a)
			if rel != "" && s.index.ContextNote(rel) {
				out[a.ID] = append(out[a.ID], artifactSourceLink{"note", rel, "#/note/" + url.PathEscape(rel), rel})
			}
		}
		if kind, id, route := contextSnapshotSource(a); kind == "task" || kind == "goal" {
			if knownRecords[kind] == nil {
				knownRecords[kind] = map[string]bool{}
				if records, err := s.chatContextRecords(kind, ""); err == nil {
					for _, r := range records {
						knownRecords[kind][r.ID] = true
					}
				}
			}
			if knownRecords[kind][id] {
				out[a.ID] = append(out[a.ID], artifactSourceLink{kind, id, route, id})
			}
		}
		if link, ok := conversations[a.Provenance.Session]; ok {
			out[a.ID] = append(out[a.ID], link)
		}
		if a.Provenance.Source == "runtime-changes" {
			if se, ok := runtimes[a.Provenance.Run]; ok && s.runtimeArtifactScope(se) == a.Provenance.Session {
				d := terminalConversation(se)
				out[a.ID] = append(out[a.ID], artifactSourceLink{"execution", se.ID, d.Route, se.Kind + " execution"})
			}
		} else if a.Provenance.Source == "run" && a.Provenance.Run != "" {
			if h := s.findHarness(orStr(a.Harness, s.primaryHarnessName())); h != nil && h.Spirits != nil {
				if _, _, ok := h.Spirits.Run(a.Provenance.Run); ok {
					route := "#/artifact/run-in/" + url.PathEscape(h.Name) + "/" + url.PathEscape(a.Provenance.Run)
					out[a.ID] = append(out[a.ID], artifactSourceLink{"run", a.Provenance.Run, route, "run " + a.Provenance.Run})
				}
			}
		}
	}
	return out
}
