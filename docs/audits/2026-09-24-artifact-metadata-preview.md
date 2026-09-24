# Exact-version generic artifact previews

The workspace requests an opt-in preview descriptor for the selected immutable revision. Classification uses its bytes, not the filename or latest revision. Supported raster image signatures and PDF remain inline media; UTF-8 without binary control bytes previews as text up to 1 MiB. Other files show their filename, detected media type, byte count, revision hash and an explicit limitation. Legacy exact-content API callers retain their contract.

Unsupported or oversized files retain whole-version review and an exact-revision Open file link. They offer no line selection, text editing, text comparison or agent-context handoff. A stale range draft cannot silently attach line coordinates to the metadata-only review. Unsupported original bytes download as an attachment with a safe filename; HTML/XML/SVG text remains inert plain text with sandbox/nosniff headers. Image decode failures show an explanatory metadata fallback. This does not claim a native CAD, archive, office-document or other specialized preview.

Comparing a text revision against a prior nontext/oversized revision reports the limitation rather than comparing against an invented empty file. Draft comparison does the same while retaining the draft. No file is reserialized or modified by previewing.

## Evidence

- Server tests: binary-to-text revisions under a misleading .txt filename, historical descriptor identity/size, omitted binary JSON text, exact downloaded bytes, attachment disposition, unknown revision rejection, UTF-8 byte bounds, control bytes, media signatures and portal denial. Existing inert HTML endpoint test passed.
- Real Chromium fixture: historical generic metadata, exact download URL and whole-version review payload, absent edit/range/discussion controls, unavailable comparison, switching back to editable text, both themes at 320/390/1440px. Phone screenshot inspected.
- Existing table, hunk review and workspace recovery browser fixtures passed. `go test ./server`, build, JS syntax and diff checks passed.
- `make test` retains only the known canary source-hash re-audit failure in unchanged Hermes authority/successor files; other packages passed.

Browser review actions used mock APIs. Physical-device and integrated provider acceptance remain open, as do the other unchecked requirements in the active workbench plan.
