package server

// Conversation descriptors expose native identity without merging histories.
// A source key is independent of display title, model, runtime generation and
// task assignment. Future handoffs can bind additional sources to a conversation;
// they must not rewrite this identity or infer a binding from matching names.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"

	"manifest/threads"
)

type conversationSource struct {
	Backend   string `json:"backend"`
	Namespace string `json:"namespace"`
	ID        string `json:"id"`
}

type conversationLink struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Evidence string `json:"evidence"`
}

type conversationDescriptor struct {
	Key      string             `json:"key"`
	Source   conversationSource `json:"source"`
	Scope    string             `json:"scope"`
	Route    string             `json:"route"`
	Links    []conversationLink `json:"links"`
	Warnings []string           `json:"warnings,omitempty"`
}

func (s *Server) taskConversation(id string, comments []threads.Comment) conversationDescriptor {
	scope := s.threadKind(id)
	if scope == "re" {
		scope = "ooda"
	}
	if scope != "private" {
		scope = "team:" + scope
	}
	d := describeConversation("task", "manifest", id, scope, "#/chat/task/"+url.PathEscape(id))
	d.Links = append(d.Links, conversationLink{"task", id, "task.id"})
	seen := map[string]bool{}
	for _, c := range comments {
		m, _ := c.Meta["chat"].(map[string]any)
		agent, _ := m["agent"].(string)
		nativeID, _ := m["id"].(string)
		if agent == "" || nativeID == "" {
			continue
		}
		backend, sourceScope := "hermes", "private"
		if ag, portal := s.portalChatAgent(agent); portal {
			backend = "portal"
			if ag != nil {
				sourceScope = "team:" + ag.Domain
			}
		}
		source := agentConversation(backend, agent, nativeID, sourceScope, "")
		if seen[source.Key] {
			continue
		}
		seen[source.Key] = true
		d.Links = append(d.Links, conversationLink{"conversation", source.Key, "thread.meta.chat"})
	}
	if len(seen) > 1 {
		d.Warnings = append(d.Warnings, "Multiple source conversations; no canonical destination has been selected.")
	} else if len(seen) == 1 {
		if link := s.taskChatLink(id, comments, ""); link != nil && link.Canonical {
			d.Route = "#/chat/a/" + url.PathEscape(link.Agent) + "/" + url.PathEscape(link.ID)
		} else {
			d.Warnings = append(d.Warnings, "Promoted history and source conversation remain separate write destinations.")
		}
	}
	return d
}

func describeConversation(backend, namespace, id, scope, route string) conversationDescriptor {
	src := conversationSource{backend, namespace, id}
	b, _ := json.Marshal(src)
	h := sha256.Sum256(b)
	return conversationDescriptor{Key: "conversation-" + hex.EncodeToString(h[:16]), Source: src,
		Scope: scope, Route: route, Links: []conversationLink{}}
}

func agentConversation(backend, agent, id, scope, task string) conversationDescriptor {
	d := describeConversation(backend, agent, id, scope,
		"#/chat/a/"+url.PathEscape(agent)+"/"+url.PathEscape(id))
	if task != "" {
		d.Links = append(d.Links, conversationLink{"task", task, "session.task"})
	}
	return d
}

func terminalConversation(se termSession) conversationDescriptor {
	route := "#/terminal/" + url.PathEscape(se.ID)
	if isCodingAgent(se.Kind) && se.Device == "" {
		route = "#/chat/a/" + url.PathEscape(se.Kind) + "/" + url.PathEscape(se.ID)
	}
	d := describeConversation("terminal", "manifest", se.ID, "private", route)
	// The registry is the source of the execution link. A runtime handle is
	// not the conversation identity and can change when execution resumes.
	d.Links = append(d.Links, conversationLink{"execution", se.ID, "terminal.registry"})
	if se.ResumeID != "" {
		d.Links = append(d.Links, conversationLink{"native-session", se.ResumeID, "terminal.resumeId"})
	}
	return d
}
