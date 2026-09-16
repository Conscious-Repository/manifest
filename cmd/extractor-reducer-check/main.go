// extractor-reducer-check validates copied evidence without operational I/O.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"

	"manifest/domainextract"
)

func run(args []string, out io.Writer) int {
	flags := flag.NewFlagSet("extractor-reducer-check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	fixture := flags.String("fixture", "", "absolute copied reducer JSON fixture path")
	copied := flags.Bool("copied-fixture", false, "attest fixture is a copy, not operational data")
	var report domainextract.ReducerReport
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		report = domainextract.DecodeReducer(nil)
	} else {
		report = domainextract.ReadReducerFixture(*fixture, *copied)
	}
	if json.NewEncoder(out).Encode(report) != nil {
		return 1
	}
	// Mechanical validation is not a successful semantic reduction.
	if report.MechanicalState != "reducer-valid-mechanical-only" {
		return 1
	}
	return 2
}
func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
