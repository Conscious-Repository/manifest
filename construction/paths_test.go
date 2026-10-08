package construction

import (
	"os"
	"path/filepath"
	"testing"
)

// Containment compares whole path elements, so "/" holds every path, a
// shared name prefix is not containment, and "..x" is an ordinary name.
func TestPathWithin(t *testing.T) {
	for _, c := range []struct {
		p, root string
		want    bool
	}{
		{"/tmp/restored", "/", true},
		{"/", "/", true},
		{"/data/vault", "/data/vault", true},
		{"/data/vault/a/b", "/data/vault", true},
		{"/data/vault/..x", "/data/vault", true},
		{"/data/vault2", "/data/vault", false},
		{"/data/..vault", "/data/vault", false},
		{"/data", "/data/vault", false},
		{"/other/vault", "/data/vault", false},
	} {
		if got := pathWithin(c.p, c.root); got != c.want {
			t.Errorf("pathWithin(%q, %q) = %v, want %v", c.p, c.root, got, c.want)
		}
	}
}

// A forbidden root is made comparable however it is written: absolute and
// clean, relative paths taken from the working directory, symlinks resolved
// in the longest existing prefix even when the root itself does not exist.
func TestCanonicalPath(t *testing.T) {
	base := privateDir(t)
	realDir := filepath.Join(base, "realDir")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.Symlink(realDir, filepath.Join(base, "link"))
	for in, want := range map[string]string{
		"/":                                      "/",
		"//":                                     "/",
		realDir + "/":                            realDir,
		realDir + "//./x/../":                    realDir,
		filepath.Join(base, "link"):              realDir,
		filepath.Join(base, "link", "new", "y"):  filepath.Join(realDir, "new", "y"),
		filepath.Join(base, "absent", "z") + "/": filepath.Join(base, "absent", "z"),
	} {
		if got, err := canonicalPath(in); err != nil || got != want {
			t.Errorf("canonicalPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	t.Chdir(base)
	for in, want := range map[string]string{
		"realDir":          realDir,
		"./realDir/":       realDir,
		".":                base,
		"link/new":         filepath.Join(realDir, "new"),
		"realDir/../link/": realDir,
	} {
		if got, err := canonicalPath(in); err != nil || got != want {
			t.Errorf("relative canonicalPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
