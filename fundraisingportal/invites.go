package fundraisingportal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type InviteStore struct {
	path   string
	admin  string
	mu     sync.RWMutex
	emails []string
}

func NewInviteStore(dataDir, admin string) (*InviteStore, error) {
	s := &InviteStore{path: filepath.Join(dataDir, "fundraising", "invites.json"), admin: normalizeEmail(admin), emails: []string{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *InviteStore) Allowed(email string) bool {
	email = normalizeEmail(email)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if email != "" && email == s.admin {
		return true
	}
	for _, allowed := range s.emails {
		if email == allowed {
			return true
		}
	}
	return false
}

func (s *InviteStore) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.emails...)
}

func (s *InviteStore) Replace(emails []string) error {
	normalized := []string{}
	seen := map[string]bool{}
	for _, email := range emails {
		email = normalizeEmail(email)
		if email == "" || !strings.Contains(email, "@") || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
			return fmt.Errorf("invalid invite email %q", email)
		}
		if email != s.admin && !seen[email] {
			seen[email] = true
			normalized = append(normalized, email)
		}
	}
	sort.Strings(normalized)
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(map[string]any{"emails": normalized}, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.mu.Lock()
	s.emails = normalized
	s.mu.Unlock()
	return nil
}

func (s *InviteStore) load() error {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state struct {
		Emails []string `json:"emails"`
	}
	if err := json.Unmarshal(b, &state); err != nil {
		return err
	}
	for _, email := range state.Emails {
		email = normalizeEmail(email)
		if email != "" && email != s.admin {
			s.emails = append(s.emails, email)
		}
	}
	sort.Strings(s.emails)
	return nil
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
