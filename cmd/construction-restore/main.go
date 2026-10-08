// Command construction-restore verifies a construction recovery bundle and
// restores it into a NEW directory outside every forbidden root (plan §9,
// P9). The target must not exist; its parent must be an existing real
// directory that no other account can rename or replace. The restore never
// writes into an existing directory, never touches the vault and never
// starts or resumes an agent. Native conversation copies are written only
// to an explicit, new -native-out directory (checked the same way, written
// through an os.Root anchored on it); otherwise they are reported as not
// rebound. The bundle is read from the file, not loaded whole, within a
// decompressed budget (-max-total-mb; raise it only for your own larger
// bundle).
//
//	construction-restore -bundle FILE -verify-only [-max-total-mb N]
//	construction-restore -bundle FILE -target DIR [-forbid PATH]... [-native-out DIR] [-max-total-mb N]
package main

import (
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
	target := fs.String("target", "", "NEW absolute directory to restore into (must not exist; its parent must)")
	native := fs.String("native-out", "", "optional NEW absolute directory for native conversation copies (never resumed)")
	verify := fs.Bool("verify-only", false, "validate the bundle and print its manifest; write nothing")
	maxMB := fs.Int64("max-total-mb", construction.DefaultBundleTotalBytes>>20,
		fmt.Sprintf("decompressed budget in MiB, held in memory while checking (raise only for your own larger bundle; at most %d)", construction.HardMaxBundleTotalBytes>>20))
	var forbid multiFlag
	fs.Var(&forbid, "forbid", "a root the target must not lie under (repeatable; e.g. the vault)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *bundle == "" || (!*verify && *target == "") {
		fmt.Fprintln(stderr, "usage: construction-restore -bundle FILE (-verify-only | -target DIR [-forbid PATH]... [-native-out DIR]) [-max-total-mb N]")
		return 2
	}
	if *maxMB < 1 || *maxMB > construction.HardMaxBundleTotalBytes>>20 {
		fmt.Fprintf(stderr, "-max-total-mb must be 1–%d\n", construction.HardMaxBundleTotalBytes>>20)
		return 2
	}
	if *native != "" && *native == *target {
		fmt.Fprintln(stderr, "-native-out must be a different directory from -target")
		return 2
	}
	maxTotal := *maxMB << 20
	f, size, err := openBundle(*bundle, maxTotal)
	if err != nil {
		fmt.Fprintln(stderr, "bundle:", err)
		return 1
	}
	defer f.Close()
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if *verify {
		man, files, err := construction.ReadBundleWithin(f, size, maxTotal)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		_ = enc.Encode(map[string]any{"verified": true, "problemId": man.ProblemID, "subject": man.Subject, "generation": man.Generation,
			"files": len(files), "complete": man.Complete, "categories": man.Categories, "missing": man.Missing})
		return 0
	}
	if *native != "" {
		// checked before the restore, so a bad -native-out writes nothing
		if _, err := construction.CheckNewDir(*native, forbid); err != nil {
			fmt.Fprintln(stderr, "native-out:", err)
			return 1
		}
	}
	rep, err := construction.Restore(f, size, *target, construction.RestoreOptions{Forbidden: forbid, MaxTotalBytes: maxTotal})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	nativeState := "none in bundle"
	if len(rep.NativeFiles) > 0 {
		nativeState = "not rebound (no -native-out given); a restore never resumes an agent"
		if *native != "" {
			if err := writeNative(*native, forbid, rep); err != nil {
				fmt.Fprintf(stderr, "the store was restored at %s, but the native copies were not written: %v\n", *target, err)
				return 1
			}
			nativeState = "copied for explicit owner rebinding; not resumed"
		}
	}
	_ = enc.Encode(map[string]any{"restored": true, "report": rep, "native": nativeState, "target": *target})
	return 0
}

// openBundle opens a regular file no larger than a bundle within the budget
// can be. It is read through ReaderAt, never loaded whole; the restore
// decides only on the checked copies it reads into memory.
func openBundle(p string, maxTotal int64) (*os.File, int64, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, 0, err
	}
	if !fi.Mode().IsRegular() {
		return nil, 0, errors.New("not a regular file")
	}
	if limit := construction.MaxBundleArchiveBytes(maxTotal); fi.Size() > limit {
		return nil, 0, fmt.Errorf("%d bytes; a bundle within the %d MiB budget is at most %d", fi.Size(), maxTotal>>20, limit)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, err
	}
	if now, err := f.Stat(); err != nil || !os.SameFile(fi, now) {
		f.Close()
		return nil, 0, errors.New("the file changed while it was opened")
	}
	return f, fi.Size(), nil
}

// writeNative creates dir (new, checked like a restore target) and writes
// the native copies through an os.Root anchored on it: every name resolves
// beneath dir (no symlink or ".." leaves it) and every file is created
// exclusively. On failure the directory it created is removed.
func writeNative(dir string, forbid []string, rep *construction.RestoreReport) (err error) {
	if _, err := construction.CheckNewDir(dir, forbid); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	names := append([]string{}, rep.NativeFiles...)
	sort.Strings(names)
	for _, p := range names {
		rel := filepath.FromSlash(strings.TrimPrefix(p, "native/"))
		if d := filepath.Dir(rel); d != "." {
			if err := root.MkdirAll(d, 0o700); err != nil {
				return err
			}
		}
		out, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, werr := out.Write(rep.Native[p])
		if cerr := out.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return werr
		}
	}
	return nil
}
