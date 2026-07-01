// Package profiles manages agent "profiles" — named markdown presets (brief + model
// tier + tools + permissions + optional schedule) that parameterize a Hermes call.
// Profiles live under <dataDir>/agents/profiles/*.md, OUTSIDE the vault. Same format
// as agents.AgentDef, plus a serializer for in-app CRUD.
package profiles

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"manifest/mdfm"
)

// Profile is one preset. Name is the file stem (a slug); the rest is frontmatter + brief.
type Profile struct {
	Name        string   `json:"name"`
	Model       string   `json:"model"`
	Tools       []string `json:"tools"`
	Permissions []string `json:"permissions"`
	Schedule    string   `json:"schedule"`
	Brief       string   `json:"brief"`
}

// Store is the profiles directory.
type Store struct{ dir string }

// NewStore roots the store at <agentsDir>/profiles and ensures the dir exists.
func NewStore(agentsDir string) *Store {
	dir := filepath.Join(agentsDir, "profiles")
	_ = os.MkdirAll(dir, 0o700)
	return &Store{dir: dir}
}

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)

// Slug normalizes a name to a safe file stem.
func Slug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = slugRe.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	return s
}

// List returns all profiles, sorted by name.
func (s *Store) List() []Profile {
	entries, _ := os.ReadDir(s.dir)
	var out []Profile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if p, err := s.parse(filepath.Join(s.dir, e.Name())); err == nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get loads one profile by name (slug).
func (s *Store) Get(name string) (Profile, bool) {
	p, err := s.parse(filepath.Join(s.dir, Slug(name)+".md"))
	if err != nil {
		return Profile{}, false
	}
	return p, true
}

// Save validates and writes a profile (create or overwrite).
func (s *Store) Save(p Profile) (Profile, error) {
	slug := Slug(p.Name)
	if slug == "" {
		return Profile{}, errors.New("profile name is required")
	}
	p.Name = slug
	if err := os.WriteFile(filepath.Join(s.dir, slug+".md"), []byte(serialize(p)), 0o644); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// Delete removes a profile.
func (s *Store) Delete(name string) error {
	slug := Slug(name)
	if slug == "" {
		return errors.New("profile name is required")
	}
	err := os.Remove(filepath.Join(s.dir, slug+".md"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Store) parse(path string) (Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	fm, body := mdfm.Split(string(b))
	return Profile{
		Name:        strings.TrimSuffix(filepath.Base(path), ".md"),
		Model:       fm["model"],
		Tools:       mdfm.List(fm["tools"]),
		Permissions: mdfm.List(fm["permissions"]),
		Schedule:    fm["schedule"],
		Brief:       strings.TrimSpace(body),
	}, nil
}

func serialize(p Profile) string {
	return (&mdfm.Writer{}).
		SetRaw("type", "profile").
		Set("name", p.Name).
		Set("model", p.Model).
		SetList("tools", p.Tools).
		SetList("permissions", p.Permissions).
		Set("schedule", p.Schedule).
		String(strings.TrimSpace(p.Brief))
}
