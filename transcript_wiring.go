package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"manifest/server"
	"manifest/transcriptsync"
)

// wireTranscriptOwnership uses the standalone worker's configuration only for
// read-side ownership. It must never attach that service to the sync HTTP routes
// or start it: the worker owns its cadence and continuity-only permissions.
// Without a worker config, the in-process service remains the authority.
func wireTranscriptOwnership(cfg Config, hs []server.Harness, local *transcriptsync.Service) error {
	svc, err := transcriptOwnershipService(cfg, local)
	for _, h := range hs {
		if h.Name == "excalibur" && h.Spirits != nil {
			h.Spirits.WithConnectorHandoffs(cfg.DataDir).WithTranscriptSync(svc)
		}
	}
	return err
}

func transcriptOwnershipService(cfg Config, local *transcriptsync.Service) (*transcriptsync.Service, error) {
	b, err := os.ReadFile(filepath.Join(cfg.DataDir, "transcript-worker.json"))
	if os.IsNotExist(err) {
		return local, nil
	}
	if err != nil {
		return nil, fmt.Errorf("transcript ownership worker config: %w", err)
	}
	var worker struct {
		DataDir        string                `json:"dataDir"`
		TranscriptSync transcriptsync.Config `json:"transcriptSync"`
	}
	if err := json.Unmarshal(b, &worker); err != nil {
		return nil, fmt.Errorf("transcript ownership worker config: %w", err)
	}
	if !filepath.IsAbs(worker.DataDir) || worker.DataDir != cfg.DataDir || !filepath.IsAbs(worker.TranscriptSync.LegacyRoot) {
		return nil, fmt.Errorf("transcript ownership worker requires matching dataDir and absolute legacy root")
	}
	for _, c := range []transcriptsync.SourceConfig{worker.TranscriptSync.Granola, worker.TranscriptSync.Pocket} {
		if c.Enabled && (!c.ContinuityOnly || c.Account == "") {
			return nil, fmt.Errorf("transcript ownership worker requires named accounts and continuityOnly=true")
		}
	}
	return transcriptsync.New(worker.DataDir, worker.TranscriptSync, nil, nil).WithHandoffGuard(worker.DataDir), nil
}
