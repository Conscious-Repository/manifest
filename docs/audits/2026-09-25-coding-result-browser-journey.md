# Coding result review in the full frontend

Extended `server/testdata/chat-stage-switch-browser.cjs` through a phone-width
coding-result review journey using the actual index, scripts and styles with
controlled API responses. The preceding backend journey remains separate evidence
for real assignment, durable result ingestion and artifact capture.

The browser starts from a planning conversation containing a result attributed to
Codex, captures the displayed run/hash, opens version 1 despite version 2 being
the artifact head, and selects Discuss. The original composer text and focus are
preserved, the inspector yields to the phone composer, and the context chip names
version 1 with its exact revision. No message, runtime input or artifact save is
submitted. The rendered phone screenshot was inspected for layout and bounds.

The journey exposed an obsolete-error defect: a pending result-capture request
could fail after the user changed conversations and show its error in the new
conversation. The browser regression failed before the fix. The catch handler
now checks the same route visit, source agent and conversation identity used for
successful capture. Current failures still show their error and enable retry.

Validation includes the extended browser journey, existing thread switching and
receipt polling, JavaScript syntax, build and diff checks. Full repository results
and deployed verification are recorded in the plan checkpoint.

Limits: API responses are fixtures; this browser does not run a provider or the
Go backend. Draft persistence across full reload, real mobile keyboard behavior,
and research-deliverable revision remain separate acceptance work. Phone viewport
emulation is not physical-device verification.
