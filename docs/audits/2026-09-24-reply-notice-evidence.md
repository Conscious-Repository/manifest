# Reply notice evidence recovery — September 24

The notice watermark survived an older mailbox response, but its matching reply
preview did not: the receipt replaced its reply list with the later response.
The notice now retains one bounded verified reply preview in the same operation
record. When absent from the latest reply list, the canonical card renders that
saved preview with an explicit explanation. It does not duplicate a reply that
is already present. Older notices without saved bytes show an unavailable state;
a verified read of the same reply can hydrate them without resetting dismissal.

The saved preview keeps the existing 4,000-character clipping flag. This adds no
mailbox request, send path, notice store or scheduler. Feed list projections still
exclude reply bodies. The existing reply watermark and dismissal semantics remain.

Validation: focused Manifest MCP race tests pass for evidence retention after an
older result and restart, legacy hydration without rearming, and unrelated-reply
refusal. Existing watch and notice lifecycle tests pass. Chromium exercises saved
literal preview recovery, duplicate suppression and legacy unavailable state with
the real notice and canonical email renderers; 320/390/1440px bounds pass and the
phone-width screenshot was inspected. JS syntax, diff check and release build pass.
Full `make test` retains only the known unchanged Hermes source-hash canary failure;
the final hydration assertions passed separately under the race detector.

Live verification checks service health, running binary and served assets without
production mail or monitoring changes. This closes one notice-evidence recovery
gap, not the remaining provider reconciliation or full workbench acceptance scope.
