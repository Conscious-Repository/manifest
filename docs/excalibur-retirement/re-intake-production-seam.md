# Re-intake live handoff — implemented, default off

Owner decision, 2026-09-12: the canonical source is the existing
`POST /api/realestate/intake?name=...` raw-body upload. Source ingestion may use
its existing vaultwriter/CAS/extract path before model execution. A verified
candidate may enter the existing pending approvals inbox, with owner confirmation
still required. The first pilot is one owner-uploaded document, then stop.
This supersedes evidence 37's intentionally unwired seam, not its execution bounds.

## Configuration and owner boundary

`reIntake.productionEnabled` defaults to false, independently of `shadowEnabled`.
False preserves the existing Excalibur dependency check, source writes, request
context, spool call and response. True never spools Excalibur, including on error.

Example declaration for review, **not enabled by this change**:

```json
{
  "reIntake": {
    "productionEnabled": false,
    "ownerBoundary": "private-tailnet-owner"
  }
}
```

The exact duty remains under `hermes.duties["extractor/re-intake"]`:

```json
{"provider":"deepseek-local","model":"deepseek-v4.1-flash","endpoint":"http://192.168.87.11:8000/v1","providerBinding":"fixed-local-endpoint","costPolicy":"local-zero-marginal","tools":["none"],"mcp":"no_mcp","timeoutSeconds":120,"maxSteps":1,"ceilingUsd":0}
```

Manifest's owner cockpit has no HTTP identity/authentication layer. Its private
listener/tailnet deployment is the current owner access boundary. The exact
`ownerBoundary` value is an explicit operator acknowledgement, **not a password,
header, credential, or proof of network policy**. Missing/other declarations refuse
before source ingest. Parent must verify the actual listener/proxy/tailnet grants
before setting it. The route is absent from team and Olga listener mounts.
No auth is weakened or invented, and no public route is added.

## Canonical source and bounded successor

The handler retains `reFiles.Save` (original CAS blob plus existing `files.json`
metadata) and `extract.Doc`, then writes the existing `.extract.md` note with
`vault.WriteCap("re-files", ...)`. The normal index update is retained. No second
uploader or format parser exists. The model cannot access the vault writer.
No usable text, invalid UTF-8/NUL, or extracts larger than 16,000 bytes refuse;
large/truncated source windows are not silently submitted to the model.

`StageExtract` makes a private, exclusive, synced copy of these exact extracted
bytes under `<dataDir>/excalibur-retirement/re-intake-production/staging/<hash>.txt`.
`ProductionContract.source` and its sole `documents` item name the **original CAS**;
`textSource` names the exact staged **extract hash**. This distinction is retained
for PDF/DOCX/etc. The proposal's `payload.doc` must equal the original CAS.
Descriptor-pinned/no-follow staging reads and writes reject links and unsafe paths.
The target is fixed by the application as
`system/realestate/contracts/intake-<original-sha256>.md`, within the existing
ReContract allow-list. The model cannot select another destination.

The existing `intakeRequest` supplies contractor roster, active properties and
existing contracts; concrete work-node IDs/text are appended because there are
no tools. Context is limited to 32,000 bytes and the complete prompt to 64,000;
overflow refuses, never truncates. Existing contract and pending-card source
matches refuse before invocation. The same domain-context projection is checked
again before filing; observed changes stop the handoff. This uses the existing
index/service freshness model, not a new transactional domain snapshot.

Only `RunStaged` → the existing migrated-duty Hermes runner is used. Exact local
DeepSeek authority, standard usage validation, one step, timeout at most 120s,
Landlock/seccomp, scrubbed environment, no tools/MCP and local-zero-marginal policy
remain unchanged. Cost telemetry is still unavailable. No fallback, legacy runner,
connector, production ledger, scheduler or poller is used.

## Candidate, receipt and pending card

Strict parsing accepts one JSON envelope with exact type/actor/source/target/path
and existing `ReContractPayload`. Unknown/duplicate/case-aliased fields, malformed
or uncertain output, extra JSON/markdown, scope drift, or unverified execution
return no candidate. The application builds the fixed proposal envelope; no
model-supplied ID/status/auto/apply instruction is accepted.

The existing synced initial uncertainty and terminal receipt remain mandatory.
`CheckCandidateReceipt` rechecks the candidate digest, exact envelope, and the
still-present terminal receipt before `approvals.Store.Propose`. `Propose` now
prepares and syncs a temporary file outside the pending folder, then atomically
publishes it, so a write failure cannot expose a partial card. There is no second
approval store. The normal pending card's existing edit/confirm/reject path is
unchanged. No Confirm or apply is called by intake; automatic append processing
cannot apply a ReContract card.

A verified receipt means execution/shape verification, **not semantic correctness
or owner approval**. It records candidate digest and token metadata, no source,
raw prompt/reply, credential or arbitrary diagnostics. `itemsWritten: 0` describes
the model adapter, not the later pending-card publication. The HTTP result reports
`status: pending`, `proposalId`, and `spooled: false`. Agents/Settings show flag,
owner, source route, candidate→pending handoff and pilot status; upload UI reports
pending review and pilot stopped. These projections never declare confirmed/applied.

## One-document pilot and failure semantics

A synced `upload-reserved/` is created before reading the production upload or
writing source artifacts. It serializes concurrent uploads across processes and
survives restarts. The separate `invoked/` and `run.jsonl` protect model execution.
Success, ingest failure, refusal, timeout, cancellation, missing/replaced receipt,
or pending filing failure leave the pilot stopped. No automatic second attempt,
cleanup, replay, reset/advance endpoint or scheduler exists. Invalid activation
configuration is rejected before a pilot reservation or model invocation.

Failures return HTTP 503, `pageOwnerRequired: true`, fixed STOP/no-retry/fallback
instructions and pilot status. This synchronous owner response plus local durable
reservation/receipt is the page mechanism; no outbound notification is sent.
A browser closed during a run cannot receive that response: Agents/Settings and
local evidence must be checked. Pre-model failures can leave only the reservation
and allowed source/staging artifacts. A final receipt that could not be persisted
cannot be claimed present. Successful pending publication followed by a lost HTTP
response is reconciled by reading the inbox, never by submitting again.

## Verified, inferred, unknown

Verified offline: exact canonical route/default-off behavior, authority/access
refusals, source writes via a real disposable vaultwriter, separate original/DOCX
extract identity, receipt-before-candidate, pending only, no automatic apply,
concurrent/restart one-shot guard, failure/no-proposal/no-spool/no-retry, protected
connector/cursor/ledger files, and Agents/Settings renderer projections. Existing
Hermes tests cover real OS isolation and usage verification; adapter tests simulate
completions and HTTP tests simulate the adapter return, not live DutyVerified.

Inferred: this handoff is ready for parent deployment/access review and the single
owner pilot after the approved cutover sequence. The exact network binding remains
an operator assertion, not cryptographic provider identity.

Unknown/unperformed: live document semantics, actual property/node correctness,
completeness of the existing tolerant index-backed domain projections, live endpoint
health since attempt 36, live auth/network grants, actual Excalibur pause/accounting,
and a deployed browser run. Existing shape validation does not prove semantic
mapping. No production config edit, real document, live model request, service
change, Excalibur pause/disable/mask, push or deployment occurred in this pass.

## Parent cutover and exact rollback/reset order

Before enabling: verify attempt 36 and this commit; deploy with production false;
verify owner boundary; pause **only** Excalibur extractor/re-intake using its
existing owner-controlled mechanism; account for queued/running/uncertain work;
snapshot/hash ritual, proposals and ownership; obtain/record the approved first
review window. Only then declare the boundary and enable the flag, restart Manifest,
upload one owner-selected document, review receipt and pending card, and stop.
No cutover action is part of this coding pass.

Rollback (parent only; preserve evidence throughout):

1. Stop new uploads and `sudo systemctl stop manifest`. Wait for the process to
   exit; do not infer a cancelled invocation had no outcome. Keep the Excalibur
   re-intake pause in place. Prevent deployment automation from restarting a
   stale enabled configuration while doing this sequence.
2. In the service's configured `/home/benjamin/src/manifest/config.json`, set
   `reIntake.productionEnabled` to **false**, preserving all other config fields.
   Do not remove the latches, staging, receipt, pending card or source artifacts.
3. Snapshot/hash the entire configured
   `<dataDir>/excalibur-retirement/re-intake-production/`, pending/approved/rejected
   inventory, and existing Excalibur queued/running evidence. Inspect receipt and
   source CAS against existing contracts and pending cards. Resolve any uncertain
   or duplicate outcome with the owner. Do not edit connector/cursor/ledger state.
4. Start Manifest with `sudo systemctl start manifest`; read Agents/Settings and
   verify `productionEnabled: false`. **Do not POST a test upload**: false restores
   the legacy spool path. Keep uploads stopped and the Excalibur pause in place.
5. Only after explicit owner duplicate/outcome review, restore Excalibur re-intake
   through its existing owner-controlled pause mechanism. Record the restored
   ownership. Never enable both writers concurrently.

An advance/reset is a **separate owner action**, not rollback or automatic recovery.
With Manifest stopped, production false and old-owner pause retained, first complete
step 3, preserve/hash and archive the **whole** production directory to an exclusive
owner-chosen evidence destination outside its active path (never just delete
`run.jsonl` or `invoked/`), and explicitly authorize a new review window before
restarting/enabling. This implementation provides no reset command or API. Do not
change `dataDir` to bypass the pilot latch. Source/approval artifacts remain intact.
