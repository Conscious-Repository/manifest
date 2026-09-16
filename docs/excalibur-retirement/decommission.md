# Excalibur decommission

Excalibur is deprecated as an active engine. **Retired means engine unavailable**,
not extraction complete. Manifest owns the existing email, Granola and Pocket
lanes. `extractor/aion`, `extractor/ooda-email` and `extractor/real-estate` are
paused/unavailable capabilities, with no successor parity or migration claim.
Semantic parity and the existing application safety holds remain future capability
gaps. They are not prerequisites for the truthful paused end state.

The code change does not stop the live engine, alter the harness, replay requests,
change approvals or write the vault. Manifest refuses new legacy extractor
requests and resume edits. Existing artifacts, queues, cursors and uncertain
history remain in place. No evaluator/reducer or alternative scheduler is added.

## Inspect and prepare (separate operational step)

Run as `benjamin` on metis, from `/home/benjamin/src/manifest`:

```sh
go build -o /tmp/excalibur-decommission ./cmd/excalibur-decommission
/tmp/excalibur-decommission -mode plan \
  -harness /private/harnesses/excalibur \
  -data-dir /home/benjamin/.config/manifest \
  -receipt /tmp/excalibur-before-pause.json
sha256sum /tmp/excalibur-before-pause.json
```

A blocked plan is still a valid inventory receipt and exits nonzero. Read its
`blockers`. It contains per-file content hashes, hashed relative names, exact
file/byte counts, service definition/binary/consumer hashes and the explicit preserved,
unreplayed disposition. Names, requests, account identifiers and content are not
printed. Inventory includes all legacy artifacts/state/spool and both Manifest
extractor dispatch cursors, including unfinished history. Symlinks, unreadable
state, malformed or unaccounted queued consumers, active/unknown run or chat
status, unmanaged engine processes and uncertain ownership refuse.
No missing state is interpreted as successful extraction.

For the inspected deployment (`e6fb1dc`), all three extractor fences were revision
zero, all three rituals lacked `enabled: false`, and the spool was empty. The
installed binary reports that revision, including shared extractor dispatch
fencing. If that has changed, inspect the new fence histories and deployed engine
before proceeding. Do not replace an installed binary with an older unfenced one.
The following operations use that initial receipt as explicit pause evidence;
revision zero is a compare-and-swap, never a force reset:

```sh
pause_hash=$(sha256sum /tmp/excalibur-before-pause.json | cut -d' ' -f1)
for duty in extractor/aion extractor/ooda-email extractor/real-estate; do
  /home/benjamin/.local/bin/excalibur-engine dispatch-owner \
    -root /private/harnesses/excalibur "$duty" 0 excalibur blocked "$pause_hash" || exit 1
done
python3 - <<'PY'
from pathlib import Path
for ritual in ('aion', 'ooda-email', 'real-estate'):
    path = Path('/private/harnesses/excalibur/spirits/extractor/rituals') / (ritual + '.md')
    text = path.read_text()
    assert text.startswith('---\n')
    head, body = text[4:].split('\n---', 1)
    lines = [line for line in head.splitlines()
             if not line.startswith(('enabled:', 'paused_reason:'))]
    lines += ['enabled: false', 'paused_reason: Capability unavailable; legacy history preserved; no replay or migration claim.']
    updated = '---\n' + '\n'.join(lines) + '\n---' + body
    if updated != text:
        path.write_text(updated)
PY
```

If partially paused, inspect each latest revision/owner and skip already-blocked
lanes; never roll fences back or discard queues to make a plan pass. Fence locks
serialize against active dispatch. Existing blocked requests remain quarantined
in place. An unknown/chat queue requires a separate, explicit disposition; the
command does not silently classify it as extractor work.

## Final plan and explicit apply

```sh
/tmp/excalibur-decommission -mode plan \
  -harness /private/harnesses/excalibur \
  -data-dir /home/benjamin/.config/manifest \
  -receipt /tmp/excalibur-final-plan.json
cat /tmp/excalibur-final-plan.json
sha256sum /tmp/excalibur-final-plan.json
```

Review the receipt, including all counts and paused capabilities. Apply only with
zero blockers. Use a fresh filename if state changes; a receipt is immutable.
The only remaining user decision is **when to retire engine availability while
accepting those three paused capabilities**. No semantic-parity approval or
migration claim is required. Apply is never called automatically:

```sh
plan_hash=$(sha256sum /tmp/excalibur-final-plan.json | cut -d' ' -f1)
/tmp/excalibur-decommission -mode apply \
  -harness /private/harnesses/excalibur \
  -data-dir /home/benjamin/.config/manifest \
  -receipt /tmp/excalibur-final-plan.json -plan-sha256 "$plan_hash"
```

Apply needs existing sudo authorization for system service operations; it uses
`sudo -n` and never prompts or approves itself. It holds all six existing fence
locks, rechecks the exact plan, persists intent, disables/stops the service,
preserves `/etc/systemd/system/excalibur-engine.service` as
`excalibur-engine.service.pre-decommission`, and installs a persistent `/dev/null`
mask. It verifies inactive+masked, successor ownership/services and unchanged
inventoried data before publishing
`~/.config/manifest/excalibur-decommission/retired.json`. The original plan and
intent are retained beside it. Repeating a successful apply with the same receipt
is a no-op while bound state is unchanged; different receipts or later state drift
refuse. Live successor polls/heartbeats may make a
plan stale: regenerate and review, never force past a drift refusal.

A failed or interrupted stop leaves evidence and may leave the service stopped;
it never automatically restarts or falsely marks retirement. Inspect systemd and
the saved unit. If already masked and data changed during stop, a new clean plan
can finish the receipt. If interrupted between moving the unit and masking it,
finish the `/dev/null` symlink and `daemon-reload` explicitly, then plan again.
Never remove locks, queues, state or approvals. The masked engine cannot be revived
by ordinary `start`, autodeploy or `units-deploy`. Existing target Wants are benign
under the mask; the repository target no longer includes the engine.

## Restore engine availability only

Restoration preserves all migrated connector fences, blocked extractor fences,
disabled rituals and every byte of queued/state data. It does not replay, enable
extraction, change approvals, start a worker or imply parity. First inspect the
retained original plan/unit and confirm the three extractor owners remain blocked.
Then, for a successfully retired engine:

```sh
set -e
test -f /etc/systemd/system/excalibur-engine.service.pre-decommission
test ! -e /home/benjamin/.config/manifest/excalibur-decommission/retired.restored.json
test "$(readlink /etc/systemd/system/excalibur-engine.service)" = /dev/null
sudo mv /etc/systemd/system/excalibur-engine.service.pre-decommission \
  /etc/systemd/system/excalibur-engine.service
sudo systemctl daemon-reload
mv /home/benjamin/.config/manifest/excalibur-decommission/retired.json \
  /home/benjamin/.config/manifest/excalibur-decommission/retired.restored.json
systemctl show excalibur-engine -p ActiveState -p UnitFileState
```

Stop if any command fails. Retain `retired.restored.json`, the plan and intent as
history (choose a new archival filename if already present). Availability is
restored, but the service stays disabled/inactive and extractor capabilities stay
paused. Starting any legacy runtime is a separate operational action; none is
needed for the three migrated connectors. Do not publish rollback fence revisions,
restore old queues into new workers, or weaken the extraction application holds.
