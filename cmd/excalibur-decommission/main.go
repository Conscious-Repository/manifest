// excalibur-decommission never runs apply implicitly and never writes the vault.
package main

import (
	"flag"
	"fmt"
	"manifest/excaliburretire"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "plan", "plan or apply")
	root := flag.String("harness", "", "absolute legacy harness")
	data := flag.String("data-dir", "", "absolute Manifest data directory")
	receipt := flag.String("receipt", "", "plan receipt file")
	hash := flag.String("plan-sha256", "", "exact reviewed plan hash (apply only)")
	flag.Parse()
	c := excaliburretire.Config{Root: *root, DataDir: *data}
	m := excaliburretire.Systemd{Config: c}
	switch *mode {
	case "plan", "preflight":
		p := excaliburretire.Build(c, m)
		b := p.Bytes()
		if *receipt == "" {
			return fmt.Errorf("receipt path required")
		}
		// Exclusive creation prevents accidental destruction of unrelated data.
		f, e := os.OpenFile(*receipt, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(e) {
			old, re := os.ReadFile(*receipt)
			if re != nil || excaliburretire.Hash(old) != p.Hash() {
				return fmt.Errorf("receipt already exists with different state; use a new path")
			}
		} else {
			if e != nil {
				return e
			}
			_, e = f.Write(b)
			if e == nil {
				e = f.Sync()
			}
			ce := f.Close()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
		}
		fmt.Printf("plan_sha256=%s blockers=%d paused_capabilities=3 replay=false\n", p.Hash(), len(p.Blockers))
		if len(p.Blockers) > 0 {
			return fmt.Errorf("preflight blocked; inspect redacted receipt")
		}
		return nil
	case "apply":
		b, e := os.ReadFile(*receipt)
		if e != nil {
			return e
		}
		if e = excaliburretire.Apply(c, m, b, *hash); e != nil {
			return e
		}
		fmt.Println("engine retired/unavailable; extractors paused; state preserved; replay=false")
		return nil
	default:
		return fmt.Errorf("mode must be plan or apply")
	}
}
