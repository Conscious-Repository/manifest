package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"manifest/chatstate"
	"manifest/record"
	"manifest/vaultwriter"
)

// Project names and assignments are owner-authored workbench data, not a
// rebuildable UI cache. Keep the old endpoint/IDs so existing links survive.
// The kernel owns serialization; all writes use the narrowly scoped capability.
const projectBlock = "manifest-projects"

type projectSnapshot struct {
	chatstate.Snapshot
	RecordVersion string `json:"record_version"`
	RecordPath    string `json:"record_path"`
}

// UseChatProjects migrates once, before serving. Existing vault records always
// win, including hand edits. The original cache remains an untouched backup.
func (s *Server) UseChatProjects(root string) error {
	if s.vault == nil || s.chatState == nil {
		return errors.New("projects require vault and chat state")
	}
	s.chatProjectsPath = path.Join(root, "projects.md")
	return s.vault.UpdateCap("chat-projects", s.chatProjectsPath, func(raw []byte) ([]byte, error) {
		if len(raw) > 0 {
			_, err := readProjectRecord(raw)
			return raw, err
		}
		old, err := s.chatState.Read("inbox", "workstreams")
		if err != nil {
			return nil, err
		}
		if len(old.Value) == 0 || string(old.Value) == "null" {
			old.Value = json.RawMessage(`{"groups":{},"members":{}}`)
		}
		if err = validateProjects(old.Value); err != nil {
			return nil, err
		}
		doc, err := record.AppendJSONBlock("# Chat projects\n\nProject names and conversation assignments. Other notes in this file are preserved.\n", projectBlock, old)
		return []byte(doc), err
	})
}

func validateProjects(value json.RawMessage) error {
	if len(value) > 96000 {
		return chatstate.ErrInvalid
	}
	var v struct {
		Priorities map[string]int    `json:"priorities"`
		Groups     map[string]string `json:"groups"`
		Members    map[string]string `json:"members"`
		Contexts   map[string]struct {
			Instructions string `json:"instructions"`
		} `json:"contexts"`
	}
	if json.Unmarshal(value, &v) != nil || v.Groups == nil || v.Members == nil {
		return chatstate.ErrInvalid
	}
	for key, priority := range v.Priorities {
		if key == "" || priority < 0 || priority > 3 {
			return chatstate.ErrInvalid
		}
	}
	for id, name := range v.Groups {
		if id == "" || strings.TrimSpace(name) == "" || len(name) > 320 {
			return chatstate.ErrInvalid
		}
	}
	for key, id := range v.Members {
		if key == "" || v.Groups[id] == "" {
			return chatstate.ErrInvalid
		}
	}
	for id, context := range v.Contexts {
		if v.Groups[id] == "" || len(context.Instructions) > 24000 {
			return chatstate.ErrInvalid
		}
	}
	return nil
}

func readProjectRecord(raw []byte) (projectSnapshot, error) {
	var out projectSnapshot
	blocks, err := record.JSONBlocks(string(raw), projectBlock)
	if err != nil {
		return out, err
	}
	if len(blocks) != 1 {
		return out, fmt.Errorf("expected one %s block", projectBlock)
	}
	if err = json.Unmarshal(blocks[0], &out.Snapshot); err != nil {
		return out, err
	}
	if out.Key != "inbox" || out.Slot != "workstreams" {
		return out, chatstate.ErrInvalid
	}
	if err = validateProjects(out.Value); err != nil {
		return out, err
	}
	out.RecordVersion = vaultwriter.Revision(raw)
	return out, nil
}

func (s *Server) handleChatProjects(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	var result projectSnapshot
	var err error
	if r.Method == http.MethodGet {
		var raw []byte
		raw, err = s.vault.ReadVaultFile(s.chatProjectsPath)
		if err == nil {
			result, err = readProjectRecord(raw)
		}
	} else {
		var b struct {
			Revision      *uint64         `json:"revision"`
			RecordVersion string          `json:"record_version"`
			Value         json.RawMessage `json:"value"`
		}
		if decode(r, &b) != nil || b.Revision == nil || validateProjects(b.Value) != nil {
			http.Error(w, "invalid project state", 400)
			return
		}
		if b.RecordVersion == "" {
			http.Error(w, "refresh project state before saving", 428)
			return
		}
		err = s.vault.UpdateCap("chat-projects", s.chatProjectsPath, func(raw []byte) ([]byte, error) {
			var e error
			result, e = readProjectRecord(raw)
			if e != nil {
				return nil, e
			}
			// Compare the entire record, not only a counter: Obsidian edits need the
			// same protection as another browser. Never overwrite unknown fields.
			if result.RecordVersion != b.RecordVersion || result.Revision != *b.Revision {
				return nil, chatstate.ErrConflict
			}
			var fields, incoming map[string]json.RawMessage
			if json.Unmarshal(result.Value, &fields) != nil || json.Unmarshal(b.Value, &incoming) != nil {
				return nil, chatstate.ErrInvalid
			}
			canonical, _ := json.Marshal(fields)
			for key, val := range incoming {
				fields[key] = val
			}
			next, e := json.Marshal(fields)
			if e != nil {
				return nil, e
			}
			if bytes.Equal(next, canonical) {
				return raw, nil
			}
			if result.Revision >= 9007199254740991 {
				return nil, chatstate.ErrInvalid
			}
			result.Value = next
			result.Revision++
			result.Updated = time.Now().UTC().Format(time.RFC3339Nano)
			doc, e := record.RewriteJSONBlocks(string(raw), projectBlock, func(f map[string]json.RawMessage) bool {
				f["value"] = next
				f["revision"], _ = json.Marshal(result.Revision)
				f["updated"], _ = json.Marshal(result.Updated)
				return true
			})
			result.RecordVersion = vaultwriter.Revision([]byte(doc))
			return []byte(doc), e
		})
	}
	result.RecordPath = s.chatProjectsPath
	if errors.Is(err, chatstate.ErrConflict) {
		w.WriteHeader(409)
		writeJSON(w, result)
		return
	}
	if errors.Is(err, chatstate.ErrInvalid) {
		http.Error(w, "invalid project record; original preserved", 400)
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "project record missing; restore it before editing projects", 503)
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, result)
}
