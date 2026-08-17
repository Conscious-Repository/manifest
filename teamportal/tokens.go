package teamportal

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const tokenPrefix = "aiontok_"

// TokenRecord is the non-secret metadata for one programmatic portal token.
// The plaintext token is returned only by Mint; the on-disk record contains a
// SHA-256 hash in storedToken instead.
type TokenRecord struct {
	ID       string     `json:"id"`
	Email    string     `json:"email"`
	Name     string     `json:"name"`
	Label    string     `json:"label"`
	Created  time.Time  `json:"created"`
	LastUsed *time.Time `json:"last_used,omitempty"`
	Revoked  bool       `json:"revoked"`
}

type storedToken struct {
	TokenRecord
	Hash string `json:"hash"`
}

type tokenFile struct {
	Tokens []storedToken `json:"tokens"`
}

// TokenStore owns revocable per-user API tokens in the secrets tier:
// <dataDir>/portals/aion-portal-tokens.json (0600).
type TokenStore struct {
	path string
	mu   sync.Mutex
}

func NewTokens(dataDir string) *TokenStore {
	return &TokenStore{path: filepath.Join(dataDir, "portals", "aion-portal-tokens.json")}
}

func (s *TokenStore) read() (tokenFile, error) {
	var f tokenFile
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("read portal tokens: %w", err)
	}
	return f, nil
}

func (s *TokenStore) write(f tokenFile) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	h, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := h.Chmod(0o600); err != nil {
		h.Close()
		return err
	}
	if _, err := h.Write(b); err != nil {
		h.Close()
		return err
	}
	if err := h.Sync(); err != nil {
		h.Close()
		return err
	}
	if err := h.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Mint creates a new token for id. The plaintext is returned once and never
// persisted; callers must show it immediately and then discard it.
func (s *TokenStore) Mint(id Identity, label string) (string, TokenRecord, error) {
	if s == nil {
		return "", TokenRecord{}, errors.New("portal token store is not configured")
	}
	id.Email = strings.ToLower(strings.TrimSpace(id.Email))
	id.Name = strings.TrimSpace(id.Name)
	if !Authorized(id.Email) {
		return "", TokenRecord{}, fmt.Errorf("not an @%s account: %s", Domain, id.Email)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "API token"
	}
	if len(label) > 80 {
		return "", TokenRecord{}, errors.New("token label too long (80 max)")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", TokenRecord{}, err
	}
	plain := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plain))
	rec := TokenRecord{
		ID:      "tok_" + base64.RawURLEncoding.EncodeToString(sum[:9]),
		Email:   id.Email,
		Name:    id.Name,
		Label:   label,
		Created: time.Now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return "", TokenRecord{}, err
	}
	for _, existing := range f.Tokens {
		if existing.ID == rec.ID {
			return "", TokenRecord{}, errors.New("token id collision; try again")
		}
	}
	f.Tokens = append(f.Tokens, storedToken{TokenRecord: rec, Hash: hex.EncodeToString(sum[:])})
	if err := s.write(f); err != nil {
		return "", TokenRecord{}, err
	}
	return plain, rec, nil
}

// Resolve verifies a plaintext token, rejects revoked/foreign-domain records,
// and best-effort stamps last_used. Hash comparison is constant-time.
func (s *TokenStore) Resolve(plaintext string) (Identity, bool) {
	if s == nil || !strings.HasPrefix(plaintext, tokenPrefix) {
		return Identity{}, false
	}
	sum := sha256.Sum256([]byte(plaintext))
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return Identity{}, false
	}
	for i := range f.Tokens {
		storedHash, err := hex.DecodeString(f.Tokens[i].Hash)
		if err != nil || len(storedHash) != sha256.Size || subtle.ConstantTimeCompare(sum[:], storedHash) != 1 {
			continue
		}
		rec := &f.Tokens[i]
		if rec.Revoked || !Authorized(rec.Email) {
			return Identity{}, false
		}
		now := time.Now().UTC()
		rec.LastUsed = &now
		_ = s.write(f) // authentication succeeds even if the usage stamp cannot land
		return Identity{Email: rec.Email, Name: rec.Name}, true
	}
	return Identity{}, false
}

// List returns only records owned by email, newest first.
func (s *TokenStore) List(email string) []TokenRecord {
	if s == nil {
		return nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return nil
	}
	out := make([]TokenRecord, 0)
	for _, tok := range f.Tokens {
		if strings.EqualFold(tok.Email, email) {
			out = append(out, tok.TokenRecord)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

// Revoke marks one of email's tokens unusable. It never permits a caller to
// address another member's token.
func (s *TokenStore) Revoke(email, id string) bool {
	if s == nil {
		return false
	}
	email = strings.ToLower(strings.TrimSpace(email))
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.read()
	if err != nil {
		return false
	}
	for i := range f.Tokens {
		if f.Tokens[i].ID != id || !strings.EqualFold(f.Tokens[i].Email, email) {
			continue
		}
		if f.Tokens[i].Revoked {
			return true
		}
		f.Tokens[i].Revoked = true
		return s.write(f) == nil
	}
	return false
}
