# Syntax in immutable diff hunks

Supported unified hunks now use the existing local syntax bundle. Each side is reconstructed from its own context/addition/removal lines, highlighted separately, and split back into line fragments while preserving nested token classes. Prefixes, before/after coordinates and snapshot-line datasets remain outside token spans. Every generated fragment must match its original source line before it is used.

A workspace-wide syntax toggle restores for the selected snapshot and applies to files expanded lazily. Unsupported grammars and malformed/combined/binary hunks remain recorded plain views. Supported hunks explicitly state that surrounding file context is unavailable: syntax is a fragment aid, not a whole-file parse or validation. Each hunk is bounded to 64 KiB across both sides and 2,000 lines; failures preserve original text. The code-file preview and hunk preview share the same language mapping and strict span-only/text-integrity check.

Evidence: real Chromium verifies multiline comments across context/removal/addition on separate sides, exact row text and prefixes, retained source coordinates, original immutable review range/payload, toggle restoration, lazy collapse state and both-theme 320/390/1440px bounds. Phone screenshot inspected. Existing code syntax, hunk review, saved-receipt and pure diff parser fixtures passed. No real reviews, working-tree edits or provider instructions were submitted.

This completes the supported code/hunk syntax increment. It does not imply semantic full-file validation, specialized binary/CAD review, generic team-file editing, full adapter/context/approval coverage or integrated physical-device acceptance. The workbench plan remains active.

Visual inspection caught a pre-existing descendant-span rule that split syntax tokens onto separate display lines. It now applies only to diff rows; a browser row-height assertion verifies that nested tokens remain on the same line. Build, syntax/diff checks passed. `make test` passed server and other packages except the known Hermes canary source-hash re-audit failure.
