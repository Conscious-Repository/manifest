# Targeted native queue cancellation — September 25

Private native-chat queued instructions now have a targeted cancellation control.
The endpoint consumes the exact accepted request ID and cancels only unstarted
work under the same store lock as Claim. If dispatch won, cancellation refuses;
it cannot interrupt the active turn. Replays return the existing cancelled
receipt without changing later work. Cancelled text remains available in the
transcript, and withdrawing the only pending instruction makes the session idle.
Public portal access remains denied.

Restart recovery now distinguishes a durable owner interruption request from an
unrequested process interruption. Its message preserves that request, explains
that cancelled instructions remain cancelled, and leaves already-started effects
uncertain. Repeated recovery does not append the explanation again.

Race tests exercise cancellation against Claim, active-turn isolation, replay,
started fake-runner queue cancellation through HTTP, and restart explanation
preservation. Chromium verifies exact target/body, failed-request retry and that
queue cancellation leaves the active interruption control available. Existing
interruption/restart/public-boundary checks pass. Full-suite/build/live results
are recorded in the workbench plan checkpoint. No production instruction is
started or cancelled for verification.
