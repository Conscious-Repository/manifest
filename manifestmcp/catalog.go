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
	text := `# Manifest local MCP — Phase 4

Local Go stdio MCP adapter over Manifest's recruiting and graph services. Uses
 the official MCP Go SDK for protocol handling and Go-type schema generation.
No portal token or HTTP owner endpoint is used. Preparation persists an immutable
operation under dataDir/operations. Source execution has bounded standing
authorization. True world changes require a separate owner decision, then
operation.execute applies the saved payload through shared domain services and
narrow approved-proposal vaultwriter capabilities.

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
content revisions and operationId. Ambiguous titles, IDs, slugs or partial matches return all matches;
callers must choose a ref. Recruiting and general graph identities stay separate.
All refs use namespace manifest. Recruiting seed (including class lab) and role
are distinct from graph org and graph task; no title/URL-based mapping is implied.
Candidate knowledge preserves its canonical person ID in the general graph, but
call entity.resolve with domain=graph and that ID to confirm registration before
using it. A recruiting person ref never silently becomes a general graph ref.
Relationships written in the general graph are not promised in the recruiting
ego projection; use the general graph view and graph.neighbors to inspect them.
Graph context covers stored general-graph claims, not server-only task, calendar,
contact or note projections. Unregistered external endpoints cannot be prepared.

Prepare returns persisted=true and a content-addressed operationId. Human changes
start pending_approval; source runs, lookup and cache pin changes start prepared and executable. Duplicate
no-change proposals finish succeeded. The receipt records versioned normalized
arguments, target/evidence previews, expected file revisions, agent:alfred,
optional conversation/turn and idempotencyKey, owner decision, confirmed files and object refs.
Dates and claims are frozen at preparation. Rejection writes a suppression
record and therefore also requires human approval.

A reproducible headless demo builds no UI and touches only a temporary vault:

` + "```sh\ngo build -o /tmp/manifest-mcp-phase2 ./cmd/manifest-mcp\npython3 integrations/manifest-mcp/exercise_loop.py /tmp/manifest-mcp-phase2\n```" + `

The owner reviews operation.get and decides outside the agent toolset:

` + "```sh\nmanifest-mcp --config config.json --decide 'sha256:…' --decision approved\n```" + `

The owner CLI also accepts rejected and cancelled. MCP exposes no decision tool,
actor field, approval token or replacement payload. operation.execute accepts
only operationId. Approval is not completion: inspect status and confirmed
objectRefs. The local owner CLI fixes approvalActor=owner:local; vault audit
uses aion-recruiting-approved / graph-approved and approved-proposal. Protect
the CLI and dataDir from agent shell access: MCP tool separation is not OS
isolation, and a process running as the owner can bypass that boundary.

Execution serializes operation processes with a dataDir lock, saves intent
before effects, checks target revisions and compares every shared-service write
against the exact approved bytes and preimage. Stale proposals require fresh
preparation and approval. Idempotency is per operationId; terminal receipts
never reapply. An explicit idempotencyKey binds one tool input and returns its
existing receipt even after the draft leaves the queue; conflicting input is
rejected. Supply a new key to request another source run with the same scope;
retain it when retrying preparation. Partial candidate/graph writes report confirmed and unconfirmed
files, intended knowledge claims and confirmed object refs. Restart reconciles
interrupted vault writes, marks partial/failed and requires owner takeover;
it does not blindly retry. Source execution saves its allocated run ID before fetching; recovery inspects
that exact cache identity and reports confirmed cache files without repeating the fetch. No
exactly-once guarantee across external systems is made.

Receipts survive source cache expiry. Canonical evidence stays in domain records;
operation payloads retain evidence for recovery. Human approvals appear inline in chat and in FEED with takeover. Unreject reverses
a pass with human approval. Queue repair after interrupted execution remains manual. File checks
are conservative across both domain trees. They detect edits before execution
and before each write, but do not atomically lock out Obsidian or owner HTTP
writes between check and write; that requires a shared cross-client transaction
boundary in a later hardening phase.

## Alfred / Hermes

Run ` + "`python3 integrations/manifest-mcp/wire_hermes.py`" + ` after building the binary.
It merges only mcp_servers.manifest into ~/.hermes/config.yaml and includes
mcp-manifest in Manifest's configured readToolsets. Existing settings are retained.
The repository default includes mcp-manifest too. Installed Hermes uses that
hyphenated toolset key and exposes tools with its mcp__manifest__ prefix.

` + "```sh\nhermes mcp test manifest\npython3 integrations/manifest-mcp/check_hermes.py\n```" + `

The check uses the installed Hermes virtualenv to discover this server and asserts
all catalog tools survive Alfred's configured toolset filter, without an LLM call.
Restart the running Manifest process after deploying a build/config change and
start a new Alfred turn. Discovery does not establish an in-gateway conversation.
`
	return []byte(text), nil
}
