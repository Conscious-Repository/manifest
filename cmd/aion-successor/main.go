// aion-successor measures explicitly supplied inputs. It never invokes a model.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"

	"manifest/domainextract"
)

type sources []string

func (s *sources) String() string     { return "" }
func (s *sources) Set(v string) error { *s = append(*s, v); return nil }
func run(args []string, out io.Writer) error {
	f := flag.NewFlagSet("aion-successor", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	root := f.String("fixture-root", "", "explicit copied fixture root")
	vault := f.String("vault-root", "", "explicit live vault or excluded boundary")
	copied := f.Bool("copied-fixture", false, "attest immutable offline copy")
	live := f.Bool("live-read", false, "explicitly read live vault")
	noWrite := f.Bool("no-write", false, "required for live reads")
	cfg := f.String("config", "", "explicit successor config JSON")
	expected := f.String("expected-input-sha256", "", "pin previously measured complete input")
	var names sources
	f.Var(&names, "source", "selected relative note path; repeat up to four times")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return errors.New("invalid-flags")
	}
	clean := func(p string) bool { return filepath.IsAbs(p) && filepath.Clean(p) == p && p != "/" }
	if !clean(*vault) {
		return errors.New("explicit-vault-boundary-required")
	}
	if *live {
		if !*noWrite || *copied || *root != "" {
			return errors.New("live-read-requires-no-write")
		}
		*root = *vault
	} else {
		if !*copied || !clean(*root) {
			return errors.New("explicit-copied-fixture-required")
		}
		within := func(a, b string) bool { return a == b || strings.HasPrefix(a, b+"/") }
		if within(*root, *vault) || within(*vault, *root) {
			return errors.New("fixture-vault-overlap")
		}
	}
	// Config bytes are never echoed. Bound reads and fixed errors avoid leaking
	// arbitrary paths or malformed configuration into the public diagnostic.
	file, err := os.Open(*cfg)
	if err != nil {
		return errors.New("config-read-unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	file.Close()
	if err != nil {
		return errors.New("config-read-unavailable")
	}
	c, err := domainextract.DecodeAionSuccessorConfig(raw)
	if err != nil {
		return err
	}
	s, err := domainextract.BuildAionInput(*root, names, *expected)
	if err != nil {
		return err
	}
	r := s.Readiness(c)
	if err = json.NewEncoder(out).Encode(r); err != nil {
		return errors.New("report-output-unavailable")
	}
	return errors.New(r.Reason)
}
func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"state": "refused", "reason": err.Error()})
		os.Exit(1)
	}
}
