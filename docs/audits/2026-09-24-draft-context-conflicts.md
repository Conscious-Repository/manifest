# Complete draft conflict review — September 24

A private conversation draft includes its exact context and delivery target, not
only its message. The conflict notice now exposes both the saved draft and this
device's draft: message, recipient/model, effective draft task selection, context
IDs/revision hashes and private-selection scope, and attachment names/hashes.
Equal titles or filenames no longer hide different versions. Empty message text
is distinguished from a draft with selected context. Rendering is literal.

Choosing a saved draft also clears an obsolete task selection from the local map
when the saved draft has none. It already replaced artifact/recipient state; now
the task map follows that same replacement contract. Canonical conversation task
links retain their existing fallback behavior; resolving a draft does not mutate
a task link, conversation membership or runtime.

The notice is bounded to half the viewport in chat/task composers and remains
keyboard-scrollable. Question drafts retain their answer preview; file-edit
conflicts show their artifact/base/source revisions without conversation controls.
Existing revision-checked conflict resolution still owns the write; previewing or
choosing a draft cannot send a provider message.

Evidence:

- `server/testdata/chat-draft-conflicts.cjs` runs two isolated Chromium contexts
  against one revision-checked mock state endpoint. Equal text and filenames but
  different recipients, selected versions and task scopes produce a conflict.
  Both exact states are visible; Keep this draft saves the chosen full set;
  Use saved draft applies the other device's exact set/recipient; obsolete task
  selection clears; a third fresh context recovers the chosen state.
- Both themes at 320/390/1440px have no page overflow. At 390×400 the notice stays
  within half the viewport and keyboard focus reaches the conflict choice.
  `/tmp/manifest-draft-conflict-phone.png` inspected after bounding the notice.
- Native question recovery and existing workspace/draft browser fixtures pass.
  Full-suite and deployed-asset results are recorded in the workbench plan.

These are browser isolation/recovery fixtures, not physical-device or complete
cross-adapter acceptance. Broader workbench requirements remain active.

JS syntax, release build and diff checks passed. `make test` passed server
(41.043s) and other packages except the known source-hash re-audit failure in
`cmd/re-intake-canary/TestCanarySourceCallGraphIsolation` for unchanged Hermes files.
