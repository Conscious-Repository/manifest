package vaultindex

import (
	"manifest/vaultwriter"
	"os"
	"path/filepath"
	"testing"
)

func TestPassagesScopeAndRawAnchors(t *testing.T) {
	root := t.TempDir()
	for p, raw := range map[string]string{"sources/one.md": "# Evidence\r\n🌿 attention improves discovery\r\n", "sources/two.md": "attention evidence", "sources/daily/day.md": "attention private journal", "sources/agent.md": "---\nai-authored: true\n---\nattention", "elsewhere.md": "attention", "system/a.md": "attention"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0755)
		_ = os.WriteFile(filepath.Join(root, p), []byte(raw), 0644)
	}
	ix, err := Open(Config{VaultRoot: root, AIRegions: []string{"sources/agent.md"}})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err = ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	empty, err := ix.Passages("attention", "draft.md", nil, nil)
	if err != nil || len(empty) != 0 {
		t.Fatal("unconfigured search disclosed notes")
	}
	results, err := ix.Passages("attention", "sources/two.md", []string{"sources"}, []string{"sources/daily"})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range results {
		if p.Path != "sources/one.md" {
			t.Fatal("scope leak", p.Path)
		}
		raw, _ := os.ReadFile(filepath.Join(root, p.Path))
		if string(raw[p.Start:p.End]) != p.Quote || vaultwriter.Revision(raw) != p.Revision {
			t.Fatal("unverifiable passage")
		}
	}
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	_ = os.WriteFile(filepath.Join(root, "sources/one.md"), []byte("replaced externally"), 0644)
	results, err = ix.Passages("attention", "sources/two.md", []string{"sources"}, []string{"sources/daily"})
	if err != nil || len(results) != 0 {
		t.Fatal("stale index returned an irrelevant passage", err, results)
	}
}

func TestLinkedPassageUsesFilenameAndExcludesAgentNotes(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "max example.md"), []byte("An investor interested in longevity."), 0644)
	os.WriteFile(filepath.Join(root, "agent example.md"), []byte("---\nai-authored: true\n---\nNot owner knowledge."), 0644)
	ix, err := Open(Config{VaultRoot: root, AIRegions: []string{"agent example.md"}})
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	ix.Rebuild()
	p, err := ix.LinkedPassage("max example|Max", "draft.md", nil)
	if err != nil || p == nil || p.Quote != "An investor interested in longevity." {
		t.Fatal(p, err)
	}
	p, err = ix.LinkedPassage("agent example", "draft.md", nil)
	if err != nil || p != nil {
		t.Fatal("agent context leaked", p, err)
	}
}
