// Command construction-restore verifies a construction recovery bundle and
// restores it into an EMPTY directory outside every forbidden root (plan §9,
// P9). It never merges into a populated root, never touches the vault and
// never starts or resumes an agent: native conversation copies are written
// only to an explicit, empty -native-out directory, otherwise reported as
// not rebound.
//
//	construction-restore -bundle FILE -verify-only
//	construction-restore -bundle FILE -target DIR [-forbid PATH]... [-native-out DIR]
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"manifest/construction"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run is the testable command: exit 0 ok, 1 refused/failed, 2 usage.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("construction-restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bundle := fs.String("bundle", "", "recovery bundle (.zip) to read")
	target := fs.String("target", "", "EMPTY absolute directory to restore into")
	native := fs.String("native-out", "", "optional EMPTY absolute directory for native conversation copies (never resumed)")
	verify := fs.Bool("verify-only", false, "validate the bundle and print its manifest; write nothing")
	var forbid multiFlag
	fs.Var(&forbid, "forbid", "a root the target must not lie under (repeatable; e.g. the vault)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *bundle == "" || (!*verify && *target == "") {
		fmt.Fprintln(stderr, "usage: construction-restore -bundle FILE (-verify-only | -target DIR [-forbid PATH]... [-native-out DIR])")
		return 2
	}
	raw, err := os.ReadFile(*bundle)
	if err != nil {
		fmt.Fprintln(stderr, "read bundle:", err)
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if *verify {
		man, files, err := construction.ReadBundle(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		_ = enc.Encode(map[string]any{"verified": true, "problemId": man.ProblemID, "subject": man.Subject, "generation": man.Generation,
			"files": len(files), "complete": man.Complete, "categories": man.Categories, "missing": man.Missing})
		return 0
	}
	if *native != "" {
		if err := emptyDir(*native); err != nil {
			fmt.Fprintln(stderr, "native-out:", err)
			return 1
		}
	}
	rep, err := construction.Restore(bytes.NewReader(raw), int64(len(raw)), *target, construction.RestoreOptions{Forbidden: forbid})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	nativeState := "none in bundle"
	if len(rep.NativeFiles) > 0 {
		nativeState = "not rebound (no -native-out given); a restore never resumes an agent"
		if *native != "" {
			names := append([]string{}, rep.NativeFiles...)
			sort.Strings(names)
			for _, p := range names {
				dst := filepath.Join(*native, filepath.FromSlash(strings.TrimPrefix(p, "native/")))
				if !strings.HasPrefix(dst, filepath.Clean(*native)+string(os.PathSeparator)) {
					fmt.Fprintln(stderr, "native path escapes -native-out:", p)
					return 1
				}
				if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
				if err := os.WriteFile(dst, rep.Native[p], 0o600); err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
			}
			nativeState = "copied for explicit owner rebinding; not resumed"
		}
	}
	_ = enc.Encode(map[string]any{"restored": true, "report": rep, "native": nativeState, "target": *target})
	return 0
}

func emptyDir(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return errors.New("must be an absolute clean path")
	}
	fi, err := os.Lstat(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return errors.New("must be a real directory")
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		return err
	}
	if len(ents) > 0 {
		return errors.New("must be empty")
	}
	return nil
}
