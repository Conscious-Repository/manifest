# deploy/

The metis deployment as repo artifacts (big-change Phase 3a + auto-deploy).

- `manifest.service` · `manifest-sync.service` · `excalibur-engine.service` —
  the systemd units; `engine-room.target` + `private-ready.path` self-start
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
