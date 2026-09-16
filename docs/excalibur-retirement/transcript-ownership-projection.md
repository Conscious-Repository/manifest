# Granola/Pocket ownership projection

The rituals API validates Granola and Pocket dimensions independently:

- `legacyEnabled` describes the legacy schedule file.
- `fenceProtected` comes from the validated duty fence history, independently
  of the generic connector handoff record. A surviving fence still protects
  legacy dispatch when successor evidence is missing or contradictory.
- `successorEnabled` requires the configured guarded transcript service, a
  matching harness/data directory, valid handoff phase, fence evidence,
  account binding and imported checkpoint, plus the source's enabled flag.
- `successorHealth`, `successorLastAttempt` and `successorLastSuccess` describe
  persisted transcript observations. Health is `unknown`, `error`, or
  `last-success`; the last value is historical evidence, not a liveness claim.
  Invalid continuity evidence leaves these fields absent.

A validated disabled successor projects `fence-protected`. An enabled one with
no successful latest observation also projects `fence-protected`; a recorded
error projects `blocked`. Only validated enablement plus a successful latest
observation projects `successor-enabled`. Missing or contradictory ownership
or continuity evidence projects `blocked`, even if a handoff label says
`successor-enabled` or `verified`. The fence dimension remains independent.

Continuity-only workers record source observations, not ingestion/extraction
completion. No configuration flag, last success timestamp or migration label
proves current worker liveness, semantic parity, or final decommission.
An enabled legacy schedule remains visible even when its dispatch is fenced.

Projection reads local state only. It does not acquire/create fence locks,
contact upstream APIs, transfer ownership, change schedules or worker configs,
stop an engine, or write to the vault. The shared configured service is exposed
to the projection even when both sources are disabled; its scheduler continues
to start workers only for enabled sources.
