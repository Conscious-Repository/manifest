# Manifest local MCP — Phase 4

Local Go stdio MCP adapter over Manifest's recruiting and graph services. Uses
 the official MCP Go SDK for protocol handling and Go-type schema generation.
No portal token or HTTP owner endpoint is used. Preparation persists an immutable
operation under dataDir/operations. Source execution has bounded standing
authorization. True world changes require a separate owner decision, then
operation.execute applies the saved payload through shared domain services and
narrow approved-proposal vaultwriter capabilities.

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
| `entity.resolve` | Resolve title, canonical ID or slug, then partial title/ID/slug with match provenance across recruiting people, seeds/labs, roles and registered graph entities. Multiple matches require an explicit choice; never guess. |
| `entity.get` | Read a canonical entity, evidence/provenance and content revision. |
| `sources.list` | Read the application's shared adapter registry and scope fields; no fetch or cache sweep. |
| `source_run.get` | Read one run and review queue using RunStore.Get; counts include previously passed drafts separately. |
| `graph.neighbors` | Bounded stored general-graph neighbors and optional paths (at most 3 hops, 10 paths). Server-only task/calendar derivations are not included. |
| `source_run.prepare` | Normalize a source scope using Execute's shared PrepareScope. Resolve optional seed and role refs. No fetch or source-cache write; persists an operation. Standing authorization applies; network/robots validation remains execution-time. |
| `candidate_accept.prepare` | Preview exactly one new draft through AcceptDraft with an in-memory capture writer, plus derived knowledge and decision effects. Persists an operation outside the vault. |
| `candidate_reject.prepare` | Resolve one new draft and preview durable passed.md suppression plus queue and audit effects. |
| `network_person.prepare` | Resolve a canonical person; check existing network identity and preview PeopleDoc.Add with the shared domain payload and validation. |
| `graph_edge.prepare` | Resolve both registered general-graph endpoints and preview a typed claim with shared graph validation and duplicate detection. |
| `candidate_unreject.prepare` | Human approval: reverse a pass through shared Unreject, including passed.md. Already new returns no_change. |
| `candidate_lookup.prepare` | Standing authorization: bounded shared Lookup enriches cached draft evidence. External results are execution-time; interrupted calls require takeover. |
| `source_run.pin.prepare` | Standing authorization: set/unset cache retention pin, no vault records. Already pinned/unpinned returns no_change. |
| `source_run.pin` | Standing authorization: prepare and execute cache retention pin; durable receipt and idempotency key. No vault writes. |
| `graph_entity.add.prepare` | Human approval: add a registered general graph entity through shared AddEntity validation and duplicate detection. Separate from recruiting ego view. |
| `seed.prepare` | Human approval: add a recruiting seed/lab using shared AddSeed; preserves agent origin and exact preview bytes. |
| `candidate_unreject.execute` | Execute only this verb's saved operationId; approval and terminal-receipt rules are identical to operation.execute. |
| `candidate_lookup.execute` | Execute only this verb's saved operationId; approval and terminal-receipt rules are identical to operation.execute. |
| `operation.get` | Read a durable operation, approval and execution receipt. |
| `operation.execute` | Execute the saved payload. Source runs have standing authorization; world changes require an owner decision outside MCP. Terminal receipts are idempotent; incomplete effects require takeover. |

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

```sh
go build -o /tmp/manifest-mcp-phase2 ./cmd/manifest-mcp
python3 integrations/manifest-mcp/exercise_loop.py /tmp/manifest-mcp-phase2
```

The owner reviews operation.get and decides outside the agent toolset:

```sh
manifest-mcp --config config.json --decide 'sha256:…' --decision approved
```

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
all catalog tools survive Alfred's configured toolset filter, without an LLM call.
Restart the running Manifest process after deploying a build/config change and
start a new Alfred turn. Discovery does not establish an in-gateway conversation.
