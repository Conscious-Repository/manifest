# Native chat interruption — September 25

Private native-chat running turns now offer Interrupt turn, including an explicit
queued-instruction count when applicable. The private POST targets the exact
running delivery ID, durably marks its stop request and cancels instructions
already queued in the same atomic store update. Their text remains in cancelled
receipts and is shown in expandable transcript entries. Stop retries cannot
cancel later explicit submissions or another delivery. A stop arriving after
completion is refused instead of targeting newer work. Shared/native-portal
access does not acquire this control.

The server owns a per-invocation cancellation context. Registration checks the
durable stop flag, covering requests arriving before the worker registers.
Cancellation reaches the existing Hermes runner context. The receipt remains
running until the runner returns, then becomes interrupted rather than completed.
An invocation already finishing can retain its returned text with an interrupted
receipt. Process restart retains uncertainty and does not restart cancelled
instructions. New explicit submissions remain independently eligible for dispatch.
This is interruption of the runner, not rollback or proof that detached tools or
already-started external effects stopped; the UI says those effects may continue.

Race tests cover a started fake CLI process interrupted through the HTTP handler,
queue cancellation/text retention, pending versus final receipt state, repeated
stop and acceptance identities, late stop after completion, later instructions,
and restart between durable request and runner return. Public portal denial is
also tested. Chromium exercises exact target/body, failed-request retry, disabled
pending state, retained cancelled text and 320/390/1440px bounds. Existing chat
stage/load-race tests pass. Full-suite/build/live evidence is in the plan checkpoint.
No production run is interrupted or started for verification.
