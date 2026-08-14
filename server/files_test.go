package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAgentKeyDerivationAndTickets(t *testing.T) {
	dir := t.TempDir()
	master := filepath.Join(dir, "agent_master")
	f := &filesCfg{masterPath: master}

	// master auto-mints once (0600) and derivation is stable
	k1, err := f.DeriveAgentKey("macbook")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := f.DeriveAgentKey("macbook")
	if err != nil || k1 != k2 {
		t.Fatalf("derivation unstable: %v", err)
	}
	if fi, err := os.Stat(master); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("master file mode: %v %v", fi, err)
	}
	// different hosts get different keys (host-bound)
	k3, _ := f.DeriveAgentKey("phone")
	if k3 == k1 {
		t.Fatal("keys must differ per host")
	}

	// a minted ticket verifies against the derived key (the agent's check)
	tk, err := f.mintTicket("macbook")
	if err != nil {
		t.Fatal(err)
	}
	i := strings.IndexByte(tk, '.')
	expStr, sig := tk[:i], tk[i+1:]
	exp, _ := strconv.ParseInt(expStr, 10, 64)
	if exp < time.Now().Unix() {
		t.Fatal("ticket already expired")
	}
	key, _ := hex.DecodeString(k1)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(expStr))
	if hex.EncodeToString(mac.Sum(nil)) != sig {
		t.Fatal("ticket does not verify against the derived key")
	}
}

func TestFilesResolveLocal(t *testing.T) {
	root := t.TempDir()
	f := &filesCfg{localRoots: []string{root}}
	if _, err := f.resolveLocal(filepath.Join(root, "sub", "file.txt")); err != nil {
		t.Fatalf("in-root: %v", err)
	}
	// dotfiles inside the roots are allowed now (hidden toggle governs
	// visibility client-side) — the roots + traversal rules stay the boundary
	for _, ok := range []string{
		filepath.Join(root, ".ssh", "id_rsa"),
		filepath.Join(root, "sub", ".env"),
	} {
		if _, err := f.resolveLocal(ok); err != nil {
			t.Fatalf("dotfile in-root must resolve %q: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"/etc/passwd",
		filepath.Join(root, "..", "out.txt"),
		"relative/path",
	} {
		if _, err := f.resolveLocal(bad); err == nil {
			t.Fatalf("must refuse %q", bad)
		}
	}
}
