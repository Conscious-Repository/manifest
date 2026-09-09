# herdr terminal operations

Manifest's local herdr adapter requires herdr 0.9.0, socket protocol 22. It reads
version metadata before allocating a pane and refuses unknown versions. The
server runs independently of Manifest; a browser or Manifest restart detaches
its client and does not own the daemon's lifetime.

On Linux, install `deploy/manifest-herdr.service` as a user service:

```sh
install -D -m 0644 deploy/manifest-herdr.service ~/.config/systemd/user/manifest-herdr.service
systemctl --user daemon-reload
systemctl --user enable --now manifest-herdr.service
```

The default named session is `manifest`. `MANIFEST_HERDR_SESSION` selects another
local named daemon. Its API socket is under
`${XDG_CONFIG_HOME:-$HOME/.config}/herdr/sessions/<session>/herdr.sock`; permissions
must exclude group/other access. There is no remote herdr transport in this pass.
Remote tmux Keep remains supported by the legacy path.

`terminals.json.pre-herdr-v1.bak` retains the original row file before the
idempotent metadata import. Missing backend means tmux. Do not delete the mapping:
it holds stable chat IDs, exact conversation IDs and board work-order links.
Labels are display metadata. A missing socket, changed daemon generation or
unresolved launch does not authorize automatic relaunch or prompt replay.

A process whose allocation/submission reply was lost can remain visible in
herdr without an associated Manifest identity. Inspect it explicitly; do not
adopt it by its title. Daemon restart/reboot process survival is not promised.
Result files and the existing reconciliation sweep remain authoritative.

Codex discovery validates session_meta and the requested conversation ID. For
new sessions on Linux, the exact pane's foreground Codex process supplies an
open rollout file descriptor; its inode, root, cwd and metadata must match.
If no exact identity is available, the screen remains available and transcript
identity stays unresolved. Never select a newest rollout by cwd.

Terminal's live inventory uses `/api/terminal/live`, not the conversation
registry. Unassociated panes carry an explicit `herdr:<base64url identity>`
handle; attach and close validate host, daemon generation and occupant. New local
shells use herdr too; confirmed-absent local shell mappings can be retired,
while conversation and board associations remain durable.

External agent callers can migrate explicitly:

```json
{"kind":"codex","backend":"herdr","cwd":"/path/to/checkout","model":"gpt-6-astra"}
```

POST that to `/api/terminal/agent-session`. Use the returned `handle`, or the
stable Manifest `id` with the input/transcript endpoints. Herdr responses omit
`tmux`; they never pretend that the handle is a tmux session name. Omitting
`backend` retains the old tmux behavior for compatibility callers.

Caller inventory at migration: Chats uses `/api/terminal/session` and stable IDs;
board uses `createBoardHerdrSession`; Terminal uses live inventory plus exact
handle/stable-ID attachment. The `/api/terminal/agent-session` compatibility
handler and local Manifest skill references still serve legacy tmux callers.
Rename/forget APIs still have Chat consumers. Remote Keep still needs the tmux
helpers on both hosts. Therefore those helpers/endpoints remain until their real
sessions and callers have drained; no live legacy session was killed to satisfy
cleanup. The removed Terminal history/pin/default-name UI is not the board ledger
or conversation history, both of which remain in their existing surfaces.

Local working directories are validated before saving a launch intent or a board
session marker. An invalid cwd returns 400 from session creation, explicit herdr
agent-session creation, or stopped-conversation resume. Blank uses the launcher's
default directory; `~` and `~/` use that same home/default. Other relative paths
are rejected. No shell expressions or environment variables are expanded.
Directories and symlinks to directories are accepted; the selected absolute path
spelling is preserved. Remote tmux/Keep and legacy agent-session tmux behavior
are unchanged. A rejected resume preserves the original conversation mapping.

The adapter checks again immediately before `workspace.create`. If the directory
disappeared and no allocation request was attempted, checked persistence removes
only a fresh ordinary intent or restores the prior conversation row. Board links
and markers remain for the existing board error flow. A failed rollback returns
500 with the retained row ID and original cause. Stat cannot guarantee a later
chdir: after a mutating request may have been sent, errors and lost replies retain
the conservative intent/allocated/submitted posture and return 502. Never replay
or allocate a replacement automatically.

DELETE has one explicit metadata-forget exception: a non-board herdr row with
`launchPhase=intent`, `Started=false`, and an entirely empty runtime identity.
It removes the row without inspecting or closing a runtime. This does **not**
claim a process was stopped: historical intents can also represent lost allocation
replies. Any unassociated pane must be inspected and closed separately using its
exact handle in Terminal's live inventory, never matched by label or cwd. `/kill`
remains strict. Board-linked rows remain protected with 409; partially populated
identities and later launch phases require the existing exact-identity close.
Launch, resume, and DELETE serialize on the same stable session ID so forgetting
cannot race an allocation into recreating its row. No startup/list cleanup is added.
