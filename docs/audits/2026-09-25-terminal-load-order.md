# Terminal conversation load ordering

The Codex/Claude terminal-backed loader now checks request order, route visit, selected agent and visible thread identity before applying its full transcript response. A newer tail or run-evidence update also invalidates a pending full read. Late errors cannot replace the current stage with the landing page. After asynchronous draft preparation, composer/reading restoration, title updates and runtime follow-up require the same request/visit and painted object.

Evidence:
- `chat-terminal-load-order.cjs` drives the production loader with controlled asynchronous responses. It verifies newer load versus older success/404, navigation away and back to the same ID, newer tail and run evidence, and late draft preparation. The latter preserves the new visit's composer and submits no automatic title update.
- Existing terminal recovery tests pass: retry/backoff, daemon return, hidden/navigation state, identity checks, detach/handshake and no input replay.
- Chromium terminal recovery passes controlled inventory/socket recovery, bottom-docked exact-session behavior, stopped/resumed inventory, phone bounds, keyboard resizing and stage restoration without page errors.
- Full-frontend Chat browser regressions pass. Exact-session reveal and task-route preservation pass; the older context fixture now loads the existing todoPanelPlace helper and supplies its DOM lookup, fixing a stale fixture dependency.
- Build, JavaScript syntax and diff checks pass.

The ordering fixture does not invoke a real Codex/Claude provider. Browser recovery uses controlled transport, not a physical phone/network trial. No production runtime is launched, renamed, stopped or sent input for verification.

Full `make test` passed server (44.679s) and other packages except the two pre-existing source-hash canary failures in unchanged Hermes authority/successor files.
