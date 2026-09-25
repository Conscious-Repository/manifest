# Ordered conversation polling

The native/planning conversation poller previously launched a read every interval even if its previous read was still pending. Slow overlapping reads could land out of order. A pending poll could also replace a newer snapshot rendered by explicit refresh.

Each poll timer now permits one in-flight read. A 15-second AbortController deadline bounds a stalled fetch/body read; completion, HTTP/network failure and timeout release the guard. Before applying the result, the poll verifies that the displayed signature has not changed since the request began, in addition to existing route/agent/conversation/visibility checks. A discarded response starts no mutation or navigation. Existing polling cadence and eager thread loading remain unchanged.

Evidence:
- `chat-poll-order.cjs` runs the production poll function with controlled asynchronous responses: repeated timer ticks cause one request; a newer explicit snapshot fences the old response; the next current response lands; network/HTTP failure and abort release polling; a late response cannot redirect a departed route; deadline cleanup is verified.
- `chat-load-race.cjs` and JavaScript syntax/diff checks pass.
- Full-frontend Chromium `chat-stage-switch-browser.cjs` passes actual polling, metadata refresh, composer text/focus/selection preservation, cached/eager thread switching and phone error navigation without page errors.
- Release build passes.

This is ordering protection for this poller. It does not add server-side event sequencing, certify all adapter transports or constitute a physical-network/device trial. No production prompt or consequential action is needed for verification.

Full `make test` passed server (41.684s) and other packages except the two pre-existing canary hash failures for unchanged Hermes authority/successor files.
