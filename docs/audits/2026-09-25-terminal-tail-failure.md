# Terminal tail failure recovery

Terminal transcript tail reads now have a 15-second abort deadline and require a successful HTTP response before parsing. A response must carry a nonnegative safe-integer offset and an array/null/absent turns field. Invalid or failed reads return before changing transcript, questions, planning metadata or run evidence. The deadline is cleared after every read.

The existing serialized tail/final-read path remains intact. Previously a stalled fetch or body read could hold chatTermTailing indefinitely and keep a stop-triggered final read pending. Timeout now releases that path; the next final read can ingest the result without resending input.

Evidence:
- `chat-terminal-tail-failure.cjs` drives the production tail/final-read functions. HTTP errors, null/invalid payloads and JSON failures preserve the complete retained object. A controlled stalled read queues a final read, aborts, releases the second read and ingests its planning reply without discarding existing native turns. Deadline cleanup and read serialization are checked.
- Existing terminal pending-echo reconciliation, planning/approval final-tail refresh and reading-position tests pass. Their fetch fixtures now include the HTTP-success contract and timer/controller globals.
- Release build, JavaScript syntax and diff checks pass.

The read failure does not claim the provider stopped or completed. This is bounded read recovery, not replay, server event sequencing or a physical network/device acceptance trial. No production input or stop request is needed for verification.

Chromium terminal recovery/docking and full-frontend Chat regressions pass without page errors. Full `make test` passed server (45.054s) and other packages except the two pre-existing canary hash failures in unchanged Hermes authority/successor files.
