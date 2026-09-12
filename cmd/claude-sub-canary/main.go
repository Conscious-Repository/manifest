// claude-sub-canary is a stdout-only readiness refusal, never a provider adapter.
package main

import (
	"encoding/json"
	"io"
	"os"

	"manifest/reintake"
)

func run(args []string, out io.Writer) int {
	fixture := len(args) == 1 && args[0] == "--test-fixture"
	if len(args) != 0 && !fixture {
		return 2
	}
	report, _ := reintake.ClaudeCanary(fixture)
	if json.NewEncoder(out).Encode(report) != nil {
		return 2
	}
	// A passing synthetic test never changes the readiness exit status.
	return 1
}

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }
