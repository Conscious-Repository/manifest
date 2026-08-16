#!/usr/bin/env bash
# kairos-deploy.sh — build + ship the kairos-runner to lab-apps (kairos plan).
# Run from a manifest checkout on a box with ssh access to lab-apps (metis or
# the mac). Manifest's autodeploy does not reach lab-apps; this script IS the
# deploy step for the runner.
set -euo pipefail
HOST="${1:-benjamin@lab-apps}"
cd "$(dirname "$0")/.."
echo "building kairos-runner (linux/amd64)…"
GOOS=linux GOARCH=amd64 go build -o /tmp/kairos-runner ./cmd/kairos-runner
echo "shipping to $HOST…"
scp /tmp/kairos-runner "$HOST":/tmp/kairos-runner
ssh "$HOST" 'sudo install -o kairos -g kairos -m 0755 /tmp/kairos-runner /home/kairos/.local/bin/kairos-runner && rm /tmp/kairos-runner && (sudo systemctl restart kairos-runner 2>/dev/null || true) && echo deployed'
