# Connector handoff substrate — 2026-09-16

**No duty is ready for live cutover. Email, Granola and Pocket remain owned by
Excalibur.** This is an offline state/reconciliation implementation, not a completed
migration. No real connector request, cursor import, credential read/copy, approval,
vault write, production configuration change or live runtime operation was made.
The harness and vault repositories were read only. No push or deployment belongs
to this change.

## Contract audit

The live-run evidence supplied with this task establishes an active personal
`ben@aion.bio` email lane: a 100-thread page, new proposals and confirmed-thread
appends. The earlier checkpoint's statement about a missing successor means a
missing **Manifest** successor; it must not imply absent legacy personal sync.
No live API was queried again during this coding pass.

Read-only implementation evidence is in Excalibur's
`engine/internal/casts/{email,emailaccounts,granola,pocket}.go`, its three connector
spellbooks, and Manifest's `gmailauth/accounts.go`, `gmailsync/`, `approvals/`,
`transcriptsync/`, `portals/`, `spirits/`, `config.go` and `main.go`.

| Contract | Excalibur | Manifest / remaining gap |
| --- | --- | --- |
| Email identity and credentials | Primary Gmail grant plus extra-account grants; primary `state.json`, extra `state-<slug>.json`; account settings select sync/extraction/workspace | `gmailauth` exposes the personal account contract; `gmailsync` instead uses portal member tokens and candidates. No credential was opened. A typed account label is not credential/account proof. |
| Email relevance | Vault people index, at least one known non-owner participant; calendar-only threads excluded | Portal loop resolves the OODA roster. Reusing it would change personal-mail scope. |
| Email lifecycle | `proposed`, `synced`, `muted`; preserves `proposal_id`, `last_msg_id`, `last_internal_ms`, `filename` | New checkpoint decoder preserves all fields and exact old-state hash, rejects duplicate JSON keys/unknown fields, matches canonical create IDs, and holds uncertain effects. It does not poll or mutate the lifecycle. |
| Email proposals | Canonical create card per thread; confirmed growth becomes append card, identified through final message, with optional date-range rename | Portal loop uses account/thread/sequence candidate IDs and fresh confirmation candidates. It is not the canonical append lane. The future adapter must retain original canonical IDs and use existing `AutoApplyAppends` / vaultwriter rules. |
| Email retry/cursor | One-hour overlap, up to 100 threads; unreadable threads can be skipped while newer activity advances the cursor; state write errors are ignored by the legacy helper | Future personal poller must persist per-thread outcomes before cursor movement, drain pagination, retain cursor on partial failure and reconcile lost acknowledgements. This pass does not copy those unsafe legacy behaviors into a new poller. |
| Granola | Watermark, source-ID/name/date dedupe, canonical create proposals | Existing successor conversion/pagination retained. New reconciliation reconstructs outcome rows from all canonical decisions and frozen vault source identities, including old IDs and edited filenames. |
| Pocket | Day overlap, completed/one-hour settling gate; transcript-less/unready older rows can be overtaken by newer rows | Successor already holds its watermark on unfinished items. Import must still prove historical coverage beyond the one-day overlap; local state alone cannot recover identities legacy never recorded. |
| Canonical history | Shared pending/approved/rejected files, writable by legacy and Manifest | Full inventory now hashes raw bytes plus file/status identity, rejects duplicate card IDs/source IDs and disagreeing source aliases, and never treats unreadable history as empty. Source-ID lookup no longer returns the first conflicting match. |

The personal successor remains a state substrate, **not an executable email
replacement**. Missing pieces are the account-bound read adapter, exact personal
contact/workspace conversion parity, durable create/append publication with
message-batch recovery, and source-note/append-receipt reconciliation. Building it
by routing the portal loop would violate the owner-approved contract.

## Delivered behavior

- `approvals/connector_inventory.go`: read-only, content-free canonical inventory;
  its constructor-free entry point does not create missing folders. Full-inbox
  hashes change on edits, decisions and additions. Conflicts refuse reconciliation.
- `connectorhandoff/email.go`: versioned personal ledger checkpoint, retaining all
  lifecycle/message fields. Approved-but-still-proposed, synced threads and
  unsettled append cards require review; none is automatically replayed/promoted.
- `transcriptsync/checkpoint.go`: imports outcomes into an **in-memory checkpoint**,
  retaining pending/rejected/approved IDs. Approved without a source note,
  pending/rejected with a source note, duplicate indexed source IDs, and changing
  approvals/watermarks refuse the checkpoint. Existing successor state is not
  overwritten. Standalone source notes remain `existing-note` outcomes.
- `transcriptsync/outcomes.go`: polling checks saved outcomes before source calls.
  A lost canonical card or source note is uncertain, not a reason to republish.
  Ambiguous source identities stop the run. Normal outcomes still persist before
  cursor advancement. Proposals still use the canonical inbox; no new code can
  confirm a proposal or call a vault writer.
- `connectorhandoff/checkpoint.go`: optional immutable, 0600, hash-addressed JSON
  snapshots under dataDir, fsynced and atomically published without replacement,
  including concurrent attempts. These are retained evidence, not second mutable
  cursors. A conservative pause/queue/run scanner refuses missing inventories,
  enabled schedules, any queued/claimed work and running/unknown run outcomes.
- `main.go`: transcript approvals bind explicitly to the Excalibur inbox, not the
  first harness in the UI list. Production polling requires handoff evidence and
  remains blocked pending a real legacy dispatch fence.

## Operator command

Use an owner-prepared frozen harness copy and a consistent SQLite backup of the
source index. The SQLite file must have no WAL/journal sidecars. Immutable,
read-only SQLite access prevents dry-run journal/SHM writes; it cannot make an
inconsistent externally copied database valid. The command never makes a backup
of production itself and never reads tokens.

```sh
go run ./cmd/connector-checkpoint \
  -source granola -legacy-root /offline/excalibur \
  -account source-account-label -index-snapshot /offline/index.sqlite \
  -data-dir /offline/manifest-data

go run ./cmd/connector-checkpoint \
  -source email -legacy-root /offline/excalibur \
  -account source-account-label -email-state /offline/excalibur/vessel/state/email/state.json \
  -data-dir /offline/manifest-data
```

Pocket uses `-source pocket`. For an extra email account, select its exact
`state-<slug>.json` explicitly; a slug does not establish account identity.
Default dry-run performs no writes. Reports contain counts/hashes and
`[REDACTED]` account identity, never transcripts or credentials. Compare against a
prior preview with `-expect-state-hash <sha256>` and
`-expect-approval-hash <sha256>`; either mismatch refuses staging.

Optional `-stage` saves the complete private checkpoint under
`<dataDir>/connector-handoff/checkpoints/<source>/<hash>.json`. Repeating the same
publication refuses to overwrite it. Keep this file with rollback evidence; its
account and thread fields are private operational data. Staging does not create
`transcript-sync/*/state.json`, set routing, pause a duty or enable a successor.
`checkpoint-ready` in this report means local checkpoint preparation succeeded;
it does not assert source-history coverage or live cutover readiness.

`-apply` checks the legacy pause/drain prerequisites and then **always refuses**
because there is no shared engine dispatch fence. The old
`cmd/transcript-sync-import -apply` also refuses before reading credentials or
writing anything; its default watermark preview remains available. The former
watermark-plus-key recipe is withdrawn. There is no supported activation command
or bypass flag in this implementation.

## Migration states and dispatch boundary

The routing record schema is version 1 at
`<dataDir>/connector-handoff/<email|granola|pocket>.json`. This pass defines and
consumes the schema but does not provide a command to mint live routing evidence.
Do not hand-create records to bypass the missing handoff protocol.

| State | Meaning / Manifest legacy dispatch |
| --- | --- |
| `not-ready` | No reconciled checkpoint; still-live legacy duty remains available. |
| `checkpoint-ready` | Checkpoint evidence present; legacy continues until explicit pause. |
| `paused-awaiting-reconciliation` | Owner pause recorded; manual dispatch/re-enable refused. |
| `successor-enabled` | Explicit Manifest ownership plus checkpoint, approval, exclusion, account and source-reconciliation evidence required; legacy dispatch refused. |
| `verified` | Also requires successor-run, restart/no-change and approval/vaultwriter receipt hashes; never inferred from enablement. Legacy dispatch stays refused when flags change. |
| `blocked` | Uncertain evidence; no legacy fallback. Corrupt/unreadable records also refuse dispatch. |

Rollback owner remains `excalibur`; old artifacts/history are retained. The guards
cover Manifest spool entry points, launch inventory and ritual re-enablement.
**They do not control Excalibur's independent scheduler or another direct spool
writer.** Read-only inspection of `scheduler.dispatch` shows its lock is only an
in-process ritual mutex, not a shared ownership fence. Disabling a cadence cannot
serialize with that dispatcher. Even a syntactically valid manually authored
`verified` record cannot enable production transcript polling in this version.
This is the exact remaining execution-boundary blocker, not a successful cutover.

## Next owner-controlled handoff and rollback

1. Implement and deploy per-duty dispatch exclusion honored by Excalibur's
   scheduler, manual dispatch and Manifest throughout each in-flight run and
   ownership transition. Establish who owns queued/claimed work. No other duty
   should stop for a single connector handoff.
2. Prove account binding without logging/copying credentials into evidence.
   Freeze complete canonical decisions and source-index evidence; resolve all
   missing/conflicting source IDs and uncertain effects. Inventory historical
   unfinished Pocket/Granola items beyond the current overlap before selecting
   the successor cursor. A complete approval inventory is not a complete upstream
   source inventory.
3. Under that exclusion, recompare hashes, refuse existing successor state,
   publish one atomic authoritative state and transfer one duty's ownership.
   Keep checkpoints/history and rollback ownership. Do not automatically replay
   uncertain creates or appends.
4. Obtain a real candidate and owner confirmation through canonical
   approvals/vaultwriter, then incremental no-change and restart evidence before
   marking that duty verified. No such real evidence was collected here.
5. Rollback must stop successor dispatch, reconcile its subsequent outcomes and
   transfer current state back under the same exclusion. Never simply remove a
   routing record, flip a flag, or resume a stale legacy watermark.

Granola is the smallest next candidate after these gates; Pocket additionally
needs historical unfinished-recording coverage. Email remains legacy pending its
full deterministic adapter. Reasoning lanes, models/providers, tools, production
configuration and live runtime were left unchanged.

## Verification

Passed with isolated fixtures/local HTTP servers: focused tests for approvals,
connector handoffs, transcripts, spirits and both operator commands; full
`go test ./...`; `go build ./...`; relevant `go vet`; race tests for approvals,
connectorhandoff, transcriptsync, spirits and the checkpoint command; and
`git diff --check`. No JavaScript changed, so `node --check` is not applicable.
Regression coverage includes immutable concurrent staging, read-only previews,
hash drift, duplicate/conflicting IDs, preserved personal lifecycle fields,
uncertain outcomes/no replay, saved-card loss, queued/running work, production
activation refusal and verified-record legacy dispatch refusal without flags.
Existing approval byte-contract and vaultwriter suites run in the full suite.

## Exact changed files

- Approvals: `approvals/connector_inventory.go`,
  `approvals/connector_inventory_test.go`, `approvals/transcripts.go`.
- Portable handoff substrate: `connectorhandoff/checkpoint.go`,
  `connectorhandoff/email.go`, `connectorhandoff/state.go`,
  `connectorhandoff/state_test.go`.
- Operator commands: `cmd/connector-checkpoint/main.go`,
  `cmd/connector-checkpoint/main_test.go`, `cmd/transcript-sync-import/main.go`.
- Runtime wiring and legacy guards: `main.go`, `spirits/connector_handoff.go`,
  `spirits/connector_handoff_test.go`, `spirits/ownership.go`,
  `spirits/rituals.go`, `spirits/spirits.go`.
- Transcript reconciliation: `transcriptsync/checkpoint.go`,
  `transcriptsync/checkpoint_test.go`, `transcriptsync/outcomes.go`,
  `transcriptsync/service.go`, `transcriptsync/service_test.go`.
- Operator documentation: this file,
  `docs/excalibur-retirement/legacy-ownership-checkpoint.md`,
  `docs/excalibur-retirement/transcript-extractor-migration.md`.
