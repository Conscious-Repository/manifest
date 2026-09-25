# Research revision with full-page recovery

`server/testdata/chat-research-revision-browser.cjs` serves the real frontend and
uses controlled artifact/draft APIs in Chromium with iPhone 13 viewport settings.
It opens a research Markdown artifact through the production workspace action,
edits the brief, reviews the diff and saves a new version. The fixture commits
that save but drops its acknowledgment.

A full page reload reconstructs the workspace and restores its comparison view
automatically. The user returns through Edit text to the retained draft, retries
Save new version, compares the recovered version against the original and selects
Discuss. The composer receives the exact version 2 hash. The fixture requires the
save request identity to be persisted before the artifact write, compares the
entire retry payload against the original and checks that only two artifact
versions exist after two save attempts. Unexpected mutation endpoints fail the
fixture; no message, provider input, task change or approval is submitted.

The browser journey passes without page errors or horizontal page overflow. The
phone discussion screenshot was inspected. The separate real-backend
`TestArtifactTextSaveReceipt` passes under the race detector. Syntax/build/diff and
full repository results are recorded in the plan checkpoint. No production code
change was required by this journey.

Limits: this is viewport emulation, not a physical phone or mobile keyboard test.
Research content and API receipts are fixtures, not evidence of provider research
quality or integrated Go/browser execution. Entry invokes the same workspace
action as Files but does not certify artifact discovery. Full-page restoration is
on the same browser with retained storage; another device remains separate work.
