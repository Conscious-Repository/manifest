package artifacts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// A private pool creates owner-only directories and files for everything it
// writes — blobs, objects, extracts, indexes — and reports durability.
func TestPrivatePoolModes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "construction", "artifacts")
	pool, err := NewWithOptions(root, Options{Private: true})
	if err != nil {
		t.Fatal(err)
	}
	if !pool.Durable() {
		t.Fatal("private pool must be durable")
	}
	reg, err := NewRegistry(pool)
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Retain(Put{Kind: "construction-problem", Harness: "construction", Content: []byte(`{"a":1}`), Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.PutExtract(res.Revision.Hash, "text"); err != nil {
		t.Fatal(err)
	}
	if err := pool.Add("construction", Entry{Ref: Ref{Hash: res.Revision.Hash, Name: "a.json", Size: 7}}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{root, filepath.Join(root, "blobs"), filepath.Join(root, "blobs", res.Revision.Hash[:2]), filepath.Join(root, "objects"), filepath.Join(root, "index"), filepath.Join(root, "extracts")} {
		if m := mode(t, d); m != 0o700 {
			t.Fatalf("%s mode %o, want 0700", d, m)
		}
	}
	for _, f := range []string{pool.BlobPath(res.Revision.Hash), reg.path(res.Artifact.ID), pool.ExtractPath(res.Revision.Hash), pool.indexPath("construction")} {
		if m := mode(t, f); m != 0o600 {
			t.Fatalf("%s mode %o, want 0600", f, m)
		}
	}
	got, err := reg.Content(res.Revision.Hash)
	if err != nil || !bytes.Equal(got, []byte(`{"a":1}`)) {
		t.Fatalf("content %q %v", got, err)
	}
	// a retained snapshot is content-addressed: the same bytes under the same
	// kind and harness are the same artifact, so a restore can recreate ids
	again, err := reg.Retain(Put{Kind: "construction-problem", Harness: "construction", Content: []byte(`{"a":1}`), Actor: "someone-else"})
	if err != nil || again.Artifact.ID != res.Artifact.ID || again.Created {
		t.Fatalf("retain replay %+v %v", again, err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "objects"))
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
}

// The shared pool keeps its historic filesystem policy: New is unchanged.
func TestSharedPoolModesUnchanged(t *testing.T) {
	root := t.TempDir()
	pool, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if pool.Durable() {
		t.Fatal("shared pool is not the durable private mode")
	}
	reg, err := NewRegistry(pool)
	if err != nil {
		t.Fatal(err)
	}
	res, err := reg.Put(Put{Ref: "notes/a.md", Content: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	if m := mode(t, filepath.Join(root, "objects")); m != 0o755&^currentUmask() {
		t.Fatalf("objects dir mode %o", m)
	}
	if m := mode(t, reg.path(res.Artifact.ID)); m != 0o644&^currentUmask() {
		t.Fatalf("object mode %o", m)
	}
}

func currentUmask() os.FileMode {
	// probe: create a 0777 file and read back what the umask left
	dir, _ := os.MkdirTemp("", "umask")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "probe")
	f, _ := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o777)
	f.Close()
	fi, _ := os.Stat(p)
	return 0o777 &^ fi.Mode().Perm()
}
