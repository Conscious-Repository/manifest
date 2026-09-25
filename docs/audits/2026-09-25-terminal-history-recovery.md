# Full snapshot after unavailable terminal history

After native history becomes unavailable, tail requests now start at byte zero until a readable snapshot returns. The retained offset is not applied to a possibly replaced file. The first successful snapshot replaces old file-derived turns regardless of whether its size is smaller, equal or larger. Title/cost also reset from that snapshot, including empty/zero values. Subsequent reads resume incrementally from its offset.

Recent pending send echoes remain separate. Their comparison boundary moves after the recovered history, so older identical text cannot serve as acknowledgment of that pending instruction. Subsequent incoming turns can reconcile them through the existing echo path; no input is resent. This remains the existing UI echo mechanism, not a new provider receipt contract.

Evidence:
- `chat-terminal-history-recovery.cjs` drives the production tail, merge and echo functions for smaller/equal/larger replacements. It verifies byte-zero retries, retained content while still unavailable, exact snapshot replacement, title/cost clearing, pending echo retention and subsequent incremental reconciliation.
- Tail timeout/final-read, pending-echo and reading-position tests pass. The final-read fixture supplies the full recovered snapshot under the new contract.
- Chromium read-health and full-frontend Chat regressions pass; no production transcript file is modified or runtime input submitted.
- Build, JavaScript syntax and diff checks pass.

Replacement without an observed unavailable-history response still follows the existing offset/truncation behavior; arbitrary file generation detection and all-provider/device acceptance are not claimed.

Full `make test` passed server (42.334s) and other packages except the two pre-existing source-hash canary failures in unchanged Hermes authority/successor files.
