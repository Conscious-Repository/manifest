// extractor-check is a read-only ownership and offline proposal-shape probe.
// It never invokes a model, publishes an approval, or writes operational state.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"manifest/domainextract"
)

func main() {
	data := flag.String("data-dir", "", "absolute Manifest data directory")
	harness := flag.String("harness", "", "absolute Excalibur harness directory")
	ritual := flag.String("ritual", "", "aion, real-estate, or ooda-email")
	enabled := flag.Bool("enabled", false, "successor configuration intent (does not enable anything)")
	inputPath := flag.String("input", "", "optional saved domainextract.Input JSON snapshot")
	replyPath := flag.String("reply", "", "optional saved model reply for offline shape validation")
	flag.Parse()
	if !filepath.IsAbs(*data) || !filepath.IsAbs(*harness) || (*ritual != "aion" && *ritual != "real-estate" && *ritual != "ooda-email") || (*inputPath == "") != (*replyPath == "") {
		fail(fmt.Errorf("absolute data-dir/harness and supported ritual required; supply both input and reply or neither"))
	}
	state, detail := domainextract.Readiness(*data, *harness, *ritual, *enabled)
	report := map[string]any{"duty": "extractor/" + *ritual, "state": state, "detail": detail, "replay": false, "effects": false, "semanticValidationPerformed": false}
	if *inputPath != "" {
		b, err := os.ReadFile(*inputPath)
		if err != nil {
			fail(err)
		}
		var input domainextract.Input
		if err = json.Unmarshal(b, &input); err != nil {
			fail(err)
		}
		if input.Ritual != *ritual {
			fail(fmt.Errorf("input ritual mismatch"))
		}
		reply, err := os.ReadFile(*replyPath)
		if err != nil {
			fail(err)
		}
		proposals, err := domainextract.ValidateReply(input, string(reply))
		if err != nil {
			fail(err)
		}
		shape := []map[string]string{}
		for _, p := range proposals {
			shape = append(shape, map[string]string{"id": p.ID, "type": p.Type, "applyPath": p.ApplyPath})
		}
		report["shapeValidated"] = true
		report["proposals"] = shape
		report["sourceSnapshotId"] = input.ID()
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
