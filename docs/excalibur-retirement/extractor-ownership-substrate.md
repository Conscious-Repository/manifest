# Extractor ownership substrate — 2026-09-16

This is a historical substrate checkpoint, **not an extractor cutover**.
The current [decommission end state](decommission.md) pauses all three extractor
capabilities without claiming migration. The active-engine statements below
record this earlier checkpoint. No provider execution, live semantic comparison,
approval decision, vault write, configuration change or deployment occurred.

## Verified boundary and gaps

The harness at `5bcfcc7` has AION and real-estate event/request rituals (USD 4,
20 steps) and confirmed OODA email extraction (USD 2, 12 steps). They publish
through `write_approval` into `artifacts/approvals/pending`; canonical Manifest
approval decisions and vaultwriter apply remain the write boundary. AION includes
commitment, heuristic and closure sweeps; real-estate excludes heuristics; OODA
allows backlog and contract proposals with the source artifact hash. None
requires autonomous outreach.

Manifest already has a single event-driven `domainextract.Service`, persisted
source snapshots, deterministic proposal IDs and restart reconciliation. It has
no reason to add another scheduler. Existing category sinks continue to route
`aion` and `real-estate`/`ooda`/`ooda-group`; a note in both domains reaches both.
Confirmed OODA email retains its explicit artifact source and matching context.

The actual safety gap was that `domainExtraction` flags could authorize this
worker without shared ownership evidence. The legacy engine's
`internal/dispatchfence.Managed` at the inspected revision only includes the
three connectors, **not these extractors**. Pausing Markdown alone cannot fence
manual, queued or direct engine dispatch.

## Implemented

- Reuse the connector fence's immutable version-1 history and shared flock inode
  for exact duties `extractor/aion`, `extractor/real-estate`, `extractor/ooda-email`.
  This extends Manifest's reader/claim support only, not the running engine.
- Submission, execution and verified proposal publication require Manifest
  ownership plus a hash-bound, duty/revision-specific handoff receipt. The shared
  claim remains held through the model call, approval publication and durable
  job updates. No fallback is introduced.
- Jobs record version, ownership revision and `replay:false`. Old unfinished jobs
  become uncertain; completed historical receipts remain unchanged. Interrupted
  execution stays uncertain, verified publication resumes without another model
  call, and another ownership revision refuses an old job. Existing rejection
  decisions remain authoritative.
- UI status distinguishes legacy ownership, blocked configuration/evidence,
  successor disabled and successor enabled. No state implies final decommission.
  Legacy spooling/pickers/resume edits also consult extractor fences, even if the
  successor flag is absent. This is Manifest-side refusal, not engine-side proof.
- `cmd/extractor-check` reads live ownership or validates a saved input/reply pair
  without invoking a model, filing approvals, writing state or touching the vault.

A future handoff receipt lives at
`<dataDir>/domain-extraction/handoffs/extractor/<ritual>/<sha256>.json`.
The fence's `evidence_sha256` must match the exact receipt bytes. Its JSON fields
are `version:1`, `duty`, `revision` (the intended transfer revision),
`replay:false`, `legacyHistorySha256`, `engineFenceSha256`, and
`liveSemanticSha256`. Each evidence field addresses a retained owner-reviewed
artifact. These hashes are owner attestations, not automatic proof of their
contents. The reader rejects missing fields, malformed hashes, changed bytes,
unknown/duplicate fields, wrong versions, duties or revisions, and replay=true.
There is intentionally no command in this phase to fabricate or publish a
transfer. Do not use test fixture receipts as operational evidence.

## Read-only verification

For each ritual (`aion`, `real-estate`, `ooda-email`):

```sh
go run ./cmd/extractor-check -data-dir /absolute/dataDir -harness /absolute/excalibur -ritual aion
```

`-enabled` previews configured successor intent; it changes nothing. Optional
`-input /private/input.json -reply /private/reply.json` validates a saved
`domainextract.Input` and structured reply, returning proposal IDs, types and
apply paths without printing source text. Shape validation is never semantic
validation. This tool always reports `semanticValidationPerformed:false`.

Focused verification:

```sh
go test ./domainextract ./connectorhandoff ./spirits ./approvals ./aion ./cmd/extractor-check
```

Tests cover source/path/quote routing, private read-only readiness, receipt hash
binding, all three duty scopes, shared lock exclusion, stale revision refusal,
stale source refusal before publication, no publication from historical jobs,
restart reconciliation preserving owner rejection, and Manifest legacy dispatch
refusal. Fixtures prove mechanics only. Stale **publication** refusal is not a
claim of source-version validation at later owner confirmation.

## Required carry-forward before activation

1. Extend and test the harness engine's managed-duty whitelist at scheduler,
   manual spool and direct runner boundaries; deploy and prove refusal against
   the actual binary for all three duties. Preserve queued requests and receipts.
2. Inventory source identities against existing pending/approved/rejected
   proposals, run reports, AION/RE sink dispatch state and confirmed OODA email
   artifacts. Retain uncertain history with replay=false; do not infer successful
   extraction from a watermark or missing proposal.
3. Resolve the previously unverified subscription canary. Retain verified
   provider/model/usage evidence, then compare actual source documents and prior
   outputs with owner review of commitments, closures, heuristic reinforcement,
   participant coverage, property/work-node matching and money allocations.
   The bounded one-turn replacement is not presumed equivalent to 12/20-step
   legacy rituals. No uncertain request may be retried automatically.
4. Close the remaining semantic/application gaps: confirm-time source/context
   staleness protection (publication checks alone are insufficient), transactional
   OODA property/contract context validation, and hard domain-reference validation
   for contract candidates. Preserve category/provenance semantics in live cases.
5. Only with those proofs: prepare immutable receipts, pause/reconcile one legacy
   duty under the shared fence, transfer ownership and enable its successor.
   Verify a real pending proposal, no-change duplicate submission and restart
   behavior without approving proposals automatically. Observe it before moving
   another duty. Engine stop/mask/removal remains a separate final gate.

Rollback first disables the successor, preserves every job, receipt and proposal,
and reconciles outcomes before publishing a new ownership revision. An old job
cannot run under that new revision. Never reset state or silently replay history.

On this pass, read-only probes for all three live duties reported
`legacy-retiring`; all four services (`manifest`, `manifest-transcripts`,
`manifest-personal-email`, `excalibur-engine`) were active. No live semantic
checkpoint was available or invented. That checkpoint did not retire the engine; the later decommission procedure
permits retirement with these capabilities explicitly paused.

## Validation completed

`gofmt`, focused tests, `go test ./...`, `go build ./...`, `go vet ./...`,
`go test -race ./...`, `node server/testdata/agents-legacy-labels.cjs` and
`git diff --check` passed. The read-only live ownership probes and the offline
shape probe above also passed with the stated blocked/legacy results. No live
semantic validation was performed.

## Application gate update, 2026-09-16

[The bounded application-safety hold](extractor-application-safety.md) now binds
successor proposals to source/context evidence and blocks Confirm pending an
atomic dependency CAS. OODA contract references are validated against bounded
canonical records. This is a mechanical refusal gate, not a claim that application
CAS, semantic validation, ownership cutover or final decommission is complete.

## Offline comparison checkpoint

The [refusal-only comparison checker](extractor-comparison.md) retains explicitly
hash-bound artifact evidence and reports `comparison-unrun`. Existing native
formats do not bind the complete structural evidence needed for a safe match.
Its reports never establish semantic parity or change migration readiness.

### 2026-09-16 context planning follow-up

[Offline context planning](../extractor-context-planning.md) now inventories the
complete copied context and measures the unchanged serialized-input budget.
Oversized inputs refuse with `contextPartitionUnavailable`: there is no merge
protocol preserving global deduplication, closure and reference semantics.
Single-input byte fit still requires semantic review. This diagnostic neither
reads live context by default nor establishes successor enablement or migration.
