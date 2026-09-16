// transcript-sync-import previews or imports one legacy connector checkpoint.
// It never pauses a duty, activates polling, or modifies approval history.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"manifest/transcriptsync"
	"os"
	"strings"
)

func main() {
	config := flag.String("config", "", "Manifest config path (required)")
	source := flag.String("source", "", "granola or pocket")
	keyFile := flag.String("key-file", "", "existing credential file; bytes are never printed")
	apply := flag.Bool("apply", false, "import state and credential; requires source disabled")
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
	var key string
	if apply {
		key = strings.TrimSpace(os.Getenv(strings.ToUpper(source) + "_API_KEY"))
		if key == "" {
			b, e = os.ReadFile(keyFile)
			if e != nil {
				return fmt.Errorf("credential file unreadable")
			}
			key = strings.TrimSpace(string(b))
		}
	}
	svc := transcriptsync.New(cfg.DataDir, cfg.TranscriptSync, nil, nil)
	st, e := svc.Import(source, cfg.ExcaliburPath, key, apply)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"applied": apply, "source": source, "checkpoint": st.Watermark, "checkpointHash": st.ImportedFrom, "activation": "disabled; pause and reconcile the legacy duty before enabling"})
}
