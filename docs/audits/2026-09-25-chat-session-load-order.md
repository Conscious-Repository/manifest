# Explicit conversation load ordering

Native/planning session loads previously checked only the current agent/thread identity after asynchronous work. Leaving and returning to that identity could let a request from the earlier visit render, evict the current cache on a late 404, show an obsolete error or follow a stale sharing redirect. Two explicit refreshes could also finish out of order.

Each load now captures a monotonically increasing request ticket, route version and displayed signature. The response must remain current after fetch, JSON parsing, origin-context reads and draft/reading-state preparation before it can update the stage. Stale failures are ignored as well. A newer poll result fences an older explicit load, complementing the poller's existing newer-refresh guard.

Evidence:
- `chat-session-load-order.cjs` drives the production loader with controlled asynchronous responses. Newer success wins over older success, 404, network error and sharing redirect; stale calls neither prepare drafts nor update/evict the cache. A round trip to the same route invalidates the original request. A newer polled snapshot and navigation during JSON parsing also fence the response.
- Existing artifact-load navigation race and cached-stage loader tests pass; current 404 still evicts its cache, and unchanged revalidation preserves DOM.
- Full-frontend Chromium passes eager/cached thread switching, actual receipt polling, composer preservation and phone error navigation without page errors.
- Build, JavaScript syntax and diff checks pass.

The terminal-specific loader keeps its own adapter path. This change does not add server revision sequencing or certify every provider's recovery path. No production instruction or consequential action is needed for verification.

Full `make test` passed server (42.615s) and other packages except the two pre-existing source-hash canary failures in unchanged Hermes authority/successor files.
