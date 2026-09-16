# Read-only connector reconciliation report

Run from Manifest against an operator-provided frozen legacy tree. No connector
calls, credential reads, approval decisions, vault writes, staging, or ownership
changes are performed. `-report` refuses `-stage` and `-apply`.

```sh
go run ./cmd/connector-checkpoint -report -source email \
  -legacy-root /path/to/frozen-excalibur -account explicit-account \
  -email-state /path/to/frozen-email-state.json

go run ./cmd/connector-checkpoint -report -source granola \
  -legacy-root /path/to/frozen-excalibur -account explicit-account \
  -index-snapshot /path/to/consistent-backup.sqlite \
  -data-dir /path/to/manifest-data
```

Use `pocket` for Pocket. Obtain consistent SQLite backups through the database's
supported backup mechanism; do not copy a live database alone or remove its WAL.
Report generation does not create a backup. `-expect-state-hash` and
`-expect-approval-hash` compare with previous observations. JSON goes to stdout;
a successful exit means the report was emitted, **not** that handoff is ready.
`ready` is always false. Inspect both `inventory.issues` and `blockers`.

The inventory covers all canonical pending, approved, and rejected Markdown
files, across all connector categories. Counts represent files and proposals
in each issue, not unique blockers; a proposal can appear in multiple issues.
Missing directories and unreadable artifacts mean the inventory is incomplete.
Hashes cover raw bytes and ordered relative paths/statuses using SHA-256.
Source identities, proposal IDs, relative approval paths, and apply paths are
hashed in full, including IDs that happen to contain personal information.
No content, account, raw path, arbitrary metadata, or underlying error is printed.
Hashes are correlation identifiers, not encryption or proof of account binding.
Reports have no timestamps and remain identical for identical observations.

## Required owner actions

Record decisions in a separate, durable owner-authored reconciliation record,
referencing report state/inventory hashes, source category and identity hash,
all proposal ID/artifact/content hashes, the decision, rationale, owner, and date.
Keep the original approval files and decisions byte-for-byte. This command does
not consume reconciliation records: a recorded decision alone cannot bypass the
existing strict gate. Any future decision-aware gate requires separate reviewed
implementation and explicit authorization.

| Class | Required owner action |
| --- | --- |
| duplicate-create-identity | Identify every proposal for the identity; explicitly record which historical effects occurred and the intended authoritative lineage. Preserve every duplicate. |
| create-append-lineage | Establish the original create and ordered append lineage; verify each historical vault effect and record unresolved or intentionally rejected appends. Never treat append history as another create or automatic retry. |
| rejected-duplicates | Explicitly decide whether rejection covers that proposal or the source as a whole, and record treatment of each sibling. Never infer retry permission from rejection. |
| missing-source-identity | Establish source/account provenance from owner-accessible evidence; record the mapping separately. Unattributed ordinary creates are conservatively included for review. Do not invent an identity. |
| malformed-conflicting-aliases | Determine the intended identity for each alias or malformed field using original evidence; document all competing values and the chosen mapping in the private owner record. Do not rewrite history. |
| invalid-proposal-identity / invalid-connector-type | Reconcile duplicate decisions, filename/ID mismatches, or unsupported proposal types explicitly. Preserve artifacts and document provenance. |
| inventory-unavailable | Provide a complete readable frozen canonical inventory, preserving missing/unreadable artifact evidence; rerun. |
| index-invalid / index-wal-invalid | Provide a verified consistent SQLite backup with the expected notes schema and unique source identities. Reconcile duplicate or invalid indexed identities against historical effects; never discard WAL or edit runtime state. |
| state-invalid | Supply valid frozen state for the exact account/source. Preserve corrupt or missing-state evidence and establish continuity explicitly. |
| state-drift | Freeze a coherent new snapshot and compare it with the prior one. Record the reason for changes, then rerun with the new expected hashes. Repeated equal reads are an observation, not dispatch exclusion. |
| strict-reconciliation-unresolved | Existing strict checkpoint reconciliation still refuses: resolve inventory issues first, then use the existing preview on the same frozen inputs to inspect remaining state, successor-state, index, or approval/vault discrepancies. No errors are auto-collapsed into success. |
| historical-outcome-unresolved | Verify uncertain email lifecycle and append outcomes against durable approval and vault-effect evidence; explicitly decide their historical disposition. |
| activation-evidence-required | Separately obtain reviewed dispatch-exclusion, account-binding, complete source-reconciliation, and live continuity evidence. This report cannot activate a connector. |

The report deliberately leaves the activation inventory unchanged. It detects
changes between observations, but cannot prove that a live tree stayed frozen
between reads. Supply frozen inputs for reproducible evidence. No report output
or owner decision silently deletes, deduplicates, rewrites, or retries history.
