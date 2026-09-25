# Conversation prefetch ordering

Background pointer-intent prefetch previously wrote its response into the stage cache unconditionally. A late response could replace a newer foreground load. The terminal branch also reused the registry row on paths where chatTermApplyState returns its input, replacing its run evidence and offset with the prefetch response.

Prefetch now checks the captured route visit, absence of a newer cache entry and that the target is not already open before applying a response. It skips prefetch for the open target, whose foreground loader/tail owns freshness. Terminal prefetch applies state to a copied registry row. A current background response can still warm the cache before navigation; otherwise eager foreground loading proceeds as before.

Evidence:
- `chat-prefetch-order.cjs` runs the production function for native and terminal entries. Controlled late responses cannot overwrite newer cache, survive an intervening route visit or populate the open target. Current responses still populate the cache. Both accepted and discarded terminal responses leave original registry run evidence and offset unchanged.
- Existing stage-cache loader tests pass, including synchronous cached paint, eager loading, unchanged revalidation and current 404 eviction.
- Full-frontend Chromium passes thread switching, receipt polling, composer preservation and phone error navigation without page errors.
- Build, JavaScript syntax and diff checks pass.

This protects speculative background reads; it does not change runtime execution, native transport or server permissions. No production prompt, runtime input or sharing action is needed for verification.

Full `make test` passed server (41.293s) and other packages except the two pre-existing source-hash canary failures in unchanged Hermes authority/successor files.
