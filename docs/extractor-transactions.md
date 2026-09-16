# Extractor transaction journal — refusal/recovery phase

Snapshot-bearing extraction proposals remain held. The available substrate is a
private, durable prepare/refusal/recovery journal; **there is no commit API**.
Legacy proposals without an extraction snapshot retain their existing behavior.
Nothing in this phase enables extraction, changes ownership, replays a model run,
auto-confirms, or establishes semantic parity or final decommission.

## Why the hold remains

`approvals.checkExtractionSnapshot` has only V1 positive file hashes. It cannot
express expected-absent candidates, the complete category namespace (including
ambiguous names elsewhere), a vault-index generation, or the identity and revision
of the artifact store that resolved an attachment. `domainextract.ReadInput` reads
files and enumerates selected directories independently. Those observations are
not a single consistent snapshot.

`vaultwriter` serializes application writes with `editMu`, but external editors
and filesystem synchronization do not honor that lock. `UpdateCap` can observe an
external source edit while its transform holds the lock; `WriteCap` does not CAS
all source dependencies. Per-file renames cannot atomically publish a multi-file
write set. `applyReContract` writes contractor, property trees and contract in
sequence, and `realestate.FileStore.Save` writes blob and index separately.
`vaultindex` is a separately updated SQLite projection. `artifacts` has independent
store/object mutexes and indexes. None shares a storage transaction with the vault.

Audit failure currently records “write landed without audit trace”; it does not
undo the write. Approval confirm writes the approved file and removes pending
after apply, in a different store. Its directory fence and decision mutex exclude
cooperating approval operations, not vault editors. The decision-record store
also has its own mutex and injected single-file writer.

`vaultwriter/TestExtractionExternalEditorBoundary` deterministically demonstrates
source drift while the writer lock is held and a visible first write despite a
failed second write. Existing audit-failure tests establish the independent audit
boundary. These prove limitations of the current APIs, not that transactions on
all possible future storage architectures are impossible.

## File contract

The main application's approval stores configure
`<dataDir>/extraction-transactions/<transaction-id>.json` and run recovery before
serving. Other constructors must explicitly configure `WithExtractionJournal`;
without it they still refuse snapshots and report the journal unavailable.
dataDir must already exist outside the canonical vault path. Journal directories
are created mode 0700 and files mode 0600; symlink journal directories/records are
refused. Treat these private operational receipts as recovery evidence, not an
index to rebuild from model execution.

Version 1 JSON includes:

- `id`: SHA-256 over approval-store path, approval ID and approval digest, separated
  by newlines. Exact duplicate confirms reuse the receipt without another attempt.
- `actor` and `capabilities`: attempted actor and configured capability names;
  they record intent, not a grant or a validated write set.
- `approvalStore`, `approvalId`, `approvalDigest`, `approvalBytes`, `snapshot`:
  exact serialized reviewed proposal bytes (JSON base64), their SHA-256, and the
  original snapshot, including malformed/unsupported evidence when refused.
- `dependencies`: predicates with `kind`, `path`, `expectedAbsent`, `sha256`, and
  `identity`. A future complete manifest must bind every positive read, every
  absence influencing selection, category namespace membership, index generation,
  and artifact store identity/revision and selected blob bytes. Current entries
  are explicitly `unverified-v1`; `dependenciesComplete` is always false.
- `writes`: path, named capability, and before/after images. Each image has
  `known`, `absent`, `sha256` and exact base64 `bytes`. A known absent image has no
  hash/bytes; an existing empty file has SHA-256 of zero bytes. Unknown is neither.
  This phase records only the proposed target with unknown images;
  `writesComplete` is false. It never invokes legacy transforms to guess images.
- `missingEvidence`: the explicit missing manifest, identities, absence predicates,
  exact write images, and shared write/audit/decision transaction.
- `state`, timestamped `transitions`, and `replay` (always false on output).

Unknown fields/versions are not a future execution authorization. The format is
portable, file-readable JSON; the current runtime locking follows the repository's
Unix directory-flock convention. No multi-file filesystem atomicity is claimed.

## Durability, locking and recovery

Normal order: approval decision mutex → approval directory fence → nonblocking
journal directory flock. Startup recovery takes only the journal lock. The
snapshot-bearing operation-callback refusal also takes only the journal lock;
it never dispatches the callback. No journal code acquires a vaultwriter, index,
artifact or audit lock, writes the vault, or settles an approval.

Each record update writes a private temporary file, syncs it, renames it, then
syncs the journal directory. The dataDir entry is synced as well. Errors from
writes, syncs and renames are reported as journal unavailable and keep the hold.
This relies on the local filesystem honoring fsync/rename; unsupported storage
must fail closed. Temporary files left before publication have no execution
authority and are ignored, never replayed.

| State | Meaning in this phase |
| --- | --- |
| `prepare` | Durable refusal preparation, explicitly incomplete evidence; no vault writes authorized |
| `aborted` | Refusal durably recorded; approval remains pending |
| `committing` | Reserved for a future writer; any encountered record is quarantined |
| `committed` | Reserved for a future proven write/audit/settlement protocol; this implementation never emits it |
| `uncertain` | Interrupted/unsupported state, or replay request; owner reconciliation required |

Recovery turns nonterminal/unsupported records (including foreign `committed`
records) into `uncertain`, retaining prior transitions. Even all-after vault
hashes cannot prove audit and approval settlement, so this phase does not infer
success from them. It never rolls back, rewrites intervening owner edits, dispatches
model work, silently re-confirms, or removes a pending/approved duplicate left by
a settlement crash. Repeated recovery preserves terminal receipts. Malformed
records remain untouched with an error; a failed recovery is logged at startup.

The approval UI distinguishes journal availability from commit and warns that
uncertain recovery, semantic review, and final decommission are separate gates.
A refusal response includes transaction ID and actual receipt state when durable;
it never claims a receipt when journaling fails. Test success does not mark a duty
migrated or advance its ownership/readiness state.

## Remaining blocker and validation

A successor commit lane still needs a complete, review-bound dependency manifest
and exact capability-checked write set, plus a storage/coordination contract that
excludes external edits throughout validation/publication and reconciles durable
audit and approval settlement. Application locks and preflight hashes alone do
not provide that contract. Owner-reviewed live semantic evidence and final
retirement remain separate requirements after the mechanical blocker is solved.

Temporary-fixture tests cover source/context drift, missing and newly present
paths, category/index/artifact identity gaps, unavailable audit/journal storage,
partial/interrupted writes, restart, duplicate transaction, replay, approval
settlement crash, corrupt receipts, and unchanged vault/approval bytes on refusal.
Recovery scenarios synthesize interrupted future-writer records; they do not
claim this phase executes or safely commits those writes.

## Commit feasibility decision (2026-09-16): outcome B

The narrowest currently safe extractor boundary ends at durable refusal. Even a
single AION backlog append cannot bind its source/context observations, target
publication, audit, and approval decision into a safe commit using today's APIs.
A journal around those calls would document partial effects, not exclude owner
edits or establish a committed outcome. No transaction coordinator or commit
support was added, and production routing/configuration remains unchanged.

New receipts carry `feasibility`, an additive version-1 assessment with
`state: "commitUnavailable"`, `replay: false`, and stable blocker codes. Each
blocker names the observed boundary and required replacement contract. This is
separate from transaction `state`: `aborted` means a durable refusal, while
`uncertain` means evidence requires reconciliation. The assessment is architectural,
not a claim to have revalidated a live dependency manifest. All seven barriers
remain even if the declared positive hashes match. Refusal errors also include
`commitUnavailable` when the journal cannot be written. Old terminal receipts
are not backfilled or rewritten; absence of `feasibility` never grants authority.

| Blocker code | Inspected implementation |
| --- | --- |
| `external-writers-uncoordinated` | `vaultwriter/edit.go` (`UpdateCap`, revision check then rename); `cmd/manifest-sync/main.go` (`cycle`, process-local mutex then git rebase); Obsidian writes are outside these locks |
| `dependency-manifest-incomplete` | `domainextract/service.go` (`ReadInput`) input enumeration and `contract.go` snapshot encoding; `approvals/extraction_safety.go` V1 positive hashes omit absence, namespace and external identities |
| `write-set-not-prepared` | `approvals/aion.go`, `re.go`, `resolve.go` read/transform before `WriteCap`; `recontract.go` selects free suffixes, then contractor/property/contract writes |
| `stores-not-enlisted` | `realestate/cas.go` blob then files.json; `vaultindex/index.go` and `watch.go` SQLite-only transactions; `artifacts/artifacts.go` pool and `object.go` registry mutexes, blob then object publication |
| `audit-not-transactional` | `vaultwriter/capability.go` mutation then append-only audit, optional audit configuration, no fsync or before/after hashes; `traced` reports landed failures |
| `settlement-not-transactional` | `approvals/approvals.go` Confirm: apply, write approved, remove pending; Settle independently archives; `email_fence.go` fences cooperating decisions only |
| `recovery-refusal-only` | `main.go` configures and recovers before serving; `approvals/extraction_journal.go` quarantines unsupported states without mutation/settlement, logging recovery errors while keeping snapshots held |

The router (`domainextract/router.go`) chooses configured successor versus legacy
routes; sinks (`aion/sink.go`, parameterized for RE) queue inputs, not storage
transactions. `domainextract.Service` publishes snapshot-bearing proposals via
`ProposeOnce`; readiness and proposal publication do not confer commit authority.
OODA-email requires explicit source submission and reaches the same snapshot
approval gate. Neither routing nor recovery supplies an external editor fence.
Artifact pool/object publication has no joint vault audit or settlement record;
a hash-addressed filename alone does not prove current blob bytes or store identity.

### Minimum prerequisite

First establish an **enforced coordination boundary**, by owner decision: either
one owner-controlled write service that mediates *all* writes (including editor
and sync imports), or an explicit filesystem protocol every editor, sync process,
app writer and transaction reader participates in. An advisory flock added only
to Manifest is insufficient. This task does not change the current hand-editable
vault doctrine or stop any writer.

Inside that boundary a future implementation must capture the complete consistent
read manifest (including absence and namespace predicates), pin external store
identities/revisions, render and capability-check all writes before mutation, and
persist exact images plus settlement intent. It must publish one durable commit
generation with hash-linked audit and approval settlement evidence. Separate
derived indexes may follow that generation only if they cannot supply unpinned
inputs during validation. Failures after any write remain uncertain unless the
entire write/audit/settlement evidence proves commit; recovery must preserve owner
edits and never blindly roll back. A filesystem fence alone does not supply this
crash/settlement protocol. These are prerequisites, not implemented guarantees.

### Deterministic offline checker

Run the machine-readable architecture assessment (no arguments, config reads,
store reads, model calls, upstream calls or operational writes):

```sh
go run ./cmd/extractor-boundary-check
```

Exit 0 means the assessment printed successfully, **not** that commits are
available; consumers must inspect `state`. This command reports compiled-in API
constraints, not live storage health. Run its behavioral witnesses in disposable
fixtures to verify the boundary against the implementation:

```sh
go test ./approvals ./vaultwriter -run 'TestExtraction|TestAuditFailure' -count=1
```

The checker suite demonstrates a source changing while `editMu` is held, partial
multi-file publication, a real FileStore blob surviving failed files.json
publication, SQLite projection lag/advance and artifact head drift independent of
a matching V1 source hash, and a legacy AION write surviving failed approval
settlement. The same settlement obstacle with a snapshot proves no vault write.
It also checks machine-readable receipts, unavailable journaling, immutable old
receipts, duplicate/restart behavior, and the existing interrupted/foreign
commit/audit/settlement recovery cases. Those recovery cases are synthetic;
they do not claim successful atomic commit or power-loss certification.
