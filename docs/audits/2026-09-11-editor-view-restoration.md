# Artifact editing view continuity

The inspector now restores an existing draft directly into its prior editing or unsaved-change review view. It restores textarea selection and scroll without taking keyboard focus. Restoration uses the existing revision-bound draft store and does not save an artifact or send a message. Missing drafts fall back to the ordinary preview.

Validation: actual component browser fixture exercises disposal/recreation, draft content, selection, scroll and unsaved-change comparison; the integrated workspace browser fixture passes. These are fixture checks, not real provider or physical-phone certification.

The implementation branch was rebased onto origin/main at 486c84c, preserving the concurrent recruiting changes. Full original workbench scope remains open, including live journeys and release verification.

Integrated validation passed: `make test`, `go test ./...`, `go build ./...`, JavaScript syntax and whitespace checks. No deployment performed.
