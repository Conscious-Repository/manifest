# Research drafts across isolated browser sessions

Extended `chat-research-revision-browser.cjs` with two independent Chromium
contexts: an emulated phone and a desktop viewport. They share the fixture's
versioned server records but have independent local storage and controllers.

Both open the same research artifact. The desktop persists a new draft, then the
phone edits its older snapshot and tries to save the artifact. The draft revision
conflict prevents that file request. The phone's expanded conflict controls show
both complete texts while preserving its editor content. Choosing Keep this draft
updates draft storage only. The desktop then makes another edit against its older
snapshot, encounters a conflict and chooses Use saved draft; its editor adopts
the phone text. Neither resolution changes the saved artifact or sends a message.

The fixture asserts that the artifact-save request count stays unchanged through
both conflicts and resolutions, that the artifact content remains at its previous
saved version, and that expanded phone conflict content has no horizontal page
overflow. Existing edit/diff/lost-ack/reload/exact-retry/discussion checks remain.
The conflict screenshot and full validation results are noted in the checkpoint.

No production change was necessary. API conflict responses are fixtures with
revision checks, not evidence of the Go server's concurrency implementation.
These are separate browser contexts on one machine, not physical devices or
real-network interruption trials. Timer flushes are synchronized explicitly in
the fixture to make the competing revision order deterministic.
