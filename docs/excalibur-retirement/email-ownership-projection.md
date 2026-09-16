# Email ownership projection

The 2026-09-16 discrepancy was a missing projection source, not a mismatch
between the live fence and activation receipt. The old spirits projection read
`<dataDir>/connector-handoff/email.json`, which is absent. The personal-email
worker does not publish that connector label.

Read-only inspection found the actual evidence at:

- `/private/harnesses/excalibur/vessel/state/dispatch-fence/ea-coordinator/email-sync/00000000000000000001.json`
- `/home/benjamin/.config/manifest/personal-email/active.json`
- `/home/benjamin/.config/manifest/personal-email-worker.json`

The fence owner is `manifest`, revision 1. Its `evidence_sha256` and the active
state's `planHash` both equal
`0a52f420e86714f2296953b92ee1f6a5551ffcbba3bdcf1d2474ba76bfc3d068`.
The worker config is enabled and points to those same harness/data directories.

`personalemail.OwnershipSnapshot` validates the fence history, reads the worker
config at the deployment's `<dataDir>/personal-email-worker.json` path, and reuses
the worker's activation/receipt/config validation, local mailbox binding checks,
and ownership matcher. It does not acquire a dispatch lock, refresh tokens,
poll mail, extract, approve, or write operational state. Deployments using a
worker config elsewhere remain blocked until that configuration path is made
explicitly available to the projection.

`spirits.projectEmailOwnership` uses that evidence instead of a missing connector
label. Missing, unreadable, or contradictory evidence yields `blocked`, retains
`excalibur` as the displayed owner, and does not advertise legacy actionability.
An existing contradictory connector label also blocks the projection. The
legacy dispatch guard independently consults the fence, including when no
migration data directory is configured; an absent fence retains its existing
legacy-dispatch semantics. No dispatch fence was weakened.

The independent projected dimensions are:

- `legacyEnabled`: the ritual file's schedule flag;
- `fenceProtected`: a valid explicit fence excludes the legacy owner;
- `successorEnabled`: a validated activation with worker configuration enabled;
- `migrationState`: `fence-protected` with the worker disabled, or
  `successor-enabled` with it enabled; `blocked` when evidence cannot be verified.

The disabled ritual file and
`/private/harnesses/excalibur/vessel/state/ritual-status.json` still describe the
legacy scheduler. Its paused observation is not successor health. Worker
enablement is not a liveness claim, semantic parity, or final decommission.

The updated code projected the live files as owner `manifest`,
`legacyEnabled=false`, `legacyActionable=false`, `fenceProtected=true`,
`successorEnabled=true`, and `migrationState=successor-enabled`. This was a
read-only invocation, not a deployment. The running API will need the updated
binary before its projection changes. Ownership records, ritual files, worker
config, and vault were untouched.
