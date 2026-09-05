package manifestmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"manifest/graph"
	"manifest/recruiting"
)

func TestCatalogFresh(t *testing.T) {
	for path, generate := range map[string]func() ([]byte, error){"catalog.json": Catalog, "README.md": README} {
		want, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s stale; run go generate ./manifestmcp", path)
		}
	}
}
func fixture(t *testing.T) (*Adapter, recruiting.Run, string) {
	t.Helper()
	base := t.TempDir()
	vault := filepath.Join(base, "vault")
	data := filepath.Join(base, "data")
	write := func(p string, b []byte) error {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return err
		}
		return os.WriteFile(p, b, 0600)
	}
	r := recruiting.NewStore(vault, "system/aion/recruiting", write)
	if err := r.Ensure(); err != nil {
		t.Fatal(err)
	}
	runs, err := recruiting.NewRunStore(filepath.Join(data, "recruiting/runs"), r)
	if err != nil {
		t.Fatal(err)
	}
	runs.RegisterDefaults()
	run, err := runs.Execute(context.Background(), recruiting.RunRequest{Source: "manual", Query: "Ada Example", Max: 1}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	g := graph.NewStore(vault, "system/graph", write)
	for _, e := range []graph.Entity{{ID: "ada", Kind: "person", Title: "Ada Example", Source: "fixture"}, {ID: "lab", Kind: "org", Title: "Example Lab", Ref: "https://example.org/people", Source: "fixture"}} {
		if _, _, err := g.AddEntity(e); err != nil {
			t.Fatal(err)
		}
	}
	a, err := New(vault, data, "system")
	if err != nil {
		t.Fatal(err)
	}
	return a, run, base
}
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestAllToolsOverMCPAndNoVaultEffects(t *testing.T) {
	a, run, _ := fixture(t)
	before := revision(snapshot(t, a.Vault))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := a.Server().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 13 {
		t.Fatalf("got %d tools", len(list.Tools))
	}
	person := Ref{"graph", "manifest", "person", "ada"}
	lab := Ref{"graph", "manifest", "org", "lab"}
	cases := map[string]any{
		"capabilities.list": Object{}, "entity.resolve": ResolveInput{Query: "Ada Example"}, "entity.get": person, "sources.list": Object{}, "source_run.get": RunInput{run.ID}, "graph.neighbors": NeighborsInput{Ref: person, To: &lab},
		"source_run.prepare":       SourceInput{Request: recruiting.RunRequest{Source: "web", Query: "imaging", Max: 999}, Seed: &lab},
		"candidate_accept.prepare": DraftInput{RunID: run.ID, DraftID: "d1"}, "candidate_reject.prepare": DraftInput{RunID: run.ID, DraftID: "d1", Reason: "not this role"},
		"network_person.prepare": PersonInput{Ref: person, Person: recruiting.NetworkPerson{Source: "test"}},
		"graph_edge.prepare":     EdgeInput{From: person, To: lab, Edge: graph.Edge{Kind: "member_of", Basis: "lab page", Source: "test", Confidence: "0.8", Inferred: true}},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError {
				t.Fatalf("tool error: %+v", res.Content)
			}
			b, err := json.Marshal(res.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(name, ".prepare") && name != "source_run.prepare" && !bytes.Contains(b, []byte(`"pending_approval"`)) {
				t.Fatalf("not pending: %s", b)
			}
			if strings.HasSuffix(name, ".prepare") && (!bytes.Contains(b, []byte(`"persisted":true`)) || name != "source_run.prepare" && !bytes.Contains(b, []byte(`"executable":false`))) {
				t.Fatalf("unsafe contract: %s", b)
			}
		})
	}
	if after := revision(snapshot(t, a.Vault)); after != before {
		t.Fatal("read/prepare changed vault files")
	}
	// Protocol errors must never become a route to execution.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "operation.decide", Arguments: Object{}})
	if err == nil && !res.IsError {
		t.Fatal("agent decision tool succeeded")
	}
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "entity.get", Arguments: Object{"namespace": "manifest", "domain": "graph", "kind": "person", "id": "ada", "unexpected": true}})
	if err == nil && !res.IsError {
		t.Fatal("unknown argument accepted")
	}
}
func TestScopeAndIdentityFailures(t *testing.T) {
	a, run, _ := fixture(t)
	lab := Ref{"graph", "manifest", "org", "lab"}
	if _, err := a.sourcePrepare(SourceInput{Seed: &lab, Request: recruiting.RunRequest{Source: "web", Query: "mri", Fields: map[string]string{"seed_url": "https://different.example"}}}); err == nil {
		t.Fatal("conflicting seed allowed")
	}
	if _, err := a.sourcePrepare(SourceInput{Request: recruiting.RunRequest{Source: "web", Query: "mri", Fields: map[string]string{"seed_url": "http://127.0.0.1/"}}}); err == nil {
		t.Fatal("unsafe web scope accepted")
	}
	if _, err := a.draftPrepare(DraftInput{RunID: run.ID, DraftID: "d999"}, true); err == nil {
		t.Fatal("missing draft accepted")
	}
	if _, err := a.get(Ref{"graph", "other", "person", "ada"}); err == nil {
		t.Fatal("wrong namespace resolved")
	}
	if _, err := a.get(Ref{"graph", "manifest", "person", "../../etc/passwd"}); err == nil {
		t.Fatal("raw path resolved")
	}
	if _, err := a.edgePrepare(EdgeInput{From: Ref{"graph", "manifest", "person", "ada"}, To: lab, Edge: graph.Edge{Kind: "invented", Basis: "test", Source: "test"}}); err == nil {
		t.Fatal("invalid edge kind accepted")
	}
}
func TestAcceptancePreviewMatchesConverter(t *testing.T) {
	a, run, _ := fixture(t)
	out, err := a.draftPrepare(DraftInput{RunID: run.ID, DraftID: "d1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	p := out["operation"].(Object)["preview"].(Object)
	now := p["asOf"].(time.Time)
	files := p["vaultFiles"].(map[string]string)
	var wrote int
	live := recruiting.NewStore(a.Vault, a.Root, func(path string, b []byte) error {
		rel, _ := filepath.Rel(a.Vault, path)
		if files[rel] != string(b) {
			t.Errorf("preview differs from converter: %s", rel)
		}
		wrote++
		return nil
	})
	if _, err := live.AcceptDraft(run.Drafts[0].Draft, now); err != nil {
		t.Fatal(err)
	}
	if wrote == 0 {
		t.Fatal("no candidate file preview")
	}
}

func TestRejectPreviewMatchesPersistence(t *testing.T) {
	a, run, _ := fixture(t)
	out, err := a.draftPrepare(DraftInput{RunID: run.ID, DraftID: "d1", Reason: "not now"}, false)
	if err != nil {
		t.Fatal(err)
	}
	preview := out["operation"].(Object)["preview"].(Object)
	now := preview["asOf"].(time.Time)
	files := preview["vaultFiles"].(map[string]string)
	r := recruiting.NewStore(a.Vault, a.Root, func(path string, b []byte) error {
		rel, _ := filepath.Rel(a.Vault, path)
		if files[rel] != string(b) {
			t.Errorf("suppression preview differs: %s", rel)
		}
		return nil
	})
	runs, err := recruiting.NewRunStore(a.Runs.Root(), r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.Reject(run.ID, "d1", "not now", now); err != nil {
		t.Fatal(err)
	}
	for path, want := range preview["cacheFiles"].(map[string]string) {
		got, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(a.Runs.Root())), path))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("queue preview differs: %s", path)
		}
	}
}
