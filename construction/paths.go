package construction

import (
	"os"
	"path/filepath"
	"strings"
)

// Forbidden-root containment, shared by the restore-target check
// (CheckNewDir) and the store-root check (Open).

// forbiddenRootOf returns the first forbidden root that path equals or lies
// beneath ("" when none does). path must be absolute, clean and resolved (no
// symlinks). Each root is first made comparable (canonicalPath); a root that
// cannot be is returned with the error, so the caller refuses rather than
// skips it. Empty entries are unset roots and are skipped.
func forbiddenRootOf(path string, forbidden []string) (string, error) {
	for _, f := range forbidden {
		if f == "" {
			continue
		}
		root, err := canonicalPath(f)
		if err != nil {
			return f, err
		}
		if pathWithin(path, root) {
			return f, nil
		}
	}
	return "", nil
}

// canonicalPath makes p absolute and clean (a relative p is taken from the
// working directory, as a command-line path is) and resolves symlinks in its
// longest existing prefix, so a root that does not exist yet still compares
// correctly with a resolved path.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rest := ""
	for cur := abs; ; cur = filepath.Dir(cur) {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if filepath.Dir(cur) == cur {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}

// pathWithin reports whether p is root or lies beneath it, comparing whole
// path elements of two absolute, clean paths (filepath.Rel): "/" contains
// every path, "/a" does not contain "/ab", and an entry named "..x" under
// root is inside it. Where no relative path exists (as across Windows
// volumes) it answers true, because callers use it to refuse.
func pathWithin(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
