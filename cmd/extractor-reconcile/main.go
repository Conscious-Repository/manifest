// extractor-reconcile performs one explicit owner-authorized pre-provider retry.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"manifest/approvals"
	"manifest/domainextract"
	"manifest/hermes"
)

func main() {
	config := flag.String("config", "", "Manifest configuration path")
	id := flag.String("job", "", "exact original job ID")
	source := flag.String("source", "", "exact vault-relative source path")
	auth := flag.String("owner-authorization", "", "owner instruction reference authorizing this attempt")
	operation := flag.String("operation", "pre-provider", "pre-provider or post-runtime-fix")
	sourceHash := flag.String("source-sha256", "", "exact original source SHA256")
	parent := flag.String("parent-attempt", "", "exact parent attempt ID")
	runtime := flag.String("runtime-fix", "", "runtime fix fingerprint or commit reference")
	flag.Parse()
	if err := run(*config, *id, *source, *auth, *operation, *sourceHash, *parent, *runtime); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(path, id, source, auth, operation, sourceHash, parent, runtime string) error {
	var cfg struct {
		DataDir   string `json:"dataDir"`
		Vault     string `json:"vaultPath"`
		Harness   string `json:"excaliburPath"`
		Harnesses []struct {
			Path string `json:"path"`
		} `json:"harnesses"`
		Extraction domainextract.Config `json:"domainExtraction"`
		Hermes     hermes.Config        `json:"hermes"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	if cfg.Harness == "" && len(cfg.Harnesses) > 0 {
		cfg.Harness = cfg.Harnesses[0].Path
	}
	if !filepath.IsAbs(cfg.DataDir) || !filepath.IsAbs(cfg.Vault) || !filepath.IsAbs(cfg.Harness) || os.Getenv("TMPDIR") != filepath.Join(cfg.DataDir, "hermes-tmp") {
		return fmt.Errorf("absolute configured roots and existing private Hermes TMPDIR required")
	}
	info, err := os.Stat(os.Getenv("TMPDIR"))
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return fmt.Errorf("private Hermes scratch unavailable")
	}
	svc := domainextract.New(context.Background(), cfg.DataDir, cfg.Vault, cfg.Harness, cfg.Extraction, hermes.NewRunner(cfg.Hermes), approvals.NewStore(filepath.Join(cfg.Harness, "artifacts")))
	var job domainextract.Job
	switch operation {
	case "pre-provider":
		if sourceHash != "" || parent != "" || runtime != "" {
			return fmt.Errorf("post-runtime-fix arguments require explicit operation")
		}
		job, err = svc.RetryPreProvider(id, source, auth)
	case "post-runtime-fix":
		job, err = svc.RetryPostRuntimeFix(id, source, sourceHash, parent, auth, runtime)
	default:
		return fmt.Errorf("unknown reconciliation operation")
	}
	if err != nil {
		return err
	}
	// Never emit transcript text, context or candidate payloads to the terminal.
	summary := struct {
		ID, ParentID, State, Reason, Model string
		Published                          int
		Execution                          *hermes.ExtractionExecution
	}{job.ID, job.ParentID, job.State, job.Reason, job.Model, job.Published, job.Execution}
	if err = json.NewEncoder(os.Stdout).Encode(summary); err != nil {
		return err
	}
	if job.State != "completed" {
		return fmt.Errorf("attempt %s retained as %s", job.ID, job.State)
	}
	return nil
}
