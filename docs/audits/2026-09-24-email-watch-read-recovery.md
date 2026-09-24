# Reply watch read recovery — September 24

Each reply poll now persists a random claim under the existing operation lock.
Its result can update the watch only while that claim, schedule, enabled state
and successful delivery status still match. Toggling monitoring clears the claim.
Existing watches acquire one on their next poll without a migration.

Previously, the next-check timestamp alone identified a read. Stopping and
restarting monitoring could allow a replacement poll at the same tick; a late
response from the old poll could overwrite newer replies or add a stale error.
The persisted claim distinguishes those reads across adapter instances.

Validation:

- `go test -race ./manifestmcp -run TestEmailWatch -count=1` passed. The new
  test uses two adapters sharing persisted state, blocks the original read,
  stops/restarts monitoring, completes a newer read at the identical tick,
  then releases either stale replies or a stale provider error. Both preserve
  the newer result; stopping clears its claim.
- `make test` passed server (42.458s) and other packages except the existing
  `cmd/re-intake-canary/TestCanarySourceCallGraphIsolation` source-hash failure
  for unchanged Hermes files.
- Release build and `git diff --check` passed.

The five-minute cadence, mailbox/thread scope, reply bounds and send path are
unchanged. This fixes read-result recovery, not uncertain-delivery reconciliation.
Proactive reply notices, explicit monitoring end conditions and the remaining
workbench acceptance requirements remain open. Production verification is
limited to service health and the running release binary; it does not enable
monitoring, read a real mailbox or send mail.
