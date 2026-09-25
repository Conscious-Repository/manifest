# Terminal history availability

The transcript endpoint now exposes the existing projection's historyAvailable flag. Readable empty history differs from unavailable history; draft sessions retain their existing available projection. The field describes native transcript availability, independently of runtime connectivity and planning/task overlays.

The terminal workspace carries the flag through cache/paint identity. An initial unavailable history shows an explicit message directing the owner to Terminal, with the existing read-health status. Draft sessions keep their start guidance. Full refresh and tail reads retain previously received native turns/offset, questions and fallback run/title/cost when native history is unavailable. Incoming independent planning/task data can still refresh. Pending echoes do not expire solely during this unavailable-history path. Readable empty/truncated history retains the existing reconciliation behavior; older servers without the flag retain compatibility behavior.

Evidence:
- Focused server race test verifies the real transcript endpoint for a populated file, readable empty file, missing formerly readable file and absent initial file.
- Loader and tail fixtures preserve retained native content/offset/run while a new planning result arrives without the native file.
- Chromium component test verifies explicit unavailable-history text and distinct draft guidance. Existing reading, pending-echo, tail-failure and full-frontend Chat browser regressions pass.
- Build, syntax and diff checks pass.

This does not prove that every native file format or replacement scenario is recoverable, or claim complete live-provider/device acceptance. No production file is removed and no runtime input or stop is submitted for verification.

Full `make test` passed server (41.474s) and other packages except the two pre-existing source-hash canary failures in unchanged Hermes authority/successor files.
