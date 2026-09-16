// extractor-evaluate validates copied evidence without operational I/O.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"

	"manifest/domainextract"
)

func run(args []string, out io.Writer) int {
	flags := flag.NewFlagSet("extractor-evaluate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	fixture := flags.String("fixture", "", "absolute copied evaluation JSON fixture path")
	copied := flags.Bool("copied-fixture", false, "attest fixture is a copy, not operational data")
	var report domainextract.EvaluationReport
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		report = domainextract.DecodeEvaluation(nil)
	} else {
		report = domainextract.ReadEvaluationFixture(*fixture, *copied)
	}
	if json.NewEncoder(out).Encode(report) != nil {
		return 1
	}
	// Mechanical validation is not a successful semantic reduction.
	if report.EvidenceState != "consistent-copied-claims-only" {
		return 1
	}
	return 2
}
func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
