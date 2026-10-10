# Pending terminal message state

The screenshot showed a pending terminal echo labeled “delivered · waiting for the agent.” The request was already accepted: the matching terminal receipt was `sent`, and the session's live projection reported `working`. The label was hard-coded for every pending echo while the transcript waited to record the user turn, so it described the echo's reconciliation state rather than the agent's actual runtime state.

Pending terminal echoes now say “delivered · agent working” while the runtime is working, “delivered · agent needs input” when blocked, and retain “delivered · waiting for the agent” only when no active state is known. The echo reconciliation behavior and 15-minute stale-echo expiry are unchanged.

Validation:

- Live receipt and transcript inspection confirmed the exact request was sent and the run was working.
- Attachment-turn and terminal-echo fixtures pass, including working and blocked labels.
- JavaScript syntax, build, diff checks and server tests pass.
- Deployed executable matches the release; service is active, root returns HTTP 200, and read-only production Chromium verifies the served label code without page errors.

No terminal input or external action was submitted during production verification.
