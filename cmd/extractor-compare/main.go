// extractor-compare is an offline refusal-only evidence checker.
package main

import (
	"flag"
	"fmt"
	"io"
	"manifest/extractorcompare"
	"os"
)

type hashes []string

func (h *hashes) String() string     { return "" }
func (h *hashes) Set(s string) error { *h = append(*h, s); return nil }
func run(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("extractor-compare", flag.ContinueOnError)
	// Flag errors can echo private values. Emit only fixed diagnostics below.
	fs.SetOutput(io.Discard)
	var q extractorcompare.Request
	var legacy, successor hashes
	fs.StringVar(&q.Directory, "evidence-dir", "", "private opaque staging directory")
	fs.StringVar(&q.Vault, "vault", "", "absolute vault path, exclusion only")
	fs.StringVar(&q.Ritual, "ritual", "", "aion, real-estate, ooda-email")
	fs.Var(&legacy, "legacy", "explicit SHA-256; repeat for each legacy artifact")
	fs.Var(&successor, "successor", "explicit SHA-256; repeat for each successor artifact")
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		fmt.Fprintln(out, "comparison arguments refused")
		return 2
	}
	q.Legacy = legacy
	q.Successor = successor
	_, err := extractorcompare.Check(q)
	if err == extractorcompare.ErrRefused {
		fmt.Fprintln(out, "comparison-unrun; durable refusal: report.json; semantic review required")
		return 1
	}
	fmt.Fprintln(out, "comparison refused; no completed report; check staging and explicit inputs")
	return 2
}
func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
