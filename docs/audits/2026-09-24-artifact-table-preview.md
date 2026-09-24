# Delimited artifact preview and review

Registered CSV and TSV revisions now render as tables, with all records preserved as data rather than assuming the first record is a header. Quoted delimiters, escaped quotes, multiline fields, BOM, empty fields, ragged records, and LF/CRLF endings are supported. Values remain literal text; formulas and markup are never executed. Original source remains available unchanged.

Selecting a record prepares the existing review form with its exact physical source-line range and record number. Recording binds to the selected immutable artifact revision; discussion still creates a draft for explicit Send. The renderer never reserializes or saves the table. Source disclosure and horizontal table position restore through existing revision-specific workspace state.

Preview limits are 1 MiB UTF-8, 200 records, 50 columns, 10,000 cells and 16,000 UTF-16 code units per field. Malformed quotes, NUL, unsupported lone-CR record endings or exceeded limits produce a visible explanation and original source, without silently displaying a partial table.

## Evidence

- Node parser fixtures cover quoting, BOM, CRLF multiline physical ranges, TSV, empty/trailing fields, malformed input and size/record/column limits.
- Real Chromium fixture covers version-bound multiline record review payload and discussion, no write on selection, literal markup/formulas, original-source disclosure restoration, horizontal scroll restoration, malformed fallback and both themes at 320/390/1440px. Phone screenshot inspected.
- Existing hunk-review browser fixture passed; `go test ./server`, JS syntax, diff checks and release build passed.
- `make test` retained only the known canary source-hash re-audit failure in unchanged Hermes authority/successor files; other packages passed.

Tests use mock browser APIs; no real owner reviews or provider messages were submitted. This is a bounded table preview increment. Broader previews, generic authorized file editing, integrated provider journeys and physical-phone acceptance remain open in the workbench plan.
