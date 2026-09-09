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
