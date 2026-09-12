# deploy/

The metis deployment as repo artifacts (big-change Phase 3a + auto-deploy).

- `manifest.service` · `manifest-sync.service` · `excalibur-engine.service` —
  the systemd units; `engine-room.target` + `private-ready.path` + `private-unlock` self-start
  the room after reboot + `private-unlock`.
- `manifest-autodeploy.{sh,service,timer}` — **push = deploy**: every minute
  metis pulls origin/main (ff-only), rebuilds what moved (dashboard, sync
  daemon, engine), restarts. The tailnet dashboard
  (https://metis.tail8f89de.ts.net) never lags the repo by more than ~90s.
- `com.benjamin.manifest-sync.plist` — the laptop sync daemon (launchd).
- `com.benjamin.excalibur.plist.laptop-fallback` — the retired laptop engine
  job, kept for dev/fallback re-install.
- `make deploy` / `engine-deploy` / `units-deploy` — immediate operator runs
  (unit-file changes always go through `units-deploy`; the timer only
  rebuilds binaries).

The writing vault uses a 2-second change debounce and a 5-second pull interval
on both machines, configured with per-root overrides in the two sync units.
Harness roots retain the default 15-second debounce and 60-second interval.
After changing sync flags, install the updated units and restart `manifest-sync`
on both machines; rebuilding the binary alone does not reload a running daemon.
On macOS, replacing an ad-hoc-signed executable can leave its existing Documents
permission stale. If file access stalls after an upgrade, refresh the existing
`manifest-sync` Documents Folder toggle in Privacy & Security → Files & Folders,
then restart the launch agent. Watch registration cannot block interval sync.

## Excalibur successor Phase 1 (2026-09-12; not deployed)

`hermes-gateway.service` is the exact installed system unit, with the exact
absolute target of `engine-room.target.wants/hermes-gateway.service` recorded
beside it. `units-deploy` includes both representations. The target's declared
Wants are manifest, manifest-sync, excalibur-engine, and zeck-runner; installed
wants-directory links additionally include hermes-gateway and olga. This phase
does not change that effective membership or manage olga.

D6 approves America/Chicago for Hermes schedules. The installed gateway unit
has no TZ. To preserve the requested exact installed representation and a
no-op unit-content comparison, the TZ mutation remains a deployment follow-up:
add `Environment=TZ=America/Chicago` under `[Service]`, then deploy and restart
only in the separately authorized deployment phase. Manifest's Excalibur
schedule projection explicitly uses America/Chicago now, independent of host TZ.

D7: a duplicate user `hermes-gateway.service` remains on disk. Removal is an
owner decision; no user unit was removed, stopped, or changed. Starts-log churn
has not been attributed to it by this implementation. D9's governance gap is
accepted: liveness signals do not prove policy compliance.

The successor has two schedulers: Manifest pollers and the supervised Hermes
ticker. No third scheduler is added. Excalibur retains every current duty until
its own approved cutover phase. Hermes cron currently merges enabled MCP
servers into per-job toolsets and falls back to the full default toolset on
resolution failure; migrated duties must not use it. The one-shot path also
loads configured fallback models, starts MCP discovery, and does not pass an
invocation step cap. Consequently the new migrated-duty authority guard refuses
launches until a bounded, isolated invocation is implemented and verified.
The existing zeck unit's ProtectSystem=strict / InaccessiblePaths pattern is
the filesystem precedent; an asserted config boolean is not isolation evidence.

Corrective pass: `hermes/successor.py` now provides a separate, tool-free local
DeepSeek completion path, using Linux Landlock and seccomp process restrictions
before inference. It permits only `deepseek-local/deepseek-v4.1-flash`, explicit
`tools: ["none"]`, `mcp: "no_mcp"`, one completion, a parent-enforced timeout,
and verified reported cost exactly zero. Other scopes still refuse. This helper
has deterministic isolation/usage tests; no duty is routed and the live endpoint's
required provider/cost report contract remains unverified. It does not change
Hermes runtime files or require installing another unit. The legacy one-shot
limitations above still apply to `hermes -z`.
