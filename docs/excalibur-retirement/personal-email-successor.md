# Personal email continuity prerequisite — 2026-09-16

Excalibur remains the live personal email owner. A default-off Manifest-owned
personal-mailbox successor and explicit fenced cutover now exist (see below).
Historical conflicts can transfer as permanent quarantine without weakening
the older strict importer. No live ownership or service change was made.

`personalemail.Check` is disabled by default. The standalone
`cmd/personal-email-preflight` command always reconciles identity; `-live-read`
explicitly enables Gmail anchor verification for clean rows only. It reuses
`gmailauth.ReadSource` and the `gmailsync` GET transport, without the OODA loop,
roster, tokens store, or candidate store. Requests pin the selected account and
fetch only thread/message IDs and internal dates. No bodies or headers are
requested. OAuth refresh, if needed, can POST to the token endpoint but keeps
refreshed credentials in memory. No service or timer is installed or enabled; a default-off unit is provided.

Example offline preflight (replace ACCOUNT with the independently verified
personal mailbox before requesting live reads):

```sh
go run ./cmd/personal-email-preflight \
  -email-state /private/harnesses/excalibur/vessel/state/email/state.json \
  -artifacts /private/harnesses/excalibur/artifacts \
  -account ACCOUNT
```

Output is a redacted derived report on stdout, not an active checkpoint. The
command has no write/stage/apply flag. It rechecks state bytes and inventory hash
after observation and refuses drift; two observations are not a dispatch lock.
Every row has `replay=false`; every ambiguity has a stable stop reason and
`legacy-reconciled-uncertain` disposition. Unknown approval threads remain
quarantined. Invalid inventories, duplicate canonical proposal IDs, duplicate
JSON keys, and unknown state fields fail closed. The existing strict checkpoint
importer and owner reconciliation guards remain unchanged. Only a completely
identity-reconciled input gets a verbatim in-memory derived copy of legacy bytes;
that copy is never included in the report or written to disk.

## Read-only observation

[The per-thread evidence](personal-email-continuity-evidence.json) was generated
without live reads, credential access, or external writes. The account hash is
bound to the literal placeholder `legacy-primary-unverified`, **not verified
mailbox identity**. It covers all 57 legacy threads and 41 additional approval
thread identities absent from this state (98 report rows). There are 48 clean
identity rows and 50 quarantined rows: 41 absent from state, seven needing append
effect evidence, and the two conflicting historical associations. Stop reasons
can overlap. Clean identity does not establish vault effects or live continuity.

The exact conflicting associations are:

| Legacy thread | Legacy proposal | Approval claims thread | Decision |
| --- | --- | --- | --- |
| `19fdd26132e9cc60` | `2a71f54cd7b3` | `19fdd282744d15a0` | rejected |
| `19fdd282744d15a0` | `d75af2b821e6` | `19fdd282744d15a0` | rejected |

Both rows are quarantined. Neither is merged, repaired, replayed, or treated as
reconciled by the preexisting owner decision. The evidence includes hashes of
each approval artifact and each decoded legacy row; the root legacy hash binds
the original file bytes. No historical file was changed.

## Successor implementation — 2026-09-16

The default-off `personal-email-worker` now discovers personal Gmail threads,
consumes all pages, applies the canonical Manifest contact predicate and calendar
filter, and files create/append proposals through the existing approval store.
It uses the existing primary and explicitly configured extra token paths with
in-memory refresh only. It does not
send mail, apply approvals, copy credentials, or write the vault. The dashboard
continues to own approval, vaultwriter capabilities, and extraction routing.
`aion` and `real-estate` categories and participant wikilinks match legacy routing;
append renames preserve the owner's title while extending the date range.

The worker owns only `<dataDir>/personal-email/active.json`. It never changes a
legacy Gmail cursor. Discovery uses the earlier of the watermark minus one hour
and the configured rolling backfill window. Failed reads retain the watermark;
completed per-thread proposal intents remain durable. Pending proposals are
revisited even after they leave the discovery window. Growth requires the exact
message ID and internal timestamp anchor, an approved canonical identity, and
one indexed thread note. Missing anchors, ambiguous effects, and interrupted
proposal publication are terminal `uncertain`, never automatic retries. Rejected
threads are muted. A full thread-ID filename suffix disambiguates new notes.

The historical mismatch is no longer an identity-complete-import prerequisite
for this separate successor. Cutover carries the **entire** continuity receipt,
including raw-state hash, decoded-row hashes, original proposal associations,
claim identities and artifact hashes. The 41 approval-only identities, seven
append lineages and two conflicting associations remain
`legacy-reconciled-uncertain`, `replay=false`. They neither merge nor become
replayable; clean new threads can proceed alongside them. The older strict
checkpoint importer remains unchanged. Identity-clean historical rows lacking
sufficient lifecycle evidence are also conservatively quarantined.

A permanent effect claim in the canonical approval store prevents successor
create/append effects from replaying after a crash between the vault effect and
decision publication. Successor proposals require the configured vaultwriter
capability; there is no writerless fallback. An effect claim is never cleared
automatically, including after a failed effect. Recovery requires independently
reconciling the proposal, claim and actual note; do not delete claims to retry.
These changes must be deployed to the dashboard before enabling the worker.

The worker covers the **primary and every explicitly configured extra mailbox**.
Preparation requires exact coverage of the existing extra token inventory and
configured settings, including paused accounts. Each active extra requires its
matching legacy state file. Each mailbox has a separate durable
cursor, receipt and lifecycle map inside the same atomic activation. Polling
honors each account's sync and extraction settings. Missing coverage refuses the
whole duty transfer; nothing silently pauses or disconnects another account.

Disconnected legacy extra state is preserved separately in the durable activation's
`orphans` receipts with disposition `legacy-orphan-quarantined`, `replay=false`,
its original absolute path, exact file hash and decoded thread-row hashes. These
receipts have no account binding, approval matching, active cursor or proposal
queue. The quarantine hash binds all orphan receipts alongside the unchanged
primary and active-extra continuity receipts. Apply rechecks the inventory and
hashes; changing, adding or removing a source file invalidates the prepared plan.
Polling preserves these receipts without opening an orphan mailbox.

Only files without a matching live extra token **or configured settings** can
receive this classification. Settings without a token, unknown/non-matching live
extras, missing active state, colliding account slugs, malformed state/settings,
and ambiguous filenames refuse preparation. A slug is never decoded into an
account identity. Primary-only cutover requires exactly zero live extra bindings,
zero configured extras, and explicit orphan quarantine for every extra state file.
Legacy files remain in place and unchanged; credentials are never copied into
receipts. Existing historical conflicts remain permanently quarantined.

The supplied September 16 handoff records a connected primary `ben@aion.bio`,
48 anchor-verified and 50 quarantined primary identities with replay disabled,
an empty extra-token directory and zero configured extra accounts. It identifies
`/private/harnesses/excalibur/vessel/state/email/state-ben-ooda-group.json`
(41 threads) as disconnected legacy state. Those observations permit the narrow
orphan classification; they do not establish thread/proposal identity or authorize
replaying its history. This coding change does not transfer live ownership.

## Explicit prepare / transfer / apply

Build `./cmd/personal-email-worker`. Create an explicit config outside the repo,
initially with `enabled: false`, substituting independently verified paths and
account/settings. A representative shape is:

```json
{
  "enabled": false,
  "account": "VERIFIED-PRIMARY-ACCOUNT",
  "legacyRoot": "/private/harnesses/excalibur",
  "dataDir": "/home/benjamin/.config/manifest",
  "index": "/ABSOLUTE/PATH/TO/LIVE/MANIFEST/INDEX.db",
  "extract": true,
  "workspace": "aion",
  "backfillDays": 30,
  "extraAccounts": [
    {"account": "VERIFIED-EXTRA-ACCOUNT", "sync": true, "extract": false, "workspace": ""}
  ]
}
```

`sync`, `extract` and `workspace` must match the existing settings for every
account. Primary `sync` defaults to true when omitted; explicitly set it false
for a paused primary account. Extra `sync` is explicit. Accounts must be lowercase. No tokens belong in
this file. For the verified primary-only case, use `"extraAccounts": []`; do not
invent an account entry for orphan history. Runtime state binds the exact primary token path, account, config,
all legacy-state/approval hashes, quarantine receipts and ownership revision. Changing
routing, paths or backfill requires a reviewed new activation, not an in-place
config edit. Only `enabled` may change after apply.

1. Run `personal-email-worker -config CONFIG -mode prepare`. This reads metadata
   and evidence without network access, creating state, or publishing ownership.
   Review its plan hash and quarantine hash against the offline continuity report.
2. Drain legacy scheduled/manual runs and canonical approval activity, and verify
   every process uses the shared fences. Excalibur's existing runner and scheduler
   use the email dispatch fence. Only Excalibur may publish ownership:
   `excalibur dispatch-owner -root ROOT ea-coordinator/email-sync REVISION excalibur manifest PLAN_HASH`.
3. Run `personal-email-worker -config CONFIG -mode apply -revision REVISION -expect-plan-hash PLAN_HASH`.
   Apply holds both the legacy dispatch fence and a cross-process canonical
   approval decision fence, rechecks the full evidence, and exclusively publishes
   one atomic file containing state, quarantine receipt and activation. A second
   apply, stale revision, changed evidence or existing activation refuses.
   Failure leaves legacy fenced; it does not quietly restore legacy ownership.
4. Only after the live evidence below is accepted, change `enabled` to `true`
   and explicitly install/start/enable `deploy/manifest-personal-email.service`.
   The unit has activation/config conditions and no existing service starts it.
   Its vault access is read-only. No unit was installed or enabled in this work.

The service holds the dispatch fence through each scan, proposal and state save.
A legacy launch is refused after transfer. An ownership rollback/pause stops the
successor on its next poll, and cannot race an in-progress poll. For rollback,
stop the worker, reconcile all successor proposals/effects and its newer cursor
against the frozen legacy snapshot, then use **Excalibur dispatch-owner** with
the current revision and reviewed evidence. Do not resume legacy using its stale
cursor after successor effects. Returning ownership to Manifest does not reuse
an old activation: the exact revision check deliberately refuses it. Preserve
old activation/state and prepare a reviewed recovery before restarting.

## Final live evidence required

- Independently verify the primary token account and exact effective settings,
  OAuth read scope, extra-account inventory and explicit worker configuration, legacy state-file coverage, and the
  live index path/freshness. This coding run did not inspect credentials or make
  Gmail requests. The offline placeholder is not account verification.
- Run the existing preflight with the verified account and explicit `-live-read`;
  capture clean-thread exact-anchor results for each account and restart/no-change observations.
  Quarantined rows need no identity repair to transfer, but the owner must review
  their permanent no-replay receipt. Recheck the final 98-row inventory rather
  than treating these historical counts as an invariant if new mail arrived.
- Verify canonical approvals and read-only vault/index identity for historical
  synced threads. Missing/ambiguous targets must remain uncertain. Confirm
  dashboard deployment includes the decision fence and successor effect claims,
  the existing vaultwriter note capability, and both domain extraction sinks.
- Verify the deployed Excalibur binary fences both scheduled and manual email
  launches, drains queued/running work, and is the only ownership publisher.
  Obtain a stable final state/approval snapshot and reviewed prepare plan; prove
  rejected stale apply, exclusion, and rollback/restart behavior on deployment.
- After explicit apply, collect successful account-pinned paginated scans,
  unchanged-pass/restart evidence, known-contact filtering, a human-reviewed
  clean create and confirmed growth through canonical approvals, and correct
  extraction routing. No real send or auto-approval was exercised in coding.
- Keep Excalibur running until all remaining retirement duty/consumer gates and
  the agreed idle-week evidence pass. This implementation alone is not evidence
  that the entire Excalibur service can be stopped.

Synthetic tests cover conflicting and missing identities, duplicate proposals,
quarantine alongside clean reads, default-off behavior, exact anchor checks,
GET-only account-pinned requests, redaction, verbatim clean-state preservation,
and unchanged state/approval/vault/cursor fixtures. No real send/apply is tested.

Prior continuity-only validation passed: `gofmt`, `go test ./...`, `go build ./...`, `go vet ./...`,
`go test -race ./...`, and `git diff --check`. Both disabled and activated
synthetic CLI checks leave approval, vault, state, and cursor fixtures unchanged.
The live environment verification was offline only; no service state changed.

Successor synthetic coverage includes paginated account-pinned discovery,
canonical contact scope, create and confirmed growth proposals, extraction
categories and date-range renames, disabled/paused accounts, extra-mailbox
coverage, quarantine across accounts, interrupted filing/effect claims, exact
anchor loss, approval exclusion, CAS drift, rollback and unchanged legacy cursors.
The standalone worker never constructs a vault writer and opens only an existing
canonical inbox. Synthetic effect-claim tests do not apply or auto-approve notes.

Final successor validation passed: `gofmt`, `go test ./...`, `go build ./...`,
`go vet ./...`, `go test -race ./...`, and `git diff --check`. The offline
preflight again reported 98 primary identities and 50 quarantined rows, with
41 approval-only identities, seven append lineages and both conflicting
associations intact. No live Gmail requests, ownership publication, service
changes, historical approval edits, or vault writes were performed.

Orphan coverage tests exercise primary-only acceptance, durable receipt retention
through apply/poll, receipt deletion detection, source/inventory drift, live-extra
mismatch and settings-only refusal, ambiguous state and slug collisions. Checks
use synthetic fixtures and leave live credentials, approvals, vault and ownership
unchanged.
