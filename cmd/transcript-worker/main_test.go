package main

import (
	"os"
	"path/filepath"
	"testing"

	"manifest/approvals"
	"manifest/transcriptsync"
)

func TestProposalStoreContinuityDoesNotCreateInbox(t *testing.T) {
	root := t.TempDir()
	cfg := transcriptsync.Config{LegacyRoot: root, Granola: transcriptsync.SourceConfig{Enabled: true, ContinuityOnly: true}}
	ap, err := proposalStore(cfg)
	if err != nil || ap != nil {
		t.Fatalf("continuity store: %v, %v", ap, err)
	}
	if _, err := os.Stat(filepath.Join(root, "artifacts")); !os.IsNotExist(err) {
		t.Fatal("continuity created artifacts")
	}
	cfg.Granola.ContinuityOnly = false
	if _, err := proposalStore(cfg); err == nil {
		t.Fatal("accepted missing canonical inbox")
	}
}

func TestProposalStoreFilesDeduplicatesAndCannotApply(t *testing.T) {
	for _, source := range []string{"granola", "pocket"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			approvals.NewStore(filepath.Join(root, "artifacts"))
			cfg := transcriptsync.Config{LegacyRoot: root}
			if source == "granola" {
				cfg.Granola.Enabled = true
			} else {
				cfg.Pocket.Enabled = true
			}
			ap, err := proposalStore(cfg)
			if err != nil || ap == nil {
				t.Fatalf("proposal store: %v", err)
			}
			p := approvals.Proposal{Action: "Create fixture note", Type: approvals.TypeCreateVaultNote, ApplyPath: "2026-09-18 fixture.md", Proposed: "---\n" + source + "-id: fixture\n---\nTranscript"}
			first, created, err := ap.ProposeTranscript(source, "fixture", p)
			if err != nil || !created || first.Status != "pending" {
				t.Fatalf("proposal: %+v, %v, %v", first, created, err)
			}
			second, created, err := ap.ProposeTranscript(source, "fixture", p)
			if err != nil || created || second.ID != first.ID {
				t.Fatalf("dedupe: %+v, %v, %v", second, created, err)
			}
			if err := ap.Confirm(first.ID); err == nil {
				t.Fatal("worker store must not apply vault notes")
			}
			if len(ap.List("pending")) != 1 {
				t.Fatal("proposal did not stay pending")
			}
		})
	}
}
