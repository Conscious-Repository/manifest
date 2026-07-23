package portals

import (
	"errors"
	"path/filepath"
	"strings"
)

var errUnknownPortal = errors.New("unknown portal")

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func filepathDir(p string) string { return filepath.Dir(p) }

func hasPrefix(s, p string) bool { return strings.HasPrefix(s, p) }

// mustDef returns a registry def by id (only called with known ids).
func mustDef(id string) Def {
	d, _ := defByID(id)
	return d
}
