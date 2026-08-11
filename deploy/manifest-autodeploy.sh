#!/usr/bin/env bash
# Auto-deploy on metis (owner decision 2026-08-11): the tailnet dashboard is
# the live testing surface, so it tracks origin/main hands-free — push = deploy.
# Runs every minute from manifest-autodeploy.timer:
#   - manifest repo: fetch; if origin/main moved → pull --ff-only, rebuild the
#     dashboard + the sync daemon, restart manifest.service.
#   - harness repo: if excalibur/engine/ changed since the last engine build →
#     rebuild the engine, restart excalibur-engine.service.
# ff-only means a diverged repo (local commits on the box) stops auto-deploy
# loudly in the journal rather than merging anything on its own.
set -euo pipefail
export PATH=/home/benjamin/.local/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
STAMPS=/home/benjamin/.config/manifest/autodeploy
mkdir -p "$STAMPS"

# ---- manifest repo → dashboard + sync daemon ----
cd /home/benjamin/src/manifest
git fetch -q origin main
LOCAL=$(git rev-parse HEAD)
REMOTE=$(git rev-parse origin/main)
if [ "$LOCAL" != "$REMOTE" ]; then
  git pull --ff-only -q
  go build -o manifest .
  go build -o /home/benjamin/.local/bin/manifest-sync ./cmd/manifest-sync
  sudo systemctl restart manifest
  echo "autodeploy: manifest $LOCAL -> $(git rev-parse --short HEAD), restarted"
fi

# ---- harness repo → engine (source syncs in via manifest-sync) ----
if [ -d /private/harnesses/excalibur/engine ]; then
  cd /private/harnesses
  ENG=$(git log -1 --format=%H -- excalibur/engine 2>/dev/null || echo none)
  LAST=$(cat "$STAMPS/engine.built" 2>/dev/null || echo unbuilt)
  if [ "$ENG" != "$LAST" ] && [ "$ENG" != "none" ]; then
    (cd excalibur/engine && go build -o /home/benjamin/.local/bin/excalibur-engine ./cmd/excalibur)
    sudo systemctl restart excalibur-engine
    echo "$ENG" > "$STAMPS/engine.built"
    echo "autodeploy: engine rebuilt at $ENG, restarted"
  fi
fi
