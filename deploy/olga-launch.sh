#!/bin/sh
# Starts Olga's Manifest. After Liber ships a change (the app installs the new
# build, writes "pending" and exits 75 so systemd restarts it), this proves the
# new build answers within 20 s; if it doesn't, the previous build goes back.
# The verdict is written to "settled" ("<change> live|failed") for Liber to
# report in the conversation.
#   OLGA_RUNTIME  directory with olga, olga.prev, pending, settled
#   OLGA_HEALTH   URL that must answer (default http://127.0.0.1:7781/)
D="${OLGA_RUNTIME:-$HOME/.local/share/olga}"
BIN="$D/olga"
[ -x "$BIN" ] || BIN="$HOME/.local/bin/olga"
HEALTH="${OLGA_HEALTH:-http://127.0.0.1:7781/}"

if [ -f "$D/pending" ]; then
  change=$(head -n1 "$D/pending")
  rm -f "$D/pending"
  "$BIN" "$@" &
  pid=$!
  trap 'kill $pid 2>/dev/null' TERM INT
  ok=0
  i=0
  while [ $i -lt 20 ]; do
    sleep 1
    i=$((i + 1))
    kill -0 "$pid" 2>/dev/null || break
    if curl -fsS -o /dev/null -m 2 "$HEALTH" 2>/dev/null; then ok=1; break; fi
  done
  if [ "$ok" = 1 ]; then
    echo "$change live" > "$D/settled"
    wait "$pid"
    exit $?
  fi
  kill "$pid" 2>/dev/null
  wait "$pid" 2>/dev/null
  if [ -x "$D/olga.prev" ]; then
    cp "$D/olga.prev" "$D/olga"
    BIN="$D/olga"
  fi
  echo "$change failed" > "$D/settled"
fi
exec "$BIN" "$@"
