# Olga Manifest

`manifest.olgasobkiv.com` → existing metis Cloudflare tunnel → `127.0.0.1:7781`.

The standalone `cmd/olga` binary uses Manifest's DAY, GOALS and TASKS handlers
and the same view modules and styles. The small Olga shell replaces the main
boot/navigation and task conversation inspector. No agents, chat, calendar
connections, feed, external projects, file browser, or owner services are wired.
Her goals are entered directly in GOALS; no import step is needed.

## Source of truth

- Laptop: `/Users/benjamin/Documents/index.ben/system/olga/`
- Metis: `/private/consciousrepo/system/olga/`
- `goals.md`, `tasks.md`, `daily/YYYY-MM-DD.md`, and goal/task archive Markdown.
- Existing vault sync carries these files between machines. Edit through the
  metis app for multi-device use; the laptop is not the public origin.
- All application writes use the `olga` vaultwriter capability, restricted to
  `system/olga/**`, with audit in `/private/olga/write-audit.log`.
- Olga is an explicitly authorized additional vault editor within this one
  subtree. Her handlers never receive the owner's stores or integrations.

## Install / update

Build `go build -o olga ./cmd/olga` on Linux, install as
`/home/benjamin/.local/bin/olga`, install `deploy/olga.service` into
`/etc/systemd/system/`, then `sudo systemctl daemon-reload` and
`sudo systemctl enable --now olga`. Subsequent updates replace that binary
and restart only `olga`. It is enabled under `engine-room.target`, so the
existing private-vault unlock flow starts it after a reboot. The main Manifest autodeployer does not deploy it.

Keep `/private/olga` mode 0700 owned by benjamin, outside the synced vault.
Run `python3 deploy/olga-password.py` on the laptop to set the password in
`/private/olga/password` (0600) without putting it in shell history. A missing/empty password
returns 503 and exposes no planner data. Password changes apply on the next
request and invalidate all existing sessions. Sessions also expire on service
restart, last at most 30 days, and use HttpOnly/SameSite cookies (Secure over
HTTPS). The public URL must use HTTPS. Five failed sign-ins trigger a 30-second
cooldown. Request bodies are bounded and cross-origin writes rejected.

## Cloudflare

Add a proxied DNS CNAME in the `olgasobkiv.com` zone:

- Name: `manifest`
- Target: `741774b4-e834-4786-b4c9-4ab39363f669.cfargotunnel.com`

The tunnel's ingress must also map `manifest.olgasobkiv.com` to
`http://127.0.0.1:7781`, ahead of the catch-all rule. Preserve the Aion, OODA and
RSS ingress entries. For a remotely managed tunnel, set this in its published
application routes; a local config edit alone is insufficient.

Check `systemctl status olga`, `curl -i http://127.0.0.1:7781/`, then the public
HTTPS URL. Before password setup, 503 with the preparation message is expected.
After setup, verify login, goal creation, day scheduling and task completion.

## Validation

`go test ./server -run TestOlga -count=1` verifies auth, owner-data isolation,
endpoint exclusion, goal/task creation, daily scheduling, task completion
writeback, cross-origin rejection and password rotation. The UI is smoke-tested
against a disposable vault, never by adding sample goals to Olga's real files.

## Installation status (2026-09-11)

Installed and enabled `olga.service` on metis; the origin responds on 7781.
The user has configured the password using `deploy/olga-password.py`.
The live Cloudflare tunnel is remotely managed. The Olga published application
route and its automatic DNS record were added through the authenticated Chrome
session; metis received configuration version 7. Public HTTPS sign-in is live. Final verification passed for password login,
Secure session cookies, all three authenticated planner APIs, excluded AI/chat/file
routes, and sign-out. The verification did not add data to Olga’s real vault.

The isolated Linux release was built from the Manifest HEAD plus only the Olga
files, staged at `/tmp/olga-release-20260911`; unrelated local changes to
`server/chat_shared_plans.go` were not included. Planner/auth tests pass on both
macOS and Linux. The broader local server run has two failures in existing
shared-plan tests associated with that unrelated working-tree change.

Browser validation covered desktop and 390px phone navigation, creating an
area/goal/milestone/task, task editing, choosing daily focus, schedule persistence
across dates, and daily completion reflected in Tasks.

## Shared Home and task details (2026-09-11)

Olga keeps the white/ochre palette. TASKS offers List and Board (Open/Done),
search, priority, descriptions, and timestamped human comments. The board uses
same task records as the list; completion/reopening also works without dragging.

`system/home/goals.md` and `system/home/tasks.md` are the canonical Home sections
projected into both planners. Other areas and daily schedules remain private.
`cmd/share-home` migrates the owner's existing Home sections, retains Home-only
backups in `system/home/import-backup`, and carries existing descriptions and
human comments forward. Stop both planner services while running the migration.
It refuses to overwrite an existing Olga Home with content or to strip a private
Home section changed since import. Both services must have write access to
`system/home`; Olga's capability is limited to that directory and `system/olga`.

Descriptions and comments are Markdown under each planner's `notes/<task-hash>`
folder; shared tasks use `system/home/notes`. Comments preserve author/time.
The main task inspector reads/writes those same Home descriptions and comments;
Olga never loads agent execution or chat machinery. Task identity is pinned before
adding details so title changes retain notes. Existing tasks cannot be moved
across the Home sharing boundary; create a task in Home to share it.

Home writes compare a load snapshot under an interprocess lock. Conflicting Home
edits fail with a reload message instead of silently replacing newer work.
Private-only changes preserve newer Home content. Descriptions also check their
revision; comments use separate immutable files. Shared completed tasks remain
visible to both people rather than being swept into one person's private archive.

Validation: goals/tasks/sharedhome package tests, server integration tests,
migration smoke test on a disposable vault, and Chrome desktop/mobile checks.
