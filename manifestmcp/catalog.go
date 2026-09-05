package manifestmcp

import (
	"encoding/json"
	"fmt"
)

// Catalog is generated without loading any private domain records.
//
//go:generate sh -c "go run ../cmd/manifest-mcp --catalog > catalog.json"
//go:generate sh -c "go run ../cmd/manifest-mcp --readme > README.md"
func Catalog() ([]byte, error) {
	a, err := New("/manifest-catalog/vault", "/manifest-catalog/data", "system")
	if err != nil {
		return nil, err
	}
	a.Server()
	b, err := json.MarshalIndent(Object{"version": Version, "tools": a.Tools, "sources": a.Runs.Sources(), "vocabulary": a.Graph.Vocabulary()}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	return append(b, '\n'), nil
}

// README is generated alongside the catalog, including the complete tool list.
func README() ([]byte, error) {
	a, err := New("/manifest-catalog/vault", "/manifest-catalog/data", "system")
	if err != nil {
		return nil, err
	}
	a.Server()
	text := `# Manifest local MCP — Phase 1

Local Go stdio MCP adapter over Manifest's recruiting and graph services. Uses
 the official MCP Go SDK for protocol handling and Go-type schema generation.
No portal token or HTTP owner endpoint is used. Stores have no live vault writer;
acceptance uses a capture writer that retains proposed file content in memory.
No source execution, lookup, sweep, approval, or write tool is exposed.

Build and run from the repository root:

` + "```sh\ngo build -o /home/benjamin/.local/bin/manifest-mcp ./cmd/manifest-mcp\nmanifest-mcp --config /home/benjamin/src/manifest/config.json\n```" + `

The config supplies vaultPath, dataDir and systemRoot (default system). Startup
reads configuration only and does not seed files. This process runs as the local
owner and can read private recruiting data; tool filtering is not OS isolation.

## Keep up to date

The single registration table uses Go types (including RunRequest, NetworkPerson
and graph.Edge) to generate input schemas. Both the app and MCP use
RunStore.RegisterDefaults; source scope fields and graph vocabulary come from
those domain definitions. No separately authored JSON schema exists.

` + "```sh\ngo generate ./manifestmcp\ngo test ./manifestmcp\ngo run ./cmd/manifest-mcp --catalog\n```" + `

Generation rewrites this README and catalog.json. TestCatalogFresh fails on either
stale file. Review generated changes and bump Version when semantics change;
types cannot automatically detect a changed semantic contract. The full schemas,
source scope fields and graph vocabulary are in [catalog.json](catalog.json).

## Tools

| Tool | Contract |
| --- | --- |
`
	for _, t := range a.Tools {
		text += fmt.Sprintf("| `%s` | %s |\n", t.Name, t.Description)
	}
	text += `
## Preparation contract and limitations

Results are structured objects with canonical namespace/domain/kind/ID refs,
content revisions and operationId. Ambiguous exact names return all matches;
callers must choose a ref. Recruiting and general graph identities stay separate.
Graph context covers stored general-graph claims, not server-only task, calendar,
contact or note projections. Unregistered external endpoints cannot be prepared.

Prepare returns pending_approval, persisted=false and executable=false. The ID is
a content hash, not an approval token or durable operation record. Previews name
one draft and exact proposed vault files, knowledge additions, suppression,
queue transitions and ledger effects. Generated dates are frozen at preparation;
Phase 2 must bind time and revisions or regenerate the preview. Source scopes
resolve supported fields and web bounds with shared services; external network,
robots and response validation cannot be done without retrieval. Manual source
has no network effect. Source execution is standing_authorization; true changes,
including rejection's durable tombstone, are human_approval.

HTTP acceptance and graph handlers currently stamp owner audit/default source;
the people marking handler also assumes owner consent. This adapter does not
assert owner consent automatically. Phase 2 needs actor-aware authorization,
durable operations, stale-state checks over every affected file, approval-bound
payloads, idempotency, queue persistence/recovery, ledger receipts, partial
knowledge outcomes and undo/takeover. No approval or execution is implemented here.

## Alfred / Hermes

Run ` + "`python3 integrations/manifest-mcp/wire_hermes.py`" + ` after building the binary.
It merges only mcp_servers.manifest into ~/.hermes/config.yaml and includes
mcp-manifest in Manifest's configured readToolsets. Existing settings are retained.
The repository default includes mcp-manifest too. Installed Hermes uses that
hyphenated toolset key and exposes tools with its mcp__manifest__ prefix.

` + "```sh\nhermes mcp test manifest\npython3 integrations/manifest-mcp/check_hermes.py\n```" + `

The check uses the installed Hermes virtualenv to discover this server and asserts
all eleven tools survive Alfred's configured toolset filter, without an LLM call.
Restart the running Manifest process after deploying a build/config change and
start a new Alfred turn. Discovery does not establish an in-gateway conversation.
`
	return []byte(text), nil
}
