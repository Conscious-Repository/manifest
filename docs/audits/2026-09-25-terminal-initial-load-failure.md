# Terminal initial-load failure recovery

Terminal full transcript reads now have a 15-second abort deadline, including response-body parsing, and validate offset/turns before applying data. HTTP/network/JSON/malformed/timeout failures use the existing thread retry UI when no transcript has loaded. When the target's cached work is visible, failure preserves that object and marks read health instead. The selected thread remains selected; a read failure no longer sends it to the landing page. Existing request/route/snapshot guards also fence these failure paths.

Evidence:
- `chat-terminal-load-failure.cjs` runs the production loader with controlled failures, both with and without cached work. It verifies retry-error versus health rendering, retained object/run state, unchanged target identity, timeout cleanup and late-failure isolation after a round trip.
- Existing terminal load-order tests pass, including preparation/auto-name isolation. Their successful payloads now include the endpoint's offset/turns contract and allow the read-only abort signal.
- Full-frontend Chat browser regressions pass, including existing retry/error navigation behavior.
- Build, JavaScript syntax and diff checks pass.

This bounds the transcript fetch/body read; subsequent context/draft preparation uses its existing recovery contracts. Terminal inventory lookup and live-provider/device acceptance remain separate. No production runtime input, launch, rename or stop is submitted for verification.

Full `make test` passed server (40.743s) and other packages except the two pre-existing source-hash canary failures in unchanged Hermes authority/successor files.
