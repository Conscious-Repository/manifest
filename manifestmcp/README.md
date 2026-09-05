# Manifest local MCP — Phase 1

Local Go stdio MCP adapter over Manifest's recruiting and graph services. Uses
 the official MCP Go SDK for protocol handling and Go-type schema generation.
No portal token or HTTP owner endpoint is used. Stores have no live vault writer;
acceptance uses a capture writer that retains proposed file content in memory.
No source execution, lookup, sweep, approval, or write tool is exposed.

Build and run from the repository root:

```sh
go build -o /home/benjamin/.local/bin/manifest-mcp ./cmd/manifest-mcp
manifest-mcp --config /home/benjamin/src/manifest/config.json
```

The config supplies vaultPath, dataDir and systemRoot (default system). Startup
reads configuration only and does not seed files. This process runs as the local
owner and can read private recruiting data; tool filtering is not OS isolation.

## Keep up to date

The single registration table uses Go types (including RunRequest, NetworkPerson
and graph.Edge) to generate input schemas. Both the app and MCP use
RunStore.RegisterDefaults; source scope fields and graph vocabulary come from
those domain definitions. No separately authored JSON schema exists.

```sh
go generate ./manifestmcp
go test ./manifestmcp
go run ./cmd/manifest-mcp --catalog
```

Generation rewrites this README and catalog.json. TestCatalogFresh fails on either
stale file. Review generated changes and bump Version when semantics change;
types cannot automatically detect a changed semantic contract. The full schemas,
source scope fields and graph vocabulary are in [catalog.json](catalog.json).

## Tools

| Tool | Contract |
| --- | --- |
| `capabilities.list` | Read the versioned tools, generated input schemas, domain vocabulary and source scopes. |
| `entity.resolve` | Resolve exact name or ID across recruiting people, seeds/labs, roles and registered graph entities. Multiple matches require an explicit choice; never guess. |
| `entity.get` | Read a canonical entity, evidence/provenance and content revision. |
| `sources.list` | Read the application's shared adapter registry and scope fields; no fetch or cache sweep. |
| `source_run.get` | Read one run and review queue using RunStore.Get; counts include previously passed drafts separately. |
| `graph.neighbors` | Bounded stored general-graph neighbors and optional paths (at most 3 hops, 10 paths). Server-only task/calendar derivations are not included. |
| `source_run.prepare` | Normalize a source scope using Execute's shared PrepareScope. Resolve optional seed and role refs. No fetch/cache write. Standing authorization applies in Phase 2; network/robots validation remains execution-time. |
| `candidate_accept.prepare` | Preview exactly one new draft through AcceptDraft with an in-memory capture writer, plus derived knowledge and decision effects. No file is written. |
| `candidate_reject.prepare` | Resolve one new draft and preview durable passed.md suppression plus queue and audit effects. |
| `network_person.prepare` | Resolve a canonical person; check existing network identity and preview PeopleDoc.Add with the shared domain payload and validation. |
| `graph_edge.prepare` | Resolve both registered general-graph endpoints and preview a typed claim with shared graph validation and duplicate detection. |

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

Run `python3 integrations/manifest-mcp/wire_hermes.py` after building the binary.
It merges only mcp_servers.manifest into ~/.hermes/config.yaml and includes
mcp-manifest in Manifest's configured readToolsets. Existing settings are retained.
The repository default includes mcp-manifest too. Installed Hermes uses that
hyphenated toolset key and exposes tools with its mcp__manifest__ prefix.

```sh
hermes mcp test manifest
python3 integrations/manifest-mcp/check_hermes.py
```

The check uses the installed Hermes virtualenv to discover this server and asserts
all eleven tools survive Alfred's configured toolset filter, without an LLM call.
Restart the running Manifest process after deploying a build/config change and
start a new Alfred turn. Discovery does not establish an in-gateway conversation.
