package domainextract

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// OpenSparksReceipt only creates a new private file in an existing private
// directory outside both input and vault. No operational stores are opened.
func OpenSparksReceipt(path, vault, input string) (*os.File, error) {
	fail := errors.New("private-receipt-path-refused")
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fail
	}
	parent := filepath.Dir(path)
	resolved, e := filepath.EvalSymlinks(parent)
	if e != nil || resolved != parent {
		return nil, fail
	}
	for _, root := range []string{vault, input} {
		root, e = filepath.EvalSymlinks(root)
		if e != nil {
			return nil, fail
		}
		if parent == root || strings.HasPrefix(parent, root+string(os.PathSeparator)) {
			return nil, fail
		}
	}
	st, e := os.Stat(parent)
	if e != nil || st.Mode().Perm()&0077 != 0 {
		return nil, fail
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, fail
	}
	return f, nil
}
