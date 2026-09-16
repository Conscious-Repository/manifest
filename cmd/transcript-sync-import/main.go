// transcript-sync-import previews one legacy connector watermark. Applied
// imports are blocked; connector-checkpoint prepares reconciled evidence.
// It never pauses a duty, activates polling, or modifies approval history.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"manifest/transcriptsync"
	"os"
)

func main() {
	config := flag.String("config", "", "Manifest config path (required)")
	source := flag.String("source", "", "granola or pocket")
	keyFile := flag.String("key-file", "", "deprecated; applied imports are blocked and this file is never read")
	apply := flag.Bool("apply", false, "request handoff (currently blocked pending legacy dispatch fence)")
	flag.Parse()
	if e := run(*config, *source, *keyFile, *apply); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(path, source, keyFile string, apply bool) error {
	var cfg struct {
		DataDir       string `json:"dataDir"`
		ExcaliburPath string `json:"excaliburPath"`
		Harnesses     []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"harnesses"`
		TranscriptSync transcriptsync.Config `json:"transcriptSync"`
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, &cfg); e != nil {
		return e
	}
	for _, h := range cfg.Harnesses {
		if h.Name == "excalibur" {
			cfg.ExcaliburPath = h.Path
		}
	}
	if cfg.DataDir == "" || cfg.ExcaliburPath == "" {
		return fmt.Errorf("explicit dataDir and excalibur harness required")
	}
	if apply {
		return fmt.Errorf("handoff blocked: legacy dispatch fence and complete reconciliation required; credentials were not read")
	}
	svc := transcriptsync.New(cfg.DataDir, cfg.TranscriptSync, nil, nil)
	st, e := svc.Import(source, cfg.ExcaliburPath, "", apply)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"applied": apply, "source": source, "checkpoint": st.Watermark, "checkpointHash": st.ImportedFrom, "activation": "blocked; shared legacy dispatch fence and reconciled handoff required"})
}
