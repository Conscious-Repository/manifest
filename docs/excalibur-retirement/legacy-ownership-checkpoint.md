# Legacy ownership checkpoint — 2026-09-16

**Migration is incomplete. No live duty changed owners in this slice.**

The later [connector handoff substrate](connector-handoff-substrate.md) supersedes
this document's import recipe and successor-substrate status. Personal legacy
email is proven active by the supplied live run (personal account, 100-thread
page, new proposals and confirmed appends). Its executable Manifest replacement
is still missing; the new personal ledger checkpoint is not a poller.
Watermark-only applied imports are now refused, and no duty is cutover-ready.

This checkpoint follows the owner's September 12 decisions in
`system/workbench/plans/2026-09-11-excalibur-deprecation.md`, ARCHITECTURE.md,
and the disabled implementations in `dc5f470`.

## Observed ownership

Read-only localhost GETs to `/api/spirits/rituals`, `/api/spirits/status`,
`/api/spirits/runs` and `/api/agents/hermes` confirmed the following. Configuration
was inspected only for relevant enablement/path declarations. The Excalibur
heartbeat was alive; the run projection showed zero queued or running jobs at
inspection. That snapshot does not establish a drained handoff or an idle week.
Cron times below are America/Chicago.

| Duty | Current owner / trigger | Successor readiness and remaining gate |
| --- | --- | --- |
| EA email-sync | Excalibur, enabled, daily 07:00 | Legacy personal sync is active. Manifest has a ledger checkpoint substrate but no executable personal successor. `gmailsync.Loop` is roster-filtered portal mail, not personal parity. |
| EA granola-sync | Excalibur, enabled, 08:00/13:00/18:00 | `transcriptsync` implemented and offline-tested, not enabled or handed off. |
| EA pocket-sync | Excalibur, enabled, 09:00/18:00 | `transcriptsync` implemented and offline-tested, not enabled or handed off. |
| extractor/aion | Excalibur, enabled, event/request | Default-off `domainextract` replacement; verified subscription completion and quality comparison remain blocked. |
| extractor/real-estate | Excalibur, enabled, event/request | Same gate; no authority or model change. |
| extractor/ooda-email | Excalibur, enabled, confirmed-email/request | Same gate; no authority or model change. |
| extractor/re-intake | Legacy retired; Manifest owner-upload pilot configured | Live projection says production routed, synthetic canary passed, one-document pilot unused. This is not evidence of a production extraction. Existing stop/latch gates retained. |
| concierge/briefing | Already retired, file disabled and manual stop gate present | No replacement; history retained. |
| EA waiting-on | Already retired, file disabled and manual stop gate present | No replacement; personal email remains enabled. |
| sage/skill-cast | Already retired, file disabled and manual stop gate present | No replacement; history retained. |
| warden/audit | Already retired by separate owner request in harness metadata | Existing retirement retained; an independent successor findings sweep was not established by this audit. |
| Alfred AION scout | Hermes, enabled, daily 07:00, DeepSeek v4.1 Flash | Existing current runtime job; not a connector or extractor replacement. |
| Legacy chat / delegate / team runners | Existing file-contract routes remain | No cutover or drain proven here; no changes to their execution or history. |

The three retained extractor rows report `claude-sub / claude-sonnet-5`.
EA connector rows report `deepseek-local / discover` for the legacy orchestrator;
that label is not a model dependency of the deterministic replacement. Full
provider, toolset, step and budget metadata remain in the API; this slice does not
modify them. Harness provenance remains `/private/harnesses/excalibur`, with
ritual definitions under `spirits/<spirit>/rituals/` and reports under
`artifacts/runs/`.

## Why connector activation stops here

Email's engine state is an account-specific watermark plus per-thread
`status`, `proposal_id`, `last_msg_id`, `last_internal_ms`, and `filename`.
Its proposed/synced/muted lifecycle now has a lossless checkpoint decoder in
Manifest. Known-contact/workspace filtering and accepted-thread append behavior
still have no executable personal successor.
Replacing it with the portal loop would change its contract.

Granola and Pocket retain their actual watermark files, respectively
`2026-09-13T21:13:24Z` and `2026-09-02T21:33:36Z` at inspection. Their legacy
per-item decisions reside in canonical approvals and vault source identities;
the original importer started an empty outcome map. The later checkpoint
reconciler now reconstructs outcomes from those stores and rejects uncertain
results. Copying the watermark alone is not proof of continuity. In
particular, old loops can advance past unfinished/empty transcripts; the new
one-day overlap cannot prove it recovers every older unresolved item. No complete
approval/source reconciliation or frozen handoff snapshot was made here.

Neither `transcriptSync` nor `domainExtraction` is enabled in the inspected
configuration. Their prerequisite gates in
[the implementation checkpoint](transcript-extractor-migration.md) still apply:
source-account binding, old automatic/manual dispatch exclusion, drained work,
checkpoint and complete approval snapshot/hash, source-ID reconciliation,
restart/no-change continuity, and candidate/confirmation path evidence. The prior
subscription canary failed to establish verified completion. No new canary,
connector request, real cursor advancement, credential access, approval, or vault
write was performed in this slice.

## Safe changes delivered

- Ritual API projects configured routing independently of the raw legacy
  `enabled` flag. A configured successor is `handoff-unverified`; a still-enabled
  old file is `ownership-conflict`. A retired policy with enabled markdown is
  `retirement-conflict`. None means migration completed.
- Legacy duties display `legacy · retiring` and their remaining gate. Current
  Hermes jobs sort first within schedule groups. Retired history remains readable
  with disabled launch controls; source harness/path stays attributable.
- Configured successor duties are removed from legacy launch pickers and casts.
  The existing spool refusal remains; the file editor now also refuses legacy
  re-enablement. A conflicting enabled schedule can still be paused.
- Historical behavior, now superseded by unconditional apply refusal pending a
  shared dispatch fence: applying a transcript import refused an unreadable or
  enabled legacy definition, or a pause without a reason **before writing successor state or a key**.
  Read-only preview remains available. This prerequisite check cannot exclude
  queued/manual work or an operator re-enabling the engine file concurrently;
  it does not replace the handoff protocol.

All live schedules, source state, approval/history files, credentials and vault
bytes remain untouched. Existing retirement metadata passed the opt-in read-only
`EXCALIBUR_PHASE2_HARNESS` check for all five retired duties. No deployment or push
is part of this change; the new projection takes effect only after deployment.

## Rollback and next handoff

This slice needs only a code revert before deployment; it moved no operational
state. For a later live cutover, retain the old watermark and canonical inventory
with hashes, account binding and rollback ownership. Stop new dispatch before
rolling back; reconcile all outcomes since the snapshot before returning ownership.
Never restart the old owner with a stale cursor or replay uncertain effects.

Full decommission remains blocked by personal email implementation, connector
handoff proof, retained extractor execution/quality gates, remaining consumer and
governance verification, and the idle-week gate. Keep Excalibur running.

## Validation

Passed: focused `go test ./spirits ./transcriptsync`, `go test ./server`,
`go test ./...`, `go build ./...`, and
`go vet ./spirits ./transcriptsync ./server`. The opt-in harness metadata check
passed for the actual five retired definitions. JavaScript ownership/controls
assertions passed in `node server/testdata/agents-legacy-labels.cjs`; syntax checks
passed for that file and `server/web/js/41-agents-schedule.js`.
`git diff --check` passed. Tests use isolated fixtures and local HTTP servers;
no live connector sync was used as QA.
