// Owner-invoked synthetic contract check; never registered as a production duty.
package main

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"manifest/hermes"
)

const receiptDirectory = "/home/benjamin/workbench-staging/excalibur-retirement"
const receiptName = "33-deepseek-primary-canary.jsonl"

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

func run(args []string, out io.Writer) int {
	if len(args) == 0 {
		return emit(out, hermes.PrimaryCanaryRefusal("confirmation required"), 2)
	}
	if len(args) != 1 || args[0] != "--confirm-live" {
		return emit(out, hermes.PrimaryCanaryRefusal("invalid arguments"), 2)
	}
	root, err := os.OpenRoot(receiptDirectory)
	if err != nil {
		return emit(out, hermes.PrimaryCanaryRefusal("receipt unavailable"), 1)
	}
	defer root.Close()
	return once(context.Background(), root, out, hermes.RunPrimaryCanary)
}

// Exclusive creation is both the receipt and the permanent one-shot latch.
// A crash leaves the synced refusal in place. No cleanup/retry path exists.
func once(ctx context.Context, root *os.Root, out io.Writer, invoke func(context.Context) hermes.PrimaryCanaryReport) int {
	f, err := root.OpenFile(receiptName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return emit(out, hermes.PrimaryCanaryRefusal("prior or uncertain invocation"), 1)
	}
	defer f.Close()
	initial := hermes.PrimaryCanaryRefusal("outcome uncertain")
	if json.NewEncoder(f).Encode(initial) != nil || f.Sync() != nil {
		return emit(out, hermes.PrimaryCanaryRefusal("receipt unavailable"), 1)
	}
	// Sync the directory entry too before permitting the external request.
	dir, err := root.Open(".")
	if err != nil {
		return emit(out, hermes.PrimaryCanaryRefusal("receipt unavailable"), 1)
	}
	err = dir.Sync()
	dir.Close()
	if err != nil {
		return emit(out, hermes.PrimaryCanaryRefusal("receipt unavailable"), 1)
	}
	report := invoke(ctx)
	if json.NewEncoder(f).Encode(report) != nil || f.Sync() != nil {
		return emit(out, hermes.PrimaryCanaryRefusal("outcome persistence failed"), 1)
	}
	if report.Status != "canary passed" {
		return emit(out, report, 1)
	}
	return emit(out, report, 0)
}

func emit(out io.Writer, report hermes.PrimaryCanaryReport, code int) int {
	if json.NewEncoder(out).Encode(report) != nil {
		return 1
	}
	return code
}
