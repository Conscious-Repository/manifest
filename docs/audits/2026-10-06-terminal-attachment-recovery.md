# Hermes terminal attachment recovery

The reported terminal failure was a stale Hermes attachment lease. The WebSocket reached the running `herdr` daemon, but `herdr terminal attach` refused the second client with `terminal ... already has an attached client; retry with --takeover`. A prior attach child was still alive under Manifest, so the UI remained disconnected while retrying the same exclusive attach.

Recovery retries now mark the WebSocket query with `takeover=1`. The server accepts that marker only for a recovery attempt and, for the Hermes runtime, starts `herdr terminal attach --takeover`. First attachments remain exclusive. Non-Hermes runtimes ignore takeover and keep their existing attach behavior.

Validation:

- The screenshot and live process inspection identified the exact daemon error and an orphaned `herdr ... terminal attach` child.
- Focused terminal event, mapping, frontend, and recovery tests pass.
- The browser recovery fixture passes restored inventory, explicit reconnect, outage recovery, layout bounds, and zero page errors.
- Server package tests pass; build, JavaScript syntax, and diff checks pass.
- The deployed executable matches the release, service is active, root returns HTTP 200, and read-only production Chromium confirms the served terminal recovery asset with no page errors.

No terminal input, run submission, or external action was sent during production verification.
