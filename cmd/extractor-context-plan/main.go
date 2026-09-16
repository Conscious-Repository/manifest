// extractor-context-plan reads only an explicit copied fixture and writes only
// a new redacted report. No configuration, model, service or vault is discovered.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"manifest/domainextract"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flag := flag.NewFlagSet("extractor-context-plan", flag.ContinueOnError)
	flag.SetOutput(io.Discard)
	fixture := flag.String("fixture-root", "", "absolute copied fixture root")
	excluded := flag.String("excluded-vault", "", "absolute real vault boundary; never opened")
	output := flag.String("report", "", "new absolute report path outside fixture and vault")
	ritual := flag.String("ritual", "", "aion, real-estate, or ooda-email")
	budget := flag.Int("budget", 56000, "hard serialized Input byte budget, at most 56000")
	copied := flag.Bool("copied-fixture", false, "attest fixture is an offline copy, not operational state")
	if err := flag.Parse(args); err != nil {
		return fmt.Errorf("invalidPlanningFlags")
	}

	if !*copied || flag.NArg() != 0 {
		return fmt.Errorf("explicitCopiedFixtureRequired")
	}
	if err := domainextract.PlanningPaths(*fixture, *excluded, *output); err != nil {
		return err
	}
	m, err := domainextract.ReadContextManifest(*fixture, *ritual)
	if err != nil {
		return err
	}
	p, err := domainextract.PlanContext(m, nil, *budget)
	if err != nil {
		return err
	}
	if err = domainextract.WriteContextReport(*output, domainextract.RedactedContextReport(m, p)); err != nil {
		return err
	}
	return nil
}
