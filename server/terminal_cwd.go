package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// invalidTerminalCwd is client input rejected before any allocation request.
type invalidTerminalCwd struct{ message string }

func (e *invalidTerminalCwd) Error() string { return e.message }

type terminalServerError struct{ err error }

func (e *terminalServerError) Error() string { return e.err.Error() }
func (e *terminalServerError) Unwrap() error { return e.err }

func terminalLaunchStatus(err error) int {
	var serverErr *terminalServerError
	if errors.As(err, &serverErr) {
		return http.StatusInternalServerError
	}
	var cwdErr *invalidTerminalCwd
	if errors.As(err, &cwdErr) {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

// Resolve only the launcher's home shorthand, never shell expressions. Retain
// symlink traversal semantics by not cleaning interior path components.
func resolveTerminalCwd(requested, defaultWd string) (string, error) {
	cwd := strings.TrimSpace(requested)
	if cwd == "" || cwd == "~" {
		cwd = defaultWd
	} else if strings.HasPrefix(cwd, "~/") {
		cwd = strings.TrimRight(defaultWd, string(os.PathSeparator)) + string(os.PathSeparator) + cwd[2:]
	}
	invalid := func(reason string) (string, error) {
		return "", &invalidTerminalCwd{fmt.Sprintf("terminal cwd %s: %s", reason, cwd)}
	}
	if !filepath.IsAbs(cwd) {
		return invalid("must be absolute")
	}
	st, err := os.Stat(cwd)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return invalid("does not exist")
	case errors.Is(err, syscall.ENOTDIR):
		return invalid("is not a directory")
	case errors.Is(err, os.ErrPermission):
		return invalid("permission denied")
	case err != nil:
		return "", &terminalServerError{fmt.Errorf("check terminal cwd %s: %w", cwd, err)}
	case !st.IsDir():
		return invalid("is not a directory")
	}
	return cwd, nil
}

// Only this pre-request failure proves workspace.create was never attempted.
type terminalAllocationNotAttempted struct{ err error }

func (e *terminalAllocationNotAttempted) Error() string { return e.err.Error() }
func (e *terminalAllocationNotAttempted) Unwrap() error { return e.err }
