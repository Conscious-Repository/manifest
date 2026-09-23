# Extractor application safety — bounded hold, 2026-09-16

This phase adds a **fail-closed application gate**, not an extractor cutover or
semantic-parity receipt. Harness fences and legacy duties are unchanged.

Successor proposals retain an application-owned snapshot of exact source and
context SHA-256 values, bound to the proposal payload, type, ritual and target.
Source/replay identity still excludes context: context drift cannot authorize a
second model execution. Recovered verified jobs without matching snapshot
metadata become uncertain with replay=false rather than publishing unguarded
candidates.

Confirm compares the available vault dependencies and refuses changed or edited
snapshots. **Even matching snapshots remain pending and cannot write:** the
existing apply functions do not support atomic dependency comparison and commit.
The refusal explicitly reports uncertain/stale, pending and replay=false. There
is no retry, fallback, direct vault write or new approval lane. Rejection remains
available. Snapshot metadata survives approval serialization and card edits;
editing payloads invalidates the binding instead of silently refreshing evidence.
Artifact and portal dependencies now explicitly refuse as unverifiable instead
of being skipped during diagnostic checks. Noncanonical paths and malformed
hashes refuse. A snapshot on an unsupported proposal type refuses, including
the operation dispatcher that otherwise runs before the ordinary apply gate.
The approval card disables Confirm for snapshot-bearing proposals and names the
application hold separately from semantic comparison, live validation and retirement.

## Atomic CAS feasibility review — 2026-09-16

**Atomic extraction CAS is not implemented.** This phase takes the explicitly
authorized bounded-hold outcome. The current APIs cannot meet the requested
contract; a journal added solely around `Confirm` would not repair them.

| Boundary inspected | Concrete blocker |
| --- | --- |
| `vaultwriter/edit.go`, `capability.go` | `editMu` serializes cooperating writers only within one process. Each capability call releases it after one file. `atomicBytes` renames one file; it cannot commit a read/write set. External editors and sync do not acquire this lock. Directory sync errors are ignored after rename. |
| `approvals/aion.go`, `re.go`, `resolve.go` | Corpus reads and transforms occur before the capability write lock. Matching source/context hashes in Confirm cannot prevent changes between those reads and the write. |
| `approvals/recontract.go` | Contractor creation, property section changes and the contract record are independent writes. `freeContractPath` probes up to 20 names outside the writer lock and can select an undeclared suffix. Earlier writes survive a later failure. The bounded successor currently forbids tree additions, but its references and destination checks still lack a transaction. |
| `domainextract/contract.go`, `service.go` | V1 snapshots are maps of present file hashes, not complete dependency manifests. They lack expected-absent targets, namespace membership and category-index revisions. Bounded canonical-folder context cannot prove absence of a same-category slug elsewhere in the vault. RE backlog payloads lack explicit property/contract reference fields. |
| `realestate/cas.go`, `artifacts/` | RE blob and `files.json` updates are separate capability writes; `Lookup` checks existence, not the blob hash, and index read/parse errors collapse to an empty index. The separate artifact pool has its own store identity and locks. A bare `sha256:` entry in V1 does not identify which store to revalidate. Raw email bytes and extracted document text must not be conflated. |
| `vaultindex/index.go`, `watch.go` | SQLite transactions cover derived tables only. `Rebuild` and `ReindexPaths` read filesystem bytes independently; neither pins vault namespace membership nor participates in writer locks. A SQL rollback cannot undo vault writes. |
| `vaultwriter/capability.go`, `approvals/approvals.go` | Audit appends occur after bytes land, have no durable transaction identity, and failure is reported through health state rather than failing the write. Confirm separately writes approved state and removes pending state after apply. A crash between these steps leaves replay ambiguity. |

A correct next substrate needs a versioned complete dependency manifest, pure
rendering of the entire write set, and a common lock/recovery protocol for all
participating writers and readers. It must cover category namespace additions
and removals, exact source/artifact store identity, bytes and metadata, explicit
property/contract identities, destination absence, and the reviewed approval.
Define a single lock order before integrating the domain and index locks.

For a journal design, prepare must durably retain transaction identity, exact
before/after hashes and bytes, declared capabilities and actor, approval digest,
and dependency evidence outside the vault. Sync prepare before any vault write;
perform each write through the canonical capability boundary. Audit receipts and
decision settlement need durable transaction identities and a commit record.
Startup must quarantine unfinished transactions before accepting more writes or
serving partially applied projections. Recovery must inspect recorded evidence,
fail closed on uncertainty, and never replay the model or silently re-Confirm.
Blind rollback is also unsafe: it could overwrite an intervening owner edit.
External editors do not honor application locks, so their coordination or a
stronger storage visibility boundary must be resolved explicitly. Independent
renames plus a preflight or best-effort rollback cannot satisfy atomic CAS.

No prepare/commit/recovery journal is introduced here. No successful snapshot
apply, transactional audit receipt, partial-write recovery, semantic parity,
live validation, ownership cutover or final decommission is claimed. Existing
legacy/non-snapshot behavior and audit semantics remain unchanged.

OODA successor input uses bounded raw canonical property, contractor and contract
records rather than the legacy portal summary. The source name must be the exact
SHA-256 of the email artifact bytes. Contract payload provenance remains that
same hash. Contract candidates require exact existing canonical property and
contractor slugs and work-node IDs; missing or ambiguous supplied references are
refused. Contractor creation and proposed tree additions are held for a later
phase. Missing directories or an oversized corpus fail closed, without truncation.
The unchanged legacy route still uses its original request format.

## Remaining gates

- Atomic writer CAS over the complete dependency and write set, including added
  records, deleted records, category routing, and category-index ambiguity across
  the entire vault. A preflight followed by independent writes is insufficient.
- Confirm-time artifact-store revalidation and a safe multi-file contract commit.
- Broader property locations, contract tree additions, and explicit property or
  contract references in RE backlog proposals. Current backlog schemas do not
  represent those references; the application hold prevents unsafe acceptance.
- Reconciliation of historical snapshot-less successor proposals already in an
  inbox. They cannot be distinguished from legacy proposals by current metadata;
  existing legacy proposals retain their previous behavior. Do not enable a
  successor against such an inbox without reconciliation.
- Semantic comparison against legacy AION commitment/closure/heuristic and
  RE/OODA outputs, live evidence, ownership handoff, and final decommission.

Offline adversarial tests cover source/context drift, proposal edits, missing and
ambiguous references, invalid nodes/contractors, exact artifact provenance,
unchanged replay identity, and no vault writes on Confirm refusal. Their success
is mechanical evidence only, never semantic parity.

The bounded-hold regression additionally uses a fully configured capability
writer, checks whole vault/audit trees, and exercises category and `files.json`
drift, removed property/contract dependencies, newly added ambiguous properties
and contracts, an unavailable audit destination, artifact/portal evidence,
repeated Confirm and duplicate publication, and reopening the store. Unchanged
snapshots must refuse too. These tests do **not** establish SQL-index CAS,
partial-write rollback or journal recovery: those remain acceptance requirements
for the missing transaction substrate. Existing non-snapshot successful apply
tests continue to require canonical approved-proposal audit lines.

Validation for this phase: gofmt, `go test ./...`, `go build ./...`,
`go vet ./...`, `go test -race ./...`, and `git diff --check` passed locally.

## Bounded gate — 2026-09-23

The blanket hold above kept every snapshot-bearing candidate pending forever,
including a fresh one whose every declared dependency still matched: the
in-app extractor could file candidates but never land a line, and the card
told the owner only to "reject or leave pending". That is not a safety gain
over the legacy extractor, whose proposals of the same types apply through the
same lanes with no snapshot at all, and it left the owner's review with no
effect.

`Store.ExtractionHold` now names the condition that holds a candidate, and
Confirm applies when there is none:

- A **source document** (any snapshot dependency not under `system/`) must
  still carry the exact bytes the candidate was extracted from — that is what
  the quote and the owner's review were made against. A changed source holds
  the candidate ("source changed since extraction — reject and re-run").
- A **context record** (`system/…`: backlog, heuristics, people, RE records)
  must still exist but may have moved on. A backlog append does not depend on
  the backlog's bytes, and a resolve refuses on its own when its title is gone.
  Binding context bytes made every candidate in a batch stale the moment the
  first one landed.
- **Owner edits ride Confirm.** An edited payload no longer invalidates the
  evidence; the edit is the review, made through the card's own endpoint.
- **Still held:** replay snapshots, malformed hashes or paths, artifact and
  portal dependencies this snapshot version cannot revalidate, snapshots on
  unsupported types, and the multi-file `re-contract` lane, which is not
  transactional.

The approval card blocks Confirm only on a reported hold and shows the reason
(`approvalRow.extractionHold`). Refusals still journal as before; a successful
apply leaves no refusal receipt. The remaining gates listed above stay open for
the contract lane and for a transactional multi-file writer; single-file
appends and resolves no longer wait on them.
