# Native receipt metadata refresh

The transcript change detector previously compared delivery identity, state and user/reply turn numbers, plus the conversation timestamp. Stop requests and dispatch metadata can change while the receipt remains running; second-resolution timestamps can remain identical. Polling and cached-stage revalidation could therefore miss those changes.

The signature now includes stopRequested, toolScope, historyOmitted, result and error. Unchanged snapshots retain their existing signature, while metadata changes refresh the existing transcript/Context and composer guidance. No transport, queue, receipt storage or execution semantics change.

Evidence:
- `chat-load-race.cjs` independently changes each metadata field with identical state/timestamp and verifies invalidation; cloned unchanged snapshots remain stable.
- `chat-stage-switch-browser.cjs` uses the full frontend and actual polling against a mutable fixture API. It observes tool scope in Context, then a stop request disabling the active-turn control, without a state/timestamp change. The same textarea, unfinished text, focus and selection survive. Existing thread switching, native queued/running guidance and phone error navigation pass without page errors.
- The fixture roster now explicitly advertises durableSend, which is required for native Context availability. This is simulated remote receipt change, not a physical second-device trial.
- Build, JavaScript syntax and diff checks pass. Full `make test` passed server (41.424s) and other packages except the two pre-existing Hermes source-hash canary failures.

No production run or stop is submitted. Broader cross-device recovery, adapter lifecycle and physical-device acceptance remain open.
