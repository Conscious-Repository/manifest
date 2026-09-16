// aion-successor measures inputs and optionally runs a fixed-local no-write canary.
package main

import (
	"context"
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
	models := f.String("models", "", "explicit saved /v1/models JSON")
	accounting := f.String("accounting", "", "owner-reviewed exact-request token accounting JSON")
	accountingPin := f.String("accounting-sha256", "", "independently reviewed accounting file digest")
	reserve := f.Int("reserved-output", 4096, "reserved completion tokens")
	canary := f.Bool("canary", false, "invoke only fixed Sparks endpoint")
	receipt := f.String("receipt", "", "new private receipt outside vault/input")
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
	if *models != "" {
		read := func(path string, limit int64) ([]byte, error) {
			f, e := os.Open(path)
			if e != nil {
				return nil, errors.New("evidence-read-unavailable")
			}
			defer f.Close()
			b, e := io.ReadAll(io.LimitReader(f, limit+1))
			if e != nil || int64(len(b)) > limit {
				return nil, errors.New("evidence-read-unavailable")
			}
			return b, nil
		}
		b, e := read(*models, 1<<20)
		if e != nil {
			return e
		}
		cap, e := domainextract.LoadSparksCapability(c, b)
		if e != nil {
			return e
		}
		request, e := s.SparksRequest(c, *reserve)
		if e != nil {
			return e
		}
		if !*canary {
			return json.NewEncoder(out).Encode(domainextract.SparksMeasurement(s.Readiness(c), cap, request))
		}
		if !*noWrite || *expected == "" || *receipt == "" {
			return errors.New("canary-requires-no-write-pin-and-receipt")
		}
		b, e = read(*accounting, 8192)
		if e != nil {
			return e
		}
		a, e := domainextract.DecodeSparksAccounting(b, *accountingPin)
		if e != nil {
			return e
		}
		if a.ReservedOutput != *reserve {
			return errors.New("reserve-mismatch")
		}
		// Reserve a private receipt before any network activity, so an unsafe output
		// location cannot cause a model call. Replace contents through the open fd.
		return runCanary(out, *receipt, *vault, *root, s, c, cap, a)
	}
	if *canary || *accounting != "" || *receipt != "" {
		return errors.New("explicit-model-capability-required")
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

func runCanary(out io.Writer, path, vault, root string, s domainextract.AionInput, c domainextract.AionSuccessorConfig, cap domainextract.SparksCapability, a domainextract.SparksAccounting) error {
	f, err := domainextract.OpenSparksReceipt(path, vault, root)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = s.Revalidate(root); err != nil {
		return err
	}
	r := s.RunSparksCanary(context.Background(), c, cap, a)
	if err = f.Truncate(0); err != nil {
		return errors.New("receipt-write-failed")
	}
	if err = json.NewEncoder(f).Encode(r); err != nil {
		return errors.New("receipt-write-failed")
	}
	if err = f.Sync(); err != nil {
		return errors.New("receipt-write-failed")
	}
	if err = json.NewEncoder(out).Encode(r); err != nil {
		return errors.New("report-output-unavailable")
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}
