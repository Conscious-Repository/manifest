// extractor-boundary-check prints the implemented commit boundary, without
// opening configuration, a vault, a harness, or any operational store.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"manifest/approvals"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: extractor-boundary-check (no arguments; offline architecture assessment)")
		os.Exit(2)
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	if err := e.Encode(approvals.CheckExtractionCommitBoundary()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
