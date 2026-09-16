# Transcript and extractor replacement — implementation checkpoint, 2026-09-16

The replacement code is disabled by default. Granola/Pocket and the retained
extractors still belong to the live engine. No live state import, duty pause,
new approval, vault write, or engine shutdown was performed by this change.

## Delivered

- `transcriptsync/` owns Granola and HeyPocket HTTP reads, the existing pure
  transcript conversion rules, index-backed attendee resolution/dedupe,
  account-bound versioned state, and source-scoped polling. It does not invoke
  an LLM. Conversion and wire formats were ported from the deployed Excalibur
  casts; no runtime imports or subprocess calls to that engine remain.
- The original Chicago wall-clock slots remain: Granola 08:00/13:00/18:00,
  Pocket 09:00/18:00. Manifest's in-process poller lifecycle owns dispatch.
  A persisted last attempt prevents repeated dispatch in one slot. First startup
  after a due slot catches up; failed polls remain visible for manual retry or
  the next slot. There is no additional daemon or cron service.
- Paginated responses must complete; missing/repeated cursors, page ceilings,
  malformed responses, auth errors and unreadable indexes refuse the run. A
  one-day overlap handles timestamp/day boundaries. An unfinished transcript
  holds the checkpoint; durable handled-item identities suppress duplicates
  while a delayed source becomes ready. Pocket retains its one-hour settle rule.
- `approvals.Store.ProposeTranscript` reconciles source IDs across pending,
  approved and rejected proposals while holding the decision lock. Existing
  IDs, edited titles and decisions are retained. A source already in the vault
  is skipped; a filename occupied by a different/unknown source is a conflict,
  not permission to overwrite. Cross-source fuzzy title matches remain warnings.
  Transcripts containing backtick fences round-trip intact.
- Candidate publication precedes checkpoint advancement. Recovery reconciles
  canonical approvals by source identity even if the item checkpoint was lost.
  Secrets and operational state stay under configured dataDir with private file
  modes. `GRANOLA_API_KEY` / `POCKET_API_KEY` retain precedence. Migrated
  credential test/replace/disconnect and manual poll use the same store.
- `domainextract/` provides default-off event/request dispatch for AION,
  real-estate and OODA email. Existing watchers and confirmed-email entry points
  supply explicit source snapshots and domain context. No input is silently
  truncated. Per-request durable state and OS claims serialize execution;
  an interrupted run is held as uncertain. Verified candidates can resume
  publication without another model call. Stable proposal IDs preserve decisions
  across partial publication. Source/domain changes refuse stale publication.
- Extraction proposals retain the existing AION/RE backlog, heuristic, resolve,
  and OODA contract types. Strict JSON rejects unknown/duplicate/case-aliased
  fields, scope drift, fabricated quotes and secret-shaped output. Models never
  receive approval-store or vaultwriter handles. Confirmation remains canonical.
- The subscription adapter preserves `claude-sonnet-5` for the three retained
  extraction lanes: one turn, explicit timeout and budget, no tools/MCP,
  customization loading disabled, a scrubbed environment, private copied OAuth
  credentials and Linux Landlock/seccomp isolation. A completion is accepted
  only when init/assistant/result evidence corroborates model, authority, usage,
  one turn and completion. The existing DeepSeek re-intake pilot and its latches
  remain separate; neither subscription fallback nor a re-intake model change
  was enabled. CLI flags were checked against installed help and the official
  [CLI reference](https://code.claude.com/docs/en/cli-usage).
- All Manifest legacy spool entry points refuse duties explicitly assigned to
  the new owner. This does **not** pause Excalibur's own scheduler: the operator
  must pause and reconcile that duty before changing the flag. New extraction
  reports retain the harness file contract and identify their executor as
  Manifest; historical reports keep their original identity.

## Configuration and state transfer

The following is an example only; no deployed configuration was changed:

```json
{
  "transcriptSync": {
    "granola": {"enabled": false, "account": "owner"},
    "pocket": {"enabled": false, "account": "owner"}
  },
  "domainExtraction": {
    "aion": false,
    "realEstate": false,
    "oodaEmail": false
  }
}
```

An account label binds imported state to the operator-selected source account;
it is not a claim that the API independently proved account identity. Never
reuse that label/state for a different source account.

Preview the actual legacy checkpoint without reading/copying a secret or writing
state (explicit absolute `dataDir` and Excalibur harness paths required):

```sh
go run ./cmd/transcript-sync-import -config /path/to/offline-config.json -source granola
go run ./cmd/transcript-sync-import -config /path/to/offline-config.json -source pocket
```

For the eventual transfer, stop the old duty's automatic/manual dispatch, resolve
queued/running work, and back up/hash its watermark plus the complete canonical
approval inventory. While the source is disabled in configuration, repeat the
import with `-apply -key-file /path/to/existing/key` (or its environment override).
The importer refuses an existing imported state rather than overwriting it.
It also requires the legacy ritual file to declare `enabled: false` and a
nonempty `paused_reason` before an applied import writes state or a key. This
check does not prove queued/running work has drained; reconcile that separately.
Approval files remain in place; only checkpoint/credential ownership moves.
Enable one source only after reconciliation. The state/credential paths are
`<dataDir>/transcript-sync/<source>/{state.json,key}`. Old files are retained;
the enabled source never reads the old key as a hidden fallback.

Each enabled extraction lane requires an explicit `hermes.duties` declaration:
provider `claude-sub`, model `claude-sonnet-5`, tools `["none"]`, MCP `no_mcp`,
`maxSteps: 1`, timeout at most 120 seconds, and a positive `ceilingUsd` no greater
than the old duty's limit (4 for AION/real-estate, 2 for OODA email). There is no
implicit provider/model substitution. The synthetic canary failed (below), so
these routes must stay off pending verified execution and semantic comparison.

## Evidence and limits

Offline verification covers conversion, source pagination/failure, unfinished
Pocket recovery, lost checkpoints after owner rejection, source/account state
validation, Chicago/DST scheduling, default-off configuration, exact proposal
contracts, stale/interrupted extraction, resuming verified publication, route
ownership, and real OS denial of reads/writes outside the subscription sandbox.
The existing re-intake canary call graph was re-audited: its fixed duty/provider
cannot reach the subscription branch. The reviewed-source hashes include the new
pure duty whitelist and its implementation.

Both existing historical re-intake fixtures (`single`, `split`) were replayed in
isolated offline directories. Structured parity passed for each; these fixtures
are redacted historical approved proposal/run pairs, not new model completions
and not proof of semantic extraction quality. Their comparison receipts stay
under the private evidence directory below.

Checkpoint previews read the actual old state, without applying imports:

- Granola: 2026-09-13T21:13:24Z.
- Pocket: 2026-09-02T21:33:36Z.

One synthetic Claude subscription canary was attempted with a 60-second timeout
and USD 0.25 ceiling. It did not return verified completion evidence. The local
process ended at its bound; remote completion/spend are unknown. No source
transcript was submitted, no proposal was created, and no automatic retry was
performed. Its private receipt is
`<dataDir>/excalibur-retirement/20260916-connector-extractor-check/claude-canary.jsonl`.
The new launcher is therefore **implemented and offline-tested, not live-proven**.

A source HTTP fixture passing does not prove live API availability. No Granola
or Pocket network poll was made in this pass. Input/output bounds, source quote
checks and shape validation do not prove semantic completeness or matching of
property/work-node references. OODA's application-supplied property/contract
projection is a snapshot, not a transactional lock over all domain records.
Failed publication is preserved and reconciled on restart/another queue wake;
uncertain executions are never retried automatically.

## Remaining gates and rollback

1. Diagnose the bounded subscription canary without replaying an uncertain
   extraction; obtain complete provider/model/usage evidence, then compare old
   documents against their prior outputs and review extraction quality.
2. For each connector, perform the snapshot/pause/import handoff above and prove
   a new candidate, an incremental no-change poll and restart behavior. Preserve
   the actual owner-confirmed note path and post-confirm extraction notification.
3. Cut over reasoning lanes individually only after their comparison gate. Keep
   the current re-intake one-document pilot/reset rules. Observe the first lane's
   real outputs before advancing further lanes.
4. Before full engine shutdown, resolve personal email sync and every remaining
   engine consumer; then satisfy the existing idle-week gate. This change does
   not claim that complete retirement is ready.

Rollback stops the new source/duty first. Preserve its entire operational state,
receipts and canonical proposals, reconcile post-transfer outcomes and move state
ownership back explicitly. Re-enabling an old ritual against a stale checkpoint
is not rollback. Never enable both owners, reset an uncertain extraction, delete
pilot latches, or reapply already decided proposals as an automatic recovery step.

## Completed checks

- `go test ./...` — passed, including the existing server and re-intake suites.
- `go test -race ./domainextract ./transcriptsync ./approvals ./hermes ./spirits`
  — passed.
- `node server/testdata/agents-legacy-labels.cjs` — passed.
- `git diff --check` — passed.
- Import previews and the two isolated historical fixture replays — passed as
  described above. These were read-only previews/offline comparisons.
- Live subscription canary — failed to establish verified completion; activation
  remains blocked. No source/approval effects were attempted by that canary.
