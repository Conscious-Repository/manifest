# Granola and Pocket cutover — 2026-09-16

Granola and Pocket now have immutable Excalibur fence revision **1**, owner
`manifest`, and Manifest `successor-enabled` records. The live
`manifest-transcripts.service` runs the existing successor cadence in
**read-only continuity mode**. It creates no proposals and never applies vault
writes. This is not full end-to-end approval verification: neither connector is
marked `verified`. Email remains Excalibur-owned; its source-ID mismatch is not
changed or bypassed.

The dashboard was not restarted or reconfigured. The narrow worker allows the
cutover to run without enabling unrelated dashboard writers. Its systemd sandbox
makes the entire vault and harness artifact tree read-only. Only Manifest dataDir
and the two existing fence directories are writable. It uses the live index in
SQLite read-only mode and the existing credential files, without copying secrets.
The worker is intentionally restricted to `continuityOnly=true`; it refuses
startup if configured for proposal generation. Normal proposal-only polling is
still available through the dashboard's existing transcript service after a
separate deployment/configuration change. It has no automatic approval path.

## Ownership and evidence

| Source | Account binding | Items | Reconciled uncertain, replay=false | Fence revision |
| --- | --- | ---: | ---: | ---: |
| Granola | personal-granola | 78 | 1 | 1 |
| Pocket | personal-pocket | 13 | 1 | 1 |

Staged checkpoint hashes:

- Granola: `0f22e6b15d96fd65fce023928fe6e76b7cb0095e75cc128b9dfd7194d3f07e19`
- Pocket: `0a7f032606b21f429525a03a26a7eee77ec618c943814f41e900351fb6756be6`

Prepared plan / dispatch evidence hashes:

- Granola: `7e3136310ea58f83141887da4f1c2fa5afd179c3c523ccbf92578172e64be249`
- Pocket: `5096cf2394615d83bc19b0cbb01f9c73d105cd90984987feca15714193ef6871`

Plans are preserved under dataDir `connector-handoff/cutover-receipts/`.
Active cursors are under `transcript-sync/<source>/state.json`; activation records
are `connector-handoff/<source>.json`. The harness's append-only transfer records
are `vessel/state/dispatch-fence/ea-coordinator/<source>-sync/00000000000000000001.json`.
These are the existing retirement/dispatch exclusion mechanism; ritual files and
all legacy history remain intact. Disabling the worker never surrenders ownership.

`connectorhandoff/fence.go` mirrors the strict record reader and lock contract from
harness commit `f9c1ea1`. It does not publish ownership. The deployed harness CLI
controls publication through its existing local OS-account and private-state
permissions, fixed positional arguments and compare-and-swap contract. Both
successor effects and legacy runs hold the same lock inode for their entire run;
an explicit rollback cannot overlap successor polling.

Prepare validates the explicit account, immutable checkpoint hash, current legacy
watermark, complete scoped approval hash, source-index outcomes and owner receipt.
Apply requires the exact prepared hash in the next ownership revision, repeats
reconciliation under the fence, refuses existing cursors, then publishes the
staged state before its activation record. Any failure leaves legacy fenced;
there is no automatic rollback, fallback or replay. Interrupted publication must
be inspected, not blindly retried. Approved no-note exceptions remain terminal
only with the exact unchanged owner receipt; new note evidence requires review.

## Exact execution commands

The following records the commands executed, with shell variables for readability.
They are **not** a rerunnable migration: the revision-zero transfers and applies
will now refuse because the ownership and active cursors already exist.

```bash
cd /home/benjamin/src/manifest
go build -o /tmp/manifest-transcript-cutover ./cmd/transcript-cutover
go build -o /tmp/manifest-transcript-worker ./cmd/transcript-worker
root=/private/harnesses/excalibur
data=/home/benjamin/.config/manifest
index=/tmp/manifest-cutover-index.db
granola_stage=0f22e6b15d96fd65fce023928fe6e76b7cb0095e75cc128b9dfd7194d3f07e19
pocket_stage=0a7f032606b21f429525a03a26a7eee77ec618c943814f41e900351fb6756be6
granola_plan=7e3136310ea58f83141887da4f1c2fa5afd179c3c523ccbf92578172e64be249
pocket_plan=5096cf2394615d83bc19b0cbb01f9c73d105cd90984987feca15714193ef6871
```

A current consistent snapshot was made with SQLite's backup API before prepare,
and refreshed after fencing before apply:

```bash
python3 - <<'PY'
import sqlite3
src = sqlite3.connect('file:/home/benjamin/.config/manifest/index.db?mode=ro', uri=True)
dst = sqlite3.connect('/tmp/manifest-cutover-index.db')
src.backup(dst)
dst.close()
src.close()
PY
```

For each source, prepare was run with its explicit account and staged hash; the
same arguments plus `-preflight -key-file` performed upstream GET-only checks.
The prepared JSON was retained in dataDir before transferring ownership.

```bash
/tmp/manifest-transcript-cutover -source granola -legacy-root "$root" \
  -data-dir "$data" -account personal-granola -staged-hash "$granola_stage" \
  -index-snapshot "$index"
/tmp/manifest-transcript-cutover -source pocket -legacy-root "$root" \
  -data-dir "$data" -account personal-pocket -staged-hash "$pocket_stage" \
  -index-snapshot "$index"

/home/benjamin/.local/bin/excalibur-engine dispatch-owner -root "$root" \
  ea-coordinator/granola-sync 0 excalibur manifest "$granola_plan"
/home/benjamin/.local/bin/excalibur-engine dispatch-owner -root "$root" \
  ea-coordinator/pocket-sync 0 excalibur manifest "$pocket_plan"

# Refresh the index snapshot using the backup command above, then:
/tmp/manifest-transcript-cutover -source granola -legacy-root "$root" \
  -data-dir "$data" -account personal-granola -staged-hash "$granola_stage" \
  -index-snapshot "$index" -apply -revision 0 -expect-plan-hash "$granola_plan"
/tmp/manifest-transcript-cutover -source pocket -legacy-root "$root" \
  -data-dir "$data" -account personal-pocket -staged-hash "$pocket_stage" \
  -index-snapshot "$index" -apply -revision 0 -expect-plan-hash "$pocket_plan"

# Both returned exit 1 with owner=manifest refusal before a legacy run:
/home/benjamin/.local/bin/excalibur-engine run -root "$root" ea-coordinator granola-sync
/home/benjamin/.local/bin/excalibur-engine run -root "$root" ea-coordinator pocket-sync

# Read-only verification; safe to repeat with a fresh consistent snapshot:
for source in granola pocket; do
  /tmp/manifest-transcript-cutover -source "$source" -legacy-root "$root" \
    -data-dir "$data" -account "personal-$source" -index-snapshot "$index" \
    -verify -key-file "/home/benjamin/.config/excalibur/${source}_key"
done
```

Worker configuration at `/home/benjamin/.config/manifest/transcript-worker.json`
(mode 0600):

```json
{
  "dataDir": "/home/benjamin/.config/manifest",
  "index": "/home/benjamin/.config/manifest/index.db",
  "transcriptSync": {
    "legacyRoot": "/private/harnesses/excalibur",
    "granola": {"enabled": true, "continuityOnly": true, "account": "personal-granola"},
    "pocket": {"enabled": true, "continuityOnly": true, "account": "personal-pocket"}
  },
  "keyFiles": {
    "granola": "/home/benjamin/.config/excalibur/granola_key",
    "pocket": "/home/benjamin/.config/excalibur/pocket_key"
  }
}
```

```bash
install -m 0755 /tmp/manifest-transcript-worker /home/benjamin/.local/bin/manifest-transcript-worker
sudo -n install -m 0644 deploy/manifest-transcripts.service /etc/systemd/system/manifest-transcripts.service
sudo -n systemctl daemon-reload
sudo -n systemctl enable --now manifest-transcripts.service
sudo -n systemctl restart manifest-transcripts.service
sudo -n systemctl is-active manifest-transcripts.service
```

## Observed results and boundaries

- First scheduled continuity checks succeeded at 18:08 UTC. Granola listed 1
  known item, 0 new; Pocket listed 6 items, 3 known and 3 new. Both filed 0.
  New Pocket items remain waiting; they were not replayed or turned into proposals.
- Watermarks stayed `2026-09-13T21:13:24Z` (Granola) and
  `2026-09-02T21:33:36Z` (Pocket); the complete staged item maps remained intact.
- All 848 baseline approval/ritual/Email-state files remained byte-identical.
  Email has no fence record. No ritual or historical artifact was deleted.
- The worker was restarted successfully; state file hashes were unchanged across
  restart. Repeated read-only checks returned the same counts.
- `gofmt`, full `go test ./...`, `go build ./...`, `go vet ./...`, full
  `go test -race ./...`, `git diff --check`, and systemd unit validation passed.
  Tests cover missing/wrong fence, malformed histories, lock exclusion, rollback,
  checkpoint/account/approval drift, no-replay receipts, scoped Email isolation
  with global filename collision protection, and read-only scheduler continuity.

This proves ownership exclusion and read-only overlap continuity, not complete
upstream historical coverage or a new approval/vaultwriter cycle. Those broader
claims are intentionally not encoded as `verified` evidence.

Rollback is explicit: stop the worker, reconcile current successor outcomes and
legacy watermark under the same fence, retain every receipt/cursor/history file,
then use `dispatch-owner` with current revision 1, from `manifest`, to `excalibur`,
and a **new reviewed rollback evidence hash**. Never delete the fence or reuse the
transfer receipt as rollback evidence. The successor refuses after rollback;
stale legacy manual requests remain fenced until explicitly reissued at the new
revision. A later proposal-runtime deployment must stop this continuity worker
before taking over the same duties.
