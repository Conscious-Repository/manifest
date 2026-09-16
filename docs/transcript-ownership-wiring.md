# Transcript ownership projection

The dashboard and `manifest-transcripts.service` have separate configurations.
The deployed dashboard's `config.json` has no `transcriptSync` section, while
`manifest-transcripts.service` reads `<dataDir>/transcript-worker.json`. Building
an ownership service only from the dashboard config therefore produces an empty
legacy root/account binding, even when the worker is active.

At dashboard startup, ownership wiring reads `transcript-worker.json` from the
dashboard's data directory when present. It builds a guarded service with that
worker configuration solely for the Excalibur spirits projection. It does not
start this service, attach it to sync HTTP routes, load credentials, open the
worker index, or grant proposal/vault writes. The dashboard's existing local
sync service and its enablement settings remain independent.

The worker data directory must exactly match the dashboard's, its legacy root
must be absolute and match the projected store, and enabled worker sources must
have named accounts and `continuityOnly=true`. Invalid/unreadable worker config
leaves the projection without a service (blocked); an absent worker config uses
the existing local guarded service. Config is a startup snapshot, so worker
configuration changes require a dashboard restart to update the projection.
Deployments using a different worker config location must explicitly adapt this
wiring; this is not systemd process discovery or proof that the worker is running.

Ownership still validates the current duty fence, handoff, checkpoint and account
binding. The service's checkpoint directory must also match the handoff data
directory. Disabled sources cannot project as successor-enabled. Historical
success timestamps describe recorded results only, not present liveness, semantic
parity, or permission to decommission Excalibur.

Read-only verification on 2026-09-16 with the new wiring accepted the deployed
Granola and Pocket evidence: both projected `successor-enabled`, with recorded
`last-success` and disabled legacy schedules. No sync or extraction ran. The
running dashboard still needs deployment/restart with the new code to replace
its old mismatched-service projection; no service or worker configuration was
changed during this fix. Full proposal/extraction parity remains unverified by
this wiring change.
