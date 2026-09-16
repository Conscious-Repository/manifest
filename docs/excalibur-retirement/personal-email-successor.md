# Personal email continuity prerequisite — 2026-09-16

The historical mismatch no longer prevents an offline continuity report or
independent clean-thread read checks. It still blocks identity-complete import
and cutover. Excalibur remains the personal email owner. This implementation
claims no personal-email parity and is not wired into Manifest's scheduler.

`personalemail.Check` is disabled by default. The standalone
`cmd/personal-email-preflight` command always reconciles identity; `-live-read`
explicitly enables Gmail anchor verification for clean rows only. It reuses
`gmailauth.ReadSource` and the `gmailsync` GET transport, without the OODA loop,
roster, tokens store, or candidate store. Requests pin the selected account and
fetch only thread/message IDs and internal dates. No bodies or headers are
requested. OAuth refresh, if needed, can POST to the token endpoint but keeps
refreshed credentials in memory. No service or timer is installed or enabled.

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

## Evidence still required before email cutover

- Verify the actual primary and extra mailbox bindings, settings, and state-file
  coverage. Attribute the 41 approval-only identities to the right account/state;
  absence from this primary state must not be guessed away.
- Establish independent identity evidence for both conflicting proposal/thread
  associations, or carry their explicit permanent quarantine into a separately
  reviewed active successor. Do not rewrite the rejected artifacts. Unknown
  identities remain blocked; no owner record overrides the mismatch.
- Reconcile the seven append lineages and all accepted/synced outcomes against
  canonical approvals and read-only vault/source identity evidence. Identity
  matching alone is not proof of an applied or unapplied effect.
- Run explicit live anchor checks against verified accounts, with successful
  restart/no-change observations. This offline run did not access Gmail. Missing
  anchors and read failures remain unverified, never replayable.
- Implement and prove personal sync behavior: account/contact/workspace filters,
  paginated discovery and overlap/backfill, new proposals and accepted-thread
  growth via canonical approval/vault paths, extraction routing, idempotency,
  failure recovery, and durable successor cursor semantics. This seam only reads
  known clean threads; it cannot discover new mail or replace the active ritual.
- Establish shared dispatch exclusion for automatic/manual legacy launches,
  drain queued and running work, take a stable final snapshot, bind the successor
  to it, and prove rollback/restart continuity before ownership transfer.

Do not stop Excalibur on the strength of this report. Final decommission also
requires the remaining duty/consumer gates and the agreed idle-week evidence.

Synthetic tests cover conflicting and missing identities, duplicate proposals,
quarantine alongside clean reads, default-off behavior, exact anchor checks,
GET-only account-pinned requests, redaction, verbatim clean-state preservation,
and unchanged state/approval/vault/cursor fixtures. No real send/apply is tested.

Validation passed: `gofmt`, `go test ./...`, `go build ./...`, `go vet ./...`,
`go test -race ./...`, and `git diff --check`. Both disabled and activated
synthetic CLI checks leave approval, vault, state, and cursor fixtures unchanged.
The live environment verification was offline only; no service state changed.
