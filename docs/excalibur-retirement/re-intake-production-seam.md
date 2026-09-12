# Re-intake production prerequisite — disabled adapter

Attempt 36 passed the synthetic local DeepSeek contract at deployed commit
`7413bf3167ba5750d6041a9d1807a471b6c2763b`. This is transport evidence, not
extraction-quality evidence or a production ownership transfer. Excalibur remains
owner of `extractor/re-intake`. No live route, command, poller or scheduler calls
the new adapter. `reIntake.productionEnabled` is absent/default false. Even with
that flag and valid authority, `ValidateProductionRoute` refuses activation.

## Why integration stays disabled

`server/intake.go` saves the original document to vault CAS through `reFiles.Save`,
writes its extract through `vaultwriter`, assembles live contractor/property and
existing-contract context, then spools Excalibur. `extract.Doc` supports formats
including PDF via pdftotext; it is not an isolated dataDir ingest contract. Neither
is suitable as a read-only source hook for this task. The existing dark migrated
Hermes server helper also writes the production ledger, so it is not reused.

This pass implements the user's explicit bounded-adapter alternative. Missing
semantics remain: an authenticated owner trigger, a reviewed staged original ↔
extract CAS relationship for non-text formats, a bounded domain-context snapshot
and its freshness/deduplication rules, and explicit owner handoff to existing
approval filing. No substitute ingestion, authentication or approval engine was
invented. A future cutover must supply these semantics and separately establish
Excalibur pause, work accounting, ownership snapshot and owner authorization.

## Adapter contract (not an exposed API)

`reintake.RunStaged` accepts trusted application configuration, the exact duty
scope, and one `ProductionContract`: owner `owner`, actor `extractor`, one source
repeated as the sole `documents` entry, target equal to applyPath, and existing
`approvals.ReContractPathAllowed` acceptance. The owner string is an in-process
assertion, never authentication or an HTTP authorization token.

The only input shape is an already staged UTF-8 text document, at most 16,000
bytes, whose SHA256 equals `source`. It is read from exactly:

```
<dataDir>/excalibur-retirement/re-intake-production/staging/<sha256>.txt
```

The source hashes those exact document bytes. It is not a claim that extracted
text hashes to a different PDF/original CAS ref. No arbitrary path, URL, uploaded
binary, roster lookup or document extraction is accepted. Ancestors and files
use descriptor-pinned no-follow opens; symlinks, hardlinks, FIFOs, traversal,
non-UTF-8, empty/oversized input and hash mismatch refuse. No staging uploader is
provided. Tests create only synthetic documents in disposable directories.

Authority is exactly `deepseek-local`, `deepseek-v4.1-flash`, fixed endpoint
`http://192.168.87.11:8000/v1`, binding `fixed-local-endpoint`, cost policy
`local-zero-marginal`, ceiling USD 0, tools `[none]`, MCP `no_mcp`, one step and
positive timeout at most 120 seconds. The adapter creates the existing bounded
Hermes runner with only this duty and a fixed migrated-duty request. Its isolated
Python runner, scrubbed environment, Landlock/seccomp and usage verifier are
unchanged. Only that runner can mint `DutyVerified`; test completions are local
simulation, not live evidence. No retry/fallback argument or path exists.

The model returns one exact JSON envelope with `type`, `actor`, `source`,
`target`, `applyPath`, and existing `ReContractPayload` as `payload`. Strict
parsing rejects extra output, unknown/duplicate/case-aliased fields and scope
changes. Existing payload validation and fence parsing are reused. The adapter
builds an `approvals.Proposal` value with fixed agent/ritual/action, no ID, no
proposed body, no auto mode. It returns that candidate in memory only, after
receipt persistence. It does not call Propose, Confirm, any store, or vaultwriter.
Existing approval filing/confirmation must remain separate explicit owner actions.
A valid candidate still needs semantic review; property/node existence and correct
extraction are not established by shape validation.

## Receipt and stop behavior

A synced permanent `invoked/` reservation precedes creation of `run.jsonl` (0600)
in the fixed adapter directory. The receipt first records `uncertain` and syncs
both file and directory before any provider call. An independent reservation
means a lost receipt still cannot authorize a second invocation. Existing receipts,
reservations, overlapping calls and interrupted runs refuse. No reset/delete or
retry API exists, including after success; this is a one-document pilot seam.

A terminal `verified`, `refused` or `uncertain` record is synced before returning.
Verified means bounded transport and proposal shape only. Receipts hold fixed
provider/model/binding/policy, verified token counts when available, timestamps,
a candidate digest and redacted reason; no document/source/target, prompt, reply,
credentials or arbitrary diagnostics. Failed verification erases candidate output.
Lost, replaced or unsyncable final receipt returns uncertainty without a candidate.
The prior uncertainty record/reservation remains the stop evidence where storage
permits; unavailable storage is reported as an error, never a success.

Failure returns STOP/page-owner instructions and `pageOwnerRequired: true`.
There is no notification sender or production signal mutation in this unwired
adapter. Parent/owner receives local evidence; automatic delivery belongs to the
future authenticated integration. No approvals, connector/cursor, spool, portal,
vault audit or production ledger are changed. Only this derived reservation and
redacted receipt are written.

Agents/Settings continue to show shadow/not routed, with separate synthetic
canary status, production route disabled and owner Excalibur. Neither reading a
passed canary nor loading the production flag activates anything.
