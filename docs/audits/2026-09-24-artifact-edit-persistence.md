# Persist artifact edits before saving versions

Artifact workspace saves now await confirmation from the existing revision-checked draft store before changing a file version. Failed storage, a draft conflict, remaining unsynced changes or a changed submission snapshot leaves the editor intact and explains that draft recovery must be resolved. The saved draft contains the exact text, artifact ID and starting revision.

While waiting, duplicate saves and Back to preview are disabled. Closing the pane before draft persistence finishes cancels the subsequent file mutation. This preserves the existing ability to leave a pending request without submitting into a disposed workspace. File-save failures retain the draft for reopening; the server's existing starting-revision check remains authoritative.

Evidence: real Chromium fixture with the actual ChatDraftState controller and mocked APIs verifies offline persistence, lost file-save acknowledgment followed by controller reconstruction, exact text/base in server draft storage before save, duplicate invocation, closed-pane cancellation and stale-draft conflicts. The existing workspace tabs/recovery browser fixture passed. Build, JS syntax and diff checks passed.

This does not claim an idempotent file-save receipt: a lost acknowledgment can still require inspecting current history before another save. It preserves the recovery material before that uncertainty begins. Integrated provider, physical-phone and other unchecked workbench requirements remain active.

`make test` passed the server and other packages except the known canary source-hash re-audit failure in unchanged Hermes authority/successor files. No production file edits or provider messages were submitted by the fixtures.
