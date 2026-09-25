# Recruiting candidate record context — September 25

Records now offers private recruiting candidates alongside existing context
kinds. Search returns canonical ID, name, role and stage; it does not expose
record bodies. Preview contains the exact selected candidate Markdown, preserving
notes and evidence references. Linked evidence files, outreach logs and role
records are not expanded. Bounded reads reject oversized records, identity
mismatches and paths escaping the recruiting root.

Explicit selection retains the reviewed hash through the existing artifact
registry and private draft machinery. Changed-source selection is refused;
already retained versions remain exact after later candidate edits. Context has
candidate provenance and no task ownership. Artifact source links open the exact
candidate on the recruiting board, resetting board filters so it is visible.
Snapshots remain preview-only; selection cannot approve mail or change records.

Validation:

- Race-tested duplicate-name identity, body-free search, exact source bytes,
  stable/idempotent retention, stale review refusal, immutable delivery context,
  canonical source links, implicit/public access denial, oversized input,
  identity mismatch and symlink escape refusal.
- Native delivery fixture sends the frozen candidate version once despite a
  newer source and repeated request, and denies private context on shared input.
- Chromium uses the real Records inspector for keyboard selection, exact-version
  selection, kind/view restoration, source link and canonical candidate route.
  Existing stale-search, stale-preview, sharing and responsive checks pass;
  candidate phone-width screenshot inspected.
- Release build, JS syntax and diff checks pass. Final `make test` passed
  server (39.931s) and other packages except the known unchanged Hermes
  source-hash canary re-audit failure.

This provides candidate record context, not full organization/schedule context,
automatic recruiting-to-chat creation, live provider delivery or physical-phone
acceptance. Other unchecked workbench requirements remain open.
