# Visible terminal transcript read health

A failed terminal tail read now shows a quiet status beside the conversation header: Transcript updates unavailable. Showing the last received work. It is local read health, separate from authoritative run/connection state. Repeated failures retain one notice; a successful tail or full transcript load clears it. Cached rendering/header redraw preserves the notice, but does not repeatedly announce the same failure. A newly observed failure uses polite announcement.

Evidence:
- `chat-terminal-read-health.cjs` uses the production health renderer and tail reader in Chromium with fixture HTTP failure/success. It verifies visibility, no duplicate notice, retained transcript/run/draft/focus, recovery, stale-thread isolation and 320/390/1440px bounds. Header redraw suppresses repeat announcements. The component screenshot was inspected; it is not a full production layout or physical-device trial.
- Existing tail timeout/final-read, pending echo, reading-position and terminal-load-order tests pass. Their focused contexts stub the separate health renderer; the browser fixture exercises it directly.
- Full-frontend Chat browser regressions pass without page errors. Build, syntax and diff checks pass.
- Full `make test` passed server (40.983s) and other packages except the two pre-existing Hermes source-hash canary failures. The final announcement adjustment passed its browser fixture and a fresh build.

A read outage neither declares the agent stopped nor changes its result. No production failure is induced and no runtime input or stop is submitted for verification.
