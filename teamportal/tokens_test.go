package teamportal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenStoreMintResolveListAndRevoke(t *testing.T) {
	dataDir := t.TempDir()
	store := NewTokens(dataDir)
	plain, rec, err := store.Mint(Identity{Email: "Hannah@AION.BIO", Name: "Hannah Zmuda"}, "Claude Code")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if !strings.HasPrefix(plain, tokenPrefix) || rec.ID == "" || rec.Email != "hannah@aion.bio" {
		t.Fatalf("Mint = %q, %+v", plain, rec)
	}
	path := filepath.Join(dataDir, "portals", "aion-portal-tokens.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token store: %v", err)
	}
	if strings.Contains(string(b), plain) {
		t.Fatal("token store persisted the plaintext token")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token store mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}

	id, ok := store.Resolve(plain)
	if !ok || id.Email != "hannah@aion.bio" || id.Name != "Hannah Zmuda" {
		t.Fatalf("Resolve = %+v, %v", id, ok)
	}
	listed := store.List("hannah@aion.bio")
	if len(listed) != 1 || listed[0].LastUsed == nil || listed[0].Label != "Claude Code" {
		t.Fatalf("List = %+v", listed)
	}
	if store.Revoke("rj@aion.bio", rec.ID) {
		t.Fatal("another member revoked Hannah's token")
	}
	if !store.Revoke("hannah@aion.bio", rec.ID) {
		t.Fatal("owner could not revoke token")
	}
	if _, ok := store.Resolve(plain); ok {
		t.Fatal("Resolve accepted a revoked token")
	}
}

func TestTokenStoreRejectsForeignIdentityAndLongLabel(t *testing.T) {
	store := NewTokens(t.TempDir())
	if _, _, err := store.Mint(Identity{Email: "person@gmail.com"}, "tool"); err == nil {
		t.Fatal("Mint accepted a non-aion identity")
	}
	if _, _, err := store.Mint(Identity{Email: "hannah@aion.bio"}, strings.Repeat("x", 81)); err == nil {
		t.Fatal("Mint accepted a label over 80 bytes")
	}
}
