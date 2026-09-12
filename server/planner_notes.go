package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"manifest/sharedhome"
	"manifest/threads"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type plannerNotesConfig struct {
	private, shared string
	write           func(string, []byte) error
	author          string
}

func (s *Server) UsePlannerNotes(private, shared, author string, write func(string, []byte) error) {
	s.plannerNotes = &plannerNotesConfig{private, shared, write, author}
}
func (s *Server) plannerNotesPath(id string) string {
	if s.plannerNotes == nil || s.tasksStore == nil {
		return ""
	}
	d, err := s.tasksStore.Load()
	if err != nil {
		return ""
	}
	dom, t := d.Find(id)
	if t == nil {
		return ""
	}
	root := s.plannerNotes.private
	if strings.EqualFold(dom.Name, "Home") {
		root = s.plannerNotes.shared
	}
	if root == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(id))
	return filepath.Join(root, "notes", hex.EncodeToString(hash[:]))
}
func (s *Server) plannerDescription(id string) (string, bool) {
	p := s.plannerNotesPath(id)
	if p == "" {
		return "", false
	}
	b, e := os.ReadFile(filepath.Join(p, "description.md"))
	return string(b), e == nil
}
func (s *Server) plannerComments(id string) []threads.Comment {
	p := s.plannerNotesPath(id)
	if p == "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(p, "comment-*.md"))
	var out []threads.Comment
	for _, f := range files {
		b, e := os.ReadFile(f)
		if e != nil {
			continue
		}
		parts := strings.SplitN(string(b), "\n", 2)
		var c threads.Comment
		if json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(parts[0], "<!-- "), " -->")), &c) != nil {
			continue
		}
		if len(parts) > 1 {
			c.Text = strings.TrimPrefix(parts[1], "\n")
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}
func (s *Server) addPlannerComment(id, text string, author threads.Identity) (threads.Comment, error) {
	p := s.plannerNotesPath(id)
	if p == "" {
		return threads.Comment{}, fmt.Errorf("task not found")
	}
	if strings.TrimSpace(text) == "" {
		return threads.Comment{}, fmt.Errorf("comment is empty")
	}
	var nonce [16]byte
	if _, e := rand.Read(nonce[:]); e != nil {
		return threads.Comment{}, e
	}
	c := threads.Comment{ID: hex.EncodeToString(nonce[:]), TaskID: id, Action: threads.ActComment, Author: author.ID, AuthorName: author.Name, At: time.Now().UTC()}
	metadata, _ := json.Marshal(c)
	body := "<!-- " + string(metadata) + " -->\n\n" + text
	err := s.plannerNotes.write(filepath.Join(p, "comment-"+c.ID+".md"), []byte(body))
	c.Text = text
	return c, err
}
func noteRevision(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}
func (s *Server) handlePlannerNotes(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	var b struct {
		ID, Description, Comment, Revision string
		Kind                               string
	}
	if r.Method == "POST" {
		if decode(r, &b) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		id = b.ID
	}
	p := s.plannerNotesPath(id)
	if p == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method == "POST" {
		if _, ok := s.pinTaskID(id); !ok {
			http.Error(w, "unable to save task", 409)
			return
		}
		var err error
		switch b.Kind {
		case "description":
			err = sharedhome.Locked(filepath.Join(p, "description.md"), func() error {
				current, _ := s.plannerDescription(id)
				if noteRevision(current) != b.Revision {
					return fmt.Errorf("description changed; reopen the task before saving")
				}
				return s.plannerNotes.write(filepath.Join(p, "description.md"), []byte(b.Description))
			})
		case "comment":
			_, err = s.addPlannerComment(id, b.Comment, threads.Identity{ID: s.plannerNotes.author, Name: s.plannerNotes.author})
		default:
			http.Error(w, "unknown edit", 400)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
	}
	description, _ := s.plannerDescription(id)
	writeJSON(w, map[string]any{"description": description, "revision": noteRevision(description), "comments": s.plannerComments(id)})
}
